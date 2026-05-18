# Desktop Pet — CLAUDE.md

AI 编码助手指引，供 Claude Code 在此项目中使用。

## 项目概览

**Aiko** 是一个 macOS 原生 AI 桌面宠物应用，采用 Wails v2 架构。Go 后端通过一组 `app_*.go` 文件暴露绑定方法给 Vue 3 前端；前端资源 embed 进 Go 二进制。核心特色是点击穿透（通过 `macos.go` 的 Objective-C cgo 实现）和基于 eino ReAct Agent 的智能对话系统。

> **平台支持**：当前仅支持 macOS，**短期内不计划支持 Windows 或 Linux**。macOS 专属 API（Objective-C CGO、AVAudioEngine、SFSpeechRecognizer、osascript 等）是核心功能的基础，跨平台移植工作量巨大且不在近期路线图内。

### 核心依赖

**后端技术栈：**
- [Wails v2](https://wails.io/) - 跨平台桌面应用框架
- [eino](https://github.com/cloudwego/eino) - 字节跳动 Agent Development Kit
- [chromem-go](https://github.com/philippgille/chromem-go) - 纯 Go 向量数据库
- [robfig/cron/v3](https://pkg.go.dev/github.com/robfig/cron/v3) - Cron 任务调度器
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - 纯 Go SQLite 驱动
- [zerolog](https://github.com/rs/zerolog) - 零分配结构化日志库

**前端技术栈：**
- [Vue 3](https://vuejs.org/) + Composition API - 响应式前端框架
- [Vite](https://vitejs.dev/) - 现代构建工具
- [marked](https://marked.js.org/) - Markdown 解析渲染
- [highlight.js](https://highlightjs.org/) - 代码语法高亮
- [KaTeX](https://katex.org/) - 数学公式渲染

**AI 生态：**
- OpenAI API 兼容接口 (OpenRouter, DeepSeek, 通义千问等)
- MCP (Model Context Protocol) - 工具协议标准
- lark-cli - 飞书命令行工具集成

## 架构设计

### Wails 绑定层（根目录）

Wails 绑定方法按功能域拆分到多个文件，禁止在 app_*.go 之外的文件添加绑定方法：

```
main.go             # 应用入口
app.go              # startup/shutdown、initLLMComponents、通用工具方法
app_chat.go         # 聊天：SendMessage、SendMessageWithImages/Files、Stop、History、Regenerate
app_config.go       # 配置：GetConfig、SaveConfig、ModelProfile CRUD
app_cron.go         # 定时任务：CRUD、手动触发
app_knowledge.go    # 知识库：ImportKnowledge（异步）、ListKnowledgeSources、删除
app_layout.go       # 布局：GetScreenList、GetPetSize/Position、GetChatSize/Position、Save*
app_mcp.go          # MCP：ListMCPServers、Add/Update/DeleteMCPServer（均异步热重载）
app_system.go       # 系统：权限、Lark、SMS、CheckUpdate、InstallUpdate（异步）、首次启动
app_tts.go          # TTS：SpeakText、StopTTS、GetTTSStatus
app_voice.go        # 语音识别：StartVoiceRecognition、StopVoiceRecognition、GetVoiceAutoSend
app_vrm.go          # VRM 模型：ListVRMModels、ImportVRMFile、DeleteVRMFile
macos.go            # macOS 专属：点击穿透 hitTest、全局 Option 键监控、AVAudioEngine STT
```

### 后端内部包

```
internal/
├── agent/             # eino ReAct Agent
│   ├── agent.go       # 初始化、工具装配
│   ├── chat.go        # 流式对话、chatCancel 管理
│   ├── drain.go       # 事件流处理、interrupt/resume 调度
│   ├── context.go     # 对话上下文（短期记忆拼装）
│   ├── emotion.go     # 情绪标签提取
│   └── middleware/    # 日志/重试/ErrorRecovery 中间件
├── tools/             # 内置工具系统
│   ├── appctl/        # list_running_apps / control_app
│   ├── browser/       # get_browser_url（osascript）
│   ├── calendar/      # get_calendar_events / create_calendar_event
│   ├── clipboard/     # read_clipboard / write_clipboard
│   ├── context/       # get_context（注入系统上下文）
│   ├── cron/          # list/add/update/delete_cron_job
│   ├── dev/           # JSON 格式化、编码转换、进制转换等开发者工具
│   ├── exec/          # execute_shell / execute_code（eino interrupt/resume）
│   ├── fs/            # list_directory / read_file / write_file / delete_file / ...
│   ├── growth/        # save_memory / update_user_profile / save_skill / schedule_followup
│   ├── image/         # image_resize / image_convert 等图片处理
│   ├── location/      # get_location（CoreLocation，macOS）
│   ├── mail/          # get_mail_list / read_mail（osascript Mail.app）
│   ├── reminders/     # get_reminders / complete_reminder（osascript）
│   ├── screenshot/    # take_screenshot（EnhancedInvokableTool，返回 PNG base64）
│   ├── system/        # get_system_info / get_network_status 等
│   ├── timeutil/      # get_current_time / get_timezone 等
│   ├── weather/       # get_weather
│   ├── web/           # fetch_webpage / search_web 等
│   ├── registry.go    # All() / AllContextual() / AllEino() / AllPermissionDeclarations()
│   ├── tool.go        # Tool / EnhancedTool 接口定义
│   └── permission.go  # PermissionStore（SQLite 持久化工具开关）
├── config/            # 配置持久化（SQLite，settings 表）
├── db/                # Schema 迁移管理
├── execenv/           # Python/Node/Ruby/Bash 执行环境
├── knowledge/         # RAG 知识库（chromem-go 向量 + SQLite 元数据）
├── lark/              # 飞书 lark-cli 客户端封装
├── llm/               # ChatModel / Embedder 抽象层
├── mcp/               # MCP 协议客户端（stdio + SSE）
├── memory/            # 短期记忆(SQLite messages) + 长期记忆(chromem-go)
├── notify/            # macOS UserNotifications 系统通知
├── proactive/         # 主动消息引擎（proactive_items 轮询）
├── scheduler/         # Cron 任务调度（robfig/cron 封装）
├── skill/             # YAML 自定义技能（auto-skills/ 目录）
├── sms/               # 短信监听（fsnotify chat.db + 验证码正则）
└── tts/               # TTS 后端（OpenAI / Kokoro / macOS AVSpeechSynthesizer）
```

### 前端架构

```
frontend/src/
├── components/
│   ├── ChatPanel.vue           # 聊天面板（消息列表、输入框、图片/文件粘贴）
│   ├── ChatBubble.vue          # 可拖拽/折叠聊天气泡容器（延迟关闭、全屏模式）
│   ├── Live2DPet.vue           # Live2D 宠物渲染（pixi-live2d-display）
│   ├── VRMPet.vue              # VRM 3D 宠物渲染（Three.js）
│   ├── FloatingBall.vue        # 悬浮球（折叠态快捷入口，可拖拽）
│   ├── SettingsWindow.vue      # 设置窗口（全功能配置页）
│   ├── NotificationBubble.vue  # 应用内通知气泡
│   ├── ToolConfirmModal.vue    # 工具执行确认弹窗（shell/code）
│   ├── ExecutionProgress.vue   # 工具执行进度条
│   ├── ContextMenu.vue         # 右键上下文菜单
│   └── LinkPreview.vue         # 链接预览卡片
├── composables/
│   ├── useConfirm.js           # 通用确认弹窗
│   ├── useEmotionEvents.js     # 情绪事件绑定
│   ├── useEscapeKey.js         # ESC 键关闭
│   ├── useModelPath.js         # Live2D/VRM 模型路径解析
│   ├── usePetRenderer.js       # 宠物渲染器类型判断
│   ├── usePetState.js          # 宠物状态机（idle/thinking/speaking/error）
│   ├── useSounds.js            # 音效播放
│   ├── useSpring.js            # 弹簧动画
│   ├── useTypingScheduler.js   # 打字机效果调度
│   └── useVRMModel.js          # VRM 模型加载与切换
├── utils/
│   ├── icons.js                # SVG 图标常量
│   └── timing.js               # throttle(fn, wait) / debounce(fn, wait)（均含 .cancel() 方法）
└── wailsjs/                    # Wails 自动生成的 Go 绑定（禁止手动编辑）
```

## 开发规范

### Go 后端规范

- **Go 版本**：`go 1.26.3`，可用全部 1.26 新特性
- 所有导出函数必须有 `// FuncName ...` doc comment
- 错误处理用 `fmt.Errorf("context: %w", err)` 包装上下文
- **日志**：统一使用 `github.com/rs/zerolog/log`，链式 API：`log.Info().Str("k", v).Err(err).Msg("msg")`；禁止使用 `log/slog`
- **Go 1.26 新特性**：
  - `errors.AsType[T](err)` 替代 `var t T; errors.As(err, &t)`（泛型、类型安全）
  - `new(expr)` 创建表达式值的堆上副本指针（如 `new(s.Field)` 代替 `&s.Field`）
- **性能惯用法**：
  - 遍历大结构体切片用 `for i := range slice` + 索引访问，避免值拷贝
  - 统计 Unicode 字符数用 `utf8.RuneCountInString(s)`，避免 `len([]rune(s))` 分配
  - 预估容量建 map：`make(map[K]V, n)`；多 map 批量复制用 `maps.Copy(dst, src)`
  - 按分隔符迭代字符串用 `strings.SplitSeq`，解析前缀/切割用 `strings.CutPrefix` / `strings.Cut`
  - 启动 goroutine 用 `wg.Go(func() { ... })`（Go 1.25+），替代 `wg.Add(1); go func(){ defer wg.Done() }()`
  - 固定次数循环用 `for range n`，构建 `[]byte` 格式化字符串用 `fmt.Appendf(nil, ...)`
- 涉及 `a.cfg` / `a.petAgent` / `a.longMem` / `a.knowledgeSt` / `a.ttsSpeaker` / `a.mcpClosers` 的字段读写必须持有 `a.mu`（`RLock` 读，`Lock` 写）；`GetConfig` 返回 `*a.cfg` 的值拷贝，不返回原指针
- `sched.Start(a.ctx)` 与 `engine.Start(a.ctx)` 必须在 `a.mu.Unlock()` 之后调用——cron 回调会 `EventsEmit` 触发 Wails cgo，持锁时 emit 可能死锁
- **Wails 绑定方法命名和归属**：新增绑定方法写在对应的 `app_*.go` 文件，签名遵循已有模式
- **异步 Wails 绑定**：涉及网络/IO 的绑定方法（ImportKnowledge、InstallUpdate、initLLMComponents 触发链）必须立即返回，在 goroutine 中执行，通过 `EventsEmit` 报告完成/错误
- 新增内置工具：在 `internal/tools/<category>/` 目录创建，实现 `Tool` 接口，在 `registry.go` 的 `All()` 中注册；运行时依赖工具加到 `AllContextual`；无需再在 `app.go startup` 里硬编码权限行——统一走 `AllPermissionDeclarations()`
- `execute_shell` / `execute_code` 的 `working_dir` 参数必须经 `checkPath(workingDir, t.Cfg.AllowedPaths)` 白名单校验，并在 `t.Cfg == nil` 时直接返回错误提示（避免 nil deref）
- exec 工具中用 `ctx.Err() != nil`（不是 `== context.Canceled`）判断父 context 取消，覆盖 DeadlineExceeded 场景
- **Wails 绑定结构体中禁止使用 `time.Time`**，改用 RFC3339 字符串（`string`）——Wails TS 绑定生成器不识别 `time.Time`

### Vue 前端规范

- 全部使用 `<script setup>` 语法
- 包管理用 `yarn`，不用 npm
- 调用后端方法从 `../../wailsjs/go/main/App` import
- 监听 Wails 事件用 `EventsOn`，emit 用 `EventsEmit`（from `../../wailsjs/runtime/runtime`）
- **`EventsOn(event, handler)` 返回的 off 函数必须存到组件作用域变量**，在 `onUnmounted` 中统一 `off?.()` 解绑；禁止用 `EventsOff('event-name')` 传字符串（它会移除同事件所有监听器，影响其他组件）
- **throttle/debounce 的 handler 引用必须保存**，并在 `onUnmounted` 中调用 `.cancel?.()` 清除 pending timer，防止组件卸载后回调写入已销毁响应式状态
- 拖拽类交互（mousedown → mousemove/mouseup）必须同时监听 `window.blur` 作为兜底解绑路径，防止鼠标拖出窗口后监听器悬空
- composable 里的 `setTimeout` / `setInterval` 必须通过 `onScopeDispose` 清理，避免卸载后回调仍作用于已销毁组件
- 组件内不直接操作全局状态，通过 composables 共享逻辑
- `timing.js` 提供 `throttle(fn, wait)` 和 `debounce(fn, wait)`，返回的函数均带 `.cancel()` 方法

### 核心 Wails 事件

| 事件名 | 方向 | 含义 |
|---|---|---|
| `chat:token` | backend→frontend | 流式 token 传输 |
| `chat:done` | backend→frontend | AI 响应结束 |
| `chat:error` | backend→frontend | 错误信息传递 |
| `chat:emotion` | backend→frontend | AI 返回情绪标签（驱动 Live2D/VRM 表情）|
| `chat:clear` | frontend→frontend | 清空聊天历史 |
| `chat:proactive:message` | backend→frontend | 主动消息推送（聊天框打开时直接推入）|
| `chat:cron:start` | backend→frontend | 定时任务开始（SaveToMemory=true）：推入触发者消息 + 流式助手占位符，开启 loading 状态 |
| `bubble:toggle` | any | 切换聊天气泡显示/隐藏 |
| `pet:state:change` | any | 宠物状态变更 (idle/thinking/speaking/error) |
| `knowledge:progress` | backend→frontend | 知识库导入进度更新 |
| `knowledge:done` | backend→frontend | 知识库导入完成（无 payload）|
| `knowledge:error` | backend→frontend | 知识库导入失败（payload: 错误字符串）|
| `config:model:changed` | frontend→frontend | Live2D/VRM 模型切换通知（也标志 Agent 异步重启完成）|
| `config:chat:size:changed` | frontend→frontend | 聊天框尺寸变更 |
| `notification:show` | backend→frontend | 显示应用内通知气泡 |
| `settings:open` | any | 打开设置界面 |
| `screen:changed` | backend→frontend | 屏幕列表变更（连接/断开显示器）|
| `mcp:ready` | backend→frontend | MCP 工具加载完成，携带工具数量 |
| `voice:start` | backend→frontend | 开始录音（Option 长按触发）|
| `voice:transcript` | backend→frontend | 实时 partial STT 结果 |
| `voice:final` | backend→frontend | isFinal STT 结果（可触发自动发送）|
| `voice:end` | backend→frontend | 录音结束（Option 释放时立即触发）|
| `voice:error` | backend→frontend | 语音识别错误 |
| `tts:start` | backend→frontend | TTS 开始播放 |
| `tts:audio` | backend→frontend | TTS 音频数据（base64 PCM/MP3，供前端播放）|
| `tts:done` | backend→frontend | TTS 播放结束 |
| `tts:error` | backend→frontend | TTS 错误 |
| `sms:verification_code` | backend→frontend | 检测到验证码短信 |
| `config:voice:auto-send:changed` | frontend→frontend | 语音自动发送开关状态变更 |
| `tool:confirm` | backend→frontend | 工具执行需用户确认（shell/code）|
| `tool:executing` | backend→frontend | 工具开始执行（显示进度条）|
| `tool:executed` | backend→frontend | 工具执行结束（隐藏进度条）|
| `update:progress` | backend→frontend | 应用更新下载进度（pct/msg）|
| `update:error` | backend→frontend | 应用更新失败（payload: 错误字符串）|

## 开发命令

```bash
make run               # 构建 + ad-hoc 签名 + 启动（推荐，权限持久化）
make build             # 仅构建 + 签名
wails dev              # 开发模式（前端热重载，权限每次重置）
go build ./...         # 仅检查 Go 编译
cd frontend && yarn build   # 仅构建前端资源
wails generate module  # 重新生成 Wails bindings
```

> **权限说明**：使用 `make run` 构建会自动 ad-hoc 签名（`codesign --sign -`），配合 `wails.json` 中固定的 `bundleidentifier: com.xutiancheng.aiko`，macOS TCC 权限授权后跨重新编译持久有效。`wails dev` 不签名，每次重启可能重新弹权限窗。

## 数据目录结构

`~/.aiko/`
- `pet.db` — SQLite 数据库（settings、messages、knowledge_sources、cron_jobs、model_profiles、tool_permissions、proactive_items）
- `vectors/` — chromem-go 持久化向量数据存储
- `vrm/` — 用户自定义 VRM 模型文件（`.vrm`）
- `USER.md` — 用户画像文档（由 `update_user_profile` 工具自动维护）
- `auto-skills/` — Agent 自动沉淀的可复用技能（YAML 格式，由 `save_skill` 工具写入）

## 重要注意事项

### macOS 平台特定
- `macos.go` 中的 Objective-C 代码负责按像素判断鼠标事件响应，实现点击穿透功能
- **⚠️ 不要随意修改 hitTest 逻辑**，容易破坏点击穿透机制
- hitTest JS 选择器：`.live2d-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.lightbox,.tool-confirm-modal,.execution-progress`——新增可交互的全屏覆盖层时必须加入此列表，否则鼠标事件会穿透到桌面
- `macos.go` 同时包含全局 Option 键监控：双击切换气泡，长按 ≥1s 触发语音录音（`startVoiceRecognition` / `stopVoiceRecognition`）
- 语音识别使用 `AVAudioEngine` + `SFSpeechRecognizer`，partial 结果通过 voice pipe 推送 `voice:transcript` Wails 事件；`isFinal` 结果推送 `FINAL:<text>` → Go goroutine 转为 `voice:final` 事件
- **voice:final vs voice:end**：`voice:end` 在 Option 释放时立即触发（停止 UI 动画）；`voice:final` 在 SFSpeechRecognition 异步完成后触发（携带最终文字）。若 `VoiceAutoSend` 开启，ChatPanel 收到 `voice:final` 后自动调用 `send()`
- **macOS 系统集成首选 osascript**：Wails 的 `[NSApp run]` 占用主线程，任何需要主线程的 CGO API（AXUIElement、NSWorkspace 等）都不安全；改用 `exec.Command("osascript", "-e", ...)` 子进程调用 AppleScript，无线程限制

### 工具系统
- 工具目录结构：`internal/tools/<category>/`，每个 category 是独立子包
- 普通工具实现 `Tool` 接口（`InvokableRun(ctx, string) (string, error)`），在 `All()` 中注册，`AllEino()` 自动用 `ToEino()` 包装
- **有运行时依赖**（知识库、调度器、长期记忆、config 等）的工具在 `AllContextual()` 中注册
- **多模态工具**（返回图片等）实现 `EnhancedTool` 接口，在 `AllEino()` 中用 `ToEinoEnhanced()` 单独注册（**不**放入 `All()`）；同时在 `app.go` 启动时手动调用 `permStore.EnsureRow()` 注册权限行
- **需要 eino interrupt/resume 的工具**（如 execute_shell / execute_code）：调用 `tool.Interrupt(ctx, info)` 触发中断，info 类型必须用 `gob.Register` 提前注册；`ErrorRecovery` 中间件会放行 `compose.IsInterruptRerunError` 的 error
- macOS 专属工具用 `//go:build darwin` / `//go:build !darwin` 分平台实现（non-darwin 提供 stub）
- **新工具需在 `app.go` 启动时调用 `permStore.EnsureRow()`**，否则不会出现在工具权限列表

### eino interrupt/resume 流程
- 工具调用 `tool.Interrupt(ctx, ShellConfirmInfo{...})` → eino 保存 checkpoint，返回 interrupt error
- `ErrorRecovery` 中间件检测到 `compose.IsInterruptRerunError` 后原样透传（不转字符串）
- `drainIter` 检测 `event.Action.Interrupted != nil` → 调用 `handleInterrupt`
- `handleInterrupt` 从 `InterruptContexts` 找 `IsRootCause=true` 的 ctx，读取 `ctx.Info`（类型断言为 `ShellConfirmInfo` / `CodeConfirmInfo`），emit `tool:confirm` 事件给前端
- 前端确认后调用 `ConfirmToolExecution(id, approved, editedContent)` → 写入 `pendingConfirms` sync.Map
- `handleInterrupt` 收到响应，调用 `runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{Targets: map[string]any{ictx.ID: ConfirmResult{...}}})`
- 工具被 resume，`tool.GetResumeContext[ConfirmResult](ctx)` 返回确认结果，执行实际命令

### 多模态对话
- `app_chat.go` 的 `SendMessageWithImages(userInput string, images []string)` 接收前端传来的 data URL 数组，解析后构造含图片 part 的 `*schema.Message`，调用 `agent.ChatWithMessage()`
- `SendMessageWithFiles(userInput string, images []string, files []FileAttachment)` 同时支持图片和文本文件附件（`FileAttachment{Name, Content string}`），文本文件内联为消息文本块
- eino acl/openai 会将 `Base64Data` 序列化为 `data:<mime>;base64,<data>` 格式的 image_url，与 OpenAI 多模态 API 规范一致

### 异步绑定模式
以下绑定方法均立即返回，通过 Wails 事件报告结果（**不**阻塞 JS 调用线程）：

| 方法 | 完成事件 | 错误事件 |
|---|---|---|
| `ImportKnowledge` | `knowledge:done` | `knowledge:error` |
| `InstallUpdate` | app 自动重启 | `update:error` |
| `SaveConfig` / `SaveModelProfile` / `ActivateModelProfile` | `config:model:changed` | `notification:show`（错误消息）|
| `Add/Update/DeleteMCPServer` | `mcp:ready` | — |

### 图片预览灯箱
- `ChatPanel.vue` 中用 `<Teleport to="body">` 将灯箱挂到 `document.body`，使 `position: fixed` 覆盖整个 viewport 而不受父级限制
- 灯箱 CSS class `.lightbox` 已加入 `macos.go` hitTest 选择器，鼠标悬停时窗口不会穿透

### 聊天框全屏模式
- `ChatBubble.vue` 标题栏有全屏切换按钮（`isFullscreen` ref）
- 全屏时 `.chat-bubble.fullscreen` CSS 覆盖位置/尺寸：`top: 38px`（避让 macOS 菜单栏），`width/height: 100vw/calc(100vh-38px)`
- `.chat-bubble` 已在 hitTest 选择器中，全屏状态下鼠标不会穿透

### VRM 模型
- 内置 VRM 模型 embed 在 `frontend/dist/vrm/`，通过 `assets.ReadDir` 枚举
- 用户自定义 VRM 存放在 `~/.aiko/vrm/`，通过 `ImportVRMFile(name, base64Data)` 上传
- `VRMPet.vue` 负责 3D VRM 渲染，通过 `/user-vrm/<name>` 路径访问用户模型
- 切换 Live2D/VRM 模型后 emit `config:model:changed` 事件触发前端重载

### 主动消息 (Proactive Engine)
- `internal/proactive/engine.go` 每分钟 poll SQLite `proactive_items` 表，触发到期项
- 若聊天框打开：emit `chat:proactive:message` 直接推送到聊天面板
- 若聊天框关闭：emit `notification:show` 显示应用内气泡，同时调用 `notify.System()` 发送 macOS 系统通知
- 过期超过 5 分钟（`fireDeadline`）的项静默丢弃，避免用户离开后积压的提醒轰炸
- Agent 通过 `schedule_followup` 工具写入 `proactive_items` 表安排后续提醒

### 应用更新
- `CheckUpdate()` 请求 GitHub Releases API 判断是否有新版本
- `InstallUpdate(downloadURL)` 立即返回，goroutine 中下载 dmg/zip，emit `update:progress` 报告进度（0–100），失败时 emit `update:error`，成功后替换二进制并重启

### 屏幕感知
- `startScreenWatcher()` 定期轮询屏幕列表，发现变更时 emit `screen:changed` 事件
- `GetScreenList()` 返回所有连接显示器的分辨率和位置信息，前端据此重新定位宠物和聊天框
- 前端的 `screen:active:changed` 处理器必须用 debounce(200ms) 包装，并在 `onUnmounted` 中调用 `.cancel()`

### MCP 热重载
- `app_mcp.go` 的 `AddMCPServer`、`UpdateMCPServer`、`DeleteMCPServer` 在 DB 操作完成后立即返回，goroutine 中调用 `initLLMComponents` 重建 Agent，重建完成后 emit `mcp:ready`

### 自我成长系统
- `NudgeInterval`（配置项）控制 Agent 每隔 N 轮对话后自动触发沉淀提示
- 相关工具：`save_memory`、`update_user_profile`、`save_skill`——在 `AllContextual()` 中注册

### 并发安全
- `initLLMComponents` 可能并发调用（SaveConfig / MCP 变更等异步触发）
- 所有涉及 `a.petAgent`、`a.longMem`、`a.knowledgeSt`、`a.cfg`、`a.ttsSpeaker`、`a.mcpClosers` 字段的更新必须在 `a.mu.Lock()` 保护下完成
- `chatCancel` / `ttsCancel` 在 shutdown 时由 `app.go` 持锁取消，确保 goroutine 正确退出

### Wails 绑定
- 前端 Wails bindings（`wailsjs/`）由 `wails dev/build` 自动生成，**不要手动编辑**
- 修改 Go 方法签名后需要重新运行 `wails generate module`

## MCP Tools: code-review-graph

**IMPORTANT: This project has a knowledge graph. ALWAYS use the
code-review-graph MCP tools BEFORE using Grep/Glob/Read to explore
the codebase.** The graph is faster, cheaper (fewer tokens), and gives
you structural context (callers, dependents, test coverage) that file
scanning cannot.

### When to use graph tools FIRST

- **Exploring code**: `semantic_search_nodes` or `query_graph` instead of Grep
- **Understanding impact**: `get_impact_radius` instead of manually tracing imports
- **Code review**: `detect_changes` + `get_review_context` instead of reading entire files
- **Finding relationships**: `query_graph` with callers_of/callees_of/imports_of/tests_for
- **Architecture questions**: `get_architecture_overview` + `list_communities`

Fall back to Grep/Glob/Read **only** when the graph doesn't cover what you need.

### Key Tools

| Tool | Use when |
|------|----------|
| `detect_changes` | Reviewing code changes — gives risk-scored analysis |
| `get_review_context` | Need source snippets for review — token-efficient |
| `get_impact_radius` | Understanding blast radius of a change |
| `get_affected_flows` | Finding which execution paths are impacted |
| `query_graph` | Tracing callers, callees, imports, tests, dependencies |
| `semantic_search_nodes` | Finding functions/classes by name or keyword |
| `get_architecture_overview` | Understanding high-level codebase structure |
| `refactor_tool` | Planning renames, finding dead code |

### Workflow

1. The graph auto-updates on file changes (via hooks).
2. Use `detect_changes` for code review.
3. Use `get_affected_flows` to understand impact.
4. Use `query_graph` pattern="tests_for" to check coverage.
