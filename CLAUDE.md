# Desktop Pet — CLAUDE.md

AI 编码助手指引，供 Claude Code 在此项目中使用。

## 项目概览

Wails v2 桌面应用，macOS 专属。Go 后端通过 `app.go` 暴露绑定方法给 Vue 3 前端；前端 embed 进 Go 二进制。点击穿透通过 `macos.go`（Objective-C cgo）实现。

## 架构

```
main.go → app.go (Wails bindings)
              ↓
    internal/agent/agent.go        # eino ReAct Agent
    internal/agent/middleware/     # 日志/重试/错误恢复
    internal/tools/                # 内置工具 + 权限管理
    internal/skill/                # YAML 自定义技能
    internal/memory/               # 短期(SQLite) + 长期(chromem)
    internal/knowledge/            # RAG 知识库
    internal/llm/                  # ChatModel / Embedder 工厂
    internal/config/               # 配置持久化(SQLite)
    internal/db/                   # Schema 迁移
```

前端：`frontend/src/components/` + `frontend/src/composables/`

## Go 规范

- 所有导出函数必须有 `// FuncName ...` doc comment
- 错误用 `fmt.Errorf("context: %w", err)` 包装
- 涉及 `a.petAgent` / `a.longMem` / `a.knowledgeSt` 的字段读写必须持有 `a.mu`（`RLock` 读，`Lock` 写）
- 新增 Wails 绑定方法写在 `app.go`，签名遵循已有模式
- 新增内置工具：实现 `internaltools.Tool` 接口，在 `registry.go` 的 `All()` 中注册

## Vue 规范

- 全部使用 `<script setup>` 语法
- 包管理用 `yarn`，不用 npm
- 调用后端方法从 `../../wailsjs/go/main/App` import
- 监听 Wails 事件用 `EventsOn`，emit 用 `EventsEmit`（from `../../wailsjs/runtime/runtime`）
- 组件内不直接操作全局状态，通过 composables 共享逻辑

## 关键 Wails 事件

| 事件 | 方向 | 含义 |
|---|---|---|
| `chat:token` | backend→frontend | 流式 token |
| `chat:done` | backend→frontend | 响应结束 |
| `chat:error` | backend→frontend | 错误信息 |
| `chat:clear` | frontend→frontend | 清空历史 |
| `bubble:toggle` | any | 切换聊天气泡 |
| `pet:state:change` | any | 宠物状态（idle/thinking/speaking/error） |
| `knowledge:progress` | backend→frontend | 知识库导入进度 |
| `config:model:changed` | frontend→frontend | Live2D 模型切换 |

## 开发命令

```bash
wails dev          # 开发模式（前端热重载）
wails build        # 构建 .app
go build ./...     # 仅检查 Go 编译
cd frontend && yarn build   # 仅构建前端
```

## 数据目录

`~/.desktop-pet/`
- `pet.db` — SQLite（settings、messages、knowledge_sources）
- `vectors/` — chromem-go 持久化向量

## 注意事项

- `macos.go` 中的 Objective-C 代码负责按像素判断是否响应鼠标事件，**不要随意修改 hitTest 逻辑**，容易破坏点击穿透
- 修改 `internal/tools/registry.go` 的 `All()` 后需同步更新 `AllEino()`（返回 eino Tool 接口列表）
- `app.go` 的 `initLLMComponents` 可能并发调用（SaveConfig 触发），所有字段更新必须在 `a.mu.Lock()` 内完成
- 前端 Wails bindings（`wailsjs/`）由 `wails dev/build` 自动生成，不要手动编辑
