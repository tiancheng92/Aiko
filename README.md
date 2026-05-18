# Aiko - AI 桌面宠物

<div align="center">

<img src="build/appicon.png" alt="Aiko Logo" width="120" height="120">

**你的 AI 伙伴，就在桌面上**

[![Go Version](https://img.shields.io/badge/Go-1.26+-blue.svg)](https://golang.org/)
[![Wails](https://img.shields.io/badge/Wails-v2-green.svg)](https://wails.io/)
[![Vue](https://img.shields.io/badge/Vue-3-brightgreen.svg)](https://vuejs.org/)
[![Platform](https://img.shields.io/badge/Platform-macOS%2011%2B-lightgrey.svg)](https://www.apple.com/macos/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

## ✨ 特性

- 🤖 **智能对话**：基于 eino ReAct Agent，支持多轮对话和工具调用
- 🎭 **Live2D / VRM 宠物**：可爱的 2D 或 3D 角色，支持多种模型、表情和情绪联动
- 🎙️ **语音输入**：长按 Option 键触发，macOS 原生 SFSpeechRecognizer 实时语音转文字；支持「立刻发送」模式
- 🔊 **语音输出 (TTS)**：支持 OpenAI TTS、Kokoro 本地离线、macOS 系统 TTS，可按模型 Profile 独立配置
- 🖼️ **图片粘贴**：聊天框支持直接粘贴截图/图片，发送给多模态模型；消息气泡展示缩略图，点击可全屏预览
- 📎 **文本文件附件**：拖入或选择文本文件，内联为消息内容发送给 Agent
- 🔔 **主动消息**：Agent 可安排定时 follow-up 提醒，聊天框打开时推入对话，关闭时发系统通知
- 🧠 **自我成长**：跨会话积累用户画像、记忆事实、自动沉淀可复用技能
- 📁 **文件系统工具**：Agent 可在白名单路径内读写文件、列目录、创建/删除/移动
- 🖥️ **Shell 执行**：Agent 可执行 Shell 命令，执行前弹窗请求用户确认，支持编辑命令后再执行
- 💻 **代码执行**：Agent 可运行 Python/Node/Ruby/Bash 代码片段，同样需用户确认后执行
- 📋 **剪贴板工具**：Agent 可读取和写入系统剪贴板
- 📸 **截图工具**：Agent 可截取全屏并以图片形式返回多模态结果
- 📱 **应用控制**：Agent 可列出运行中 App、激活或退出指定应用
- 🌐 **浏览器感知**：通过 osascript 读取当前浏览器 URL 并抓取页面内容
- 📅 **系统集成**：读取 macOS 提醒事项、标记完成；读取 Mail.app 邮件；读写 macOS 日历
- 📱 **短信监听**：监听 macOS 信息 App，自动识别验证码并复制到剪贴板
- 🛠️ **内置工具**：系统信息、天气、位置、网页抓取、时间、开发者工具（JSON/编码/转换）等
- 📚 **知识库**：RAG 支持，可导入文档进行问答
- ⏰ **定时任务**：支持 Cron 表达式的计划任务
- 🔧 **MCP 协议**：兼容 Model Context Protocol，可扩展第三方工具，添加后热重载无需重启
- 🪶 **飞书集成**：通过 lark-cli 操作飞书（消息、日历、文档等）
- 🎨 **毛玻璃 UI**：现代化深色主题界面，录音时呈现 Apple Intelligence 风格彩虹光边框
- 🖱️ **点击穿透**：宠物不遮挡桌面操作，智能响应交互
- 🖥️ **多屏感知**：连接/断开显示器时自动重定位宠物和聊天框
- 🔄 **自动更新**：检查 GitHub Releases 并在后台异步下载安装，带进度反馈
- 💾 **数据持久化**：SQLite 存储聊天记录，chromem-go 向量数据库

## 📋 兼容性

| 平台 | 状态 |
|------|------|
| **macOS 11.0+** | ✅ 完整支持 |
| **Windows** | ❌ 短期内不计划支持 |
| **Linux** | ❌ 短期内不计划支持 |

> Aiko 深度依赖 macOS 专属 API（Objective-C CGO 点击穿透、AVAudioEngine 语音识别、SFSpeechRecognizer、osascript 系统集成等），**跨平台移植不在近期路线图内**。

## 🏗️ 技术架构

### 后端 (Go)

**核心框架**
- [Wails v2](https://wails.io/) - 跨平台桌面应用框架
- [eino](https://github.com/cloudwego/eino) - 字节跳动 Agent Development Kit
- [chromem-go](https://github.com/philippgille/chromem-go) - 纯 Go 向量数据库

**数据存储**
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - 纯 Go SQLite 驱动（聊天记录、配置、权限等）
- [zerolog](https://github.com/rs/zerolog) - 零分配结构化日志库

**工具生态**
- [robfig/cron/v3](https://pkg.go.dev/github.com/robfig/cron/v3) - Cron 任务调度
- MCP (Model Context Protocol) - 工具协议标准
- lark-cli - 飞书命令行工具集成

### 前端 (Vue 3)

**核心技术栈**
- [Vue 3](https://vuejs.org/) + Composition API + [Vite](https://vitejs.dev/)

**UI 增强**
- [marked](https://marked.js.org/) + [KaTeX](https://katex.org/) + [highlight.js](https://highlightjs.org/) - Markdown / LaTeX / 代码高亮
- CSS3 backdrop-filter - 毛玻璃视觉效果

**角色渲染**
- Live2D Cubism SDK + pixi-live2d-display - 2D 角色动画
- VRM / Three.js - 3D 角色渲染

### macOS 平台

- Objective-C CGO 桥接 - 点击穿透 + 全局热键
- AVAudioEngine + SFSpeechRecognizer - 实时语音识别
- osascript - 浏览器、提醒事项、邮件、日历、截图等系统集成

## 🚀 快速开始

### 环境要求

- **Go 1.26+**
- **Node.js 18+** + yarn
- **macOS 11.0+**
- **Wails CLI**：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### 安装依赖

```bash
git clone git@github.com:tiancheng92/Aiko.git
cd Aiko
go mod download
cd frontend && yarn install
```

### 开发模式

```bash
wails dev                   # 前端热重载开发
go build ./...              # 仅检查后端编译
cd frontend && yarn build   # 仅构建前端
```

### 生产构建

```bash
make build   # 构建 + ad-hoc 签名，输出: build/bin/Aiko.app
make run     # 构建 + 签名 + 启动（推荐，TCC 权限持久化）
```

> **为什么用 `make run` 而不是 `wails dev`？**  
> `make run` 执行 ad-hoc 签名（`codesign --sign -`），配合固定 Bundle ID `com.xutiancheng.aiko`，macOS TCC 权限授权后跨重新编译持久有效。

## ⚙️ 配置

首次启动在设置界面配置：

1. **模型配置**：API Key、Base URL、模型名称（支持多 Profile 切换）
2. **系统配置**：Live2D/VRM 模型选择、宠物大小、聊天框尺寸
3. **工具权限**：启用/禁用内置工具；文件系统白名单路径和执行超时
4. **知识库**：导入文档建立 RAG 知识库
5. **定时任务**：创建 Cron 计划任务
6. **MCP 服务器**：接入外部 MCP 工具（热重载）
7. **飞书集成**：配置 lark-cli 路径和认证
8. **短信监听**：启用/禁用验证码自动识别
9. **语音设置**：TTS 后端选择、语音消息立刻发送开关
10. **自我成长**：配置 Nudge 间隔（每隔 N 轮提示 Agent 沉淀知识）

## 📁 项目结构

```
├── main.go                    # 应用入口
├── app.go                     # Wails 绑定主文件（startup/shutdown，通用方法）
├── app_chat.go                # 聊天相关绑定（Send、Stop、History、Regenerate）
├── app_config.go              # 配置相关绑定（Config、ModelProfile）
├── app_cron.go                # 定时任务绑定
├── app_knowledge.go           # 知识库绑定（异步导入，事件驱动）
├── app_layout.go              # 布局相关绑定（位置、尺寸、屏幕）
├── app_mcp.go                 # MCP 服务器管理绑定（异步热重载）
├── app_system.go              # 系统绑定（更新、权限、Lark、SMS 等）
├── app_tts.go                 # TTS 绑定
├── app_voice.go               # 语音识别绑定
├── app_vrm.go                 # VRM 模型管理绑定
├── macos.go                   # macOS 平台代码（点击穿透、语音识别、全局热键）
├── internal/
│   ├── agent/                 # eino ReAct Agent 核心 + 中间件
│   │   ├── agent.go           # Agent 初始化与配置
│   │   ├── chat.go            # 流式对话实现
│   │   ├── drain.go           # 事件流处理（interrupt/resume）
│   │   ├── context.go         # 对话上下文管理
│   │   ├── emotion.go         # 情绪标签提取
│   │   └── middleware/        # 日志/重试/错误恢复中间件
│   ├── tools/                 # 内置工具
│   │   ├── appctl/            # 应用控制（list_running_apps / control_app）
│   │   ├── browser/           # 浏览器 URL 感知
│   │   ├── calendar/          # macOS 日历读写
│   │   ├── clipboard/         # 剪贴板读写
│   │   ├── context/           # 上下文工具
│   │   ├── cron/              # 定时任务管理工具
│   │   ├── dev/               # 开发者工具（JSON/编码/格式转换）
│   │   ├── exec/              # Shell 命令 & 代码执行（eino interrupt/resume）
│   │   ├── fs/                # 文件系统读写（路径白名单）
│   │   ├── growth/            # 自我成长（记忆/技能/用户画像）
│   │   ├── image/             # 图片处理工具
│   │   ├── location/          # 位置信息
│   │   ├── mail/              # macOS 邮件读取
│   │   ├── reminders/         # macOS 提醒事项
│   │   ├── screenshot/        # 截图（EnhancedInvokableTool）
│   │   ├── system/            # 系统信息、网络等
│   │   ├── timeutil/          # 时间工具
│   │   ├── weather/           # 天气查询
│   │   ├── web/               # 网页抓取
│   │   ├── registry.go        # 工具注册 & 权限门控
│   │   ├── tool.go            # Tool / EnhancedTool 接口定义
│   │   └── permission.go      # 权限持久化 (SQLite)
│   ├── config/                # 配置持久化（SQLite）
│   ├── db/                    # SQLite Schema 迁移
│   ├── execenv/               # 代码执行环境（Python/Node/Ruby/Bash）
│   ├── knowledge/             # RAG 知识库（chromem-go）
│   ├── lark/                  # 飞书 lark-cli 客户端封装
│   ├── llm/                   # ChatModel / Embedder 抽象层
│   ├── mcp/                   # MCP 协议实现
│   ├── memory/                # 短期(SQLite) + 长期(chromem-go) 记忆
│   ├── notify/                # macOS 系统通知
│   ├── proactive/             # 主动消息引擎
│   ├── scheduler/             # Cron 任务调度
│   ├── skill/                 # YAML 自定义技能
│   ├── sms/                   # 短信监听（验证码识别）
│   └── tts/                   # TTS 后端（OpenAI / Kokoro / macOS 系统）
├── frontend/
│   └── src/
│       ├── components/        # Vue 组件（ChatPanel、SettingsWindow、Live2DPet 等）
│       ├── composables/       # 组合式 API（usePetState、useVRMModel、useSounds 等）
│       ├── utils/             # 工具函数（timing.js throttle/debounce、icons.js）
│       └── wailsjs/           # Wails 生成的绑定（勿手动修改）
└── build/                     # 构建资源和输出
```

## 🛠️ 开发说明

### 添加普通工具

1. 在 `internal/tools/<category>/` 下创建工具文件（macOS 专属用 `_darwin.go` + `_other.go`）
2. 实现 `Tool` 接口：`Name()`, `Permission()`, `Info()`, `InvokableRun(ctx, string) (string, error)`
3. 在 `registry.go` 的 `All()` 中注册；有运行时依赖的工具加到 `AllContextual()`

### 添加多模态工具（返回图片）

1. 实现 `EnhancedTool` 接口：`InvokableRun(ctx, *schema.ToolArgument) (*schema.ToolResult, error)`
2. 在 `AllEino()` 中用 `ToEinoEnhanced()` 单独注册（**不**放入 `All()`）
3. 在 `app.go` 启动时手动调用 `permStore.EnsureRow(&YourTool{})` 注册权限行

### 新增全屏覆盖层

将 CSS class 名加入 `macos.go` 的 hitTest 选择器，否则鼠标事件会穿透到桌面。

## ❓ 常见问题

**Q: 为什么只支持 macOS？**  
A: 点击穿透依赖 Objective-C CGO；语音识别依赖 AVAudioEngine + SFSpeechRecognizer；系统集成依赖 osascript，均为 macOS 专属 API。

**Q: 提示"开发者无法验证"怎么办？**  
A: 系统偏好设置 → 安全性与隐私中允许运行，或执行 `xattr -cr Aiko.app`。

**Q: 图片发送后 AI 回复"不支持图片"？**  
A: 请确认使用的模型支持多模态输入（如 GPT-4o、Claude 3、Qwen-VL）。

**Q: 工具执行为什么要弹窗确认？**  
A: Shell 命令和代码执行是高风险操作，需用户确认后才真正执行，确认弹窗内还可以编辑命令。

**Q: 工具权限在哪里管理？**  
A: 设置 → 工具 → 权限，可逐个开启/关闭内置工具。截图、剪贴板、应用控制等敏感工具默认关闭。

**Q: 如何导入自定义 VRM 模型？**  
A: 设置 → 宠物 → 导入 VRM，选择 `.vrm` 文件后保存到 `~/.aiko/vrm/`，可在模型列表中切换。

## 🔒 隐私与安全

- **本地数据存储**：所有聊天记录和配置均保存在 `~/.aiko/` 目录
- **工具权限管控**：敏感工具（截图、剪贴板、应用控制）默认关闭，需手动授权
- **执行确认机制**：Shell 命令和代码执行均需用户点击确认，不会静默执行
- **网络连接**：仅在 AI 对话和工具调用时连接外部 API

## 🗺️ 下阶段计划

- 🎙️ **语音唤醒 (v2.1)** - 支持"Hey Aiko"等唤醒指令

## 📄 开源协议

本项目基于 MIT 协议开源。详见 [LICENSE](LICENSE) 文件。

## 🙏 致谢

- 字节跳动 eino 团队提供的 Agent 开发框架
- Wails 团队打造的优秀跨平台框架
- Live2D 团队的角色渲染技术支持

---

<div align="center">

**如果这个项目对你有帮助，请给一个 ⭐ Star 支持一下！**

[报告问题](https://github.com/tiancheng92/Aiko/issues) · [功能建议](https://github.com/tiancheng92/Aiko/issues/new)

</div>
