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

两个主要瓶颈：

1. **串行预处理**：`buildHistoryPrefix` 中三步操作（读文件、向量检索、SQLite 查询）完全独立却顺序执行，增加不必要的等待。
2. **历史记录用文本拼接**：所有短期历史拼成一个大字符串注入当前 user message，导致：
   - 当前 user message 过长 → TTFT 慢
   - LLM 无法利用 multi-turn attention 结构理解对话
   - USER.md / 长期记忆混入 user message → provider 侧 prefix cache 无法稳定命中

---

## 优化方案：方案 C（并行化 + multi-turn 消息格式）

### 整体架构变更

将 `buildHistoryPrefix() string` 替换为 `buildContext() ([]adk.Message, error)`：

**改前时序：**
```
⬜ read USER.md
              ⬜ longMem.Search
                            ⬜ shortMem.Recent
                                          ⬜⬜⬜ runner.Query("拼接字符串")
```

**改后时序：**
```
⬜ read USER.md  ┐
⬜ longMem.Search ┼─ errgroup 并行
⬜ shortMem.Recent┘
              ⬜⬜⬜ runner.Run([messages])
```

### runner.Run 消息结构

```
[system]     ← cfg.SystemPrompt（eino agent 静态配置，不变）
[user]       ← "User Profile:\n{USER.md}\nRelevant memories:\n{longMem}"（有则注入）
[assistant]  ← "Understood."
[user]       ← history[0]   最老的短期历史消息
[assistant]  ← history[1]
...
[user]       ← 当前用户输入（multimodal 场景保留 image parts）
```

USER.md 和长期记忆放到对话最前面的 context pair，不混入当前 user message：
- 当前 user message 只含用户原始问题 → token 最少 → TTFT 最快
- context pair 内容仅在 USER.md 更新或记忆相关时变化 → 前缀较稳定 → 支持 automatic prefix cache 的 provider（DeepSeek、部分 OpenAI 模型）可命中缓存

---

## 具体代码变更

### 1. `internal/memory/short.go` — 新增 `RecentMessages`

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

### 2. `internal/agent/agent.go` — 三处改动

#### ① `buildHistoryPrefix` → `buildContext`

```go
// buildContext fetches user profile, long-term memories, and recent history
// concurrently and returns a context message list ready for runner.Run.
func (a *Agent) buildContext(ctx context.Context, userInput string) ([]adk.Message, error) {
    var profile string
    var longMemResults []string
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
            res, err := a.longMem.Search(gctx, userInput, 3)
            if err == nil {
                longMemResults = res
            } else {
                slog.Warn("longMem.Search failed", "err", err)
            }
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

    // Inject context pair (USER.md + long-term memories) if present.
    var ctxBuf strings.Builder
    if profile != "" {
        ctxBuf.WriteString("User Profile:\n")
        ctxBuf.WriteString(profile)
    }
    if len(longMemResults) > 0 {
        ctxBuf.WriteString("\nRelevant past context:\n")
        for _, r := range longMemResults {
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
func (a *Agent) Chat(ctx context.Context, userInput string) <-chan StreamResult {
    ch := make(chan StreamResult, 64)
    go func() {
        defer close(ch)
        // ...
        ctxMsgs, err := a.buildContext(ctx, userInput)
        if err != nil {
            ch <- StreamResult{Err: err}
            return
        }

        content := userInput
        if a.nudgeInterval > 0 && a.turnCount.Load() > 0 &&
            a.turnCount.Load()%int64(a.nudgeInterval) == 0 {
            content += "\n\n" + nudgeText
        }

        msgs := append(ctxMsgs, &schema.Message{Role: schema.User, Content: content})
        checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
        fullResponse, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
        // ...
    }()
    return ch
}
```

#### ③ `ChatWithMessage` 方法 — multimodal，image message 追加到历史末尾

```go
ctxMsgs, err := a.buildContext(ctx, userText)
// ...
msgs := append(ctxMsgs, sendMsg) // sendMsg 是带 image parts 的 *schema.Message
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
| `longMem.Search` 失败 | warn log，longMemResults 保持空，不阻断流程 |
| `USER.md` 不存在 | 静默跳过（`os.IsNotExist` 判断），与原来一致 |
| nudge 时机 | 不变：turnCount 在异步 persistAndMigrate 中 +1，nudge 在下一轮触发 |
| 消息列表结构合法性 | shortMem.Recent 返回完整轮次（偶数条），追加当前 user → 始终以 user 结尾 |
| `ChatDirect` / `ChatDirectCollect` | 继续用 `runner.Query(ctx, prompt)`，无 memory 注入，不受影响 |

---

## 不在此次范围内

- `persistAndMigrate` 内的 `shortMem.Count()` 优化（已是异步，不影响用户感知）
- Summarizer LLM 调用并发问题（已是异步 goroutine，优先级较低）
- Provider 侧显式 prompt caching 配置（依赖具体 provider，automatic caching 已由消息结构改善触发）

---

## 改动文件汇总

| 文件 | 变更类型 |
|------|---------|
| `internal/memory/short.go` | 新增 `RecentMessages` 方法 |
| `internal/agent/agent.go` | `buildHistoryPrefix` → `buildContext`；`Chat` / `ChatWithMessage` 调用点；`drainRunnerMsg` 签名 |
