# 代码质量提升设计

**日期：** 2026-05-17  
**范围：** 全库 Go 代码  
**目标：** 通过 5 个独立子项目，系统性提升 Aiko 项目的代码质量——包括文档、错误可观测性、函数可读性、测试覆盖。

---

## 背景

经过前 4 个子项目的 `app.go` 拆分和重构，核心架构已清晰。当前代码质量问题集中在以下几类：

- **25+ 处被忽略的错误**（`_ =` 模式）：静默吞掉错误，调试困难
- **超长函数**：`drainIterInner`（126 行）、`buildContext`（95 行）、`New`（90 行）认知复杂度高
- **缺少 doc comment**：`internal/memory`、`internal/config`、`internal/mcp` 的 exported 标识符
- **魔法数字**：buffer size `64` 在 `agent.go` 中硬编码 3 次
- **测试覆盖严重不足**：`scheduler`（0 测试）、`mcp/client`（0 测试）、`agent`（19 行测试）、`memory/long`（32 行测试）

---

## 子项目一览

| # | 子项目 | 变更性质 | 主要文件 |
|---|--------|---------|---------|
| 1 | Doc comment + 常量 + panic fix | 零行为变更 | `main.go`、`agent.go`、`internal/memory/*`、`internal/config/*`、`internal/mcp/client.go` |
| 2 | 错误处理 | 低风险行为变更（错误变可见） | `app.go`、`agent.go`、`internal/config/profile.go`、`internal/tools/*`、`internal/mcp/client.go` |
| 3 | 函数重构 | 等价重构（无行为变更） | `internal/agent/agent.go` |
| 4 | 测试：scheduler + mcp/client | 纯新增 | `internal/scheduler/scheduler_test.go`、`internal/mcp/client_test.go` |
| 5 | 测试：agent + memory/long | 纯新增 | `internal/agent/agent_test.go`、`internal/memory/long_test.go` |

---

## 子项目 1：Doc Comment + 常量提取 + panic fix

### 目标文件

- `main.go`
- `internal/agent/agent.go`
- `internal/memory/long.go`、`short.go`
- `internal/config/config.go`
- `internal/mcp/client.go`

### 具体改动

**`main.go`**
- 将 `panic(err)` 替换为 `slog.Error(...); os.Exit(1)`（应用初始化失败不应 panic）

**`internal/agent/agent.go`**
- 新增包级常量 `const streamResultBufSize = 64`
- 替换 3 处硬编码 `64`（make channel 的 buffer size）

**Doc comment 补齐（exported 类型/函数）**

| 文件 | 缺失标识符 |
|------|-----------|
| `internal/memory/long.go` | `LongStore`、`Search`、`SearchSplit` |
| `internal/memory/short.go` | `ShortStore` 及主要方法 |
| `internal/config/config.go` | `Store` struct |
| `internal/mcp/client.go` | `LoadToolsAsync` 及关联 exported 函数 |

### 验证

```bash
go build ./...
go vet ./...
```

---

## 子项目 2：错误处理

### 原则

- **可观测的**（副作用操作，如 DB 写、资源关闭）→ `slog.Warn("context", "err", err)`
- **可传播的**（影响调用方结果的操作）→ 返回 `error`
- **真正可忽略的**（已有 fallback 路径，忽略是设计意图）→ 保留 `_`，加注释说明原因

### 具体改动

| 文件 | 位置 | 当前 | 改后 |
|------|------|------|------|
| `app.go` | 启动时 `permStore.EnsureRow` | `_ = ...` | `slog.Warn("permStore: ensure row", "tool", name, "err", err)` |
| `app.go` | `sqlDB.Exec` 删除 | `_, _ = ...` | `slog.Warn("db: exec", "err", err)` |
| `internal/agent/agent.go` | `shortMem.AddWithImagesAndFiles` / `AddFull`（2 处） | `_, _ = ...` | `slog.Warn("short memory: add", "err", err)` |
| `internal/config/profile.go` | `res.LastInsertId()` | `p.ID, _ = ...` | 检查 `err`，通过 `fmt.Errorf` 包装返回 |
| `internal/tools/dev_tools.go` | JSON unmarshal fallback | `_ = json.Unmarshal(...)` | 加注释：`// best-effort: fall back to string split if JSON parse fails` |
| `internal/tools/weather_tools.go` | JSON unmarshal | `_ = json.Unmarshal(...)` | `slog.Warn("weather: unmarshal response", "err", err)` |
| `internal/tools/tool.go` | JSON unmarshal | `_ = json.Unmarshal(...)` | `slog.Warn("tool: unmarshal args", "err", err)` |
| `internal/mcp/client.go` | `client.Close()`（3 处） | `_ = client.Close()` | `slog.Warn("mcp: close client", "err", err)` |

### 约束

- 不改任何 exported 函数签名（`profile.go` 内部 unexported 函数除外）
- 不引入新的业务逻辑

### 验证

```bash
go build ./...
go vet ./...
```

---

## 子项目 3：函数重构

**范围：** `internal/agent/agent.go` 的 3 个超长函数。所有新函数均为私有，不暴露任何新 API，行为完全等价。

### `drainIterInner()`（126 行 → ~35 行）

提取 4 个私有辅助函数：

| 新函数 | 职责 | 目标行数 |
|--------|------|---------|
| `handleStreamToken(ep *EmotionParser, token string, emit func(string,any))` | token 经 EmotionParser 分离 emotion/text，emit 对应事件 | ~20 行 |
| `handleThinkingToken(token string, emit func(string,any))` | emit `chat:thinking` 事件 | ~10 行 |
| `handleStreamImages(images []string, emit func(string,any))` | emit `chat:image` 事件 | ~10 行 |
| `handleStreamInterrupt(ctx context.Context, ictxs []InterruptContext, ...)` | 调用现有 `handleInterrupt` | ~15 行 |

`drainIterInner` 保留主循环框架，调用上述辅助函数。

### `buildContext()`（95 行 → ~30 行）

提取 1 个私有辅助函数：

| 新函数 | 职责 | 目标行数 |
|--------|------|---------|
| `fetchContextSources(ctx context.Context, query string) (location, knowledge, memory string, err error)` | 用 errgroup 并行拉取 location/knowledge/memory，返回结果 | ~55 行 |

`buildContext` 保留组装逻辑，调用 `fetchContextSources`。

### `New()`（90 行 → ~30 行）

提取 1 个私有辅助函数：

| 新函数 | 职责 | 目标行数 |
|--------|------|---------|
| `initAgentRunner(ctx context.Context, chatModel, tools, cfg, ...) (*runner, error)` | 初始化 eino runner（工具注册、graph 构建） | ~50 行 |

`New` 保留结构体组装逻辑，调用 `initAgentRunner`。

### 验证

```bash
go build ./...
go vet ./...
```

---

## 子项目 4：测试 — scheduler + mcp/client

### `internal/scheduler/scheduler_test.go`（新建）

**测试策略：** in-memory SQLite；`chatFn` 用 stub（返回固定字符串）；`onResult` 用 channel 捕获结果。

| 测试用例 | 覆盖内容 |
|----------|---------|
| `TestNewScheduler` | 初始化成功，空 job 列表 |
| `TestCreateAndListJobs` | 创建 job 后 List 能查到 |
| `TestUpdateJob` | 更新后持久化 |
| `TestDeleteJob` | 删除后 List 不再返回 |
| `TestSetJobEnabled` | 启用/禁用状态正确切换 |
| `TestRunJobNow` | 手动触发：chatFn 被调用，onResult 收到结果 |
| `TestJobDisabledSkipsCron` | disabled job 不触发 chatFn |
| `TestConcurrentJobRun` | 并发触发不 panic、不 data race |

### `internal/mcp/client_test.go`（新建）

**测试策略：** mock `transport.ClientTransport` 接口（若存在）或用 `httptest` 搭建 stub SSE server。

| 测试用例 | 覆盖内容 |
|----------|---------|
| `TestLoadToolsAsync_Success` | stub server 返回工具列表，回调收到正确 tools |
| `TestLoadToolsAsync_Timeout` | server 无响应，30s 后回调收到 error |
| `TestLoadToolsAsync_CloseOnCancel` | ctx cancel 后 client 正确关闭，无 goroutine 泄漏 |
| `TestNewClientError` | 连接失败返回 error，不 panic |

### 验证

```bash
go test ./internal/scheduler/... -race -count=1
go test ./internal/mcp/... -race -count=1
```

---

## 子项目 5：测试 — agent + memory/long

### `internal/agent/agent_test.go`（扩充）

**测试策略：** stub `model.ToolCallingChatModel`（返回固定 token 序列）；shortMem 用 in-memory SQLite；longMem/knowledgeSt 用 nil（测试路径不触达）。

| 测试用例 | 覆盖内容 |
|----------|---------|
| `TestNew_MissingModel` | chatModel 为 nil 时返回 error |
| `TestChatDirect_AgentNotReady` | petAgent 未初始化时返回 error |
| `TestStreamChat_CancelInFlight` | 第二条消息正确 cancel 第一条 |
| `TestHandleStreamToken_EmotionParsing` | token 经 EmotionParser 正确分离 emotion/text |
| `TestHandleStreamInterrupt` | interrupt 信号触发 pendingConfirms 写入 |
| `TestBuildContext_LocationCache` | 30 分钟内重复调用命中缓存，不重复 fetch |
| `TestBuildContext_KnowledgeSearch` | knowledge search 结果正确注入 context |
| `TestConcurrentChat` | 并发 Chat 不 data race |

### `internal/memory/long_test.go`（扩充）

**测试策略：** `t.TempDir()` 存放 chromem-go 向量数据；embedder 用 stub（返回固定维度随机向量）。

| 测试用例 | 覆盖内容 |
|----------|---------|
| `TestStore_RoundTrip` | Store 后 Search 能找到 |
| `TestStore_MultipleEntries` | 多条记录，Search 返回最相关的 |
| `TestSearch_EmptyStore` | 空库 Search 返回空列表，不 error |
| `TestSearchSplit_ReturnsChunks` | 长文档分块后 Search 正确返回片段 |
| `TestStore_PersistAcrossReopen` | 关闭重新打开后数据仍可查询 |
| `TestConcurrentStore` | 并发写入不 data race |

### 验证

```bash
go test ./internal/agent/... -race -count=1
go test ./internal/memory/... -race -count=1
```

---

## 约束（全局）

- 不改任何 Wails exported 方法签名（不影响前端 binding）
- 不新增 public API
- 每个子项目独立提交，`go build ./...` 必须在每次提交后通过
- 测试文件不依赖 Wails runtime（用 stub/mock 替代）

## 测试策略

- 无新增集成测试（避免依赖 macOS 专属 API）
- 所有新测试用 `-race` flag 验证并发安全
- stub/mock 只实现测试所需的最小接口子集
