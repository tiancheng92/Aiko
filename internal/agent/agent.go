package agent

import (
	"context"
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

