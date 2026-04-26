# Desktop Pet — CLAUDE.md

AI 编码助手指引，供 Claude Code 在此项目中使用。

## 项目概览

**Aiko** 是一个 macOS 原生 AI 桌面宠物应用，采用 Wails v2 架构。Go 后端通过 `app.go` 暴露绑定方法给 Vue 3 前端；前端资源 embed 进 Go 二进制。核心特色是点击穿透（通过 `macos.go` 的 Objective-C cgo 实现）和基于 eino ReAct Agent 的智能对话系统。

### 核心依赖

**后端技术栈：**
- [Wails v2](https://wails.io/) - 跨平台桌面应用框架
- [eino](https://github.com/cloudwego/eino) - 字节跳动 Agent Development Kit  
- [chromem-go](https://github.com/philippgille/chromem-go) - 纯 Go 向量数据库
- [robfig/cron/v3](https://pkg.go.dev/github.com/robfig/cron/v3) - Cron 任务调度器
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - 纯 Go SQLite 驱动

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

```
main.go → app.go (Wails bindings)
              ↓
    internal/agent/agent.go        # eino ReAct Agent 核心
    internal/agent/middleware/     # 日志/重试/错误恢复中间件
    internal/tools/                # 内置工具 + 权限管理
    internal/skill/                # YAML 自定义技能
    internal/memory/               # 短期(SQLite) + 长期(chromem-go) 记忆
    internal/knowledge/            # RAG 知识库
    internal/llm/                  # ChatModel / Embedder 抽象层
    internal/config/               # 配置持久化(SQLite)
    internal/scheduler/            # Cron 定时任务
    internal/mcp/                  # MCP 协议实现
    internal/sms/                  # 短信监听（fsnotify + 验证码识别）
    internal/db/                   # Schema 迁移管理
```

**前端架构：**
```
frontend/src/
├── components/        # Vue 组件 (ChatPanel, SettingsWindow, etc.)
├── composables/       # 可复用逻辑 (useModelPath.js)
└── wailsjs/          # Wails 自动生成的 Go 绑定
```

## 开发规范

### Go 后端规范

- 所有导出函数必须有 `// FuncName ...` doc comment
- 错误处理用 `fmt.Errorf("context: %w", err)` 包装上下文
- 涉及 `a.petAgent` / `a.longMem` / `a.knowledgeSt` 的字段读写必须持有 `a.mu`（`RLock` 读，`Lock` 写）
- 新增 Wails 绑定方法写在 `app.go`，签名遵循已有模式
- 新增内置工具：实现 `internaltools.Tool` 接口，在 `registry.go` 的 `All()` 中注册

### Vue 前端规范

- 全部使用 `<script setup>` 语法
- 包管理用 `yarn`，不用 npm
- 调用后端方法从 `../../wailsjs/go/main/App` import
- 监听 Wails 事件用 `EventsOn`，emit 用 `EventsEmit`（from `../../wailsjs/runtime/runtime`）
- 组件内不直接操作全局状态，通过 composables 共享逻辑

### 核心 Wails 事件

| 事件名 | 方向 | 含义 |
|---|---|---|
| `chat:token` | backend→frontend | 流式 token 传输 |
| `chat:done` | backend→frontend | AI 响应结束 |
| `chat:error` | backend→frontend | 错误信息传递 |
| `chat:clear` | frontend→frontend | 清空聊天历史 |
| `bubble:toggle` | any | 切换聊天气泡显示/隐藏 |
| `pet:state:change` | any | 宠物状态变更 (idle/thinking/speaking/error) |
| `knowledge:progress` | backend→frontend | 知识库导入进度更新 |
| `config:model:changed` | frontend→frontend | Live2D 模型切换通知 |
| `config:chat:size:changed` | frontend→frontend | 聊天框尺寸变更 |
| `notification:show` | backend→frontend | 显示通知气泡 |
| `settings:open` | any | 打开设置界面 |
| `voice:start` | backend→frontend | 开始录音（Option 长按触发）|
| `voice:transcript` | backend→frontend | 实时 partial STT 结果 |
| `voice:final` | backend→frontend | isFinal STT 结果（可触发自动发送）|
| `voice:end` | backend→frontend | 录音结束（Option 释放时立即触发）|
| `voice:error` | backend→frontend | 语音识别错误 |
| `sms:verification_code` | backend→frontend | 检测到验证码短信 |
| `config:voice:auto-send:changed` | frontend→frontend | 语音自动发送开关状态变更 |

## 开发命令

```bash
wails dev              # 开发模式（前端热重载）
wails build            # 构建生产 .app
go build ./...         # 仅检查 Go 编译
cd frontend && yarn build   # 仅构建前端资源
wails generate module  # 重新生成 Wails bindings
```

## 数据目录结构

`~/.aiko/`
- `pet.db` — SQLite 数据库（settings、messages、knowledge_sources、cron_jobs、model_profiles、tool_permissions）
- `vectors/` — chromem-go 持久化向量数据存储
- `USER.md` — 用户画像文档（由 `update_user_profile` 工具自动维护）
- `auto-skills/` — Agent 自动沉淀的可复用技能（YAML 格式，由 `save_skill` 工具写入）

## 重要注意事项

### macOS 平台特定
- `macos.go` 中的 Objective-C 代码负责按像素判断鼠标事件响应，实现点击穿透功能
- **⚠️ 不要随意修改 hitTest 逻辑**，容易破坏点击穿透机制
- hitTest JS 选择器：`.live2d-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.lightbox`——新增可交互的全屏覆盖层时必须加入此列表，否则鼠标事件会穿透到桌面
- `macos.go` 同时包含全局 Option 键监控：双击切换气泡，长按 ≥1s 触发语音录音（`startVoiceRecognition` / `stopVoiceRecognition`）
- 语音识别使用 `AVAudioEngine` + `SFSpeechRecognizer`，partial 结果通过 voice pipe 推送 `voice:transcript` Wails 事件；`isFinal` 结果推送 `FINAL:<text>` → Go goroutine 转为 `voice:final` 事件
- **voice:final vs voice:end**：`voice:end` 在 Option 释放时立即触发（停止 UI 动画）；`voice:final` 在 SFSpeechRecognition 异步完成后触发（携带最终文字）。若 `VoiceAutoSend` 开启，ChatPanel 收到 `voice:final` 后自动调用 `send()`
- **macOS 系统集成首选 osascript**：Wails 的 `[NSApp run]` 占用主线程，任何需要主线程的 CGO API（AXUIElement、NSWorkspace 等）都不安全；改用 `exec.Command("osascript", "-e", ...)` 子进程调用 AppleScript，无线程限制

### 工具系统
- 普通工具实现 `Tool` 接口（`InvokableRun(ctx, string) (string, error)`），在 `All()` 中注册，`AllEino()` 自动用 `ToEino()` 包装
- **多模态工具**（返回图片等）实现 `EnhancedTool` 接口（`InvokableRun(ctx, *schema.ToolArgument) (*schema.ToolResult, error)`），在 `AllEino()` 中用 `ToEinoEnhanced()` 单独注册，**不**放入 `All()`；同时在 `app.go` 启动时手动调用 `permStore.EnsureRow()` 注册权限行（`EnsureRow` 接受 `namedPerm` 接口，普通 Tool 和 EnhancedTool 均满足）
- 有运行时依赖（知识库、调度器、长期记忆等）的工具在 `AllContextual()` 中注册
- macOS 专属工具用 `//go:build darwin` / `//go:build !darwin` 分平台实现（non-darwin 提供 stub）
- **osascript 模式**：所有 macOS 系统集成（浏览器 URL、提醒事项等）使用 `exec.Command("osascript", "-e", script)` 子进程方式，**不要使用 CGO AXUIElement / AppKit API**，原因是 Wails 已占用主线程，CGO 的 `dispatch_sync(main_queue)` 会死锁

### 多模态对话
- `app.go` 的 `SendMessageWithImages(userInput string, images []string)` 接收前端传来的 data URL 数组，解析后构造含图片 part 的 `*schema.Message`，调用 `agent.ChatWithMessage()`
- `agent.go` 的 `ChatWithMessage()` 通过 `runner.Run(ctx, []adk.Message{msg})` 直接传入预构建消息，再走标准流式输出流程；`sanitiseForMemory()` 在存入短期记忆前将图片 part 替换为 `[图片×N]` 文本占位
- eino acl/openai 会将 `Base64Data` 序列化为 `data:<mime>;base64,<data>` 格式的 image_url，与 OpenAI 多模态 API 规范一致；若模型报"image input not supported"，是模型端问题而非序列化问题

### 图片预览灯箱
- `ChatPanel.vue` 中用 `<Teleport to="body">` 将灯箱挂到 `document.body`，使 `position: fixed` 覆盖整个 viewport 而不受父级限制
- 灯箱 CSS class `.lightbox` 已加入 `macos.go` hitTest 选择器，鼠标悬停时窗口不会穿透

### MCP 热重载
- `app.go` 的 `AddMCPServer`、`UpdateMCPServer`、`DeleteMCPServer` 在 DB 操作完成后会立即调用 `initLLMComponents` 重建 Agent，使新配置立即生效，无需重启应用
- 前端设置界面支持编辑已有 MCP 服务器（`editMCPServer` 函数预填充表单，`saveMCPServer` 根据是否有 `id` 决定调 Add 还是 Update）

### 自我成长系统
- `NudgeInterval`（配置项）控制 Agent 每隔 N 轮对话后自动触发沉淀提示
- 相关工具：`save_memory`（保存长期记忆事实）、`update_user_profile`（更新 `~/.aiko/USER.md` 用户画像）、`save_skill`（保存可复用技能到 `~/.aiko/auto-skills/`）
- 这三个工具在 `AllContextual()` 中注册，需要运行时依赖注入

### 并发安全
- `app.go` 的 `initLLMComponents` 可能并发调用（SaveConfig 触发配置变更时）
- 所有涉及 `a.petAgent`、`a.longMem`、`a.knowledgeSt` 字段的更新必须在 `a.mu.Lock()` 保护下完成

### Wails 绑定
- 前端 Wails bindings（`wailsjs/`）由 `wails dev/build` 自动生成，**不要手动编辑**
- 修改 Go 方法签名后需要重新运行 `wails generate module`

## 项目特色

### 技术创新点
1. **点击穿透实现** - 通过 Objective-C CGO 实现像素级鼠标事件处理
2. **语音输入** - 长按 Option 键触发，AVAudioEngine + SFSpeechRecognizer 实时 STT；isFinal 结果走 FINAL: 前缀 pipe → voice:final 事件；支持「语音消息立刻发送」模式
3. **语音输出 (TTS)** - 支持 OpenAI TTS、Kokoro（本地离线）、macOS 系统 TTS 三种后端，可按模型 profile 独立配置
4. **Apple Intelligence 视觉特效** - 录音期间 4 层 Canvas conic-gradient 彩虹光边框 + 水波纹扩散动画
5. **eino Agent 集成** - 基于字节跳动 ADK 的工具调用和中间件系统
6. **毛玻璃 UI 设计** - 现代化深色主题 + CSS backdrop-filter 效果
7. **多模态内容渲染** - 支持 Markdown、LaTeX、代码高亮、表格等；聊天框支持粘贴图片并发送给多模态模型
8. **RAG 知识库** - chromem-go 向量数据库 + 文档导入系统
9. **MCP 协议支持** - 可扩展第三方工具生态，支持热重载
10. **osascript 系统集成** - 无 CGO 的 macOS 系统集成模式（浏览器 URL、提醒事项、邮件、截图等）
11. **自我成长系统** - 跨会话用户画像、记忆事实、可复用技能自动沉淀
12. **剪贴板 & 截图工具** - Agent 可读写剪贴板、截图并以图片形式返回多模态结果
13. **应用控制工具** - Agent 可列出运行中 App、激活或退出指定应用

### 借鉴的优秀项目
- **架构设计** 借鉴了 [Wails Community Examples](https://github.com/wailsapp/awesome-wails)
- **Agent 系统** 基于 [eino](https://github.com/cloudwego/eino) 的设计思路
- **向量数据库** 使用 [chromem-go](https://github.com/philippgille/chromem-go) 的纯 Go 实现
- **UI 交互** 参考了 [Claude Desktop](https://claude.ai/download) 的用户体验
- **Live2D 渲染** 基于 [Live2D Cubism SDK](https://www.live2d.com/en/sdk/download/web/) Web 版本

## 当前状态

- ✅ 核心 AI 对话功能完备
- ✅ Live2D 宠物渲染和状态管理
- ✅ 点击穿透和窗口管理
- ✅ 毛玻璃 UI 和深色主题
- ✅ RAG 知识库和文档导入
- ✅ 定时任务和工具权限系统
- ✅ 飞书 lark-cli 集成
- ✅ MCP 协议工具扩展（添加/编辑/删除后热重载）
- ✅ 语音输入（长按 Option，SFSpeechRecognizer STT，支持「立刻发送」模式）
- ✅ 语音输出 TTS（OpenAI / Kokoro 本地离线 / macOS 系统，按 profile 配置）
- ✅ 浏览器感知（osascript 获取当前 URL + 页面内容）
- ✅ macOS 提醒事项读取与标记完成
- ✅ macOS 邮件读取（osascript 读取 Mail.app 邮件列表与正文）
- ✅ 短信监听（fsnotify 监听 chat.db，自动识别验证码并复制到剪贴板）
- ✅ 自我成长（用户画像、长期记忆、技能沉淀）
- ✅ 剪贴板读写（`read_clipboard` / `write_clipboard` 工具）
- ✅ 截图工具（`take_screenshot`，EnhancedInvokableTool，返回 PNG base64 图片）
- ✅ 应用控制（`list_running_apps` / `control_app`，osascript 激活/退出 App）
- ✅ 聊天框图片粘贴（粘贴或拖入图片，发送给多模态模型；消息气泡内展示缩略图；点击灯箱全屏预览）
- ⚠️ 仅支持 macOS（使用私有 API，不兼容 App Store）
- ❌ Windows/Linux 支持（开发中）

## 下阶段计划

### 语音唤醒 (v2.1)
- 📱 **语音唤醒** - 支持"Hey Aiko"等唤醒指令

### 跨平台支持 (v2.2)
- 🖥️ Windows 版本 - 重写点击穿透逻辑，使用 Win32 API
- 🐧 Linux 版本 - X11/Wayland 窗口管理适配

---

*最后更新：2026-04-26*
