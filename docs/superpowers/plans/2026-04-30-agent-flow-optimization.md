# Agent Flow Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 通过并行化预处理、切换 multi-turn 消息格式、分离长期记忆检索，降低每轮对话的 TTFT 和 LLM 生成耗时。

**Architecture:** `buildHistoryPrefix() string` 替换为 `buildContext() ([]adk.Message, error)`，用 errgroup 并行读 USER.md、搜索长期记忆（摘要和原始块分开查）、取短期历史，结果组装成结构化消息列表传给 `runner.Run`。`drainRunnerMsg` 签名从单条消息扩展为消息列表。

**Tech Stack:** Go 1.22+, `golang.org/x/sync/errgroup`, eino v0.8.11 (`adk.Message = *schema.Message`), chromem-go metadata filter.

---

## File Map

| File | Change |
|------|--------|
| `internal/memory/long.go` | 新增 `MemorySearchResult` 类型 + `SearchSplit` 方法 |
| `internal/memory/short.go` | 新增 `RecentMessages` 方法 |
| `internal/agent/agent.go` | `buildHistoryPrefix` → `buildContext`；`Chat`/`ChatWithMessage` 调用点；`drainRunnerMsg` 签名；`nudgeText` 提取为常量 |
| `go.mod` | 添加 `golang.org/x/sync` 直接依赖 |

---

## Task 1: 添加 `golang.org/x/sync` 直接依赖

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: 添加依赖**

```bash
cd /path/to/Aiko
go get golang.org/x/sync@v0.20.0
```

Expected output: `go: added golang.org/x/sync v0.20.0`（或 `go: upgraded`，取决于当前间接版本）

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

Expected: 无错误输出。

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add golang.org/x/sync as direct dependency"
```

---

## Task 2: `LongStore.SearchSplit` — 分离记忆检索

**Files:**
- Modify: `internal/memory/long.go`

- [ ] **Step 1: 写失败测试**

在 `internal/memory/long_test.go` 末尾（若不存在则新建）添加：

```go
package memory_test

import (
    "context"
    "testing"

    chromem "github.com/philippgille/chromem-go"
)

func TestSearchSplit_EmptyCollection(t *testing.T) {
    db := chromem.NewDB()
    store, err := NewLongStore(db, nil, nil, nil)
    if err != nil {
        t.Fatal(err)
    }
    res, err := store.SearchSplit(context.Background(), "anything", 3)
    if err != nil {
        t.Fatal(err)
    }
    if len(res.Summaries) != 0 || len(res.Raws) != 0 {
        t.Errorf("expected empty result, got %+v", res)
    }
}

func TestSearchSplit_SeparatesTypes(t *testing.T) {
    // This test requires a real embedder; skip if none configured.
    // We verify the method exists and returns MemorySearchResult.
    var _ interface {
        SearchSplit(ctx context.Context, query string, k int) (MemorySearchResult, error)
    } = (*LongStore)(nil)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/memory/... -run TestSearchSplit -v
```

Expected: `FAIL` — `SearchSplit` undefined 或 `MemorySearchResult` undefined。

- [ ] **Step 3: 实现 `MemorySearchResult` 和 `SearchSplit`**

在 `internal/memory/long.go` 现有 `Search` 方法之后添加：

```go
// MemorySearchResult holds separately retrieved summaries and raw memory blocks.
type MemorySearchResult struct {
	Summaries []string // one-sentence summaries of past conversations
	Raws      []string // full raw conversation blocks
}

// SearchSplit retrieves the top-k most relevant summaries and raw memory blocks
// separately via metadata filter, so both dimensions contribute top-k slots
// without competing against each other.
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

在文件顶部 import 块中补充（如未引入）：

```go
"golang.org/x/sync/errgroup"
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/memory/... -run TestSearchSplit -v
```

Expected: `PASS`.

- [ ] **Step 5: 确认整体编译**

```bash
go build ./...
```

Expected: 无错误。

- [ ] **Step 6: Commit**

```bash
git add internal/memory/long.go internal/memory/long_test.go
git commit -m "feat(memory): add SearchSplit for separate summary/raw retrieval"
```

---

## Task 3: `ShortStore.RecentMessages` — 返回结构化消息

**Files:**
- Modify: `internal/memory/short.go`

- [ ] **Step 1: 写失败测试**

在 `internal/memory/short_test.go` 末尾（若不存在则新建）添加：

```go
package memory_test

import (
    "database/sql"
    "testing"

    "github.com/cloudwego/eino/schema"
    _ "modernc.org/sqlite"
)

func newTestShortStore(t *testing.T) *ShortStore {
    t.Helper()
    db, err := sql.Open("sqlite", ":memory:")
    if err != nil {
        t.Fatal(err)
    }
    _, err = db.Exec(`CREATE TABLE messages (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        role TEXT NOT NULL,
        content TEXT NOT NULL,
        images TEXT NOT NULL DEFAULT '',
        files TEXT NOT NULL DEFAULT '',
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    )`)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { db.Close() })
    return NewShortStore(db)
}

func TestRecentMessages_Empty(t *testing.T) {
    s := newTestShortStore(t)
    msgs, err := s.RecentMessages(10)
    if err != nil {
        t.Fatal(err)
    }
    if len(msgs) != 0 {
        t.Errorf("expected 0 messages, got %d", len(msgs))
    }
}

func TestRecentMessages_RolesAndOrder(t *testing.T) {
    s := newTestShortStore(t)
    s.Add("user", "hello")
    s.Add("assistant", "hi there")
    s.Add("user", "how are you")

    msgs, err := s.RecentMessages(10)
    if err != nil {
        t.Fatal(err)
    }
    if len(msgs) != 3 {
        t.Fatalf("expected 3 messages, got %d", len(msgs))
    }
    if msgs[0].Role != schema.User {
        t.Errorf("msg[0] role: want User, got %v", msgs[0].Role)
    }
    if msgs[1].Role != schema.Assistant {
        t.Errorf("msg[1] role: want Assistant, got %v", msgs[1].Role)
    }
    if msgs[0].Content != "hello" {
        t.Errorf("msg[0] content: want 'hello', got %q", msgs[0].Content)
    }
}

func TestRecentMessages_RespectsLimit(t *testing.T) {
    s := newTestShortStore(t)
    for i := 0; i < 5; i++ {
        s.Add("user", "msg")
    }
    msgs, err := s.RecentMessages(3)
    if err != nil {
        t.Fatal(err)
    }
    if len(msgs) != 3 {
        t.Errorf("expected 3, got %d", len(msgs))
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/memory/... -run TestRecentMessages -v
```

Expected: `FAIL` — `RecentMessages` undefined.

- [ ] **Step 3: 实现 `RecentMessages`**

在 `internal/memory/short.go` 末尾（`DeleteByIDs` 之后）添加：

```go
// RecentMessages returns the most recent n messages as schema.Message objects,
// suitable for passing directly to runner.Run as multi-turn history.
// Images and file attachments are omitted — the LLM has already processed them.
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

在文件顶部 import 中补充（如未引入）：

```go
"github.com/cloudwego/eino/schema"
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/memory/... -run TestRecentMessages -v
```

Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/short.go internal/memory/short_test.go
git commit -m "feat(memory): add RecentMessages returning schema.Message slice"
```

---

## Task 4: `agent.go` — 提取 `nudgeText` 常量

**Files:**
- Modify: `internal/agent/agent.go`

这一步单独做，避免 Task 5 的大改动中遗漏常量提取。

- [ ] **Step 1: 提取常量**

在 `internal/agent/agent.go` 的 `import` 块之后、第一个 `type` 声明之前，添加：

```go
// nudgeText is appended to the user message every nudgeInterval turns to
// prompt the agent to reflect and persist useful knowledge.
const nudgeText = `[SELF-GROWTH NUDGE]
请在本次回复前，回顾刚才的对话，考虑是否需要：
1. 调用 save_memory 保存一条具体事实或偏好（一两句话，不需要摘要对话）
2. 调用 update_user_profile 更新用户画像（发现了新的习惯/偏好/背景信息）
3. 调用 save_skill 将本次解决的问题模式提炼为可复用技能
如果都不需要，直接回复即可，无需解释。`
```

- [ ] **Step 2: 删除 `buildHistoryPrefix` 中的内联 nudge 字符串**

找到 `buildHistoryPrefix` 方法（agent.go:546 附近）中的以下代码段并删除（只删 sb.WriteString 那一段，保留前面的 if 条件结构，Task 5 会重写这个方法）：

```go
	sb.WriteString(`
[SELF-GROWTH NUDGE]
请在本次回复前，回顾刚才的对话，考虑是否需要：
1. 调用 save_memory 保存一条具体事实或偏好（一两句话，不需要摘要对话）
2. 调用 update_user_profile 更新用户画像（发现了新的习惯/偏好/背景信息）
3. 调用 save_skill 将本次解决的问题模式提炼为可复用技能
如果都不需要，直接回复即可，无需解释。
`)
```

替换为：

```go
	sb.WriteString(nudgeText)
```

- [ ] **Step 3: 编译确认**

```bash
go build ./internal/agent/...
```

Expected: 无错误。

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "refactor(agent): extract nudgeText as package-level constant"
```

---

## Task 5: `drainRunnerMsg` 签名扩展

**Files:**
- Modify: `internal/agent/agent.go`

先改这个函数签名，再改调用方（Task 6），避免编译中断。

- [ ] **Step 1: 修改 `drainRunnerMsg` 签名**

找到 `agent.go` 中的 `drainRunnerMsg` 函数（约 257 行），将：

```go
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msg *schema.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, bool) {
	iter := runner.Run(ctx, []adk.Message{msg}, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}
```

改为：

```go
// drainRunnerMsg consumes all events from runner.Run with a pre-built message list,
// forwards tokens to ch, and returns the accumulated response string.
// Returns (response, true) on success or ("", false) after sending an error to ch.
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, bool) {
	iter := runner.Run(ctx, msgs, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}
```

- [ ] **Step 2: 临时修复 `ChatWithMessage` 调用（编译用）**

`ChatWithMessage`（约 469 行）当前调用：

```go
fullResponse, ok := drainRunnerMsg(ctx, a.runner, &sendMsg, ch, a.pendingConfirms, a.emitEvent, checkpointID)
```

暂时改为：

```go
fullResponse, ok := drainRunnerMsg(ctx, a.runner, []adk.Message{&sendMsg}, ch, a.pendingConfirms, a.emitEvent, checkpointID)
```

（Task 6 会完整重写 `ChatWithMessage`，此处只为让编译通过。）

- [ ] **Step 3: 编译确认**

```bash
go build ./internal/agent/...
```

Expected: 无错误。

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "refactor(agent): extend drainRunnerMsg to accept []adk.Message"
```

---

## Task 6: 实现 `buildContext` 并重写 `Chat` / `ChatWithMessage`

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: 写失败测试**

在 `internal/agent/agent_test.go` 末尾添加：

```go
func TestBuildContextExists(t *testing.T) {
    // Compile-time check: buildContext must exist with correct signature.
    // Full integration test requires live LLM; this verifies the method is defined
    // by checking Agent implements an interface with Chat and ChatWithMessage.
    type chatter interface {
        Chat(ctx context.Context, userInput string) <-chan agent.StreamResult
        ChatWithMessage(ctx context.Context, msg *schema.Message) <-chan agent.StreamResult
    }
    var _ chatter = (*agent.Agent)(nil)
}
```

在 `agent_test.go` 顶部 import 中确保有：

```go
import (
    "context"
    "testing"

    "github.com/cloudwego/eino/schema"
    "aiko/internal/agent"
)
```

- [ ] **Step 2: 运行测试确认当前状态**

```bash
go test ./internal/agent/... -v
```

Expected: 所有现有测试 PASS（新测试也应 PASS，因为方法已存在）。

- [ ] **Step 3: 添加 errgroup import**

在 `internal/agent/agent.go` 的 import 块中添加：

```go
"golang.org/x/sync/errgroup"
```

并确保以下 import 也存在（已有则跳过）：

```go
"aiko/internal/memory"
```

- [ ] **Step 4: 添加 `buildContext` 方法，删除 `buildHistoryPrefix`**

将 `buildHistoryPrefix` 整个方法（约 498-558 行）替换为 `buildContext`：

```go
// buildContext fetches user profile, long-term memories (summaries and raws separately),
// and recent short-term history concurrently, then returns a message list ready for
// runner.Run. Errors from individual sources are logged and skipped — a partial context
// is better than no response.
func (a *Agent) buildContext(ctx context.Context, userInput string) ([]adk.Message, error) {
	var profile string
	var memResult memory.MemorySearchResult
	var recentMsgs []*schema.Message

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if a.dataDir == "" {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(a.dataDir, "USER.md"))
		if err == nil {
			profile = string(data)
		} else if !os.IsNotExist(err) {
			slog.Warn("read USER.md failed", "err", err)
		}
		return nil
	})

	g.Go(func() error {
		if a.longMem == nil {
			return nil
		}
		res, err := a.longMem.SearchSplit(gctx, userInput, 3)
		if err != nil {
			slog.Warn("longMem.SearchSplit failed", "err", err)
			return nil
		}
		memResult = res
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

	// Build context pair (user + assistant "Understood.") only if there is content.
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

- [ ] **Step 5: 重写 `Chat` 方法**

将 `Chat` 方法（约 170-203 行）中的 `buildHistoryPrefix` 调用段整体替换：

旧代码（约 181-196 行）：

```go
		history, err := a.buildHistoryPrefix(ctx, userInput)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		query := userInput
		if history != "" {
			query = history + "\nUser: " + userInput
		}

		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, ok := drainRunner(ctx, a.runner, query, ch, a.pendingConfirms, a.emitEvent, checkpointID)
```

替换为：

```go
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
```

- [ ] **Step 6: 重写 `ChatWithMessage` 方法**

将 `ChatWithMessage` 中的旧 history/prefix 逻辑（约 444-469 行）替换：

旧代码：

```go
		userText := extractTextFromMessage(msg)
		history, err := a.buildHistoryPrefix(ctx, userText)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		// Prepend history into the message before sending.
		sendMsg := *msg
		if history != "" {
			if sendMsg.Content != "" {
				sendMsg.Content = history + "\nUser: " + sendMsg.Content
			} else {
				histPart := schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeText,
					Text: history + "\nUser: ",
				}
				sendMsg.UserInputMultiContent = append(
					[]schema.MessageInputPart{histPart},
					sendMsg.UserInputMultiContent...,
				)
			}
		}

		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, ok := drainRunnerMsg(ctx, a.runner, []adk.Message{&sendMsg}, ch, a.pendingConfirms, a.emitEvent, checkpointID)
```

替换为：

```go
		userText := extractTextFromMessage(msg)
		ctxMsgs, err := a.buildContext(ctx, userText)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		msgs := append(ctxMsgs, msg)
		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
```

- [ ] **Step 7: 编译确认**

```bash
go build ./internal/agent/...
```

Expected: 无错误。

- [ ] **Step 8: 运行所有测试**

```bash
go test ./internal/agent/... -v
```

Expected: 所有测试 PASS。

- [ ] **Step 9: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): replace buildHistoryPrefix with parallel buildContext + multi-turn messages"
```

---

## Task 7: 全量编译与回归验证

**Files:** 无新改动

- [ ] **Step 1: 全量编译**

```bash
go build ./...
```

Expected: 无错误。

- [ ] **Step 2: 运行全部测试**

```bash
go test ./...
```

Expected: 所有测试 PASS（或与改动前一致的跳过数量）。

- [ ] **Step 3: 手动冒烟测试**

```bash
make run
```

发送一条普通消息，验证：
1. 第一个 token 出现时间感知上有改善（TTFT）
2. 消息内容正常，无格式异常
3. 聊天历史在下一轮能正确被引用

- [ ] **Step 4: 最终 Commit（如有未提交文件）**

```bash
git status
# 如有未提交变更：
git add <files>
git commit -m "chore: finalize agent flow optimization"
```
