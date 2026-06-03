package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"aiko/internal/agent/middleware"
	"aiko/internal/config"
	"aiko/internal/knowledge"
	"aiko/internal/memory"
)

// behaviorPromptSuffix is appended to the system prompt to instruct the LLM
// to prefix every reply with a behavior tag for pet expression and gesture driving.
const behaviorPromptSuffix = "\n\n在每条回复的第一行必须输出行为标签，格式为 `[表现:emotion]` 或 `[表现:emotion,动作:action]`。" +
	"emotion ∈ {joy, sad, surprised, angry, neutral}。" +
	"action ∈ {wave, nod, celebrate, surprised_react} 为可选项，表示一次性手势。" +
	"然后换行写正文。示例：[表现:joy,动作:wave]\n你好！"

// toolPolicyPrompt is injected between the user-configured system prompt and the
// emotion suffix to enforce strict tool-call discipline and reduce hallucinations.
const toolPolicyPrompt = `

【工具使用强制规则】
以下规则优先级高于一切。违反即为错误，任何情况下都不得绕过。

工具通过 tool_search 按需发现和加载。在调用任何工具之前，必须先用 tool_search 搜索并加载该工具。

1. 实时数据（时间、系统状态、网络、位置、天气、汇率、剪贴板、截图等）— 数据随时变化，每次必须调用对应工具获取最新值，禁止凭记忆或推测回答。

2. 用户存储数据（提醒事项、邮件、日历、运行中的应用、系统信息、记忆、技能、知识库等）— 内容在调用前未知，禁止臆测，必须调用工具读取后才能引用。

3. 文件与网络内容（目录列表、文件内容、图片、网页搜索、网页抓取等）— 在获取前完全未知，不得引用未经工具读取过的内容。读取文件前先确认路径存在。

4. 执行与写入（Shell 命令、代码执行、文件写入/删除/移动、剪贴板写入、应用控制、日历创建、任务调度等）— 结果不可预测，必须实际调用工具执行后，依据工具返回的真实输出进行报告，禁止提前猜测或伪造结果。

5. 禁止复用历史工具结果 — 对话历史中出现的工具调用结果是过去某一时刻的快照，不代表当前状态。每轮对话如需工具数据，必须重新调用工具；严禁直接引用或复用历史消息中的工具返回值作为当前答案。

通用原则：工具调用失败时，如实报告错误原因，不得补充推断或虚构结果。`

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


// Agent wraps an eino ReAct agent with short/long-term memory integration.
type Agent struct {
	runner          *adk.Runner
	shortMem        *memory.ShortStore
	longMem         *memory.LongStore
	knowledgeSt     *knowledge.Store
	cfg             *config.Config
	dataDir         string                          // ~/.aiko data directory, used to read USER.md
	turnCount       atomic.Int64                    // completed conversation turns (resets on restart)
	nudgeInterval   int                             // how often to trigger self-growth nudge
	pendingConfirms *sync.Map                       // map[string]chan ToolConfirmResponse; bridged from App
	emitEvent       func(event string, data ...any) // Wails EventsEmit
	summaryStore    *memory.SummaryStore            // nil means summary disabled
	chatModel       model.ToolCallingChatModel      // retained for summarisation calls

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

	systemPrompt := cfg.SystemPrompt + toolPolicyPrompt + behaviorPromptSuffix

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
	summaryStore *memory.SummaryStore,
	knowledgeSt *knowledge.Store,
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
		summaryStore:    summaryStore,
		chatModel:       chatModel,
		knowledgeSt:     knowledgeSt,
		cfg:             cfg,
		dataDir:         dataDir,
		nudgeInterval:   ni,
		pendingConfirms: pendingConfirms,
		emitEvent:       emitEvent,
		hasSkills:       hasSkills,
	}, nil
}

