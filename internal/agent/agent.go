package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"golang.org/x/sync/errgroup"

	"aiko/internal/agent/middleware"
	"aiko/internal/config"
	"aiko/internal/memory"
	internaltools "aiko/internal/tools"
)

// emotionPromptSuffix is appended to the system prompt to instruct the LLM
// to prefix every reply with an emotion tag for VRM blendshape driving.
const emotionPromptSuffix = "\n\n在每条回复的第一行必须输出情绪标签，格式严格为 `[情绪:emotion/intensity]`，" +
	"其中 emotion ∈ {joy, sad, surprised, angry, neutral}，intensity ∈ [0.0, 1.0]，然后换行写正文。" +
	"示例：[情绪:joy/0.7]\n你好！"

// StreamResult is a single streamed token or a terminal signal.
type StreamResult struct {
	Token         string
	ThinkingToken string   // reasoning/thinking chunk from ReasoningContent
	Images        []string // base64 data URLs or http(s) URLs of images output by the LLM
	Err           error
	Done          bool
}

// ToolConfirmRequest is emitted via Wails event when a tool requests user confirmation.
type ToolConfirmRequest struct {
	ID         string `json:"id"`
	ToolType       string `json:"tool_type"` // "shell", "code", or "update"
	Command        string `json:"command,omitempty"`
	Code           string `json:"code,omitempty"`
	Language       string `json:"language,omitempty"`
	WorkingDir     string `json:"working_dir"`
	CurrentVersion string `json:"current_version,omitempty"`
	LatestVersion  string `json:"latest_version,omitempty"`
}

// ToolConfirmResponse is the user's response to a tool confirmation request.
type ToolConfirmResponse struct {
	Approved      bool
	EditedContent string
}

// memCheckPointStore is a simple in-memory CheckPointStore used to persist interrupt
// checkpoints within a single application session.
type memCheckPointStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

// Get retrieves a checkpoint by ID.
func (s *memCheckPointStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	return v, ok, nil
}

// Set stores a checkpoint under the given ID.
func (s *memCheckPointStore) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

// locationCache caches the IP-based location string to avoid a network call every turn.
var locationCache struct {
	sync.Mutex
	value     string
	fetchedAt time.Time
}

const locationCacheTTL = 30 * time.Minute

// cachedLocation returns the cached location string, refreshing via IP geolocation when stale.
func cachedLocation() string {
	locationCache.Lock()
	defer locationCache.Unlock()
	if locationCache.value != "" && time.Since(locationCache.fetchedAt) < locationCacheTTL {
		return locationCache.value
	}
	loc := internaltools.FetchLocation()
	if loc != "" {
		locationCache.value = loc
		locationCache.fetchedAt = time.Now()
	}
	return loc
}

// Agent wraps an eino ReAct agent with short/long-term memory integration.
type Agent struct {
	runner          *adk.Runner
	shortMem        *memory.ShortStore
	longMem         *memory.LongStore
	cfg             *config.Config
	dataDir         string                          // ~/.aiko data directory, used to read USER.md
	turnCount       atomic.Int64                    // completed conversation turns (resets on restart)
	nudgeInterval   int                             // how often to trigger self-growth nudge
	pendingConfirms *sync.Map                       // map[string]chan ToolConfirmResponse; bridged from App
	emitEvent       func(event string, data ...any) // Wails EventsEmit

	// self-growth
	hasSkills     bool        // true if auto-skills/ directory has any *.md files at startup
	lastSkillHint string      // set by SetSkillHint; cleared by shouldReflect
	skillHintMu   sync.Mutex  // guards lastSkillHint
}

// New constructs an Agent with a ReAct runner backed by the given chat model,
// memory stores, and optional tools. longMem may be nil when vector memory is
// not configured. skillMW may be nil when no skills are configured.
func New(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	shortMem *memory.ShortStore,
	longMem *memory.LongStore,
	tools []tool.BaseTool,
	cfg *config.Config,
	mw middleware.Middleware,
	skillMW adk.ChatModelAgentMiddleware,
	dataDir string,
	pendingConfirms *sync.Map,
	emitEvent func(event string, data ...any),
) (*Agent, error) {
	// Apply middleware chain to all tools if provided.
	if mw != nil && len(tools) > 0 {
		tools = middleware.WrapAll(tools, mw)
	}

	var handlers []adk.ChatModelAgentMiddleware
	if skillMW != nil {
		handlers = append(handlers, skillMW)
	}

	systemPrompt := cfg.SystemPrompt + emotionPromptSuffix

	deepCfg := &deep.Config{
		Name:                   "aiko",
		Description:            "A desktop pet AI assistant",
		Instruction:            systemPrompt,
		ChatModel:              chatModel,
		MaxIteration:           100,
		Handlers:               handlers,
		WithoutGeneralSubAgent: true, // Aiko is a single-agent; disable the Task tool and its misleading subagent prompt.
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries: 5,
			IsRetryAble: func(_ context.Context, err error) bool {
				msg := err.Error()
				return strings.Contains(msg, "429") ||
					strings.Contains(msg, "Too Many Requests") ||
					strings.Contains(msg, "rate limit")
			},
		},
	}

	if len(tools) > 0 {
		deepCfg.ToolsConfig = adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		}
	}

	agent, err := deep.New(ctx, deepCfg)
	if err != nil {
		return nil, err
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: &memCheckPointStore{m: map[string][]byte{}},
	})

	ni := cfg.NudgeInterval
	if ni <= 0 {
		ni = 5
	}

	skillsDir := filepath.Join(dataDir, "auto-skills")
	entries, _ := os.ReadDir(skillsDir)
	hasSkills := false
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			hasSkills = true
			break
		}
	}

	return &Agent{
		runner:          runner,
		shortMem:        shortMem,
		longMem:         longMem,
		cfg:             cfg,
		dataDir:         dataDir,
		nudgeInterval:   ni,
		pendingConfirms: pendingConfirms,
		emitEvent:       emitEvent,
		hasSkills:       hasSkills,
	}, nil
}

// SetSkillHint records that a skill was activated during this turn.
// Called by the skill middleware after injecting a skill into the prompt.
// The hint is consumed and cleared by shouldReflect after the reply.
func (a *Agent) SetSkillHint(skillName string) {
	a.skillHintMu.Lock()
	defer a.skillHintMu.Unlock()
	a.lastSkillHint = fmt.Sprintf("本次激活了 skill %q，回复结束后思考该 skill 是否需要更新。", skillName)
}

// shouldReflect decides whether to trigger a self-growth reflection after this turn.
// toolCallCount is the number of distinct tool names invoked during the turn, as
// counted by drainIter. Returns (trigger, hints). trigger is true under any of:
//   - periodic: turnCount % nudgeInterval == 0
//   - user correction keywords detected in userInput
//   - multi-tool chain (toolCallCount ≥ 3 distinct tools)
//   - explicit preference keywords detected in userInput
//   - hints is non-empty (skill activation signal from middleware)
func (a *Agent) shouldReflect(userInput string, toolCallCount int) (bool, []string) {
	var hints []string

	// Collect skill-activation hint (cleared here).
	a.skillHintMu.Lock()
	if a.lastSkillHint != "" {
		hints = append(hints, a.lastSkillHint)
		a.lastSkillHint = ""
	}
	a.skillHintMu.Unlock()

	// Periodic trigger.
	tc := a.turnCount.Load()
	if a.nudgeInterval > 0 && tc > 0 && tc%int64(a.nudgeInterval) == 0 {
		return true, hints
	}

	// User correction signal.
	correctionKW := []string{"不对", "你刚才说错了", "其实是", "纠正"}
	for _, kw := range correctionKW {
		if strings.Contains(userInput, kw) {
			hints = append(hints, "检测到用户纠错，检查相关 skill 是否需要更新。")
			break
		}
	}

	// Multi-tool chain signal (distinct tool names, not raw invocation count).
	if toolCallCount >= 3 {
		hints = append(hints, fmt.Sprintf("本次调用了 %d 种不同工具，考虑提炼为可复用技能。", toolCallCount))
	}

	// Explicit preference signal.
	prefKW := []string{"以后都", "我不喜欢", "记住", "每次都"}
	for _, kw := range prefKW {
		if strings.Contains(userInput, kw) {
			hints = append(hints, "检测到显式偏好表达，优先更新用户画像。")
			break
		}
	}

	return len(hints) > 0, hints
}

// buildReflectionPrompt constructs a structured fill-in-the-blank reflection prompt.
// hints is an optional list of targeted guidance lines appended after the checklist.
func buildReflectionPrompt(userInput, assistantReply string, hints []string) string {
	var sb strings.Builder
	sb.WriteString("[SELF-GROWTH REFLECTION]\n")
	sb.WriteString("决定是否 save_skill 前，请先调用 list_skills 查看已有技能，避免创建重复或名称冲突的技能。\n\n")
	sb.WriteString("本轮对话摘要（请填写）：\n")
	sb.WriteString("- 用户意图：<一句话>\n")
	sb.WriteString("- 解决方案：<一句话>\n")
	sb.WriteString("- 结果：成功 / 失败 / 部分完成\n\n")
	sb.WriteString("请选择（可不选）：\n")
	sb.WriteString("□ save_memory — 有新的具体事实或偏好值得记录\n")
	sb.WriteString("□ update_user_profile — 发现了用户新的习惯/背景\n")
	sb.WriteString("□ save_skill — 本次解决模式可复用\n")
	if len(hints) > 0 {
		sb.WriteString("\n重点提示：\n")
		for _, h := range hints {
			sb.WriteString("⚠️ ")
			sb.WriteString(h)
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("\n以下是本轮对话内容供参考：\n")
	sb.WriteString("用户：")
	sb.WriteString(userInput)
	sb.WriteString("\n助手：")
	sb.WriteString(assistantReply)
	return sb.String()
}

// reflect runs a background self-growth reflection turn after the main reply.
// It builds a structured prompt and calls ChatDirectCollect; errors are logged
// and discarded — reflection failures must never surface to the user.
func (a *Agent) reflect(ctx context.Context, userInput, assistantReply string, hints []string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("reflect panic recovered", "err", r)
		}
	}()
	prompt := buildReflectionPrompt(userInput, assistantReply, hints)
	if _, err := a.ChatDirectCollect(ctx, prompt); err != nil {
		slog.Warn("self-growth reflection failed", "err", err)
	}
}

// Chat sends a user message to the agent and returns a channel that streams
// tokens. After the final Done=true result, user and assistant messages are
// persisted to short-term memory and excess messages are migrated to
// long-term memory asynchronously.
func (a *Agent) Chat(ctx context.Context, userInput string) <-chan StreamResult {
	ch := make(chan StreamResult, 64)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				ch <- StreamResult{Err: fmt.Errorf("agent panic: %v", r)}
			}
		}()

		ctxMsgs, err := a.buildContext(ctx, userInput)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		// Inject flush callback so check_and_update can persist before restart.
		flushFn := func(assistantSummary string) {
			if a.shortMem == nil {
				return
			}
			if _, _, stripped, ok := parseEmotionTag(assistantSummary); ok {
				assistantSummary = stripped
			}
			_, _ = a.shortMem.AddWithImagesAndFiles("user", userInput, nil, nil)
			_, _ = a.shortMem.AddFull("assistant", assistantSummary, "", nil, nil)
		}
		ctx = context.WithValue(ctx, internaltools.PersistBeforeRestartKey{}, flushFn)

		content := userInput

		msgs := append(ctxMsgs, &schema.Message{Role: schema.User, Content: content})
		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}

		ch <- StreamResult{Done: true}
		go a.persistAndMigrate(context.Background(), userInput, nil, nil, fullResponse, thinkingContent, toolImgs, toolCallCount)
	}()

	return ch
}

// ChatDirect sends a prompt to the agent and streams tokens without persisting
// the exchange to memory. Used by the scheduler to avoid polluting chat history.
func (a *Agent) ChatDirect(ctx context.Context, prompt string) <-chan StreamResult {
	ch := make(chan StreamResult, 64)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				ch <- StreamResult{Err: fmt.Errorf("agent panic: %v", r)}
			}
		}()
		_, _, _, _, ok := drainRunner(ctx, a.runner, prompt, ch, nil, nil, fmt.Sprintf("direct-%d", time.Now().UnixNano()))
		if !ok {
			return
		}
		ch <- StreamResult{Done: true}
		// NOTE: No persistAndMigrate call here — intentional.
	}()

	return ch
}

// ChatDirectCollect sends a prompt to the agent, collects the full response
// as a string, and returns it. Unlike ChatDirect, no Wails events are emitted.
// Used by the ProactiveEngine when the chat panel is closed.
func (a *Agent) ChatDirectCollect(ctx context.Context, prompt string) (string, error) {
	ch := a.ChatDirect(ctx, prompt)
	var sb strings.Builder
	for r := range ch {
		if r.Err != nil {
			return "", r.Err
		}
		if r.Done {
			break
		}
		sb.WriteString(r.Token)
	}
	return sb.String(), nil
}

// drainRunner consumes all events from runner.Query, forwards tokens to ch,
// and returns the accumulated response string, thinking content, tool images, and tool-call count.
// Returns (response, thinking, images, toolCalls, true) on success or ("", "", nil, 0, false) after sending an error to ch.
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	iter := runner.Query(ctx, query, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}

// drainRunnerMsg consumes all events from runner.Run with a pre-built message list,
// forwards tokens to ch, and returns the accumulated response string, thinking content,
// tool images, and tool-call count.
// Returns (response, thinking, images, toolCalls, true) on success or ("", "", nil, 0, false) after sending an error to ch.
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	iter := runner.Run(ctx, msgs, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}

// drainIter consumes all events from an AsyncIterator, forwards tokens to ch,
// handles interrupt events, and returns the accumulated response string, thinking content,
// collected tool images, and the number of distinct tool names invoked this turn.
func drainIter(ctx context.Context, runner *adk.Runner, iter *adk.AsyncIterator[*adk.AgentEvent],
	ch chan<- StreamResult, pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	uniqueTools := make(map[string]struct{})
	var imgs []string
	resp, thinking, ok := drainIterInner(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID, uniqueTools, &imgs)
	return resp, thinking, imgs, len(uniqueTools), ok
}

// nextEvent wraps iter.Next() in a goroutine so callers can race it against
// ctx.Done(). iter.Next() blocks on sync.Cond.Wait() and has no context
// awareness; without this wrapper, StopGeneration cannot unblock a thinking LLM.
type iterResult struct {
	event *adk.AgentEvent
	ok    bool
}

func nextEvent(iter *adk.AsyncIterator[*adk.AgentEvent]) <-chan iterResult {
	c := make(chan iterResult, 1)
	go func() {
		event, ok := iter.Next()
		c <- iterResult{event, ok}
	}()
	return c
}

// drainIterInner is the recursive core of drainIter; it accepts a shared uniqueTools
// map so that distinct tool names are accumulated correctly across interrupt/resume cycles.
// imgsBuf accumulates images produced by tool calls (e.g. read_image) across all
// recursive invocations so they can be persisted by the caller.
func drainIterInner(ctx context.Context, runner *adk.Runner, iter *adk.AsyncIterator[*adk.AgentEvent],
	ch chan<- StreamResult, pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string,
	uniqueTools map[string]struct{}, imgsBuf *[]string) (string, string, bool) {
	var sb strings.Builder
	var thinkingSb strings.Builder

	for {
		var event *adk.AgentEvent
		var ok bool
		select {
		case <-ctx.Done():
			return "", "", false
		case r := <-nextEvent(iter):
			event, ok = r.event, r.ok
		}
		if !ok {
			break
		}
		if event.Err != nil {
			// Context cancellation means the user hit Stop — propagate silently.
			if errors.Is(event.Err, context.Canceled) {
				return "", "", false
			}
			// Interrupt signals must pass through for the checkpoint/resume flow.
			if _, ok := compose.IsInterruptRerunError(event.Err); ok {
				ch <- StreamResult{Err: event.Err}
				return "", "", false
			}
			// All other errors (tool failure, network glitch, etc.) are surfaced as
			// a token so the LLM can acknowledge the problem and continue.
			slog.Warn("agent: non-fatal event error, forwarding to LLM", "err", event.Err)
			errToken := fmt.Sprintf("\n\n[工具调用出错: %v]", event.Err)
			ch <- StreamResult{Token: errToken}
			sb.WriteString(errToken)
			continue
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			resumeIter, err := handleInterrupt(ctx, runner, event, ch, pendingConfirms, emitEvent, checkpointID)
			if err != nil {
				return "", "", false
			}
			if resumeIter != nil {
				resp, thinkingResp, ok := drainIterInner(ctx, runner, resumeIter, ch, pendingConfirms, emitEvent, checkpointID, uniqueTools, imgsBuf)
				if !ok {
					return "", "", false
				}
				sb.WriteString(resp)
				thinkingSb.WriteString(thinkingResp)
			}
			continue
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mo := event.Output.MessageOutput
		if mo.IsStreaming {
			for {
				m, recvErr := mo.MessageStream.Recv()
				if recvErr != nil {
					if recvErr == io.EOF {
						break
					}
					ch <- StreamResult{Err: recvErr}
					return "", "", false
				}
				if m == nil {
					continue
				}
				if m.Role == schema.Tool || len(m.ToolCalls) > 0 {
					for _, tc := range m.ToolCalls {
						uniqueTools[tc.Function.Name] = struct{}{}
					}
					// Tool results that are image data URLs or image HTTP URLs
					// (e.g. from read_image / generate_image) are forwarded as
					// images; the raw text is intentionally suppressed.
					if m.Role == schema.Tool {
						if imgs := extractToolImages(m.Content); len(imgs) > 0 {
							ch <- StreamResult{Images: imgs}
							*imgsBuf = append(*imgsBuf, imgs...)
						}
					}
					continue
				}
				if imgs := extractImages(m.AssistantGenMultiContent); len(imgs) > 0 {
					ch <- StreamResult{Images: imgs}
					*imgsBuf = append(*imgsBuf, imgs...)
				}
				if m.ReasoningContent != "" {
					ch <- StreamResult{ThinkingToken: m.ReasoningContent}
					thinkingSb.WriteString(m.ReasoningContent)
				}
				if m.Content == "" {
					continue
				}
				ch <- StreamResult{Token: m.Content}
				sb.WriteString(m.Content)
			}
		} else if mo.Message != nil && mo.Message.Role == schema.Tool {
			// Tool result: forward image data URLs / HTTP image URLs to the frontend;
			// suppress raw text so tool internals stay out of the chat panel.
			if imgs := extractToolImages(mo.Message.Content); len(imgs) > 0 {
				ch <- StreamResult{Images: imgs}
				*imgsBuf = append(*imgsBuf, imgs...)
			}
		} else if mo.Message != nil && len(mo.Message.ToolCalls) == 0 {
			if imgs := extractImages(mo.Message.AssistantGenMultiContent); len(imgs) > 0 {
				ch <- StreamResult{Images: imgs}
				*imgsBuf = append(*imgsBuf, imgs...)
			}
			if mo.Message.ReasoningContent != "" {
				ch <- StreamResult{ThinkingToken: mo.Message.ReasoningContent}
				thinkingSb.WriteString(mo.Message.ReasoningContent)
			}
			if mo.Message.Content != "" {
				ch <- StreamResult{Token: mo.Message.Content}
				sb.WriteString(mo.Message.Content)
			}
		} else if mo.Message != nil && len(mo.Message.ToolCalls) > 0 {
			for _, tc := range mo.Message.ToolCalls {
				uniqueTools[tc.Function.Name] = struct{}{}
			}
		}
	}
	return sb.String(), thinkingSb.String(), true
}

// extractImages returns data URLs or HTTP(S) URLs for any image parts in parts.
// Only assistant-generated images are passed here; tool returns are never included.
func extractImages(parts []schema.MessageOutputPart) []string {
	var imgs []string
	for _, p := range parts {
		if p.Type != "image" || p.Image == nil {
			continue
		}
		if p.Image.Base64Data != nil && *p.Image.Base64Data != "" {
			mime := p.Image.MIMEType
			if mime == "" {
				mime = "image/png"
			}
			imgs = append(imgs, "data:"+mime+";base64,"+*p.Image.Base64Data)
		} else if p.Image.URL != nil && *p.Image.URL != "" {
			imgs = append(imgs, *p.Image.URL)
		}
	}
	return imgs
}

// extractToolImages extracts image data URLs or HTTP(S) image URLs from a tool
// result string. Tool implementations that return a single data URL or image URL
// (e.g. read_image, generate_image) are detected here and forwarded to the
// frontend via StreamResult.Images so the chat panel can render them.
func extractToolImages(content string) []string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "data:image/") {
		return []string{content}
	}
	if (strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://")) &&
		isImageURL(content) {
		return []string{content}
	}
	return nil
}

// isImageURL reports whether url has a common image file extension or image-related path.
func isImageURL(url string) bool {
	lower := strings.ToLower(url)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

// handleInterrupt processes an eino interrupt event by notifying the frontend and
// blocking until the user confirms or rejects. Returns the resume iterator on success,
// or (nil, nil) if interrupt data is unrecognised, or (nil, err) on failure.
//
// The resume Targets key is the root-cause InterruptCtx.ID (the fully-qualified
// component address, e.g. "agent:aiko;node:ToolNode;tool:execute_shell:xxx").
// The interrupt payload is in InterruptCtx.Info, set by tool.Interrupt().
func handleInterrupt(
	ctx context.Context,
	runner *adk.Runner,
	event *adk.AgentEvent,
	ch chan<- StreamResult,
	pendingConfirms *sync.Map,
	emitEvent func(string, ...any),
	checkpointID string,
) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	if pendingConfirms == nil || emitEvent == nil {
		return nil, nil
	}
	if event.Action == nil || event.Action.Interrupted == nil ||
		len(event.Action.Interrupted.InterruptContexts) == 0 {
		return nil, nil
	}

	// Find the root-cause interrupt context — its ID is the Targets key for resume.
	ictx := event.Action.Interrupted.InterruptContexts[0]
	for _, c := range event.Action.Interrupted.InterruptContexts {
		if c.IsRootCause {
			ictx = c
			break
		}
	}

	var req ToolConfirmRequest
	switch info := ictx.Info.(type) {
	case internaltools.ShellConfirmInfo:
		req = ToolConfirmRequest{
			ID: info.ID, ToolType: "shell",
			Command: info.Command, WorkingDir: info.WorkingDir,
		}
	case internaltools.CodeConfirmInfo:
		req = ToolConfirmRequest{
			ID: info.ID, ToolType: "code",
			Language: info.Language, Code: info.Code, WorkingDir: info.WorkingDir,
		}
	case internaltools.UpdateConfirmInfo:
		req = ToolConfirmRequest{
			ID:             info.ID,
			ToolType:       "update",
			CurrentVersion: info.CurrentVersion,
			LatestVersion:  info.LatestVersion,
		}
	default:
		slog.Warn("handleInterrupt: unrecognized interrupt info type",
			"type", fmt.Sprintf("%T", ictx.Info), "value", ictx.Info)
		return nil, nil
	}

	respCh := make(chan ToolConfirmResponse, 1)
	pendingConfirms.Store(req.ID, respCh)
	defer pendingConfirms.Delete(req.ID)

	emitEvent("tool:confirm", req)

	select {
	case resp := <-respCh:
		resumeIter, err := runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{
			Targets: map[string]any{
				ictx.ID: internaltools.ConfirmResult{
					Approved:      resp.Approved,
					EditedContent: resp.EditedContent,
				},
			},
		})
		if err != nil {
			ch <- StreamResult{Err: fmt.Errorf("resume failed: %w", err)}
			return nil, err
		}
		return resumeIter, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// extractTextFromMessage returns the plain text from a Message's Content or
// the first text part in UserInputMultiContent. Used as memory key and query.
func extractTextFromMessage(msg *schema.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	for _, p := range msg.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeText && p.Text != "" {
			return p.Text
		}
	}
	return ""
}

// extractImagesFromMessage returns all base64 data URLs from image parts of msg.
func extractImagesFromMessage(msg *schema.Message) []string {
	var images []string
	for _, p := range msg.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeImageURL && p.Image != nil && p.Image.Base64Data != nil {
			images = append(images, "data:"+p.Image.MIMEType+";base64,"+*p.Image.Base64Data)
		}
	}
	return images
}

// ChatWithMessage sends a pre-built user Message (which may contain images via
// UserInputMultiContent) to the agent and streams tokens via the returned channel.
// After streaming, user input and assistant reply are persisted to short-term
// memory; images are stored as data URLs so history can re-render them.
func (a *Agent) ChatWithMessage(ctx context.Context, msg *schema.Message) <-chan StreamResult {
	ch := make(chan StreamResult, 64)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				ch <- StreamResult{Err: fmt.Errorf("agent panic: %v", r)}
			}
		}()

		userText := extractTextFromMessage(msg)
		ctxMsgs, err := a.buildContext(ctx, userText)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		sendMsg := msg

		// Prefer the original user text stored in Extra (no file content) for memory.
		userMemory := extractTextFromMessage(msg)
		if orig, ok := msg.Extra["_user_text"].(string); ok && orig != "" {
			userMemory = orig
		}
		userImages := extractImagesFromMessage(msg)
		// Extract file names passed via Extra by app.go.
		var userFiles []string
		if raw, ok := msg.Extra["_file_names"]; ok {
			if names, ok := raw.([]string); ok {
				userFiles = names
			}
		}

		// Inject flush callback so check_and_update can persist before restart.
		flushFn := func(assistantSummary string) {
			if a.shortMem == nil {
				return
			}
			_, _ = a.shortMem.AddWithImagesAndFiles("user", userMemory, userImages, userFiles)
			if _, _, stripped, ok := parseEmotionTag(assistantSummary); ok {
				assistantSummary = stripped
			}
			_, _ = a.shortMem.AddFull("assistant", assistantSummary, "", nil, nil)
		}
		ctx = context.WithValue(ctx, internaltools.PersistBeforeRestartKey{}, flushFn)

		msgs := append(ctxMsgs, sendMsg)
		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}

		ch <- StreamResult{Done: true}
		go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse, thinkingContent, toolImgs, toolCallCount)
	}()

	return ch
}

// userProfileCache holds a recently-read USER.md to avoid redundant disk reads on every turn.
var userProfileCache struct {
	sync.Mutex
	content   string
	expiresAt time.Time
}

// readUserProfile returns the cached USER.md content, refreshing from disk every 30 seconds.
func readUserProfile(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	userProfileCache.Lock()
	defer userProfileCache.Unlock()
	if time.Now().Before(userProfileCache.expiresAt) {
		return userProfileCache.content
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "USER.md"))
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("read USER.md failed", "err", err)
	}
	userProfileCache.content = string(data)
	userProfileCache.expiresAt = time.Now().Add(30 * time.Second)
	return userProfileCache.content
}

// buildContext fetches user profile, long-term memories (summaries and raws separately),
// and recent short-term history concurrently, then returns a message list ready for
// runner.Run. Errors from individual sources are logged and skipped — a partial context
// is better than no response.
func (a *Agent) buildContext(ctx context.Context, userInput string) ([]adk.Message, error) {
	var profile string
	var memResult memory.MemorySearchResult
	var recentMsgs []*schema.Message
	var location string

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		profile = readUserProfile(a.dataDir)
		return nil
	})

	g.Go(func() error {
		if a.longMem == nil {
			return nil
		}
		res, err := a.longMem.SearchSplit(gctx, userInput, 5)
		if err != nil {
			slog.Warn("longMem.SearchSplit failed", "err", err)
			return nil
		}
		memResult = res
		return nil
	})

	g.Go(func() error {
		if a.shortMem == nil {
			return nil
		}
		msgs, err := a.shortMem.RecentMessages(a.cfg.ShortTermLimit)
		if err != nil {
			slog.Warn("shortMem.RecentMessages error", "err", err)
			return nil
		}
		recentMsgs = msgs
		return nil
	})

	g.Go(func() error {
		location = cachedLocation()
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var msgs []adk.Message

	// Build context pair (user + assistant "Understood.") — always includes current time.
	var ctxBuf strings.Builder
	ctxBuf.WriteString("Current time: ")
	ctxBuf.WriteString(time.Now().Format("2006-01-02 15:04:05 CST"))
	ctxBuf.WriteByte('\n')
	if location != "" {
		ctxBuf.WriteString("Location: ")
		ctxBuf.WriteString(location)
		ctxBuf.WriteByte('\n')
	}
	if profile != "" {
		ctxBuf.WriteString("\nUser Profile:\n")
		ctxBuf.WriteString(profile)
	}
	if len(memResult.Summaries) > 0 || len(memResult.Raws) > 0 {
		ctxBuf.WriteString("\n[Long-term memories — retrieved by semantic similarity; may be outdated. Use as background context, not as absolute truth.]\n")
		if len(memResult.Summaries) > 0 {
			ctxBuf.WriteString("Relevant memory summaries:\n")
			for _, s := range memResult.Summaries {
				ctxBuf.WriteString("- ")
				ctxBuf.WriteString(s)
				ctxBuf.WriteByte('\n')
			}
		}
		if len(memResult.Raws) > 0 {
			ctxBuf.WriteString("Relevant memory details:\n")
			for _, r := range memResult.Raws {
				ctxBuf.WriteString(r)
				ctxBuf.WriteByte('\n')
			}
		}
		ctxBuf.WriteString("[End of long-term memories]\n")
	}
	if ctxBuf.Len() > 0 {
		msgs = append(msgs,
			&schema.Message{Role: schema.User, Content: ctxBuf.String()},
			&schema.Message{Role: schema.Assistant, Content: "Understood."},
		)
	}

	for _, m := range recentMsgs {
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// persistAndMigrate saves user and assistant messages to SQLite, then checks
// whether the total message count exceeds ShortTermLimit. If so, the oldest
// excess messages are migrated to long-term vector memory.
func (a *Agent) persistAndMigrate(ctx context.Context, userInput string, userImages []string, userFiles []string, assistantReply string, thinkingContent string, assistantImages []string, toolCallCount int) {
	if a.shortMem == nil {
		return
	}

	// Increment the turn counter on every completed conversation turn so the
	// self-growth nudge fires at the correct interval regardless of whether
	// short-term memory overflow has occurred.
	a.turnCount.Add(1)

	if _, err := a.shortMem.AddWithImagesAndFiles("user", userInput, userImages, userFiles); err != nil {
		slog.Error("save user message failed", "err", err)
		return
	}
	// Strip the leading emotion tag before persisting so it never appears in
	// chat history or long-term memory.
	if _, _, stripped, ok := parseEmotionTag(assistantReply); ok {
		assistantReply = stripped
	}
	if _, err := a.shortMem.AddFull("assistant", assistantReply, thinkingContent, assistantImages, nil); err != nil {
		slog.Error("save assistant message failed", "err", err)
		return
	}

	// Trigger async self-growth reflection if warranted.
	if trigger, hints := a.shouldReflect(userInput, toolCallCount); trigger {
		go a.reflect(ctx, userInput, assistantReply, hints)
	}

	limit := a.cfg.ShortTermLimit
	if limit <= 0 {
		limit = 30
	}

	count, err := a.shortMem.Count()
	if err != nil {
		slog.Error("count messages failed", "err", err)
		return
	}

	excess := count - limit
	if excess <= 0 {
		return
	}

	oldest, err := a.shortMem.OldestN(excess)
	if err != nil {
		slog.Error("get oldest messages failed", "err", err)
		return
	}
	if len(oldest) == 0 {
		return
	}

	// Store the block in long-term memory (only if available).
	if a.longMem != nil {
		block := memory.FormatBlock(oldest)
		if err := a.longMem.Store(ctx, block); err != nil {
			slog.Error("store long-term memory failed", "err", err)
			// Don't return — still delete from short-term.
		}
	}

	// Delete the migrated messages from short-term store.
	ids := make([]int64, len(oldest))
	for i, m := range oldest {
		ids[i] = m.ID
	}
	if err := a.shortMem.DeleteByIDs(ids); err != nil {
		slog.Error("delete migrated messages failed", "err", err)
	}
}
