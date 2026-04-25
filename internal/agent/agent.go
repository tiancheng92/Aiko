package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	localbk "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/agent/middleware"
	"aiko/internal/config"
	"aiko/internal/memory"
)

// StreamResult is a single streamed token or a terminal signal.
type StreamResult struct {
	Token string
	Err   error
	Done  bool
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
) (*Agent, error) {
	// Apply middleware chain to all tools if provided.
	if mw != nil && len(tools) > 0 {
		tools = middleware.WrapAll(tools, mw)
	}

	backend, err := localbk.NewBackend(ctx, &localbk.Config{})
	if err != nil {
		return nil, err
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
		Backend:        backend,
		StreamingShell: backend,
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
		Agent:          agent,
		EnableStreaming: true,
	})

	ni := cfg.NudgeInterval
	if ni <= 0 {
		ni = 5
	}
	return &Agent{
		runner:        runner,
		shortMem:      shortMem,
		longMem:       longMem,
		cfg:           cfg,
		dataDir:       dataDir,
		nudgeInterval: ni,
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

		history, err := a.buildHistoryPrefix(ctx, userInput)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		query := userInput
		if history != "" {
			query = history + "\nUser: " + userInput
		}

		fullResponse, ok := drainRunner(ctx, a.runner, query, ch)
		if !ok {
			return
		}

		ch <- StreamResult{Done: true}
		go a.persistAndMigrate(context.Background(), userInput, fullResponse)
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
		_, ok := drainRunner(ctx, a.runner, prompt, ch)
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
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult) (string, bool) {
	iter := runner.Query(ctx, query)
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
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mo := event.Output.MessageOutput
		if mo.IsStreaming {
			for {
				msg, recvErr := mo.MessageStream.Recv()
				if recvErr != nil {
					if recvErr == io.EOF {
						break
					}
					ch <- StreamResult{Err: recvErr}
					return "", false
				}
				if msg == nil || msg.Content == "" {
					continue
				}
				// Skip tool role messages and assistant messages that only contain tool calls.
				if msg.Role == schema.Tool || len(msg.ToolCalls) > 0 {
					continue
				}
				ch <- StreamResult{Token: msg.Content}
				sb.WriteString(msg.Content)
			}
		} else if mo.Message != nil && mo.Message.Content != "" {
			// Skip tool result messages and tool-call-only assistant messages.
			if mo.Message.Role != schema.Tool && len(mo.Message.ToolCalls) == 0 {
				ch <- StreamResult{Token: mo.Message.Content}
				sb.WriteString(mo.Message.Content)
			}
		}
	}
	return sb.String(), true
}

// buildHistoryPrefix returns recent conversation history as a formatted string,
// prepended with USER.md profile (if available) and a self-growth nudge (if due).
// Returns empty string if no history exists or an error occurs.
// userInput is used as the semantic query for long-term memory retrieval.
func (a *Agent) buildHistoryPrefix(ctx context.Context, userInput string) (string, error) {
	// Read USER.md for user profile injection.
	var profileSection string
	if a.dataDir != "" {
		profilePath := filepath.Join(a.dataDir, "USER.md")
		if data, err := os.ReadFile(profilePath); err == nil && len(data) > 0 {
			profileSection = "User Profile:\n" + string(data) + "\n"
		} else if err != nil && !os.IsNotExist(err) {
			slog.Warn("read USER.md failed", "err", err)
		}
	}

	if a.shortMem == nil {
		return profileSection, nil
	}

	// Inject relevant long-term memories if available.
	var longMemContext string
	if a.longMem != nil {
		results, err := a.longMem.Search(ctx, userInput, 3)
		if err == nil && len(results) > 0 {
			var lmb strings.Builder
			lmb.WriteString("Relevant past context:\n")
			for _, r := range results {
				lmb.WriteString(r)
				lmb.WriteByte('\n')
			}
			longMemContext = lmb.String()
		}
	}

	recent, err := a.shortMem.Recent(a.cfg.ShortTermLimit)
	if err != nil {
		slog.Warn("short memory Recent error", "err", err)
		recent = nil
	}

	// Assemble history section.
	var histSection string
	if len(recent) > 0 {
		histStr := memory.FormatBlock(recent)
		if longMemContext != "" {
			histSection = longMemContext + "\nRecent conversation:\n" + histStr
		} else {
			histSection = "Recent conversation:\n" + histStr
		}
	} else if longMemContext != "" {
		histSection = longMemContext
	}

	// Append self-growth nudge if due.
	var nudgeSection string
	if a.nudgeInterval > 0 && a.turnCount.Load() > 0 && a.turnCount.Load()%int64(a.nudgeInterval) == 0 {
		nudgeSection = `
[SELF-GROWTH NUDGE]
请在本次回复前，回顾刚才的对话，考虑是否需要：
1. 调用 save_memory 保存一条具体事实或偏好（一两句话，不需要摘要对话）
2. 调用 update_user_profile 更新用户画像（发现了新的习惯/偏好/背景信息）
3. 调用 save_skill 将本次解决的问题模式提炼为可复用技能
如果都不需要，直接回复即可，无需解释。
`
	}

	result := profileSection + histSection + nudgeSection
	return result, nil
}

// persistAndMigrate saves user and assistant messages to SQLite, then checks
// whether the total message count exceeds ShortTermLimit. If so, the oldest
// excess messages are migrated to long-term vector memory.
func (a *Agent) persistAndMigrate(ctx context.Context, userInput, assistantReply string) {
	if a.shortMem == nil {
		return
	}

	if _, err := a.shortMem.Add("user", userInput); err != nil {
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
	a.turnCount.Add(1)
}
