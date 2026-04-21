# Desktop Pet

一个运行在 macOS 桌面上的 AI 宠物，基于 Wails + Vue 3 + Live2D 构建。

## 功能

- **Live2D 角色** — 可拖拽的桌面宠物，支持多模型切换和表情动画
- **AI 对话** — 基于 OpenAI 兼容接口的流式聊天，支持 Markdown 渲染和代码高亮
- **工具调用** — 内置时间、系统信息、网络状态等工具，支持自定义 YAML 技能扩展
- **长期记忆** — 向量数据库（chromem-go）存储对话摘要，语义检索增强上下文
- **知识库** — 导入 txt/md/pdf/epub 文档，RAG 问答
- **AI 中间件** — 日志 → 重试 → 错误恢复拦截器链，保障工具调用健壮性
- **设置界面** — 拖拽式浮动窗口，支持模型选择、工具权限管理、知识库管理
- **点击穿透** — macOS 原生透明窗口，鼠标悬停在宠物/对话框上才响应事件

## 技术栈

| 层 | 技术 |
|---|---|
| 框架 | [Wails v2](https://wails.io) |
| 后端 | Go 1.25 |
| 前端 | Vue 3 + Vite（`<script setup>`） |
| LLM | [eino](https://github.com/cloudwego/eino)（OpenAI 兼容） |
| 向量库 | [chromem-go](https://github.com/philippgille/chromem-go) |
| 数据库 | SQLite（modernc） |
| Live2D | pixi-live2d-display 0.4 + PixiJS 6 |

## 快速开始

### 前置条件

- Go 1.21+
- Node.js 18+ + Yarn
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- macOS（点击穿透依赖 macOS Objective-C 实现）

### 开发模式

```bash
wails dev
```

前端热重载地址：`http://localhost:34115`

### 构建

```bash
wails build
```

产物位于 `build/bin/`。

## 配置

首次启动后右键宠物 → **打开设置**，填写以下字段：

| 字段 | 说明 | 必填 |
|---|---|---|
| Base URL | OpenAI 兼容端点，如 `http://localhost:11434/v1` | ✅ |
| API Key | API 密钥（本地模型可留空） | — |
| Model | 聊天模型名，可点击"获取模型"从接口拉取列表 | ✅ |
| Embedding Model | 向量模型名（启用长期记忆/知识库需要） | — |
| Embedding 维度 | 向量维度，默认 1536 | — |
| System Prompt | 宠物人格提示词 | — |
| Skills 目录 | 自定义工具 YAML 目录 | — |

数据存储于 `~/.desktop-pet/`。

## 自定义技能

在 Skills 目录下创建 YAML 文件即可添加自定义工具：

```yaml
name: weather
description: 查询指定城市的天气
parameters:
  city:
    type: string
    description: 城市名称
    required: true
command: curl -s "wttr.in/{{.city}}?format=3"
```

## 项目结构

```
.
├── app.go                  # Wails 绑定层（所有前端可调用方法）
├── main.go                 # 应用入口，窗口配置
├── macos.go                # macOS 点击穿透（Objective-C cgo）
├── internal/
│   ├── agent/              # AI Agent（eino ReAct）
│   │   └── middleware/     # 日志 / 重试 / 错误恢复中间件
│   ├── config/             # 配置读写（SQLite）
│   ├── db/                 # 数据库初始化与迁移
│   ├── knowledge/          # 知识库导入与 RAG 检索
│   ├── llm/                # ChatModel / Embedder 工厂
│   ├── memory/             # 短期（SQLite）/ 长期（向量）记忆
│   ├── skill/              # YAML 技能加载器
│   └── tools/              # 内置工具（时间、系统、网络）
└── frontend/
    └── src/
        ├── components/     # Vue 组件
        └── composables/    # 可复用逻辑（useModelPath, usePetState）
```
