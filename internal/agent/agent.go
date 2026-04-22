package agent

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"desktop-pet/internal/agent/middleware"
	"desktop-pet/internal/config"
	"desktop-pet/internal/memory"
)

// StreamResult is a single streamed token or a terminal signal.
type StreamResult struct {
	Token string
	Err   error
	Done  bool
}

// Agent wraps an eino ReAct agent with short/long-term memory integration.
type Agent struct {
	runner   *adk.Runner
	shortMem *memory.ShortStore
	longMem  *memory.LongStore
	cfg      *config.Config
}

// New constructs an Agent with a ReAct runner backed by the given chat model,
// memory stores, and optional tools. longMem may be nil when vector memory is
// not configured.
func New(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	shortMem *memory.ShortStore,
	longMem *memory.LongStore,
	tools []tool.BaseTool,
	cfg *config.Config,
	mw middleware.Middleware,
) (*Agent, error) {
	// Apply middleware chain to all tools if provided.
	if mw != nil && len(tools) > 0 {
		tools = middleware.WrapAll(tools, mw)
	}
	agentCfg := &adk.ChatModelAgentConfig{
		Name:          "desktop-pet",
		Description:   "A desktop pet AI assistant",
		Instruction:   cfg.SystemPrompt,
		Model:         chatModel,
		MaxIterations: 10,
	}

	if len(tools) > 0 {
		agentCfg.ToolsConfig = adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		}
	}

	cma, err := adk.NewChatModelAgent(ctx, agentCfg)
	if err != nil {
		return nil, err
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           cma,
		EnableStreaming:  true,
	})

	return &Agent{
		runner:   runner,
		shortMem: shortMem,
		longMem:  longMem,
		cfg:      cfg,
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

		// Prepend recent history as context to the query.
		history, err := a.buildHistoryPrefix(ctx, userInput)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		query := userInput
		if history != "" {
			query = history + "\nUser: " + userInput
		}

		iter := a.runner.Query(ctx, query)

		var sb strings.Builder
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				ch <- StreamResult{Err: event.Err}
				return
			}
			if event.Output == nil || event.Output.MessageOutput == nil {
				continue
			}

			mo := event.Output.MessageOutput
			if mo.IsStreaming {
				// Drain the stream and forward tokens.
				for {
					msg, recvErr := mo.MessageStream.Recv()
					if recvErr != nil {
						if recvErr == io.EOF {
							break
						}
						ch <- StreamResult{Err: recvErr}
						return
					}
					if msg != nil && msg.Content != "" {
						ch <- StreamResult{Token: msg.Content}
						sb.WriteString(msg.Content)
					}
				}
			} else if mo.Message != nil && mo.Message.Content != "" {
				ch <- StreamResult{Token: mo.Message.Content}
				sb.WriteString(mo.Message.Content)
			}
		}
		fullResponse := sb.String()

		ch <- StreamResult{Done: true}

		// Persist to memory asynchronously so we don't block the caller.
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

		iter := a.runner.Query(ctx, prompt)

		var sb strings.Builder
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				ch <- StreamResult{Err: event.Err}
				return
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
						return
					}
					if msg != nil && msg.Content != "" {
						ch <- StreamResult{Token: msg.Content}
						sb.WriteString(msg.Content)
					}
				}
			} else if mo.Message != nil && mo.Message.Content != "" {
				ch <- StreamResult{Token: mo.Message.Content}
				sb.WriteString(mo.Message.Content)
			}
		}
		ch <- StreamResult{Done: true}
		// NOTE: No persistAndMigrate call here — intentional.
	}()

	return ch
}

// buildHistoryPrefix returns recent conversation history as a formatted string.
// Returns empty string if no history exists or an error occurs.
// userInput is used as the semantic query for long-term memory retrieval.
func (a *Agent) buildHistoryPrefix(ctx context.Context, userInput string) (string, error) {
	if a.shortMem == nil {
		return "", nil
	}

	// Also inject relevant long-term memories if available.
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
		return longMemContext, nil
	}

	if len(recent) == 0 {
		return longMemContext, nil
	}

	histStr := memory.FormatBlock(recent)
	if longMemContext != "" {
		return longMemContext + "\nRecent conversation:\n" + histStr, nil
	}
	return "Recent conversation:\n" + histStr, nil
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

	// Migrate oldest messages to long-term memory when over limit.
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
