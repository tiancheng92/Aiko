package agent

import (
	"context"
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

// nudgeText is appended to the user message every nudgeInterval turns to
// prompt the agent to reflect and persist useful knowledge.
const nudgeText = `
[SELF-GROWTH NUDGE]
请在本次回复前，回顾刚才的对话，考虑是否需要：
1. 调用 save_memory 保存一条具体事实或偏好（一两句话，不需要摘要对话）
2. 调用 update_user_profile 更新用户画像（发现了新的习惯/偏好/背景信息）
3. 调用 save_skill 将本次解决的问题模式提炼为可复用技能
如果都不需要，直接回复即可，无需解释。
`

// StreamResult is a single streamed token or a terminal signal.
type StreamResult struct {
	Token string
	Err   error
	Done  bool
}

// ToolConfirmRequest is emitted via Wails event when a tool requests user confirmation.
type ToolConfirmRequest struct {
	ID         string `json:"id"`
	ToolType   string `json:"tool_type"`   // "shell" or "code"
	Command    string `json:"command,omitempty"`
	Code       string `json:"code,omitempty"`
	Language   string `json:"language,omitempty"`
	WorkingDir string `json:"working_dir"`
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

// Agent wraps an eino ReAct agent with short/long-term memory integration.
type Agent struct {
	runner        *adk.Runner
	shortMem      *memory.ShortStore
	longMem       *memory.LongStore
	cfg           *config.Config
	dataDir       string // ~/.aiko data directory, used to read USER.md
	turnCount     atomic.Int64 // completed conversation turns (resets on restart)
	nudgeInterval int    // how often to trigger self-growth nudge
	pendingConfirms *sync.Map // map[string]chan ToolConfirmResponse; bridged from App
	emitEvent       func(event string, data ...any) // Wails EventsEmit
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

	deepCfg := &deep.Config{
		Name:           "aiko",
		Description:    "A desktop pet AI assistant",
		Instruction:    cfg.SystemPrompt,
		ChatModel:      chatModel,
		MaxIteration:   30,
		Handlers:       handlers,
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
		EnableStreaming:  true,
		CheckPointStore: &memCheckPointStore{m: map[string][]byte{}},
	})

	ni := cfg.NudgeInterval
	if ni <= 0 {
		ni = 5
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
	}, nil
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

		content := userInput
		if a.nudgeInterval > 0 && a.turnCount.Load() > 0 &&
			a.turnCount.Load()%int64(a.nudgeInterval) == 0 {
			content += "\n\n" + nudgeText
		}

		msgs := append(ctxMsgs, &schema.Message{Role: schema.User, Content: content})
		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}

		ch <- StreamResult{Done: true}
		go a.persistAndMigrate(context.Background(), userInput, nil, nil, fullResponse)
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
		_, ok := drainRunner(ctx, a.runner, prompt, ch, nil, nil, fmt.Sprintf("direct-%d", time.Now().UnixNano()))
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
// and returns the accumulated response string. Returns (response, true) on
// success or ("", false) after sending an error to ch.
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, bool) {
	iter := runner.Query(ctx, query, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}

// drainRunnerMsg consumes all events from runner.Run with a pre-built message list,
// forwards tokens to ch, and returns the accumulated response string.
// Returns (response, true) on success or ("", false) after sending an error to ch.
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, bool) {
	iter := runner.Run(ctx, msgs, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}

// drainIter consumes all events from an AsyncIterator, forwards tokens to ch,
// handles interrupt events, and returns the accumulated response string.
func drainIter(ctx context.Context, runner *adk.Runner, iter *adk.AsyncIterator[*adk.AgentEvent],
	ch chan<- StreamResult, pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, bool) {
	var sb strings.Builder

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			ch <- StreamResult{Err: event.Err}
			return "", false
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			resumeIter, err := handleInterrupt(ctx, runner, event, ch, pendingConfirms, emitEvent, checkpointID)
			if err != nil {
				return "", false
			}
			if resumeIter != nil {
				resp, ok := drainIter(ctx, runner, resumeIter, ch, pendingConfirms, emitEvent, checkpointID)
				if !ok {
					return "", false
				}
				sb.WriteString(resp)
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
					return "", false
				}
				if m == nil || m.Content == "" {
					continue
				}
				if m.Role == schema.Tool || len(m.ToolCalls) > 0 {
					continue
				}
				ch <- StreamResult{Token: m.Content}
				sb.WriteString(m.Content)
			}
		} else if mo.Message != nil && mo.Message.Content != "" {
			if mo.Message.Role != schema.Tool && len(mo.Message.ToolCalls) == 0 {
				ch <- StreamResult{Token: mo.Message.Content}
				sb.WriteString(mo.Message.Content)
			}
		}
	}
	return sb.String(), true
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

		msgs := append(ctxMsgs, msg)
		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}

		ch <- StreamResult{Done: true}
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
		go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse)
	}()

	return ch
}

// buildContext fetches user profile, long-term memories (summaries and raws separately),
// and recent short-term history concurrently, then returns a message list ready for
// runner.Run. Errors from individual sources are logged and skipped — a partial context
// is better than no response.
func (a *Agent) buildContext(ctx context.Context, userInput string) ([]adk.Message, error) {
	var profile string
	var memResult memory.MemorySearchResult
	var recentMsgs []*schema.Message

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if a.dataDir == "" {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(a.dataDir, "USER.md"))
		if err == nil {
			profile = string(data)
		} else if !os.IsNotExist(err) {
			slog.Warn("read USER.md failed", "err", err)
		}
		return nil
	})

	g.Go(func() error {
		if a.longMem == nil {
			return nil
		}
		res, err := a.longMem.SearchSplit(gctx, userInput, 3)
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

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var msgs []adk.Message

	// Build context pair (user + assistant "Understood.") only if there is content.
	var ctxBuf strings.Builder
	if profile != "" {
		ctxBuf.WriteString("User Profile:\n")
		ctxBuf.WriteString(profile)
	}
	if len(memResult.Summaries) > 0 {
		ctxBuf.WriteString("\nRelevant memory summaries:\n")
		for _, s := range memResult.Summaries {
			ctxBuf.WriteString("- ")
			ctxBuf.WriteString(s)
			ctxBuf.WriteByte('\n')
		}
	}
	if len(memResult.Raws) > 0 {
		ctxBuf.WriteString("\nRelevant memory details:\n")
		for _, r := range memResult.Raws {
			ctxBuf.WriteString(r)
			ctxBuf.WriteByte('\n')
		}
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
func (a *Agent) persistAndMigrate(ctx context.Context, userInput string, userImages []string, userFiles []string, assistantReply string) {
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
	if _, err := a.shortMem.Add("assistant", assistantReply); err != nil {
		slog.Error("save assistant message failed", "err", err)
		return
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
