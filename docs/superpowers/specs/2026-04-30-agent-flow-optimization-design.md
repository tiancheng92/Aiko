# Agent 对话流程优化设计

**日期**: 2026-04-30  
**目标**: 降低每轮对话的 TTFT（首 token 延迟）和整体 LLM 生成耗时

---

## 问题背景

当前每轮对话的关键路径：

```
read USER.md → longMem.Search → shortMem.Recent   (串行)
    → runner.Query("大拼接字符串")                  (单 user message)
    → drainIter (流式消费)
    → persistAndMigrate (异步，不影响用户感知)
```

三个主要问题：

1. **串行预处理**：`buildHistoryPrefix` 中三步操作（读文件、向量检索、SQLite 查询）完全独立却顺序执行。
2. **历史记录用文本拼接**：所有短期历史拼成一个大字符串注入当前 user message，导致：
   - 当前 user message 过长 → TTFT 慢
   - LLM 无法利用 multi-turn attention 结构理解对话
   - USER.md / 长期记忆混入 user message → provider 侧 prefix cache 无法稳定命中
3. **长期记忆混合检索**：摘要向量和原始块向量存在同一 collection，单次查询 top-k 后去重，导致两种维度互相挤占名额，相关摘要可能把相关原始块挤出结果（或反之）。

---

## 优化方案：方案 C（并行化 + multi-turn 消息格式 + 分离记忆检索）

### 整体架构变更

将 `buildHistoryPrefix() string` 替换为 `buildContext() ([]adk.Message, error)`：

**改前时序：**
```
⬜ read USER.md
              ⬜ longMem.Search (混合)
                            ⬜ shortMem.Recent
                                          ⬜⬜⬜ runner.Query("拼接字符串")
```

**改后时序：**
```
⬜ read USER.md          ┐
⬜ longMem.SearchSummaries┼─ errgroup 并行
⬜ longMem.SearchRaws    ┤
⬜ shortMem.RecentMessages┘
              ⬜⬜⬜ runner.Run([messages])
```

### runner.Run 消息结构

```
[system]     ← cfg.SystemPrompt（eino agent 静态配置，不变）
[user]       ← context 块（见下方）
[assistant]  ← "Understood."
[user]       ← history[0]   最老的短期历史消息
[assistant]  ← history[1]
...
[user]       ← 当前用户输入（multimodal 场景保留 image parts）
```

**context 块内容（有则注入，无则跳过整个 context pair）：**
```
User Profile:
{USER.md 内容}

Relevant memory summaries:
- 你上周讨论了 Go 并发模式的优化...
- 你偏好用简洁风格写代码...

Relevant memory details:
user: 帮我优化这段代码...
assistant: 建议用 errgroup 替代 WaitGroup...
```

USER.md 和长期记忆放到对话最前面，不混入当前 user message：
- 当前 user message 只含用户原始问题 → token 最少 → TTFT 最快
- context pair 内容相对稳定 → 支持 automatic prefix cache 的 provider 可命中缓存

---

## 具体代码变更

### 1. `internal/memory/long.go` — 分离检索

新增 `MemorySearchResult` 和 `SearchSplit` 方法，用 chromem-go metadata filter 分别查询：

```go
// MemorySearchResult holds separately retrieved summaries and raw memory blocks.
type MemorySearchResult struct {
    Summaries []string // one-sentence summaries of past conversations
    Raws      []string // full raw conversation blocks
}

// SearchSplit retrieves the top-k most relevant summaries and raw memory blocks
// separately, so both dimensions contribute to the final context without competing
// for the same slots.
func (l *LongStore) SearchSplit(ctx context.Context, query string, k int) (MemorySearchResult, error) {
    l.mu.RLock()
    col := l.col
    l.mu.RUnlock()

    if col.Count() == 0 {
        return MemorySearchResult{}, nil
    }

    var res MemorySearchResult
    var mu sync.Mutex
    g, gctx := errgroup.WithContext(ctx)

    g.Go(func() error {
        results, err := col.Query(gctx, query, k, map[string]string{"type": "summary"}, nil)
        if err != nil {
            return err
        }
        summaries := make([]string, 0, len(results))
        for _, r := range results {
            summaries = append(summaries, r.Content)
        }
        mu.Lock()
        res.Summaries = summaries
        mu.Unlock()
        return nil
    })

    g.Go(func() error {
        results, err := col.Query(gctx, query, k, map[string]string{"type": "raw"}, nil)
        if err != nil {
            return err
        }
        raws := make([]string, 0, len(results))
        for _, r := range results {
            raws = append(raws, r.Content)
        }
        mu.Lock()
        res.Raws = raws
        mu.Unlock()
        return nil
    })

    if err := g.Wait(); err != nil {
        return MemorySearchResult{}, err
    }
    return res, nil
}
```

> 注：`Search`（原方法）保留，供其他调用方兼容。`SearchSplit` 内部两个 chromem-go query 也可并行，借用 errgroup。

### 2. `internal/memory/short.go` — 新增 `RecentMessages`

```go
// RecentMessages returns the most recent n messages as schema.Message objects.
func (s *ShortStore) RecentMessages(n int) ([]*schema.Message, error) {
    msgs, err := s.Recent(n)
    if err != nil {
        return nil, err
    }
    out := make([]*schema.Message, 0, len(msgs))
    for _, m := range msgs {
        role := schema.User
        if m.Role == "assistant" {
            role = schema.Assistant
        }
        out = append(out, &schema.Message{Role: role, Content: m.Content})
    }
    return out, nil
}
```

图片/文件附件无需回传到历史消息（LLM 已处理，历史只需文字轮次）。

### 3. `internal/agent/agent.go` — 四处改动

#### ① `buildHistoryPrefix` → `buildContext`

```go
// buildContext fetches user profile, long-term memories (summaries and raws separately),
// and recent history concurrently, then returns a message list ready for runner.Run.
func (a *Agent) buildContext(ctx context.Context, userInput string) ([]adk.Message, error) {
    var profile string
    var memResult memory.MemorySearchResult
    var recentMsgs []*schema.Message

    g, gctx := errgroup.WithContext(ctx)
    g.Go(func() error {
        if a.dataDir != "" {
            data, err := os.ReadFile(filepath.Join(a.dataDir, "USER.md"))
            if err == nil {
                profile = string(data)
            } else if !os.IsNotExist(err) {
                slog.Warn("read USER.md failed", "err", err)
            }
        }
        return nil
    })
    g.Go(func() error {
        if a.longMem != nil {
            res, err := a.longMem.SearchSplit(gctx, userInput, 3)
            if err != nil {
                slog.Warn("longMem.SearchSplit failed", "err", err)
                return nil
            }
            memResult = res
        }
        return nil
    })
    g.Go(func() error {
        if a.shortMem == nil {
            return nil
        }
        msgs, err := a.shortMem.RecentMessages(a.cfg.ShortTermLimit)
        if err != nil {
            slog.Warn("shortMem.RecentMessages error", "err", err)
            return nil
        }
        recentMsgs = msgs
        return nil
    })
    if err := g.Wait(); err != nil {
        return nil, err
    }

    var msgs []adk.Message

    // Build context block.
    var ctxBuf strings.Builder
    if profile != "" {
        ctxBuf.WriteString("User Profile:\n")
        ctxBuf.WriteString(profile)
    }
    if len(memResult.Summaries) > 0 {
        ctxBuf.WriteString("\nRelevant memory summaries:\n")
        for _, s := range memResult.Summaries {
            ctxBuf.WriteString("- ")
            ctxBuf.WriteString(s)
            ctxBuf.WriteByte('\n')
        }
    }
    if len(memResult.Raws) > 0 {
        ctxBuf.WriteString("\nRelevant memory details:\n")
        for _, r := range memResult.Raws {
            ctxBuf.WriteString(r)
            ctxBuf.WriteByte('\n')
        }
    }
    if ctxBuf.Len() > 0 {
        msgs = append(msgs,
            &schema.Message{Role: schema.User, Content: ctxBuf.String()},
            &schema.Message{Role: schema.Assistant, Content: "Understood."},
        )
    }

    for _, m := range recentMsgs {
        msgs = append(msgs, m)
    }
    return msgs, nil
}
```

#### ② `Chat` 方法 — nudge 注入到当前 user message 尾部

```go
ctxMsgs, err := a.buildContext(ctx, userInput)
// ...
content := userInput
if a.nudgeInterval > 0 && a.turnCount.Load() > 0 &&
    a.turnCount.Load()%int64(a.nudgeInterval) == 0 {
    content += "\n\n" + nudgeText  // nudgeText 提取为包级常量
}
msgs := append(ctxMsgs, &schema.Message{Role: schema.User, Content: content})
checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
fullResponse, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, ...)
```

#### ③ `ChatWithMessage` 方法 — multimodal

```go
ctxMsgs, err := a.buildContext(ctx, userText)
// ...
msgs := append(ctxMsgs, sendMsg) // sendMsg 带 image parts
fullResponse, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, ...)
```

#### ④ `drainRunnerMsg` 签名扩展

```go
// 改前：接收单条 *schema.Message
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msg *schema.Message, ...) (string, bool) {
    iter := runner.Run(ctx, []adk.Message{msg}, ...)
}

// 改后：接收完整消息列表
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ...) (string, bool) {
    iter := runner.Run(ctx, msgs, adk.WithCheckPointID(checkpointID))
    return drainIter(...)
}
```

`drainRunner`（字符串版本）保留，供 `ChatDirect` 使用，不受影响。

---

## 边界情况

| 情况 | 处理方式 |
|------|---------|
| `shortMem` 为 nil | errgroup goroutine 内直接 return nil，recentMsgs 保持空 |
| `longMem.SearchSplit` 失败 | warn log，memResult 保持零值，不阻断流程 |
| collection 内无摘要（旧数据无 summary 向量） | chromem-go 返回空切片，跳过该段注入 |
| `USER.md` 不存在 | 静默跳过（os.IsNotExist 判断），与原来一致 |
| nudge 时机 | 不变：turnCount 在异步 persistAndMigrate 中 +1，nudge 在下一轮触发 |
| nudgeText | 从 buildHistoryPrefix 提取为包级 const |
| 消息列表结构合法性 | shortMem.RecentMessages 返回完整轮次（偶数条），追加当前 user → 始终以 user 结尾 |
| `ChatDirect` / `ChatDirectCollect` | 继续用 `runner.Query(ctx, prompt)`，无 memory 注入，不受影响 |

---

## 不在此次范围内

- `persistAndMigrate` 内的 `shortMem.Count()` 优化（已是异步）
- Summarizer LLM 调用并发问题（已是异步 goroutine）
- Provider 侧显式 prompt caching 配置（automatic caching 已由消息结构改善触发）
- `Search`（原方法）废弃清理（保留兼容，后续迭代处理）

---

## 改动文件汇总

| 文件 | 变更类型 |
|------|---------|
| `internal/memory/long.go` | 新增 `MemorySearchResult` 类型和 `SearchSplit` 方法 |
| `internal/memory/short.go` | 新增 `RecentMessages` 方法 |
| `internal/agent/agent.go` | `buildHistoryPrefix` → `buildContext`；`Chat` / `ChatWithMessage` 调用点；`drainRunnerMsg` 签名；`nudgeText` 提取为常量 |
