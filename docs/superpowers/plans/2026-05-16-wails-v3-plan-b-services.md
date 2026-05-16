# Wails v3 迁移 Plan B：app.go → Service 拆分

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `app.go` 的 2900+ 行单体结构体拆分为 8 个 v3 Service，每个 Service 职责单一，通过共享状态通信。

**Architecture:** 所有 Service 共享 `*sharedState`（持有原 `App` 结构体的所有字段和锁）。`main.go` 注册 8 个独立 Service，Wails v3 自动为每个 Service 生成独立的 TypeScript bindings。`app.go` 在此计划结束时删除。

**前置条件：** Plan A 已完成（应用已在 Wails v3 下能编译启动）。

**Tech Stack:** `github.com/wailsapp/wails/v3 v3.0.0-alpha.92`，Go 1.25

---

## 文件变更总览

| 操作 | 文件 |
|------|------|
| 新增 | `internal/services/shared.go` |
| 新增 | `internal/services/chat_service.go` |
| 新增 | `internal/services/config_service.go` |
| 新增 | `internal/services/tool_service.go` |
| 新增 | `internal/services/knowledge_service.go` |
| 新增 | `internal/services/mcp_service.go` |
| 新增 | `internal/services/scheduler_service.go` |
| 新增 | `internal/services/window_service.go` |
| 新增 | `internal/services/system_service.go` |
| 修改 | `main.go` — 注册 8 个 Service，移除 `application.NewService(appInstance)` |
| 删除 | `app.go` |

---

## Task 1：创建 shared.go — 共享状态

**Files:**
- Create: `internal/services/shared.go`

`sharedState` 持有原 `App` 结构体的所有字段，并提供 `initLLMComponents` 和 `rebuildAgentTools` 逻辑（从 `app.go` 原样搬入）。

- [ ] **Step 1: 创建 shared.go 骨架**

```go
package services

import (
	"context"
	"database/sql"
	"io"
	"sync"
	"sync/atomic"

	chromem "github.com/philippgille/chromem-go"
	"github.com/wailsapp/wails/v3/pkg/application"

	"aiko/internal/agent"
	"aiko/internal/config"
	"aiko/internal/knowledge"
	"aiko/internal/llm"
	"aiko/internal/memory"
	"aiko/internal/mcp"
	"aiko/internal/proactive"
	"aiko/internal/scheduler"
	"aiko/internal/sms"
	"aiko/internal/tools"
	"aiko/internal/tts"
)

// sharedState holds all runtime state shared across services.
// Field access follows the same concurrency rules as the original App struct:
// reads take RLock, writes take Lock; scheduler/engine Start must be called
// after mu.Unlock to prevent deadlock.
type sharedState struct {
	ctx    context.Context
	cancel context.CancelFunc

	sqlDB        *sql.DB
	configStore  *config.Store
	profileStore *config.ProfileStore
	vectorDB     *chromem.DB
	shortMem     *memory.ShortStore
	permStore    *tools.PermissionStore
	mcpStore     *mcp.ServerStore

	mu              sync.RWMutex
	activeScreen    application.Screen
	cfg             *config.Config
	scheduler       *scheduler.Scheduler
	longMem         *memory.LongStore
	knowledgeSt     *knowledge.Store
	petAgent        *agent.Agent
	smsWatcher      *sms.Watcher
	chatCancel      context.CancelFunc
	chatGeneration  uint64
	ttsSpeaker      tts.Speaker
	ttsBackendKey   string
	ttsCancel       context.CancelFunc
	ttsGeneration   uint64
	isChatVisible   bool
	proactiveEngine *proactive.ProactiveEngine
	mcpClosers      []io.Closer
	llmTransport    *llm.ErrorBodyTransport
	chatModel       model.ToolCallingChatModel // import "github.com/cloudwego/eino/components/model"
	rebuildGen      atomic.Int64
	runningCmds     sync.Map
	pendingConfirms sync.Map

	watcherWG            sync.WaitGroup
	cancelWatcher        context.CancelFunc
	pendingUpdateVersion string

	app         *application.App
	mainWin     application.Window
	settingsWin application.Window
}

// NewSharedState creates and returns a new sharedState.
func NewSharedState(app *application.App) *sharedState {
	return &sharedState{app: app}
}
```

注意：`initLLMComponents` 和 `rebuildAgentTools` 方法从 `app.go` 原样复制到 `shared.go`（将 `a *App` receiver 改为 `s *sharedState`，`a.ctx` 改为 `s.ctx`，`a.mu` 改为 `s.mu` 等）。

- [ ] **Step 2: 将 initLLMComponents 搬入 shared.go**

从 `app.go` 复制 `func (a *App) initLLMComponents(ctx context.Context) error` 的完整实现到 `shared.go`，receiver 改为 `func (s *sharedState) initLLMComponents(ctx context.Context) error`，所有 `a.` 字段引用改为 `s.`，所有 `globalApp.Event.Emit(...)` 保持不变（globalApp 仍为包级变量）。

- [ ] **Step 3: 将 rebuildAgentTools 搬入 shared.go**

同上，从 `app.go` 复制 `func (a *App) rebuildAgentTools(...)` 到 `shared.go`，替换 receiver。

- [ ] **Step 4: 将 startup 逻辑搬入 shared.go**

在 `shared.go` 中创建 `func (s *sharedState) startup(ctx context.Context) error`，内容来自 `app.go` 的 `ServiceStartup`（Plan A 中已重命名）。

- [ ] **Step 5: 将 shutdown 逻辑搬入 shared.go**

创建 `func (s *sharedState) shutdown() error`，内容来自 `app.go` 的 `ServiceShutdown`。

- [ ] **Step 6: 验证编译**

```bash
go build ./internal/services/...
```

预期：无错误（此阶段 shared.go 可能有未使用 import，加 `_ "pkg"` 临时占位）。

- [ ] **Step 7: 提交**

```bash
git add internal/services/shared.go
git commit -m "feat: add shared state for service split"
```

---

## Task 2：创建 ChatService

**Files:**
- Create: `internal/services/chat_service.go`

包含原 `app.go` 中的对话相关方法：`SendMessage`、`SendMessageWithImages`、`SendMessageWithFiles`、`RegenerateLastReply`、`GetMessages`、`GetMessagesBeforeID`、`ClearChatHistory`、`ExportChatHistory`、`StopGeneration`、`ChatDirect`、`ChatDirectCollect`。

- [ ] **Step 1: 创建文件骨架**

```go
package services

import (
	"context"
	// ... 按实际需要添加 import
)

// ChatService handles AI conversation, streaming, and message history.
type ChatService struct{ s *sharedState }

// NewChatService creates a ChatService backed by the given shared state.
func NewChatService(s *sharedState) *ChatService { return &ChatService{s: s} }

func (c *ChatService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return c.s.startup(ctx)
}

func (c *ChatService) ServiceShutdown() error {
	return c.s.shutdown()
}
```

- [ ] **Step 2: 从 app.go 迁移所有聊天方法**

将以下方法从 `app.go` 复制到 `chat_service.go`，receiver 从 `(a *App)` 改为 `(c *ChatService)`，所有 `a.` 替换为 `c.s.`：

- `SendMessage(userInput string) error`
- `SendMessageWithImages(userInput string, images []string) error`
- `SendMessageWithFiles(userInput string, images []string, files []FileAttachment) error`
- `RegenerateLastReply() error`
- `GetMessages(limit int) ([]memory.Message, error)`
- `GetMessagesBeforeID(id int64, limit int) ([]memory.Message, error)`
- `ClearChatHistory() error`
- `ExportChatHistory() (string, error)`
- `StopGeneration()`
- `ChatDirect(ctx context.Context, prompt string) error`
- `ChatDirectCollect(ctx context.Context, prompt string) (string, error)`

以及私有辅助方法：`formatChatError`、`drainIter`、`handleInterrupt`、`persistAndMigrate`（凡 `app.go` 中被上述方法调用的私有方法）。

同时迁移 `FileAttachment` 类型定义到 `chat_service.go`（或 `shared.go` 若其他 service 也用到）。

- [ ] **Step 3: 编译验证**

```bash
go build ./internal/services/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/services/chat_service.go
git commit -m "feat: add ChatService"
```

---

## Task 3：创建 ConfigService

**Files:**
- Create: `internal/services/config_service.go`

- [ ] **Step 1: 创建文件，迁移方法**

```go
package services

// ConfigService handles app configuration, model profiles, and layout persistence.
type ConfigService struct{ s *sharedState }

// NewConfigService creates a ConfigService backed by the given shared state.
func NewConfigService(s *sharedState) *ConfigService { return &ConfigService{s: s} }
```

迁移以下方法（receiver `(a *App)` → `(c *ConfigService)`，`a.` → `c.s.`）：

- `GetConfig() *config.Config`
- `SaveConfig(cfg *config.Config) error`
- `SetAvatar(role string, dataURL string) error`
- `ResetAvatar(role string) error`
- `ListModelProfiles() ([]config.ModelProfile, error)`
- `SaveModelProfile(p config.ModelProfile) (config.ModelProfile, error)`
- `DeleteModelProfile(id int64) error`
- `ActivateModelProfile(id int64) error`
- `GetBallPosition(screenW, screenH int) []int`
- `SaveBallPosition(x, y, screenW, screenH int) error`
- `ResetBallPosition(screenW, screenH int) error`
- `GetPetSize(screenW, screenH int) int`
- `SavePetSize(size, screenW, screenH int) error`
- `GetChatSize(screenW, screenH int) []int`
- `SaveChatSize(width, height, screenW, screenH int) error`
- `MissingRequiredConfig() []string`
- `PingLLM() error`
- `ListLLMModels() ([]string, error)`
- `GetAvailableModels() ([]string, error)`
- `ListOpenRouterModels() ([]string, error)`
- `GetAutoLaunch() bool`
- `SetAutoLaunch(enabled bool) error`
- `GetSoundsEnabled() bool`
- `SetSoundsEnabled(enabled bool) error`
- `GetTTSAutoPlay() bool`
- `SetTTSAutoPlay(enabled bool) error`
- `GetVoiceAutoSend() bool`
- `SetVoiceAutoSend(enabled bool) error`
- `GetKokoroTTSVoices() []string`
- `SetupKokoroTTS() error`
- `SpeakText(text string) error`
- `StopTTS()`

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/services/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/services/config_service.go
git commit -m "feat: add ConfigService"
```

---

## Task 4：创建 ToolService

**Files:**
- Create: `internal/services/tool_service.go`

- [ ] **Step 1: 创建文件，迁移方法**

```go
package services

// ToolService manages tool execution permissions and confirmation flow.
type ToolService struct{ s *sharedState }

// NewToolService creates a ToolService backed by the given shared state.
func NewToolService(s *sharedState) *ToolService { return &ToolService{s: s} }
```

迁移：

- `ConfirmToolExecution(id string, approved bool, editedContent string)`
- `KillToolExecution(id string)`
- `GetToolPermissions() ([]tools.PermissionRow, error)`
- `SetToolPermission(toolName string, allowed bool) error`

- [ ] **Step 2: 编译验证并提交**

```bash
go build ./internal/services/...
git add internal/services/tool_service.go
git commit -m "feat: add ToolService"
```

---

## Task 5：创建 KnowledgeService

**Files:**
- Create: `internal/services/knowledge_service.go`

- [ ] **Step 1: 创建文件，迁移方法**

```go
package services

// KnowledgeService manages the RAG knowledge base.
type KnowledgeService struct{ s *sharedState }

// NewKnowledgeService creates a KnowledgeService backed by the given shared state.
func NewKnowledgeService(s *sharedState) *KnowledgeService { return &KnowledgeService{s: s} }
```

迁移：

- `ListKnowledgeSources() ([]knowledge.Source, error)`
- `AddKnowledgeSource(name, path string) error`
- `DeleteKnowledgeSource(id int64) error`
- `ImportKnowledge(id int64) error`

- [ ] **Step 2: 编译验证并提交**

```bash
go build ./internal/services/...
git add internal/services/knowledge_service.go
git commit -m "feat: add KnowledgeService"
```

---

## Task 6：创建 MCPService

**Files:**
- Create: `internal/services/mcp_service.go`

- [ ] **Step 1: 创建文件，迁移方法**

```go
package services

// MCPService manages MCP server configuration with hot-reload.
type MCPService struct{ s *sharedState }

// NewMCPService creates an MCPService backed by the given shared state.
func NewMCPService(s *sharedState) *MCPService { return &MCPService{s: s} }
```

迁移：

- `ListMCPServers() ([]mcp.Server, error)`
- `AddMCPServer(srv mcp.Server) (mcp.Server, error)`
- `UpdateMCPServer(srv mcp.Server) error`
- `DeleteMCPServer(id int64) error`

- [ ] **Step 2: 编译验证并提交**

```bash
go build ./internal/services/...
git add internal/services/mcp_service.go
git commit -m "feat: add MCPService"
```

---

## Task 7：创建 SchedulerService

**Files:**
- Create: `internal/services/scheduler_service.go`

- [ ] **Step 1: 创建文件，迁移方法**

```go
package services

// SchedulerService manages cron jobs and proactive message items.
type SchedulerService struct{ s *sharedState }

// NewSchedulerService creates a SchedulerService backed by the given shared state.
func NewSchedulerService(s *sharedState) *SchedulerService { return &SchedulerService{s: s} }
```

迁移：

- `ListCronJobs() ([]scheduler.Job, error)`
- `CreateCronJob(job scheduler.Job) (scheduler.Job, error)`
- `UpdateCronJob(job scheduler.Job) error`
- `DeleteCronJob(id int64) error`
- `SetCronJobEnabled(id int64, enabled bool) error`
- `RunCronJobNow(id int64) error`
- `ListProactiveItems() ([]proactive.Item, error)`
- `DeleteProactiveItem(id int64) error`

- [ ] **Step 2: 编译验证并提交**

```bash
go build ./internal/services/...
git add internal/services/scheduler_service.go
git commit -m "feat: add SchedulerService"
```

---

## Task 8：创建 WindowService

**Files:**
- Create: `internal/services/window_service.go`

- [ ] **Step 1: 创建文件，迁移方法**

```go
package services

// WindowService manages window state, screen info, and hotkey-driven UI.
type WindowService struct{ s *sharedState }

// NewWindowService creates a WindowService backed by the given shared state.
func NewWindowService(s *sharedState) *WindowService { return &WindowService{s: s} }

// OpenSettings shows and focuses the settings window.
func (w *WindowService) OpenSettings() {
	if win, ok := w.s.app.Window.GetByName("settings"); ok {
		win.Show()
		win.Focus()
	}
}
```

迁移：

- `SetChatVisible(visible bool)`
- `IsChatVisible() bool`
- `GetScreenList() []ScreenInfo`
- `GetScreenSize() ScreenInfo`
- `GetMousePosition() MousePosition`
- `AcquireKeyWindow()`
- `ReleaseKeyWindow()`
- `EmitEvent(name string, data any)`
- `OpenDirectoryDialog() (string, error)`
- `OpenFileDialog(filter string) (string, error)`

同时迁移 `ScreenInfo`、`MousePosition` 类型定义（若尚在 `app.go` 中）。

- [ ] **Step 2: 编译验证并提交**

```bash
go build ./internal/services/...
git add internal/services/window_service.go
git commit -m "feat: add WindowService"
```

---

## Task 9：创建 SystemService

**Files:**
- Create: `internal/services/system_service.go`

- [ ] **Step 1: 创建文件，迁移方法**

```go
package services

// SystemService handles app updates, VRM models, SMS watcher, and misc system calls.
type SystemService struct{ s *sharedState }

// NewSystemService creates a SystemService backed by the given shared state.
func NewSystemService(s *sharedState) *SystemService { return &SystemService{s: s} }
```

迁移：

- `GetVersion() string`
- `IsFirstLaunch() bool`
- `MarkWelcomeShown() error`
- `CheckUpdate() (map[string]any, error)`
- `InstallUpdate(downloadURL string) error`
- `FetchLinkPreview(rawURL string) LinkPreview`
- `GetVRMPath() string`
- `ListVRMModels() []string`
- `ImportVRMFile(name string, base64Data string) error`
- `DeleteVRMModel(name string) error`
- `LarkStatus() (string, error)`
- `LarkRunCommand(args []string) (string, error)`
- `StartSMSWatcher() error`
- `StopSMSWatcher() error`
- `IsSMSWatcherRunning() bool`

同时迁移 `LinkPreview` 类型定义。

- [ ] **Step 2: 编译验证并提交**

```bash
go build ./internal/services/...
git add internal/services/system_service.go
git commit -m "feat: add SystemService"
```

---

## Task 10：更新 main.go — 注册 8 个 Service

**Files:**
- Modify: `main.go`

- [ ] **Step 1: 在 main.go 中创建 sharedState 并注册所有 Service**

将 `main.go` 中的 `application.New` 调用更新为：

```go
import (
    // ...
    "aiko/internal/services"
)

func main() {
    // ... 日志和签名升级代码不变 ...

    app := application.New(application.Options{
        Name: "Aiko",
        // Services 在 setGlobalApp 之后注册，因为 sharedState 需要 app 引用
    })
    setGlobalApp(app)

    state := services.NewSharedState(app)

    chat    := services.NewChatService(state)
    cfg     := services.NewConfigService(state)
    tool    := services.NewToolService(state)
    know    := services.NewKnowledgeService(state)
    mcpSvc  := services.NewMCPService(state)
    sched   := services.NewSchedulerService(state)
    winSvc  := services.NewWindowService(state)
    sysSvc  := services.NewSystemService(state)

    app.RegisterService(application.NewService(chat))
    app.RegisterService(application.NewService(cfg))
    app.RegisterService(application.NewService(tool))
    app.RegisterService(application.NewService(know))
    app.RegisterService(application.NewService(mcpSvc))
    app.RegisterService(application.NewService(sched))
    app.RegisterService(application.NewService(winSvc))
    app.RegisterService(application.NewService(sysSvc))

    // 窗口和菜单定义不变 ...
    if err := app.Run(); err != nil {
        panic(err)
    }
}
```

注意：`application.New` 在 `setGlobalApp` 之前调用，但 `state` 需要 `app`，所以 `services.NewSharedState(app)` 在 `setGlobalApp` 之后。Service 通过 `app.RegisterService` 注册（v3 支持在 `New` 之后动态注册）。

- [ ] **Step 2: 移除 app.go 中现有的 `application.NewService(appInstance)` 注册**

（如果 Plan A 中临时保留了整个 `App` 作为单一 Service，此步将其移除）

- [ ] **Step 3: 编译验证**

```bash
go build ./...
```

- [ ] **Step 4: 提交**

```bash
git add main.go
git commit -m "feat: register 8 services in main.go, remove monolithic App service"
```

---

## Task 11：重新生成 bindings 并删除 app.go

**Files:**
- Delete: `app.go`
- Regenerate: `frontend/bindings/`

- [ ] **Step 1: 确认 app.go 中所有方法已迁移**

```bash
grep "^func (a \*App)" app.go | wc -l
```

预期：0（所有公开方法已迁移到各 Service）。

- [ ] **Step 2: 删除 app.go**

```bash
rm app.go
```

- [ ] **Step 3: 编译确认**

```bash
go build ./...
```

预期：无错误。

- [ ] **Step 4: 重新生成前端 bindings**

```bash
wails3 generate bindings -f .
```

v3 会为每个 Service 生成独立的 bindings 文件，例如：
- `frontend/bindings/aiko/internal/services/ChatService.js`
- `frontend/bindings/aiko/internal/services/ConfigService.js`
- 等等

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat: complete service split — delete app.go, regenerate bindings"
```

---

## Task 12：更新前端 import 路径（Service 拆分后）

**Files:**
- Modify: `frontend/src/` 下所有组件和 composable

Plan A 已将 import 路径从 `wailsjs/go/main/App` 更新为 v3 的单一 App bindings 路径。Plan B 之后，bindings 按 Service 分拆，需要再次更新。

- [ ] **Step 1: 确认新的 bindings 路径**

```bash
find frontend/bindings -name "*.js" | grep -v runtime | head -20
```

记录每个 Service 的实际文件路径。

- [ ] **Step 2: 按 Service 更新各组件 import**

以 `ChatPanel.vue` 为例，它调用 `SendMessage`、`GetMessages`、`RegenerateLastReply` 等，这些现在来自 `ChatService`：

```js
// 旧（Plan A 后）
import { SendMessage, GetMessages } from '../../bindings/aiko/App'

// 新（Plan B 后）
import { SendMessage, GetMessages, RegenerateLastReply } from '../../bindings/aiko/internal/services/ChatService'
```

对每个组件，根据它调用的方法，确定其来自哪个 Service，更新 import。

- [ ] **Step 3: 验证无遗漏**

```bash
cd frontend && yarn build
```

预期：构建成功，无未找到的 import。

- [ ] **Step 4: 提交**

```bash
git add frontend/src
git commit -m "refactor: update frontend imports to use split service bindings"
```

---

## Task 13：集成验证

- [ ] **Step 1: 开发模式启动**

```bash
wails3 dev
```

- [ ] **Step 2: 验证核心功能**

逐一测试：
- [ ] 发送聊天消息，收到流式响应
- [ ] 打开设置，修改配置并保存
- [ ] 切换模型 profile
- [ ] 工具执行确认弹窗（执行一条 shell 命令）
- [ ] 长按 Option 键触发语音录音
- [ ] Cmd+Shift+P 切换聊天气泡

- [ ] **Step 3: 提交最终修复**

```bash
git add -A
git commit -m "fix: service split integration fixes"
```

---

## 完成标准

- [ ] `go build ./...` 无错误
- [ ] `app.go` 已删除
- [ ] `internal/services/` 包含 9 个文件（`shared.go` + 8 个 Service）
- [ ] `cd frontend && yarn build` 无错误
- [ ] 核心功能验证全部通过
