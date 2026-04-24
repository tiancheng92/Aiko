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
- `pet.db` — SQLite 数据库（settings、messages、knowledge_sources、cron_jobs、model_profiles）
- `vectors/` — chromem-go 持久化向量数据存储

## 重要注意事项

### macOS 平台特定
- `macos.go` 中的 Objective-C 代码负责按像素判断鼠标事件响应，实现点击穿透功能
- **⚠️ 不要随意修改 hitTest 逻辑**，容易破坏点击穿透机制
- `macos.go` 同时包含全局 Option 键监控：双击切换气泡，长按 ≥1s 触发语音录音（`startVoiceRecognition` / `stopVoiceRecognition`）
- 语音识别使用 `AVAudioEngine` + `SFSpeechRecognizer`，结果通过 CGO exported callback 推送 Wails 事件

### 工具系统
- 修改 `internal/tools/registry.go` 的 `All()` 后需同步更新 `AllEino()`（返回 eino Tool 接口列表）
- 新工具需要在 `All()` 和 `AllEino()` 两处注册

### 并发安全
- `app.go` 的 `initLLMComponents` 可能并发调用（SaveConfig 触发配置变更时）
- 所有涉及 `a.petAgent`、`a.longMem`、`a.knowledgeSt` 字段的更新必须在 `a.mu.Lock()` 保护下完成

### Wails 绑定
- 前端 Wails bindings（`wailsjs/`）由 `wails dev/build` 自动生成，**不要手动编辑**
- 修改 Go 方法签名后需要重新运行 `wails generate module`

## 项目特色

### 技术创新点
1. **点击穿透实现** - 通过 Objective-C CGO 实现像素级鼠标事件处理
2. **语音输入** - 长按 Option 键触发，AVAudioEngine + SFSpeechRecognizer 实时 STT，CGO callback 回传
3. **Apple Intelligence 视觉特效** - 录音期间 4 层 Canvas conic-gradient 彩虹光边框 + 水波纹扩散动画
4. **eino Agent 集成** - 基于字节跳动 ADK 的工具调用和中间件系统
5. **毛玻璃 UI 设计** - 现代化深色主题 + CSS backdrop-filter 效果
6. **多模态内容渲染** - 支持 Markdown、LaTeX、代码高亮、表格等
7. **RAG 知识库** - chromem-go 向量数据库 + 文档导入系统
8. **MCP 协议支持** - 可扩展第三方工具生态

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
- ✅ MCP 协议工具扩展
- ✅ 语音输入（长按 Option，SFSpeechRecognizer STT）
- ⚠️ 仅支持 macOS（使用私有 API，不兼容 App Store）
- ❌ Windows/Linux 支持（开发中）

## 下阶段计划

### 语音输出 (v2.1)
- 🔊 语音输出 - TTS 文字转语音，宠物可发声
- 🎵 音色选择 - 多种声音个性化选项
- 📱 语音唤醒 - 支持"Hey Aiko"等唤醒指令

### 跨平台支持 (v2.2)
- 🖥️ Windows 版本 - 重写点击穿透逻辑，使用 Win32 API
- 🐧 Linux 版本 - X11/Wayland 窗口管理适配

---

*最后更新：2026-04-24*