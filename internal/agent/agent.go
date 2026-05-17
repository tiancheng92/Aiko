package agent

import (
	"context"
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
