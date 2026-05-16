# Wails v2 → v3 迁移设计

**日期：** 2026-05-16  
**状态：** 已批准  
**方案：** B — v3 完整迁移 + Service 拆分 + 设置窗口原生多窗口化

---

## 背景与目标

Aiko 当前使用 Wails v2.12.0。迁移到 v3 的驱动：

1. **特性需求**：v3 原生多窗口支持（彻底解决设置窗口 NSPanel hack 的反复 revert），新的 Service 绑定模型，更完善的菜单 API
2. **长期维护**：跟上 Wails 主线，v2 进入维护模式

迁移策略：一次性完整替换，不保留 v2 兼容层；同步将 `app.go` 单体结构体按业务职责拆分为 8 个 v3 Service。

---

## 第一节：依赖与构建系统

### go.mod
```
github.com/wailsapp/wails/v2 v2.12.0  →  github.com/wailsapp/wails/v3 v3.x.x
```

移除所有 v2 indirect 依赖（`go-webview2`、`mimetype` 等 wails 专属间接依赖）。

### Import path 替换规则

| v2 | v3 |
|----|----|
| `github.com/wailsapp/wails/v2` | `github.com/wailsapp/wails/v3/pkg/application` |
| `github.com/wailsapp/wails/v2/pkg/runtime` | 方法直接挂在 `*application.App` 上 |
| `github.com/wailsapp/wails/v2/pkg/menu` | `application.Menu`（同包） |
| `github.com/wailsapp/wails/v2/pkg/options` | `application.Options` |
| `github.com/wailsapp/wails/v2/pkg/options/mac` | `application.MacOptions` |
| `github.com/wailsapp/wails/v2/pkg/options/assetserver` | `application.AssetOptions` |

### 工具链
- `wails build` → `wails3 build`
- `wails dev` → `wails3 dev`
- `wails generate module` → `wails3 generate bindings`

### wails.json
- `$schema` 更新为 v3 schema URL
- 构建/dev/watcher 命令适配 v3 CLI

### 前端 bindings
- 删除 `frontend/wailsjs/`（v2 自动生成）
- 重新生成：`wails3 generate bindings`，输出到 `frontend/bindings/`
- Vue 组件 import 路径从 `../../wailsjs/go/main/App` → 分 service import，如 `../bindings/services/ChatService`

---

## 第二节：main.go 重写

### 应用初始化

```go
app := application.New(application.Options{
    Name: "Aiko",
    Services: []application.Service{
        application.NewService(newChatService(state)),
        application.NewService(newConfigService(state)),
        application.NewService(newToolService(state)),
        application.NewService(newKnowledgeService(state)),
        application.NewService(newMCPService(state)),
        application.NewService(newSchedulerService(state)),
        application.NewService(newWindowService(state)),
        application.NewService(newSystemService(state)),
    },
    Assets: application.AssetOptions{
        Handler: application.BundledAssetFileServer(assets),
        // userVRMHandler 通过 middleware 挂载 /user-vrm/ 路由
    },
    Mac: application.MacOptions{
        ApplicationShouldTerminateAfterLastWindowClosed: false,
    },
})
```

### 窗口创建

```go
// 主窗口（宠物 + 聊天气泡）
mainWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
    Width: 1440, Height: 900,
    Frameless:        true,
    AlwaysOnTop:      true,
    BackgroundColour: application.NewRGBA(0, 0, 0, 0),
    Mac: application.MacWindow{
        Backdrop: application.MacBackdropTransparent,
    },
})

// 设置窗口（v3 原生第二窗口，取代 NSPanel hack）
settingsWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
    Width:  900,
    Height: 680,
    Title:  "Aiko Settings",
    Hidden: true,  // 启动时隐藏
    URL:    "/settings",
})
```

### 菜单（v3 API）

```go
menu := app.NewMenu()
menu.AddRole(application.AppMenu)
menu.AddRole(application.EditMenu)

viewMenu := menu.AddSubmenu("View")
viewMenu.Add("Toggle Chat").
    SetAccelerator("CmdOrCtrl+Shift+P").
    OnClick(func(_ *application.Context) {
        app.Event.Emit("bubble:toggle")
    })

settingsMenu := menu.AddSubmenu("Settings")
settingsMenu.Add("Preferences...").
    SetAccelerator("CmdOrCtrl+,").
    OnClick(func(_ *application.Context) {
        settingsWin.Show()
        settingsWin.Focus()
    })

app.Menu.Set(menu)
app.Run()
```

### 关键变化
- `wails.Run(&options.App{...})` → `application.New(...) + app.Run()`
- `OnStartup` / `OnDomReady` / `OnShutdown` 回调 → 各 Service 的 `ServiceStartup` / `ServiceShutdown`
- `Bind: []any{app}` → `Services: []application.Service{...}`

---

## 第三节：app.go → 多 Service 拆分

### 共享状态（internal/services/shared.go）

所有 Service 注入同一个 `*sharedState`，保留原有并发安全语义：

```go
type sharedState struct {
    mu          sync.RWMutex
    cfg         *config.Config
    petAgent    agent.Interface
    longMem     memory.LongTermInterface
    knowledgeSt knowledge.StoreInterface
    ttsSpeaker  tts.Interface
    mcpClosers  []io.Closer
    app         *application.App
    mainWin     *application.WebviewWindow
    settingsWin *application.WebviewWindow
}
```

并发规则不变：涉及以上字段的读写必须持 `s.mu`（RLock 读，Lock 写）；`sched.Start` / `engine.Start` 在 `s.mu.Unlock()` 之后调用。

### Service 职责划分

| 文件 | 原 app.go 方法 |
|------|---------------|
| `internal/services/chat_service.go` | `SendMessage`, `SendMessageWithImages`, `SendMessageWithFiles`, `RegenerateLastReply`, `GetMessages`, `ClearMessages`, `ChatDirect`, `ChatDirectCollect` |
| `internal/services/config_service.go` | `GetConfig`, `SaveConfig`, `SetAvatar`, `ResetAvatar`, `ListModelProfiles`, `SaveModelProfile`, `DeleteModelProfile`, `ActivateModelProfile`, `GetBallPosition`, `SaveBallPosition`, `ResetBallPosition`, `GetPetSize`, `SavePetSize`, `GetChatSize`, `SaveChatSize`, `MissingRequiredConfig` |
| `internal/services/tool_service.go` | `ConfirmToolExecution`, `KillToolExecution`, `ListToolPermissions`, `SetToolPermission` |
| `internal/services/knowledge_service.go` | `AddKnowledgeSource`, `DeleteKnowledgeSource`, `ListKnowledgeSources`, `ImportKnowledgeFile`, `QueryKnowledge` |
| `internal/services/mcp_service.go` | `AddMCPServer`, `UpdateMCPServer`, `DeleteMCPServer`, `ListMCPServers` |
| `internal/services/scheduler_service.go` | `ListCronJobs`, `AddCronJob`, `UpdateCronJob`, `DeleteCronJob`, `ListProactiveItems`, `DeleteProactiveItem` |
| `internal/services/window_service.go` | `SetChatVisible`, `IsChatVisible`, `GetScreenList`, `GetMousePosition`, `AcquireKeyWindow`, `ReleaseKeyWindow`, `EmitEvent`, `OpenSettings` |
| `internal/services/system_service.go` | `CheckUpdate`, `InstallUpdate`, `IsFirstLaunch`, `MarkWelcomeShown`, `GetVersion`, `FetchLinkPreview` |

### ServiceStartup / ServiceShutdown 模式

```go
type ChatService struct{ s *sharedState }

func (c *ChatService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
    return c.s.initLLMComponents(ctx)  // 原 app.startup 逻辑迁入
}

func (c *ChatService) ServiceShutdown() error {
    c.s.mu.Lock()
    defer c.s.mu.Unlock()
    for _, closer := range c.s.mcpClosers {
        closer.Close()
    }
    return nil
}
```

LLM 组件初始化（`initLLMComponents`）和 `rebuildAgentTools` 逻辑保留在 `sharedState` 的方法上，各 Service 按需调用。

---

## 第四节：Events API 迁移 & macos.go

### v2 → v3 运行时 API 对照

| v2 | v3 |
|----|----|
| `wailsruntime.EventsEmit(ctx, name, data)` | `s.app.Event.Emit(name, data)` |
| `wailsruntime.WindowSetPosition(ctx, x, y)` | `s.mainWin.SetPosition(x, y)` |
| `wailsruntime.WindowSetSize(ctx, w, h)` | `s.mainWin.SetSize(w, h)` |
| `wailsruntime.ScreenGetAll(ctx)` | `s.app.Screen.GetAll()` |

### macos.go 变更

将持有的全局引用从 v2 context 改为 v3 App：

```go
// 现在
var globalAppCtx context.Context

// 改为
var globalApp *application.App
```

`main.go` 在 `application.New()` 后、`app.Run()` 前调用 `setGlobalApp(app)` 注入。

所有 `wailsruntime.EventsEmit(globalAppCtx, "voice:start")` → `globalApp.Event.Emit("voice:start")`。

### 设置窗口事件流

移除 `SettingsWindow.vue` 中的 NSPanel 相关逻辑。`settings:open` 事件流改为：

1. 前端触发 `EventsEmit("settings:open")` 或菜单点击
2. 后端 `WindowService.OpenSettings()` 调用 `settingsWin.Show(); settingsWin.Focus()`
3. 设置内容渲染在独立的 `WebviewWindow`（`/settings` 路由）

前端 `SettingsWindow.vue` 拆成独立页面 `views/settings.vue`，不再是主窗口内覆盖层。

### 前端 EventsOn / EventsEmit

Vue 组件中的 `EventsOn` / `EventsEmit` 来自 v3 生成的 runtime bindings（路径变化，但 API 语义相同）。所有 `import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'` 更新为 v3 对应路径。

---

## 文件变更总览

### 新增
- `internal/services/shared.go` — 共享状态
- `internal/services/chat_service.go`
- `internal/services/config_service.go`
- `internal/services/tool_service.go`
- `internal/services/knowledge_service.go`
- `internal/services/mcp_service.go`
- `internal/services/scheduler_service.go`
- `internal/services/window_service.go`
- `internal/services/system_service.go`
- `frontend/src/views/settings.vue` — 设置独立页面

### 大改
- `main.go` — 完整重写
- `app.go` — 内容拆分到各 service 后删除
- `macos.go` — 替换全局引用类型，移除 NSPanel 逻辑
- `go.mod` / `go.sum` — 依赖替换
- `wails.json` — schema 更新
- `Makefile` — CLI 工具名更新

### 删除
- `frontend/wailsjs/`（v2 生成的 bindings，全部删除）

### 新增（自动生成）
- `frontend/bindings/`（v3 `wails3 generate bindings` 生成）

---

## 风险与注意事项

1. **v3 仍为 beta**：部分 API 可能在正式发布前有小幅变动，实现时以实际 `go get` 的版本为准
2. **hitTest 选择器**：`macos.go` 的 Objective-C hitTest 逻辑不变，但设置窗口是独立 `WebviewWindow`，`.settings-win` CSS class 选择器需确认仍有效或调整
3. **wailsjs 路径**：前端组件中所有 `wailsjs` import 路径需全部更新，是工作量最大的前端部分
4. **`time.Time` 禁令延续**：v3 bindings 生成器同样不识别 `time.Time`，Wails 绑定结构体中继续使用 RFC3339 字符串
5. **`pendingConfirms` sync.Map**：eino interrupt/resume 流程不依赖 Wails 框架，不受迁移影响
6. **前端路由**：`settingsWin` 加载 `/settings` 路由，需在 `frontend/src/router/index.js` 新增该路由，指向 `frontend/src/views/settings.vue`（从 `SettingsWindow.vue` 拆出的独立页面）
7. **domReady 迁移**：v3 无 `OnDomReady` 回调，原 `app.domReady` 逻辑迁移到 `WindowService`，监听 `events.Common.WindowLoadComplete` 窗口事件执行等价操作
