# Aiko - AI 桌面宠物

<div align="center">

<img src="build/appicon.png" alt="Aiko Logo" width="120" height="120">

**你的 AI 伙伴，就在桌面上**

[![Go Version](https://img.shields.io/badge/Go-1.25+-blue.svg)](https://golang.org/)
[![Wails](https://img.shields.io/badge/Wails-v2-green.svg)](https://wails.io/)
[![Vue](https://img.shields.io/badge/Vue-3-brightgreen.svg)](https://vuejs.org/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

## ✨ 特性

- 🤖 **智能对话**：基于 eino ReAct Agent，支持多轮对话和工具调用
- 🎭 **Live2D 宠物**：可爱的动画角色，支持多种模型和表情状态
- 🎙️ **语音输入**：长按 Option 键触发，macOS 原生 SFSpeechRecognizer 实时语音转文字；支持「立刻发送」模式
- 🔊 **语音输出 (TTS)**：支持 OpenAI TTS、Kokoro 本地离线、macOS 系统 TTS，可按模型 profile 独立配置
- 🖼️ **图片粘贴**：聊天框支持直接粘贴截图/图片，发送给多模态模型；消息气泡内展示缩略图，点击可全屏预览
- 🧠 **自我成长**：跨会话积累用户画像、记忆事实、自动沉淀可复用技能
- 🛠️ **内置工具**：系统信息、网络状态、天气、位置、网页抓取等实用工具
- 📁 **文件系统工具**：Agent 可在白名单路径内读写文件、列目录、创建/删除/移动，支持通配符配置
- 🖥️ **Shell 执行**：Agent 可执行 Shell 命令，执行前弹窗请求用户确认，支持编辑命令后再执行
- 💻 **代码执行沙盒**：Agent 可运行 Python/Node/Ruby/Bash 代码片段，同样需用户确认后执行
- 📋 **剪贴板工具**：Agent 可读取和写入系统剪贴板
- 📸 **截图工具**：Agent 可截取全屏并以图片形式返回多模态结果
- 📱 **应用控制**：Agent 可列出运行中 App、激活或退出指定应用
- 🌐 **浏览器感知**：通过 osascript 读取当前浏览器 URL 并抓取页面内容
- 📅 **系统集成**：读取 macOS 提醒事项、标记完成；读取 Mail.app 邮件列表与正文
- 📱 **短信监听**：监听 macOS 信息 App，自动识别验证码并复制到剪贴板
- 📚 **知识库**：RAG 支持，可导入文档进行问答
- ⏰ **定时任务**：支持 Cron 表达式的计划任务
- 🔧 **MCP 协议**：兼容 Model Context Protocol，可扩展第三方工具，添加后热重载无需重启
- 🪶 **飞书集成**：通过 lark-cli 操作飞书（消息、日历、文档等）
- 🎨 **毛玻璃 UI**：现代化深色主题界面，录音时呈现 Apple Intelligence 风格彩虹光边框
- 🖱️ **点击穿透**：宠物不遮挡桌面操作，智能响应交互
- 💾 **数据持久化**：SQLite 存储聊天记录，chromem-go 向量数据库

## 🏗️ 技术架构

### 后端 (Go)

**核心框架**
- [Wails v2](https://wails.io/) - 跨平台桌面应用框架
- [eino](https://github.com/cloudwego/eino) - 字节跳动 Agent Development Kit
- [chromem-go](https://github.com/philippgille/chromem-go) - 纯 Go 向量数据库

**AI & 模型**
- 支持 OpenAI API 兼容接口 (OpenRouter, DeepSeek, 通义千问等)
- 自定义 embedding 模型集成

**数据存储**
- [SQLite](https://sqlite.org/) - 轻量级关系数据库
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - 纯 Go SQLite 驱动

**工具生态**
- [robfig/cron/v3](https://pkg.go.dev/github.com/robfig/cron/v3) - Cron 任务调度
- MCP (Model Context Protocol) - 工具协议标准
- lark-cli - 飞书命令行工具集成

### 前端 (Vue 3)

**核心技术栈**
- [Vue 3](https://vuejs.org/) - 渐进式 JavaScript 框架
- [Vite](https://vitejs.dev/) - 现代构建工具
- Composition API - Vue 3 响应式编程

**UI 增强**
- [marked](https://marked.js.org/) - Markdown 解析渲染
- [marked-katex-extension](https://github.com/UziTech/marked-katex-extension) - LaTeX 数学公式扩展
- [highlight.js](https://highlightjs.org/) - 代码语法高亮
- [KaTeX](https://katex.org/) - 数学公式渲染
- CSS3 backdrop-filter - 毛玻璃视觉效果

**Live2D 集成**
- Live2D Cubism SDK - 2D 角色动画渲染
- WebGL Canvas - 硬件加速渲染

### 平台特定

**macOS 集成**
- Objective-C CGO 桥接 - 点击穿透 + 全局热键实现
- Cocoa NSView - 原生窗口控制
- Core Graphics - 像素级鼠标事件处理
- AVAudioEngine + SFSpeechRecognizer - 实时语音识别
- osascript - 浏览器 URL 读取、提醒事项读写、邮件读取、截图等系统集成

## 📋 兼容性

- ✅ **macOS 11.0+** - 完整功能支持，包括点击穿透
- ❌ **Windows** - 暂不支持（开发中）
- ❌ **Linux** - 暂不支持（计划中）

## 🗺️ 下阶段计划

### 语音唤醒 (v2.1)
- 📱 **语音唤醒** - 支持"Hey Aiko"等唤醒指令

### 跨平台支持 (v2.2)
- 🖥️ **Windows 版本** - 完整功能移植
- 🐧 **Linux 版本** - 社区驱动支持

## 🚀 快速开始

### 环境要求

- **Go 1.22+**
- **Node.js 16+** (推荐使用 yarn)
- **macOS 11.0+** (当前仅支持 macOS)
- **Wails CLI**: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

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
wails build   # 输出: build/bin/Aiko.app
```

## ⚙️ 配置

首次启动需要在设置界面配置：

1. **模型配置**：API Key、Base URL、模型名称
2. **系统配置**：Live2D 模型、宠物大小、聊天框尺寸
3. **工具权限**：启用/禁用内置工具；在「工具 → 设置」中配置文件系统白名单路径（支持通配符）和执行超时
4. **知识库**：导入文档建立 RAG 知识库
5. **定时任务**：创建 Cron 计划任务
6. **MCP 服务器**：接入外部 MCP 工具（添加/编辑/删除后热重载）
7. **飞书集成**：配置 lark-cli 路径和认证
8. **短信监听**：启用/禁用验证码自动识别，配置语音消息立刻发送
9. **自我成长**：配置 Nudge 间隔（每隔 N 轮提示 Agent 沉淀知识）

### 语音输入

长按 **Option 键 ≥ 1 秒** 触发录音，松开结束：
- 识别文字实时显示在输入框，可手动编辑后发送
- 开启「语音消息立刻发送」后，松开即自动发送
- 录音期间显示 Apple Intelligence 风格彩虹光边框 + 水波纹特效
- 需在系统偏好设置中授权麦克风和语音识别权限

### 图片粘贴

在聊天输入框聚焦时，直接 **⌘V 粘贴** 截图或图片：
- 输入框上方显示图片缩略图预览，点击 × 可移除
- 发送后图片显示在用户消息气泡内
- 点击气泡内任意图片可**全屏预览**（点击背景关闭）
- 需使用支持多模态的模型（如 GPT-4o、Claude 3、Qwen-VL 等）

### 支持的 AI 提供商

- **OpenRouter** - 支持多种开源模型
- **OpenAI** - GPT 系列模型
- **DeepSeek** - 国产高性能模型
- **通义千问** - 阿里云 AI 服务
- **其他兼容 OpenAI API 的服务商**

## 📁 项目结构

```
├── main.go                 # 应用入口
├── app.go                  # Wails 绑定方法
├── macos.go               # macOS 平台特定代码（点击穿透、语音识别）
├── internal/
│   ├── agent/             # eino ReAct Agent
│   ├── tools/             # 内置工具实现
│   │   ├── filesystem.go       # 文件系统读写（路径白名单）
│   │   ├── shell_tools.go      # Shell 命令执行（用户确认）
│   │   ├── code_tools.go       # 代码执行沙盒（用户确认）
│   │   ├── clipboard_*.go      # 剪贴板读写
│   │   ├── screenshot_*.go     # 截图（EnhancedInvokableTool）
│   │   ├── app_control_*.go    # 应用列举与控制
│   │   ├── browser_*.go        # 浏览器 URL 感知
│   │   ├── reminders_*.go      # 提醒事项
│   │   ├── mail_*.go           # 邮件读取
│   │   ├── system_tools.go     # 系统信息、网络、天气等
│   │   ├── registry.go         # 工具注册 & 权限门控
│   │   └── permission.go       # 权限持久化 (SQLite)
│   ├── skill/             # YAML 自定义技能
│   ├── memory/            # 短期(SQLite) / 长期(chromem-go) 记忆
│   ├── knowledge/         # RAG 知识库
│   ├── config/            # 配置管理
│   ├── db/                # SQLite Schema 迁移
│   ├── llm/               # LLM / Embedder 抽象层
│   ├── scheduler/         # Cron 任务调度
│   ├── mcp/               # MCP 协议实现
│   └── sms/               # 短信监听（验证码识别）
├── frontend/
│   ├── src/
│   │   ├── components/    # Vue 组件
│   │   ├── composables/   # 组合式 API
│   │   └── wailsjs/       # Wails 生成的绑定
│   └── dist/              # 构建输出
└── build/                 # 构建资源和输出
```

## 🛠️ 开发说明

### 添加普通工具

1. 在 `internal/tools/` 创建工具文件（macOS 专属用 `_darwin.go` / `_other.go`）
2. 实现 `Tool` 接口：`Name()`, `Permission()`, `Info()`, `InvokableRun(ctx, string) (string, error)`
3. 在 `registry.go` 的 `All()` 中注册，`AllEino()` 会自动包装

### 添加多模态工具（返回图片）

1. 实现 `EnhancedTool` 接口：`InvokableRun(ctx, *schema.ToolArgument) (*schema.ToolResult, error)`
2. 在 `AllEino()` 中用 `ToEinoEnhanced()` 单独注册（**不**放入 `All()`）
3. 在 `app.go` 启动时手动调用 `permStore.EnsureRow(&YourTool{})` 注册权限行

### 新增全屏覆盖层（弹窗/灯箱等）

将 CSS class 名加入 `macos.go` 的 hitTest 选择器（`hitTestPoint` 函数中的 JS 字符串），否则鼠标事件会穿透到桌面。

## ❓ 常见问题

**Q: 为什么只支持 macOS？**  
A: 点击穿透功能依赖 macOS 特定的 Objective-C API。Windows/Linux 版本开发中。

**Q: 提示"开发者无法验证"怎么办？**  
A: 在系统偏好设置 → 安全性与隐私中允许运行，或执行 `xattr -cr Aiko.app`。

**Q: 图片发送后 AI 回复"不支持图片"？**  
A: 请确认使用的模型支持多模态输入（如 GPT-4o、Claude 3、Qwen-VL）。本地 llama.cpp 需加载 mmproj 多模态投影器。

**Q: 工具执行为什么要弹窗确认？**  
A: Shell 命令和代码执行是高风险操作，设计上要求用户确认后才真正执行，确认弹窗内还可以编辑命令内容。

**Q: 工具权限在哪里管理？**  
A: 设置 → 工具 → 权限，可逐个开启/关闭内置工具。截图、剪贴板、应用控制等敏感工具默认关闭。文件系统工具还需在「工具 → 设置」中配置允许访问的路径白名单。

## 🔒 隐私与安全

- **本地数据存储**：所有聊天记录和配置均保存在本地 `~/.aiko/` 目录
- **工具权限管控**：敏感工具（截图、剪贴板、应用控制）默认关闭，需用户手动授权
- **网络连接**：仅在 AI 对话和工具调用时连接外部 API
- **开源透明**：所有代码公开，可审计安全性

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
