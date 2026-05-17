package agent

import (
	"context"
	"fmt"
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

// streamResultBufSize is the buffer capacity for StreamResult channels.
// A buffer of 64 prevents the goroutine from blocking when the frontend
// is briefly slow to consume tokens.
const streamResultBufSize = 64

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

// buildAgentRunner constructs the eino deep agent and its runner from the given
// chat model, tools, config, middleware, and skill middleware.
// mw may be nil (no tool middleware); skillMW may be nil (no skills).
func buildAgentRunner(ctx context.Context,
	chatModel model.ToolCallingChatModel,
	tools []tool.BaseTool,
	cfg *config.Config,
	mw middleware.Middleware,
	skillMW adk.ChatModelAgentMiddleware,
) (*adk.Runner, error) {
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
		WithoutGeneralSubAgent: true,
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

	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming:  true,
		CheckPointStore: &memCheckPointStore{m: map[string][]byte{}},
	}), nil
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
	runner, err := buildAgentRunner(ctx, chatModel, tools, cfg, mw, skillMW)
	if err != nil {
		return nil, err
	}

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
	ch := make(chan StreamResult, streamResultBufSize)

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
			if _, err := a.shortMem.AddWithImagesAndFiles("user", userInput, nil, nil); err != nil {
				slog.Warn("short memory: add user message", "err", err)
			}
			if _, err := a.shortMem.AddFull("assistant", assistantSummary, "", nil, nil); err != nil {
				slog.Warn("short memory: add assistant message", "err", err)
			}
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
	ch := make(chan StreamResult, streamResultBufSize)

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
	ch := make(chan StreamResult, streamResultBufSize)

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
			if _, err := a.shortMem.AddWithImagesAndFiles("user", userMemory, userImages, userFiles); err != nil {
				slog.Warn("short memory: add user message", "err", err)
			}
			if _, _, stripped, ok := parseEmotionTag(assistantSummary); ok {
				assistantSummary = stripped
			}
			if _, err := a.shortMem.AddFull("assistant", assistantSummary, "", nil, nil); err != nil {
				slog.Warn("short memory: add assistant message", "err", err)
			}
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

// gatherContextSources fetches the four context inputs concurrently:
// user profile, long-term memory search results, recent short-term messages,
// and current location. Errors from individual sources are logged and treated
// as empty (non-fatal) to avoid blocking the chat turn.
func (a *Agent) gatherContextSources(ctx context.Context, userInput string) (
	profile string,
	memResult memory.MemorySearchResult,
	recentMsgs []*schema.Message,
	location string,
	err error,
) {
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

	err = g.Wait()
	return
}

// buildContext fetches user profile, long-term memories (summaries and raws separately),
// and recent short-term history concurrently, then returns a message list ready for
// runner.Run. Errors from individual sources are logged and skipped — a partial context
// is better than no response.
func (a *Agent) buildContext(ctx context.Context, userInput string) ([]adk.Message, error) {
	profile, memResult, recentMsgs, location, err := a.gatherContextSources(ctx, userInput)
	if err != nil {
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
		slog.Warn("short memory: add user message", "err", err)
		return
	}
	// Strip the leading emotion tag before persisting so it never appears in
	// chat history or long-term memory.
	if _, _, stripped, ok := parseEmotionTag(assistantReply); ok {
		assistantReply = stripped
	}
	if _, err := a.shortMem.AddFull("assistant", assistantReply, thinkingContent, assistantImages, nil); err != nil {
		slog.Warn("short memory: add assistant message", "err", err)
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
