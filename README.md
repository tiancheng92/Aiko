# Aiko — AI 桌面宠物

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

### 核心交互
- 🤖 **智能对话** — 基于 eino ReAct Agent，支持多轮对话、工具调用、思考链
- 🎭 **Live2D / VRM 宠物** — 可爱的 2D 或 3D 角色，支持多种模型、表情联动、头部跟踪
- 🎙️ **语音输入** — 长按 Option 键触发，macOS 原生 SFSpeechRecognizer 实时语音转文字，支持「立刻发送」模式
- 🔊 **语音输出 (TTS)** — 支持 OpenAI TTS、Kokoro 本地离线 TTS、macOS 系统 TTS，可按模型 Profile 独立配置

### 对话增强
- 🖼️ **多模态对话** — 聊天框粘贴截图/图片，发送给多模态模型；消息气泡缩略图，点击全屏预览
- 📎 **文件附件** — 拖入文本文件，内联为消息内容
- 📝 **WYSIWYG 编辑器** — Milkdown (Crepe) Markdown 编辑器，所见即所得输入
- 💡 **代码高亮** — Shiki 语法高亮，支持 14 种语言
- 🔢 **数学公式** — KaTeX 渲染 LaTeX 数学公式
- 🌐 **国际化** — 支持简体中文 / English / 日本語 / 한국어，自动跟随系统语言

### 桌面集成
- 🖱️ **点击穿透** — 宠物不遮挡桌面操作，像素级 Alpha 检测
- 🖥️ **多屏感知** — 连接/断开显示器自动重定位宠物和聊天框
- 📊 **系统监控** — 实时 CPU、内存、磁盘、网络面板，Top 进程排行
- ⏰ **番茄钟** — 内置 Pomodoro 计时器，专注/休息循环

### Agent 能力
- 📁 **文件系统工具** — Agent 可在白名单路径内读写文件、列目录、创建/删除/移动
- 🖥️ **Shell 执行** — 执行 Shell 命令，执行前弹窗确认，可编辑命令；可信命令自动放行
- 💻 **代码执行** — 运行 Python / Node / Ruby / Bash 代码片段，需用户确认
- 📋 **剪贴板工具** — 读取和写入系统剪贴板
- 📸 **截图工具** — 全屏截图，自动压缩为 JPEG 返回多模态结果
- 📱 **应用控制** — 列出运行中 App、激活或退出指定应用
- 🌐 **浏览器感知** — 读取当前浏览器 URL 并抓取页面内容
- 📅 **系统集成** — 读取/创建提醒事项；读取 Mail.app 邮件；读写日历
- 📱 **短信监听** — 监听 iMessage，自动识别验证码并复制到剪贴板
- 🛠️ **内置工具** — 50+ 工具：系统信息、天气、位置、网页抓取、时间、开发者工具等

### 智能系统
- 📚 **知识库** — RAG 支持，导入 PDF / TXT / EPUB / Markdown 文档
- 🧠 **长期记忆** — chromem-go 向量数据库，语义搜索 + 时间衰减排序
- 📝 **对话摘要** — LLM 滚动摘要，长对话保持上下文
- 🎯 **技能系统** — Agent 可自动沉淀和复用 YAML 技能，支持 forked 子 Agent 执行
- 🔔 **主动消息** — Agent 安排定时 follow-up 提醒，聊天框打开时推入对话
- ⏰ **定时任务** — Cron 表达式计划任务，自动触发 Agent 对话
- 🔧 **MCP 协议** — 兼容 Model Context Protocol，支持 stdio / SSE / HTTP，热重载
- 🪶 **飞书集成** — 通过 lark-cli 操作飞书（消息、日历、文档等）

### 用户体验
- 🎨 **毛玻璃 UI** — 现代化深色主题，录音时 Apple Intelligence 风格彩虹光边框
- 🌊 **代码雨背景** — ChatBubble 内 Matrix 风格 Canvas 动画
- 🔄 **自动更新** — 检查 GitHub Releases，后台下载安装带进度反馈
- 💾 **数据持久化** — SQLite 聊天记录 + chromem-go 向量数据库，全部本地存储

## 📋 兼容性

| 平台 | 状态 |
|------|------|
| **macOS 11.0+** | ✅ 完整支持 |
| Windows | ❌ 不计划支持 |
| Linux | ❌ 不计划支持 |

> Aiko 深度依赖 macOS 专属 API（Objective-C CGO 点击穿透、AVAudioEngine + SFSpeechRecognizer 语音识别、osascript 系统集成等），跨平台移植不在路线图内。

## 🚀 快速开始

### 环境要求

- **Go 1.26+**
- **Node.js 18+** + yarn
- **macOS 11.0+**
- **Wails CLI**：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### 安装与运行

```bash
git clone git@github.com:tiancheng92/Aiko.git
cd Aiko
go mod download
cd frontend && yarn install && cd ..

# 开发模式（前端热重载）
wails dev

# 生产构建 + 启动（推荐，TCC 权限持久化）
make run

# 仅构建
make build
```

> **为什么用 `make run`？** 它执行 ad-hoc 签名（`codesign --sign -`），配合固定 Bundle ID `com.xutiancheng.aiko`，macOS TCC 权限（麦克风、辅助功能、通知等）授权后跨重新编译持久有效。`wails dev` 每次重启可能重新弹权限窗。

### 其他命令

```bash
go build ./...              # 仅检查 Go 编译
cd frontend && yarn build   # 仅构建前端资源
wails generate module       # 重新生成 Wails bindings
```

## ⚙️ 配置

首次启动后在设置界面（右键宠物 → 设置）配置：

1. **通用** — 语言、主题（液态玻璃 / 磨砂）、开机启动
2. **外观** — 渲染后端（Live2D / VRM）、模型选择、宠物/聊天框尺寸、头像、音效/TTS 开关
3. **模型** — 多 Profile 管理（API Key、Base URL、模型名、Provider），TTS 后端和语音配置
4. **AI** — 系统提示词、短期记忆上限、最大上下文 Token、Nudge 间隔、技能目录
5. **工具** — MCP 服务器管理（CRUD、启用/禁用），工具权限开关，白名单路径、超时设置
6. **知识库** — 导入文档（PDF / TXT / EPUB / MD），管理知识源
7. **自动化** — Cron 定时任务、主动提醒列表
8. **飞书** — lark-cli 状态和认证
9. **短信** — iMessage 验证码监听开关
10. **番茄钟** — 专注/休息时长配置
11. **关于** — 版本号、检查更新

## 🏗️ 技术架构

```
Wails v2 桌面应用
├── Go 后端
│   ├── eino ReAct Agent（deep agent + 中间件链）
│   ├── 50+ 内置工具（19 个类别）
│   ├── MCP 协议客户端（stdio / SSE / HTTP）
│   ├── SQLite 持久化（modernc.org/sqlite，纯 Go）
│   ├── chromem-go 向量数据库（长期记忆 + 知识库）
│   ├── macOS 原生集成（CGO + osascript + AVAudioEngine）
│   └── Kokoro ONNX TTS（Python 子进程）
└── Vue 3 前端
    ├── Composition API + Vite
    ├── Live2D（pixi-live2d-display）/ VRM（Three.js + @pixiv/three-vrm）
    ├── Markdown 渲染（marked + Shiki + KaTeX）
    ├── WYSIWYG 编辑器（Milkdown Crepe）
    ├── 弹簧动画引擎（useSpring）
    └── 国际化（vue-i18n，4 种语言）
```

## 📁 项目结构

```
├── main.go                    # 应用入口
├── app.go                     # startup/shutdown、LLM 初始化、通用方法
├── app_chat.go                # 聊天：发送、停止、历史、重新生成、导出、搜索
├── app_config.go              # 配置 + ModelProfile CRUD
├── app_cron.go                # 定时任务 + 主动提醒 CRUD
├── app_knowledge.go           # 知识库：导入、列表、删除
├── app_layout.go              # 布局：屏幕、位置、尺寸、鼠标
├── app_mcp.go                 # MCP 服务器管理（异步热重载）
├── app_pomodoro.go            # 番茄钟控制
├── app_system.go              # 系统：权限、Lark、SMS、更新、系统资源、链接预览
├── app_tts.go                 # TTS：朗读、停止、Kokoro 安装
├── app_voice.go               # 语音：VoiceAutoSend、SoundsEnabled、SMS 监听
├── app_vrm.go                 # VRM 模型管理
├── macos.go                   # macOS 平台代码（点击穿透、语音识别、全局热键、通知）
├── internal/
│   ├── agent/                 # eino ReAct Agent 核心 + 中间件链
│   ├── tools/                 # 50+ 内置工具（19 个子包）
│   ├── bytesconv/             # 零分配 string/byte 转换
│   ├── config/                # 配置持久化
│   ├── db/                    # Schema 迁移
│   ├── execenv/               # 代码执行环境
│   ├── knowledge/             # RAG 知识库
│   ├── lark/                  # 飞书客户端
│   ├── llm/                   # ChatModel / Embedder 工厂
│   ├── mcp/                   # MCP 协议客户端
│   ├── memory/                # 短期记忆 + 长期记忆 + 滚动摘要
│   ├── notify/                # macOS 系统通知
│   ├── pomodoro/              # 番茄钟状态机
│   ├── proactive/             # 主动消息引擎
│   ├── scheduler/             # Cron 调度器
│   ├── skill/                 # 技能系统（Agent Hub + 中间件）
│   ├── sms/                   # 短信监听
│   └── tts/                   # TTS 后端
├── frontend/src/
│   ├── components/            # 14 个 Vue 组件
│   ├── composables/           # 10 个组合式 API
│   ├── locales/               # 4 种语言翻译
│   ├── styles/                # 全局样式
│   └── utils/                 # 工具函数
└── build/                     # 构建资源
```

## 🛠️ 开发

### 添加普通工具

1. 在 `internal/tools/<category>/` 下创建工具（macOS 专属用 `_darwin.go` + `_other.go`）
2. 实现 `Tool` 接口：`Name()`, `Permission()`, `Info()`, `InvokableRun(ctx, string) (string, error)`
3. 在 `registry.go` 的 `All()` 中注册；有运行时依赖的工具加到 `AllContextual()`

### 添加增强工具（返回图片等）

1. 实现 `EnhancedTool` 接口：`InvokableRun` 返回 `*schema.ToolResult`
2. 在 `AllEino()` 中用 `ToEinoEnhanced()` 单独注册（不放入 `All()`）
3. 在 `app.go` 启动时调用 `permStore.EnsureRow(&YourTool{})` 注册权限行

### 新增全屏覆盖层

将 CSS class 名加入 `macos.go` 的 hitTest 选择器列表，否则鼠标事件会穿透到桌面。

## ❓ 常见问题

**Q: 为什么只支持 macOS？**
A: 点击穿透依赖 Objective-C CGO；语音识别依赖 AVAudioEngine + SFSpeechRecognizer；系统集成依赖 osascript，均为 macOS 专属 API。

**Q: 提示"开发者无法验证"？**
A: 系统偏好设置 → 安全性与隐私中允许运行，或执行 `xattr -cr Aiko.app`。

**Q: 图片发送后 AI 回复"不支持图片"？**
A: 确认使用的模型支持多模态输入（如 GPT-4o、Claude 3、Qwen-VL）。

**Q: 工具执行为什么要弹窗确认？**
A: Shell 命令和代码执行是高风险操作，需用户确认后才执行，确认弹窗内还可以编辑命令。

**Q: 工具权限在哪里管理？**
A: 设置 → 工具 → 权限，可逐个开启/关闭内置工具。截图、剪贴板、应用控制等敏感工具默认关闭。

**Q: 如何导入自定义 VRM 模型？**
A: 设置 → 外观 → 导入 VRM，选择 `.vrm` 文件保存到 `~/.aiko/vrm/`。

**Q: 如何启用 Kokoro 离线 TTS？**
A: 设置 → 模型 → 选择 Kokoro 作为 TTS 后端，点击"安装 Kokoro 环境"，会自动创建 Python venv、安装依赖、下载模型。

## 🔒 隐私与安全

- **本地存储** — 所有聊天记录和配置保存在 `~/.aiko/`，不上传任何数据到云端
- **工具权限管控** — 敏感工具默认关闭，需手动授权；白名单路径限制文件访问
- **执行确认** — Shell 命令和代码执行均需用户点击确认，支持编辑后执行
- **可信命令** — 可配置命令前缀白名单（如 `git`、`ls`），匹配的自动放行
- **网络连接** — 仅在 AI 对话和工具调用时连接外部 API

## 🗺️ 路线图

- 🎙️ **语音唤醒 (v2.1)** — 支持"Hey Aiko"等唤醒指令

## 📄 开源协议

本项目基于 MIT 协议开源。详见 [LICENSE](LICENSE) 文件。

## 🙏 致谢

- 字节跳动 eino 团队 — Agent 开发框架
- Wails 团队 — 跨平台桌面应用框架
- Live2D 团队 — 角色渲染技术
- Pixiv 团队 — three-vrm 库
- Milkdown 团队 — WYSIWYG Markdown 编辑器
- Shiki 团队 — 语法高亮引擎

---

<div align="center">

**如果这个项目对你有帮助，请给一个 ⭐ Star 支持一下！**

[报告问题](https://github.com/tiancheng92/Aiko/issues) · [功能建议](https://github.com/tiancheng92/Aiko/issues/new)

</div>
