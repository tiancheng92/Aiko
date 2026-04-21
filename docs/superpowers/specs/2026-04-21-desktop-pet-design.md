# Desktop Pet — 第一期设计文档

日期：2026-04-21

## 1. 概述

一个运行在 macOS 上的桌面宠物应用，以极简悬浮球形式常驻桌面，点击后弹出聊天气泡，用户可与之对话。后端对接本地大模型（OpenAI 协议兼容），具备短期记忆、长期语义记忆和可扩展的知识库与 AI skill 系统。

记忆系统设计参考 [mempalace](https://github.com/mempalace/mempalace) 的实现理念：原样存储对话内容（不做摘要压缩），通过向量语义检索还原上下文。

## 2. 技术栈

| 层次 | 技术 |
|------|------|
| 应用框架 | Wails v2 |
| 前端 | Vue 3 + `<script setup>` |
| LLM 框架 | cloudwego/eino + eino-ext（OpenAI-compatible） |
| 短期记忆 / 设置 | SQLite（modernc.org/sqlite，纯 Go） |
| 长期记忆 / 知识库 | chromem-go（纯 Go 嵌入式向量库，无外部服务，数据持久化到本地文件） |

## 3. 窗口设计

### 3.1 悬浮球窗口
- 透明无边框，始终置顶（`AlwaysOnTop: true`）
- 可拖拽移动，位置持久化到 SQLite
- 默认位置：屏幕右下角（距边缘各 24px）
- 点击切换聊天气泡弹出 / 收起
- 大小随屏幕自适应：`ballSize = screenHeight * 0.055`，最小 48px，最大 80px
- 支持全局快捷键（默认 `Cmd+Shift+P`，可在设置中修改）快速打开 / 收起聊天窗口

### 3.2 聊天气泡窗口
- 无边框，紧贴悬浮球弹出；弹出方向朝向屏幕中心（避免超出屏幕边缘）
- 大小随屏幕自适应：`bubbleWidth = screenWidth * 0.22`，`bubbleHeight = screenHeight * 0.55`，宽最小 320px 最大 480px
- 含两个 Tab：**聊天** / **设置**
- 点击悬浮球外区域或再次点击悬浮球（或快捷键）时收起

## 4. 项目目录结构

```
desktop-pet/
├── main.go
├── app.go                  # Wails App 入口，绑定 backend
├── frontend/               # Vue 3 前端
│   └── src/
│       ├── components/
│       │   ├── FloatingBall.vue
│       │   ├── ChatBubble.vue
│       │   └── SettingsPanel.vue
│       └── App.vue
├── internal/
│   ├── agent/              # eino ReAct Agent
│   │   └── agent.go
│   ├── llm/                # eino LLM / Embedding 客户端
│   │   └── client.go
│   ├── memory/
│   │   ├── short.go        # SQLite 短期记忆
│   │   └── long.go         # chromem-go 长期记忆
│   ├── knowledge/
│   │   ├── store.go        # chromem-go knowledge collection
│   │   └── importer.go     # 文件导入（txt/md/PDF/EPUB）
│   ├── skill/
│   │   └── loader.go       # 从目录加载 skill，注册为 eino Tool
│   ├── config/
│   │   └── config.go       # SQLite settings 表读写
│   └── db/
│       └── sqlite.go       # SQLite 连接 / 迁移
└── docs/
```

## 5. 数据存储

### 5.1 SQLite 表结构

```sql
-- 对话短期记忆
CREATE TABLE messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    role       TEXT NOT NULL,   -- 'user' | 'assistant'
    content    TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 全局配置
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL
);
```

**必填配置项（启动时检查）：**
- `llm_base_url`
- `llm_model`

**选填配置项：**
- `llm_api_key`（本地模型可为空）
- `embedding_model`（未配置则向量功能全部跳过）
- `embedding_dim`（默认 `"1536"`，需与 embedding_model 输出维度一致）
- `system_prompt`
- `short_term_limit`（默认 `"30"`）
- `skills_dir`
- `hotkey`（默认 `"Cmd+Shift+P"`）
- `ball_position_x` / `ball_position_y`

### 5.2 chromem-go Collections

数据默认持久化到 `~/.desktop-pet/vectors/`，随 `.app` 打包无需外部服务。

**memories**（长期对话记忆）
```
Collection: "memories"
每条 Document:
  ID       string   -- uuid
  Content  string   -- 原样对话文本块
  Metadata map:
    created_at: unix timestamp string
  Embedding []float32  -- 由 embedding_model 生成
```

**knowledge**（知识库）
```
Collection: "knowledge"
每条 Document:
  ID       string   -- uuid
  Content  string   -- 文档分块文本
  Metadata map:
    source:      文件名
    chunk_index: 块序号 string
  Embedding []float32
```

> chromem-go 在首次初始化时自动创建 collection 目录。若未配置 `embedding_model`，向量功能（长期记忆迁移、知识库导入、语义检索）全部静默跳过，仅保留短期 SQLite 记忆。

## 6. 启动流程

```
程序启动
   │
   ▼
初始化 SQLite → 运行 schema 迁移
   │
   ▼
读取 settings 表，检查必填项
   │
   ├── 有空缺 → 直接打开聊天气泡窗口并切换到设置 Tab
   │            用户填写完成点"保存"后切换到聊天 Tab
   │
   └── 全部就绪 → 初始化 chromem-go（加载本地向量文件）→ 初始化 eino 客户端
                  → 加载 skills → 显示悬浮球（正常模式）
```

## 7. 对话数据流

```
用户发送消息
   │
   ▼
1. 从 chromem-go memories 检索 top-5 相关历史块
2. 从 chromem-go knowledge 检索 top-5 相关知识块
   │
   ▼
3. 组装 eino Messages：
   SystemMessage = system_prompt
                 + "\n\n[相关记忆]\n" + memories
                 + "\n\n[相关知识]\n" + knowledge
   + 最近 N 轮 HumanMessage / AIMessage（从 SQLite）
   + HumanMessage(当前输入)
   │
   ▼
4. eino ReAct Agent.Stream()
   - 可调用内置 Tools
   - 可调用 Skills（AI 子 agent）
   │
   ▼
5. 流式 token → Wails EventEmit → 前端逐字渲染
   │
   ▼
6. 完整响应后写入 SQLite messages（user + assistant）
   │
   ▼
7. 异步：COUNT(messages) > short_term_limit？
   是 → 取最老的超出部分 → 拼接为文本块
      → /v1/embeddings 向量化 → 写入 chromem-go memories
      → 从 SQLite 删除
```

## 8. Skill 系统

### 目录结构
```
{skills_dir}/
└── skill-name/
    └── skill.yaml
```

### skill.yaml 格式
```yaml
name: "example-skill"
description: "该 skill 的功能描述，供主 agent 决策是否调用"
system_prompt: "你是一个专门处理..."
model: ""        # 留空则继承主 agent 的 model
tools: []        # 该 skill 可用的内置工具列表
```

### 加载机制
- 启动时扫描 `skills_dir`，解析所有 `skill.yaml`
- 每个 skill 包装为 eino `InvokableTool`，注册到主 agent
- 调用时实例化子 eino Agent，独立执行后返回结果字符串

## 9. 知识库导入

支持格式：`.txt`、`.md`、`.pdf`、`.epub`

**导入流程：**
1. 设置界面选择文件
2. 解析文本（PDF 用 pdfcpu，EPUB 解压为 zip 后读取 OPF 文件排序各章 HTML，用 golang.org/x/net/html 提取纯文本）
3. 按 512 token 分块，相邻块重叠 64 token
4. 批量调用 `/v1/embeddings`（每批 32 块）
5. 写入 chromem-go `knowledge` collection
6. UI 显示进度（已处理块数 / 总块数）

## 10. 设置界面功能

| 分组 | 配置项 |
|------|--------|
| 模型 | Base URL、API Key、Model 名称、Embedding Model |
| 宠物 | System Prompt（多行文本编辑器） |
| 记忆 | 短期记忆轮数（滑块，1-100，默认 30） |
| 快捷键 | 全局快捷键（默认 Cmd+Shift+P） |
| Skill | Skills 目录路径（可浏览选择） |
| 知识库 | 已导入文件列表、导入按钮、删除按钮 |

## 11. 必要依赖

```
github.com/wailsapp/wails/v2
github.com/cloudwego/eino
github.com/cloudwego/eino-ext
github.com/philippgille/chromem-go
modernc.org/sqlite
github.com/pdfcpu/pdfcpu
golang.org/x/net/html
```
