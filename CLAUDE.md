# Aiko — CLAUDE.md

Claude Code 指引文件，用于在此项目中提供上下文感知的编码协助。

## 项目概览

**Aiko** 是一个 macOS 原生 AI 桌面宠物应用，基于 Wails v2 构建。Go 后端通过 `app_*.go` 文件暴露绑定方法给 Vue 3 前端；前端资源 embed 进 Go 二进制。核心特性包括点击穿透（Objective-C cgo）、eino ReAct Agent 智能对话、语音输入/输出，以及丰富的 macOS 系统集成。

> **平台支持**：仅支持 macOS，短期内不计划支持 Windows 或 Linux。依赖的 macOS 专属 API（Objective-C CGO、AVAudioEngine、SFSpeechRecognizer、osascript 等）是核心功能的基础。

### 核心依赖

**后端：**
- [Wails v2](https://wails.io/) — 桌面应用框架
- [eino](https://github.com/cloudwego/eino) — ReAct Agent 框架（ADK、deep agent、流式处理）
- [chromem-go](https://github.com/philippgille/chromem-go) — 纯 Go 向量数据库（长期记忆 + 知识库）
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — 纯 Go SQLite 驱动
- [mcp-go](https://github.com/mark3labs/mcp-go) — MCP 协议客户端
- [robfig/cron/v3](https://pkg.go.dev/github.com/robfig/cron/v3) — Cron 调度
- [zerolog](https://github.com/rs/zerolog) — 结构化日志
- [tiktoken-go](https://github.com/pkoukk/tiktoken-go) — token 计数

**前端：**
- [Vue 3](https://vuejs.org/) + Composition API + [Vite](https://vitejs.dev/)
- [marked](https://marked.js.org/) + [Shiki](https://shiki.style/) + [KaTeX](https://katex.org/) — Markdown / 代码高亮 / 数学公式
- [Milkdown](https://milkdown.dev/) (Crepe) — WYSIWYG Markdown 编辑器
- [vue-i18n](https://vue-i18n.intlify.dev/) — 国际化（zh-CN / en / ja / ko）
- Live2D Cubism SDK + pixi-live2d-display — 2D 角色
- Three.js + @pixiv/three-vrm — 3D VRM 角色

**AI 生态：**
- OpenAI API 兼容接口（OpenRouter、DeepSeek、通义千问等）
- MCP (Model Context Protocol) — 工具协议标准，支持 stdio / SSE / HTTP 传输
- lark-cli — 飞书命令行工具集成

## 架构设计

### Wails 绑定层（根目录）

绑定方法按功能域拆分到多个文件，**禁止在 `app_*.go` 之外添加绑定方法**：

```
main.go             # 应用入口
app.go              # startup/shutdown、initLLMComponents、通用工具方法
app_chat.go         # 聊天：SendMessage、SendMessageWithImages/Files、Stop、History、
                    #   Regenerate、ChatDirect、ChatDirectCollect、ExportChatHistory、SearchMessages
app_config.go       # 配置：GetConfig、SaveConfig、ModelProfile CRUD、ListLLMModels、
                    #   ListOpenRouterModels、SetAvatar/ResetAvatar
app_cron.go         # 定时任务 + 主动提醒：CRUD、手动触发、List/DeleteProactiveItem
app_knowledge.go    # 知识库：ImportKnowledge（异步）、ListKnowledgeSources、DeleteKnowledgeSource
app_layout.go       # 布局：屏幕列表、宠物/聊天框/悬浮球位置与尺寸、鼠标位置
app_mcp.go          # MCP：ListMCPServers、Add/Update/DeleteMCPServer（均异步热重载）
app_pomodoro.go     # 番茄钟：Start/Pause/Resume/StopPomodoro、GetPomodoroStatus
app_system.go       # 系统：权限、Lark、SMS、CheckUpdate、InstallUpdate（异步）、
                    #   FetchLinkPreview、GetSystemStats、GetTopProcesses、键窗口管理
app_tts.go          # TTS：SpeakText、StopTTS、GetTTSStatus、SetupKokoroTTS（后台安装）
app_voice.go        # 语音 + SMS：VoiceAutoSend、SoundsEnabled、SMS 监听启停
app_vrm.go          # VRM 模型：ListVRMModels、ImportVRMFile、DeleteVRMFile
macos.go            # macOS 专属：点击穿透 hitTest、全局 Option 键监控、
                    #   AVAudioEngine STT、系统通知 Sender 注册
```

### 后端内部包

```
internal/
├── agent/             # eino ReAct Agent
│   ├── agent.go       # 初始化、工具装配、自我成长 nudge
│   ├── chat.go        # 流式对话、chatCancel 管理
│   ├── drain.go       # 事件流处理、interrupt/resume 调度
│   ├── context.go     # 对话上下文拼装（摘要、记忆检索、知识库查询）
│   ├── emotion.go     # 情绪标签提取（[情绪:name/intensity]）
│   ├── summary.go     # LLM 滚动摘要生成
│   └── middleware/    # 中间件链：日志、重试、ErrorRecovery
├── tools/             # 内置工具系统（19 个子包，50+ 工具）
│   ├── appctl/        # list_running_apps / control_app
│   ├── browser/       # get_browser_url（osascript）
│   ├── calendar/      # get_calendar_events / create_calendar_event
│   ├── clipboard/     # read_clipboard / write_clipboard
│   ├── context/       # get_context（系统上下文注入）
│   ├── cron/          # list/add/update/delete_cron_job
│   ├── dev/           # JSON 格式化、编码/进制转换、UUID 等开发者工具
│   ├── exec/          # execute_shell / execute_code（eino interrupt/resume）
│   ├── fs/            # list_directory / read_file / write_file / delete_file 等
│   ├── growth/        # search_memory / update_user_profile / list_skills / save_skill
│   ├── image/         # image_resize / image_convert 等图片处理
│   ├── location/      # get_location（CoreLocation，macOS）
│   ├── mail/          # get_mail_list / read_mail（osascript Mail.app）
│   ├── reminders/     # get_reminders / complete_reminder（osascript）
│   ├── screenshot/    # take_screenshot（增强工具，返回压缩 JPEG base64）
│   ├── system/        # get_system_info / get_network_status 等
│   ├── timeutil/      # get_current_time / get_timezone 等
│   ├── weather/       # get_weather
│   ├── web/           # fetch_webpage / search_web 等
│   ├── registry.go    # All() / AllContextual() / AllEino() / AllPermissionDeclarations()
│   ├── tool.go        # Tool / EnhancedTool 接口定义
│   └── permission.go  # PermissionStore（SQLite 持久化工具开关）
├── bytesconv/         # 零分配 string ↔ []byte 转换
├── config/            # 配置 + ModelProfile 持久化（SQLite）
├── db/                # Schema 迁移管理
├── execenv/           # Python/Node/Ruby/Bash 执行环境（增强 PATH）
├── knowledge/         # RAG 知识库（chromem-go 向量 + SQLite 元数据）
├── lark/              # 飞书 lark-cli 客户端封装
├── llm/               # ChatModel / Embedder 工厂 + reasoning effort 映射
├── mcp/               # MCP 协议客户端（stdio + SSE + HTTP）
├── memory/            # 短期记忆(SQLite messages) + 长期记忆(chromem-go) + 滚动摘要
├── notify/            # macOS UserNotifications 系统通知
├── pomodoro/          # 番茄钟状态机
├── proactive/         # 主动消息引擎（proactive_items 轮询 + followup 工具）
├── scheduler/         # Cron 任务调度（robfig/cron 封装，基于轮询不依赖定时器）
├── skill/             # Agent Hub + 技能中间件（auto-skills/ SKILL.md 加载）
├── sms/               # 短信监听（fsnotify chat.db + typedstream 解析 + 验证码正则）
└── tts/               # TTS 后端（OpenAI / Kokoro ONNX / macOS AVSpeechSynthesizer）
```

### 前端架构

```
frontend/src/
├── components/
│   ├── App.vue                  # 根组件，全局状态编排，Apple Intelligence 风格语音动画
│   ├── ChatBubble.vue           # 可拖拽/调整大小聊天气泡容器（多 resize 手柄、全屏模式）
│   ├── ChatPanel.vue            # 聊天面板（消息列表、输入框、图片/文件粘贴、全文搜索）
│   ├── Live2DPet.vue            # Live2D 宠物渲染（pixi-live2d-display）
│   ├── VRMPet.vue               # VRM 3D 宠物渲染（Three.js + @pixiv/three-vrm）
│   ├── FloatingBall.vue         # 悬浮球（折叠态快捷入口，可拖拽）
│   ├── SettingsWindow.vue       # 设置窗口（13 个标签页，搜索导航）
│   ├── NotificationBubble.vue   # 应用内通知气泡
│   ├── ToolConfirmModal.vue     # 工具执行确认弹窗（shell/code/update）
│   ├── ExecutionProgress.vue    # 工具执行进度条
│   ├── ContextMenu.vue          # 右键上下文菜单
│   ├── LinkPreview.vue          # 链接预览卡片
│   ├── CodeRain.vue             # 代码雨背景动画（ChatBubble 内）
│   ├── PomodoroPanel.vue        # 番茄钟面板（圆形进度环）
│   └── SystemResourcePanel.vue  # 系统资源监控面板（CPU/内存/磁盘/网络）
├── composables/
│   ├── useConfirm.js            # 通用确认弹窗（Promise 风格）
│   ├── useEmotionEvents.js      # 情绪事件绑定
│   ├── useEscapeKey.js          # ESC 键关闭（支持条件激活）
│   ├── useMarkdown.js           # Markdown 渲染引擎（Shiki + KaTeX + 自定义表格）
│   ├── useModelPath.js          # Live2D 模型路径解析
│   ├── usePetRenderer.js        # 宠物渲染器类型判断
│   ├── usePetState.js           # 宠物状态机（idle/thinking/speaking/listening/error）
│   ├── useSounds.js             # Web Audio API 音效播放
│   ├── useSpring.js             # 弹簧动画引擎
│   ├── useTypingScheduler.js    # 打字机效果调度
│   └── useVRMModel.js           # VRM 模型加载与切换
├── locales/
│   ├── index.js                 # i18n 初始化
│   ├── en.json                  # 英文翻译
│   ├── zh-CN.json               # 简体中文翻译
│   ├── ja.json                  # 日文翻译
│   └── ko.json                  # 韩文翻译
├── styles/
│   ├── tokens.css               # 设计变量
│   └── style.css                # 全局样式
├── utils/
│   ├── icons.js                 # SVG 图标常量
│   └── timing.js                # throttle(fn, wait) / debounce(fn, wait)（均含 .cancel()）
└── wailsjs/                     # Wails 自动生成的 Go 绑定（禁止手动编辑）
```

> 注意：本项目不使用 Vue Router、Pinia、axios。所有状态通过 composables + Wails 事件系统管理，所有后端通信通过 Wails IPC。

## 开发规范

### Go 后端

- **Go 版本**：`go 1.26.3`
- 所有导出函数必须有 `// FuncName ...` doc comment
- 错误处理用 `fmt.Errorf("context: %w", err)` 包装
- **日志**：统一使用 `github.com/rs/zerolog/log`，链式 API；禁止使用 `log/slog`
- **Go 1.26 惯用法**：`errors.AsType[T](err)`、`new(expr)`、`for range n`、`strings.SplitSeq`、`strings.CutPrefix`/`strings.Cut`、`maps.Copy`、`fmt.Appendf`
- **并发**：启动 goroutine 用 `wg.Go(func() { ... })`；遍历大结构体切片用 `for i := range slice`
- 涉及 `a.cfg` / `a.petAgent` / `a.longMem` / `a.knowledgeSt` / `a.ttsSpeaker` / `a.mcpClosers` 的字段读写必须持有 `a.mu`（`RLock` 读，`Lock` 写）；`GetConfig` 返回值拷贝
- `sched.Start(a.ctx)` 与 `engine.Start(a.ctx)` 必须在 `a.mu.Unlock()` 之后调用——cron 回调会 `EventsEmit` 触发 Wails cgo，持锁时 emit 可能死锁
- **异步 Wails 绑定**：涉及网络/IO 的方法必须立即返回，goroutine 中执行，通过 `EventsEmit` 报告结果
- **Wails 绑定结构体中禁止使用 `time.Time`**，改用 RFC3339 字符串——Wails TS 绑定生成器不识别 `time.Time`
- 新增内置工具：在 `internal/tools/<category>/` 创建，实现 `Tool` 接口，在 `All()` 中注册；运行时依赖工具加到 `AllContextual()`；权限声明统一走 `AllPermissionDeclarations()`
- `execute_shell` / `execute_code` 的 `working_dir` 必须经 `checkPath(workingDir, t.Cfg.AllowedPaths)` 白名单校验；`t.Cfg == nil` 时直接返回错误
- exec 工具中用 `ctx.Err() != nil`（不是 `== context.Canceled`）判断父 context 取消
- macOS 专属工具用 `//go:build darwin` / `//go:build !darwin` 分平台实现

### Vue 前端

- 全部使用 `<script setup>` 语法；包管理用 `yarn`
- 调用后端从 `../../wailsjs/go/main/App` import
- 监听 Wails 事件用 `EventsOn`，emit 用 `EventsEmit`（from `../../wailsjs/runtime/runtime`）
- **`EventsOn(event, handler)` 返回的 off 函数必须存到组件作用域变量**，在 `onUnmounted` 中 `off?.()` 解绑；禁止用 `EventsOff('event-name')` 传字符串
- **throttle/debounce 的 handler 引用必须保存**，`onUnmounted` 中调用 `.cancel?.()` 清除 pending timer
- 拖拽类交互必须同时监听 `window.blur` 作为兜底解绑路径
- composable 里的 `setTimeout` / `setInterval` 必须通过 `onScopeDispose` 清理
- 组件通过 composables 共享逻辑，不直接操作全局状态
- 本项目不使用 Pinia/Vue Router/axios——状态通过 composables + Wails 事件管理

### 核心 Wails 事件

| 事件名 | 方向 | 含义 |
|---|---|---|
| `chat:token` | backend→frontend | 流式 token 传输 |
| `chat:done` | backend→frontend | AI 响应结束 |
| `chat:error` | backend→frontend | 错误信息传递 |
| `chat:emotion` | backend→frontend | 情绪标签（驱动 Live2D/VRM 表情）|
| `chat:clear` | frontend→frontend | 清空聊天历史 |
| `chat:proactive:message` | backend→frontend | 主动消息推入聊天 |
| `chat:cron:start` | backend→frontend | 定时任务触发（SaveToMemory=true 时推入对话）|
| `bubble:toggle` | any | 切换聊天气泡显示/隐藏 |
| `pet:state:change` | any | 宠物状态变更（idle/thinking/speaking/listening/error）|
| `knowledge:progress` | backend→frontend | 知识库导入进度 |
| `knowledge:done` | backend→frontend | 知识库导入完成 |
| `knowledge:error` | backend→frontend | 知识库导入失败 |
| `config:model:changed` | frontend→frontend | Live2D/VRM 模型切换（也标志 Agent 异步重启完成）|
| `config:chat:size:changed` | frontend→frontend | 聊天框尺寸变更 |
| `config:voice:auto-send:changed` | frontend→frontend | 语音自动发送开关变更 |
| `notification:show` | backend→frontend | 显示应用内通知气泡 |
| `settings:open` | any | 打开设置 |
| `screen:changed` | backend→frontend | 屏幕列表变更 |
| `mcp:ready` | backend→frontend | MCP 工具加载完成 |
| `voice:start` | backend→frontend | 开始录音（Option 长按触发）|
| `voice:transcript` | backend→frontend | 实时 partial STT 结果 |
| `voice:final` | backend→frontend | isFinal STT 结果（可触发自动发送）|
| `voice:end` | backend→frontend | 录音结束（Option 释放时立即触发）|
| `voice:error` | backend→frontend | 语音识别错误 |
| `tts:start` | backend→frontend | TTS 开始播放 |
| `tts:audio` | backend→frontend | TTS 音频数据（base64）|
| `tts:done` | backend→frontend | TTS 播放结束 |
| `tts:error` | backend→frontend | TTS 错误 |
| `sms:verification_code` | backend→frontend | 检测到验证码 |
| `tool:confirm` | backend→frontend | 工具执行需用户确认 |
| `tool:executing` | backend→frontend | 工具开始执行 |
| `tool:executed` | backend→frontend | 工具执行结束 |
| `update:progress` | backend→frontend | 更新下载进度 |
| `update:error` | backend→frontend | 更新失败 |
| `pomodoro:tick` | backend→frontend | 番茄钟倒计时 |
| `pomodoro:state:changed` | backend→frontend | 番茄钟状态变更（running/paused/stopped）|
| `pomodoro:phase:changed` | backend→frontend | 番茄钟阶段变更（focus/break）|
| `stats:update` | backend→frontend | 系统资源统计更新 |

## 开发命令

```bash
make run               # 构建 + ad-hoc 签名 + 启动（推荐，权限持久化）
make build             # 仅构建 + 签名
wails dev              # 开发模式（前端热重载，权限每次重置）
go build ./...         # 仅检查 Go 编译
cd frontend && yarn build   # 仅构建前端资源
wails generate module  # 重新生成 Wails bindings
```

> **权限说明**：`make run` 会执行 ad-hoc 签名（`codesign --sign -`），配合 `wails.json` 中固定的 `bundleidentifier: com.xutiancheng.aiko`，macOS TCC 权限授权后跨重新编译持久有效。`wails dev` 不签名，每次重启可能重新弹权限窗。

## 数据目录结构

`~/.aiko/`
- `pet.db` — SQLite（settings、messages、knowledge_sources、cron_jobs、model_profiles、tool_permissions、proactive_items、summaries）
- `vectors/` — chromem-go 向量数据
- `vrm/` — 用户自定义 VRM 模型（`.vrm`）
- `USER.md` — 用户画像文档（`update_user_profile` 工具维护）
- `auto-skills/` — Agent 自动沉淀的技能（`SKILL.md` YAML + Markdown）
- `tts-venv/` — Kokoro TTS Python 虚拟环境（可选）

## 重要注意事项

### macOS 平台特定
- `macos.go` 中的 Objective-C 代码负责按像素判断鼠标事件响应，实现点击穿透
- **⚠️ 不要随意修改 hitTest 逻辑**，容易破坏点击穿透机制
- hitTest JS 选择器：`.live2d-pet,.vrm-pet,.chat-bubble,.pomodoro-panel,.system-panel,.settings-win,.ctx-menu,.notif-bubble,.execution-progress,.resize-handle,.lightbox-fullscreen`——新增可交互的全屏覆盖层时必须加入此列表
- VRM 宠物和 Live2D 宠物通过 WebGL alpha 检测实现像素级点击穿透（透明区域穿透，不透明区域响应）
- `macos.go` 同时包含全局 Option 键监控：双击切换气泡，长按 ≥1s 触发语音录音
- 语音识别使用 `AVAudioEngine` + `SFSpeechRecognizer`，partial 结果推送 `voice:transcript`，`isFinal` 结果推送 `voice:final`
- **voice:final vs voice:end**：`voice:end` 在 Option 释放时立即触发（停止 UI 动画）；`voice:final` 在识别异步完成后触发（携带最终文字）。若 `VoiceAutoSend` 开启，ChatPanel 收到 `voice:final` 后自动发送
- **macOS 系统集成首选 osascript**：Wails 的 `[NSApp run]` 占用主线程，任何需要主线程的 CGO API（AXUIElement、NSWorkspace 等）都不安全；改用 `exec.Command("osascript", "-e", ...)` 子进程调用 AppleScript
- 2px 光标移动阈值避免 JavaScript 引擎饱和；启动时 2 秒延迟 `setIgnoresMouseEvents:YES` 避免 macOS 26 (Tahoe) 上的 `_NSTrackingAreaAKManager` 竞态导致 SIGABRT

### 工具系统
- 工具目录：`internal/tools/<category>/`，每个 category 是独立子包
- 普通工具实现 `Tool` 接口（`InvokableRun(ctx, string) (string, error)`），在 `All()` 中注册，`AllEino()` 自动用 `ToEino()` 包装
- **有运行时依赖**的工具在 `AllContextual()` 中注册
- **增强工具**（返回图片等）：实现 `EnhancedTool` 接口，在 `AllEino()` 中用 `ToEinoEnhanced()` 单独注册（**不**放入 `All()`）；同时在 `app.go` 启动时手动调用 `permStore.EnsureRow()` 注册权限行
- **需要 interrupt/resume 的工具**：调用 `tool.Interrupt(ctx, info)` 触发中断，info 类型必须用 `gob.Register` 提前注册；`ErrorRecovery` 中间件会放行 `compose.IsInterruptRerunError`
- 新工具需在 `app.go` 启动时调用 `permStore.EnsureRow()`，否则不出现在权限列表
- 可信 shell 命令（匹配 `ShellTrustedCommands` 前缀列表）跳过确认弹窗直接执行

### eino interrupt/resume 流程
1. 工具调用 `tool.Interrupt(ctx, info)` → eino 保存 checkpoint，返回 interrupt error
2. `ErrorRecovery` 中间件检测到 `compose.IsInterruptRerunError` → 原样透传
3. `drainIter` 检测 `event.Action.Interrupted != nil` → `handleInterrupt`
4. `handleInterrupt` 从 `InterruptContexts` 找 `IsRootCause=true` 的 ctx，emit `tool:confirm` 事件
5. 前端确认后调用 `ConfirmToolExecution(id, approved, editedContent)` → 写入 `pendingConfirms` sync.Map
6. `handleInterrupt` 收到响应，`runner.ResumeWithParams` 恢复执行
7. 工具通过 `tool.GetResumeContext[ConfirmResult](ctx)` 获取确认结果

### 多模态对话
- `SendMessageWithImages` 接收前端传来的 data URL 数组，构造含图片 part 的 `*schema.Message`
- `SendMessageWithFiles` 同时支持图片和文本文件附件（`FileAttachment{Name, Content string}`）
- eino acl/openai 将 `Base64Data` 序列化为 `data:<mime>;base64,<data>` 格式
- 截图工具通过 `sips` 压缩为 JPEG（max 1280px, quality 75），大幅减少 base64 payload

### 异步绑定模式

| 方法 | 完成事件 | 错误事件 |
|---|---|---|
| `ImportKnowledge` | `knowledge:done` | `knowledge:error` |
| `InstallUpdate` | app 自动重启 | `update:error` |
| `SaveConfig` / `SaveModelProfile` / `ActivateModelProfile` | `config:model:changed` | `notification:show`（错误消息）|
| `Add/Update/DeleteMCPServer` | `mcp:ready` | — |

### 聊天框全屏与调整大小
- `ChatBubble.vue` 标题栏有全屏切换按钮
- 全屏时 CSS 覆盖：`top: 38px`（避让 macOS 菜单栏），`width/height: 100vw/calc(100vh-38px)`
- 四个 resize 手柄（east/west/south/north），3 分钟空闲自动关闭（流式输出期间暂停计时）
- `.chat-bubble` + `.resize-handle` 已在 hitTest 选择器中

### 图片预览灯箱
- `ChatPanel.vue` 中用 `<Teleport to="body">` 挂载到 `document.body`
- CSS class `.lightbox-fullscreen` 已加入 hitTest 选择器

### VRM 模型
- 内置 VRM 模型 embed 在 `frontend/dist/vrm/`
- 用户自定义 VRM 存放在 `~/.aiko/vrm/`，通过 `ImportVRMFile(name, base64Data)` 上传
- `VRMPet.vue` 支持 3D 渲染、表情系统、VRMA 动画、头部 IK 跟踪、空闲动作变化、眨眼等
- 切换 Live2D/VRM 后 emit `config:model:changed` 触发前端重载

### 主动消息 (Proactive Engine)
- 每分钟轮询 `proactive_items` 表，触发到期项
- 聊天框打开：emit `chat:proactive:message` 直接推入对话
- 聊天框关闭：emit `notification:show` + macOS 系统通知
- 过期超过 5 分钟的项静默丢弃
- Agent 通过 `schedule_followup` 工具安排后续提醒

### 番茄钟 (Pomodoro)
- Go 后端状态机（focus → short break → long break 循环）
- 前端 `PomodoroPanel.vue` 显示圆形进度环和阶段指示器
- 所有逻辑在后端，前端仅反射状态

### 系统资源监控
- `startStatsTicker` 定期收集 CPU/内存/磁盘/网络数据
- `GetTopProcesses` 返回 top CPU/内存进程
- 前端 `SystemResourcePanel.vue` 实时展示，支持展开详情

### 全文搜索
- SQLite FTS5 全文索引用于消息搜索
- `SearchMessages(query)` 返回匹配消息列表
- ChatPanel 支持键盘导航跳转到消息

### 对话摘要
- `SummaryStore` 在 SQLite 中持久化单行滚动摘要
- Agent 在上下文拼装时自动管理摘要生命周期
- 摘要与新消息合并构造精简的对话上下文

### 技能系统
- `internal/skill/` 扫描 `auto-skills/` 和配置的 `SkillsDirs` 目录
- 每个技能是子目录下的 `SKILL.md` 文件（YAML frontmatter + Markdown 内容）
- Agent Hub 支持 forked 子 agent 执行技能（上下文隔离或共享）
- `save_skill` 工具支持 Agent 自动沉淀可复用技能

### Kokoro TTS
- 本地离线 TTS，基于 Kokoro-82M ONNX 模型 + misaki 中文 G2P
- 通过 embed 的 Python 脚本调用，需要 Python venv
- `SetupKokoroTTS()` 在后台自动创建 venv、安装依赖、下载模型
- 初始化失败时优雅降级为系统 `say` 命令

### 应用更新
- `CheckUpdate()` 请求 GitHub Releases API
- `InstallUpdate(downloadURL)` 立即返回，goroutine 中下载 DMG、挂载、替换二进制、重签名、重启

### 屏幕感知
- `startScreenWatcher()` 定期轮询屏幕列表
- 鼠标移动时自动检测跨屏并迁移窗口
- 前端的屏幕变更处理器必须用 debounce(200ms) 包装，`onUnmounted` 中 `.cancel()`

### MCP 热重载
- `AddMCPServer`、`UpdateMCPServer`、`DeleteMCPServer` 在 DB 操作完成后立即返回，goroutine 中重建 Agent，完成后 emit `mcp:ready`
- 支持 stdio（子进程）、SSE、HTTP 三种传输方式

### 自我成长系统
- `NudgeInterval` 控制 Agent 每隔 N 轮对话自动触发沉淀提示
- 相关工具：`search_memory`、`update_user_profile`、`list_skills`、`save_skill`

### 并发安全
- `initLLMComponents` 可能并发调用（SaveConfig / MCP 变更等异步触发）
- 所有涉及 `a.petAgent`、`a.longMem`、`a.knowledgeSt`、`a.cfg`、`a.ttsSpeaker`、`a.mcpClosers` 的更新必须在 `a.mu.Lock()` 保护下完成
- `chatCancel` / `ttsCancel` 在 shutdown 时持锁取消

### Wails 绑定
- 前端 Wails bindings（`wailsjs/`）由 `wails dev/build` 自动生成，**不要手动编辑**
- 修改 Go 方法签名后需重新运行 `wails generate module`

### 国际化
- 支持 zh-CN / en / ja / ko 四种语言，通过 `vue-i18n` 实现
- 语言配置存储在 `Language` 字段，首次启动根据系统 locale 自动检测
- 所有 UI 文案在 `locales/*.json` 中维护
