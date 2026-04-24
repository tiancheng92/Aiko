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
- 🎙️ **语音输入**：长按 Option 键触发，macOS 原生 SFSpeechRecognizer 实时语音转文字；支持「立刻发送」模式，松开后自动提交
- 🧠 **自我成长**：跨会话积累用户画像、记忆事实、自动沉淀可复用技能
- 🛠️ **内置工具**：系统信息、网络状态、天气、位置、网页抓取等实用工具
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
- [database/sql](https://pkg.go.dev/database/sql) - Go 标准数据库接口
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
- osascript - 浏览器 URL 读取、提醒事项读写等系统 App 集成

## 📋 兼容性

### 当前支持平台
- ✅ **macOS 10.15+** - 完整功能支持，包括点击穿透
- ❌ **Windows** - 暂不支持（开发中）
- ❌ **Linux** - 暂不支持（计划中）

### Windows 用户
由于点击穿透和窗口管理需要平台特定实现，Windows 版本正在开发中。请耐心等待后续版本，我们会尽快提供跨平台支持。

## 🗺️ 下阶段计划

### 语音输出 (v2.1)
- 🔊 **语音输出** - TTS 语音合成，宠物可以"说话"
- 🎵 **声音个性化** - 多种音色选择，匹配宠物角色
- 📱 **语音唤醒** - 支持语音指令唤醒和控制

### 跨平台支持 (v2.2)
- 🖥️ **Windows 版本** - 完整功能移植
- 🐧 **Linux 版本** - 社区驱动支持

## 🚀 快速开始

### 环境要求

- **Go 1.22+**
- **Node.js 16+** (推荐使用 yarn)
- **macOS 10.15+** (当前仅支持 macOS)
- **Wails CLI**: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### 安装依赖

```bash
# 克隆项目
git clone git@github.com:tiancheng92/Aiko.git
cd Aiko

# 安装后端依赖
go mod download

# 安装前端依赖
cd frontend && yarn install
```

### 开发模式

```bash
# 前端热重载开发
wails dev

# 仅检查后端编译
go build ./...

# 仅构建前端
cd frontend && yarn build
```

### 生产构建

```bash
# 构建 macOS .app 应用
wails build

# 输出位置: build/bin/Aiko.app
```

## ⚙️ 配置

首次启动需要在设置界面配置：

1. **模型配置**: API Key、Base URL、模型名称
2. **系统配置**: Live2D 模型、宠物大小、聊天框尺寸
3. **工具权限**: 启用/禁用内置工具
4. **知识库**: 导入文档建立 RAG 知识库
5. **定时任务**: 创建 Cron 计划任务
6. **MCP 服务器**: 接入外部 MCP 工具（添加/编辑/删除后热重载）
7. **飞书集成**: 配置 lark-cli 路径和认证
8. **短信监听**: 启用/禁用验证码自动识别，配置语音消息立刻发送
9. **自我成长**: 配置 Nudge 间隔（每隔 N 轮提示 Agent 沉淀知识）

### 语音输入

长按 **Option 键 ≥ 1 秒** 触发录音，松开结束：
- 识别文字实时显示在输入框，可手动编辑后发送
- 开启「语音消息立刻发送」后，松开 Option 键等待转录完成即自动发送（设置 → 短信监听 → 语音设置）
- 录音期间显示 Apple Intelligence 风格彩虹光边框 + 水波纹特效
- 需在系统偏好设置中授权麦克风和语音识别权限

### 支持的 AI 提供商

- **OpenRouter**: 支持多种开源模型
- **OpenAI**: GPT 系列模型
- **DeepSeek**: 国产高性能模型
- **通义千问**: 阿里云 AI 服务
- **其他兼容 OpenAI API 的服务商**

## 📁 项目结构

```
├── main.go                 # 应用入口
├── app.go                  # Wails 绑定方法
├── macos.go               # macOS 平台特定代码（点击穿透、语音识别）
├── internal/
│   ├── agent/             # eino ReAct Agent
│   ├── tools/             # 内置工具实现（含 osascript 系统集成）
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

## 🤝 主要借鉴项目

感谢这些优秀的开源项目为 Aiko 提供了灵感和技术基础：

- [eino](https://github.com/cloudwego/eino) - Agent 框架设计思路
- [chromem-go](https://github.com/philippgille/chromem-go) - 向量数据库实现
- [Wails Community Examples](https://github.com/wailsapp/awesome-wails) - 桌面应用开发模式
- [Live2D Web SDK](https://www.live2d.com/en/sdk/download/web/) - 角色渲染技术
- [Claude Desktop](https://claude.ai/download) - AI 桌面应用交互设计
- [MCP Specification](https://modelcontextprotocol.io/) - 工具协议标准

## 🛠️ 开发说明

### 添加新工具

1. 在 `internal/tools/` 创建新工具文件（macOS 专属工具使用 `_darwin.go` / `_other.go` 分平台）
2. 实现 `Tool` 接口：`Name()`, `Permission()`, `Info()`, `InvokableRun()`
3. 在 `internal/tools/registry.go` 的 `All()` 中注册（`AllEino()` 会自动包装，无需单独修改）
4. macOS 系统集成优先使用 `osascript` 子进程方式，避免 CGO 主线程冲突

### 自定义 Live2D 模型

1. 将模型文件放到 `frontend/public/live2d/` 目录
2. 按照 Live2D Cubism SDK 格式组织文件
3. 在设置界面选择对应模型目录名

### MCP 工具扩展

支持通过 MCP 协议集成外部工具，配置路径：设置 → MCP 服务器

## ❓ 常见问题

### 安装和运行
**Q: 为什么只支持 macOS？**
A: 点击穿透功能依赖 macOS 特定的 Objective-C API。Windows 和 Linux 版本正在开发中。

**Q: 提示"开发者无法验证"怎么办？**
A: 在系统偏好设置 → 安全性与隐私中允许运行，或使用 `xattr -cr Aiko.app` 命令。

### 配置相关
**Q: 支持哪些AI模型？**
A: 支持所有兼容 OpenAI API 的服务，包括 OpenRouter、DeepSeek、通义千问等。

**Q: 如何导入知识库文档？**
A: 在设置界面选择"知识库"标签，支持 PDF、TXT、Markdown 等格式文档。

### 功能使用
**Q: 宠物不响应点击怎么办？**
A: 检查点击穿透设置，确保在宠物的非透明区域点击。可在设置中调整响应区域。

**Q: 如何添加自定义工具？**
A: 可通过 MCP 协议扩展，或参考 `internal/tools/` 目录添加内置工具。

更多问题请查看 [Issues](https://github.com/tiancheng92/Aiko/issues) 或创建新的讨论。

## 🔒 隐私与安全

- **本地数据存储**：所有聊天记录和配置均保存在本地 `~/.aiko/` 目录
- **API 密钥安全**：配置信息加密存储，不会上传到服务器
- **网络连接**：仅在AI对话和工具调用时连接外部API
- **开源透明**：所有代码公开，可审计安全性

## 📄 开源协议

本项目基于 MIT 协议开源。详见 [LICENSE](LICENSE) 文件。

## 🙏 致谢

特别感谢：
- 字节跳动 eino 团队提供的 Agent 开发框架
- Wails 团队打造的优秀跨平台框架  
- Live2D 团队的角色渲染技术支持
- 所有贡献代码和建议的开发者们

## 🤝 贡献者

感谢所有为 Aiko 项目做出贡献的开发者们！

<!-- 贡献者列表将自动更新 -->

如果你想加入贡献者行列，请阅读我们的 [贡献指南](CONTRIBUTING.md)。

---

<div align="center">

**如果这个项目对你有帮助，请给一个 ⭐ Star 支持一下！**

[报告问题](https://github.com/tiancheng92/Aiko/issues) • 
[功能建议](https://github.com/tiancheng92/Aiko/issues/new) • 
[参与贡献](CONTRIBUTING.md)

</div>