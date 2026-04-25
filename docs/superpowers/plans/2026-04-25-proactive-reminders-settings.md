# 提醒事项设置界面 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在设置界面新增「提醒事项」tab，展示待触发的动态提醒列表并支持删除；同步清理技术债：移除晨/晚问候、将 fired 标记改为触发后删除、去掉 fired 字段。

**Architecture:** `proactive_items` 表重建去掉 `fired` 列，变为纯待触发队列；`Store` 接口将 `MarkFired` 替换为 `Delete` 并新增 `List`；`engine.Poll()` 触发后删行（不标记）；`app.go` 新增两个 Wails 绑定；前端 `SettingsWindow.vue` 新增 tab。

**Tech Stack:** Go + SQLite (`modernc.org/sqlite`) + Wails v2 + Vue 3 `<script setup>`

---

### Task 1: DB 迁移 — 重建 proactive_items 表去掉 fired 列

**Files:**
- Modify: `internal/db/sqlite.go`
- Modify: `internal/db/sqlite_test.go`

#### 背景

`migrate()` 函数末尾有一段 `CREATE TABLE IF NOT EXISTS proactive_items`，其中有 `fired BOOLEAN DEFAULT FALSE` 列。需要追加迁移：DROP 旧表，重建无 `fired` 的新表。

现有 `internal/db/sqlite_test.go` 只有一个 `TestMigrateProactiveItems` 测试，测试中向 `proactive_items` 插入行。需要新增一个测试验证 `fired` 列不再存在。

- [ ] **Step 1: 在 `sqlite_test.go` 写失败测试**

```go
// TestMigrateDropsFiredColumn verifies that after migration proactive_items has no fired column.
func TestMigrateDropsFiredColumn(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// Try inserting with fired column — should fail.
	_, err = database.Exec(`INSERT INTO proactive_items (trigger_at, prompt, fired) VALUES ('2099-01-01 00:00:00', 'test', 1)`)
	if err == nil {
		t.Fatal("expected error inserting fired column, but got none — fired column still exists")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /Users/xutiancheng/code/self/Aiko
go test ./internal/db/... -run TestMigrateDropsFiredColumn -v
```

Expected: FAIL（`fired` 列目前仍存在，INSERT 成功，测试期望报错）

- [ ] **Step 3: 在 `sqlite.go` 的 `migrate()` 末尾追加迁移**

在 `return nil` 之前追加：

```go
// Rebuild proactive_items without the fired column (trigger-and-delete model).
_, err = db.Exec(`DROP TABLE IF EXISTS proactive_items`)
if err != nil {
    return fmt.Errorf("drop proactive_items: %w", err)
}
_, err = db.Exec(`
    CREATE TABLE IF NOT EXISTS proactive_items (
        id          INTEGER PRIMARY KEY AUTOINCREMENT,
        trigger_at  DATETIME NOT NULL,
        prompt      TEXT NOT NULL,
        created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
    )
`)
if err != nil {
    return fmt.Errorf("recreate proactive_items: %w", err)
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/db/... -v
```

Expected: 所有测试 PASS（包括原有的 `TestMigrateProactiveItems`）

- [ ] **Step 5: 提交**

```bash
git add internal/db/sqlite.go internal/db/sqlite_test.go
git commit -m "feat(db): rebuild proactive_items without fired column"
```

---

### Task 2: Store — 替换 MarkFired 为 Delete，新增 List

**Files:**
- Modify: `internal/proactive/store.go`
- Modify: `internal/proactive/store_test.go`

#### 背景

`store.go` 当前 `Store` 接口有 `MarkFired`，`Item` 有 `Fired bool`，`DueItems` 查询带 `fired = FALSE` 过滤。需要：
1. 接口 `MarkFired` → `Delete`，新增 `List`
2. `Item` 删除 `Fired bool`
3. `DueItems` 去掉 `fired` 相关列和过滤
4. 实现 `Delete` 和 `List`
5. 删除 `MarkFired` 实现

- [ ] **Step 1: 在 `store_test.go` 写失败测试**

完整替换 `store_test.go`（旧的 `TestStoreMarkFired` 会编译失败，因为接口变了）：

```go
package proactive_test

import (
	"testing"
	"time"

	"aiko/internal/db"
	"aiko/internal/proactive"
)

func openStore(t *testing.T) *proactive.ProactiveStore {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return proactive.NewStore(database)
}

// TestStoreInsertAndQuery verifies insert + DueItems returns due rows.
func TestStoreInsertAndQuery(t *testing.T) {
	s := openStore(t)
	triggerAt := time.Now().Add(-time.Second)

	if err := s.Insert(t.Context(), triggerAt, "hello world"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	items, err := s.DueItems(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("due items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 due item, got %d", len(items))
	}
	if items[0].Prompt != "hello world" {
		t.Errorf("unexpected prompt: %q", items[0].Prompt)
	}
}

// TestStoreDelete verifies that Delete removes the row and DueItems returns empty.
func TestStoreDelete(t *testing.T) {
	s := openStore(t)
	triggerAt := time.Now().Add(-time.Second)

	if err := s.Insert(t.Context(), triggerAt, "ping"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	items, err := s.DueItems(t.Context(), time.Now())
	if err != nil || len(items) == 0 {
		t.Fatalf("expected due item, got err=%v items=%v", err, items)
	}

	if err := s.Delete(t.Context(), items[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	after, err := s.DueItems(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("due items after delete: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected 0 due items after delete, got %d", len(after))
	}
}

// TestStoreDeleteIdempotent verifies that deleting a non-existent ID returns no error.
func TestStoreDeleteIdempotent(t *testing.T) {
	s := openStore(t)
	if err := s.Delete(t.Context(), 9999); err != nil {
		t.Errorf("expected no error for missing id, got: %v", err)
	}
}

// TestStoreList verifies List returns all rows ordered by trigger_at ascending.
func TestStoreList(t *testing.T) {
	s := openStore(t)
	t1 := time.Now().Add(time.Hour)
	t2 := time.Now().Add(2 * time.Hour)

	if err := s.Insert(t.Context(), t2, "second"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.Insert(t.Context(), t1, "first"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	items, err := s.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Prompt != "first" {
		t.Errorf("expected first item prompt 'first', got %q", items[0].Prompt)
	}
	if items[1].Prompt != "second" {
		t.Errorf("expected second item prompt 'second', got %q", items[1].Prompt)
	}
}

// TestStoreFutureItemNotDue verifies that future items are not returned by DueItems.
func TestStoreFutureItemNotDue(t *testing.T) {
	s := openStore(t)
	triggerAt := time.Now().Add(time.Hour)

	if err := s.Insert(t.Context(), triggerAt, "future"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	items, err := s.DueItems(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("due items: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 due items, got %d", len(items))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/proactive/... -run "TestStoreDelete|TestStoreList" -v
```

Expected: FAIL（`Delete` 和 `List` 方法不存在）

- [ ] **Step 3: 更新 `store.go`**

完整替换 `store.go`：

```go
// Package proactive implements the ProactiveEngine, which sends time-driven and
// context-derived messages to the user without polluting the memory system.
package proactive

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Item is a single scheduled proactive message.
type Item struct {
	ID        int64
	TriggerAt time.Time
	Prompt    string
	CreatedAt time.Time
}

// Store is the interface for managing scheduled proactive items.
type Store interface {
	Insert(ctx context.Context, triggerAt time.Time, prompt string) error
	DueItems(ctx context.Context, now time.Time) ([]Item, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]Item, error)
}

// ProactiveStore manages the proactive_items SQLite table.
type ProactiveStore struct {
	db *sql.DB
}

// NewStore returns a ProactiveStore backed by the given database.
func NewStore(db *sql.DB) *ProactiveStore {
	return &ProactiveStore{db: db}
}

// Insert creates a new pending proactive item.
func (s *ProactiveStore) Insert(ctx context.Context, triggerAt time.Time, prompt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO proactive_items (trigger_at, prompt) VALUES (?, ?)`,
		triggerAt.UTC().Format("2006-01-02 15:04:05"), prompt,
	)
	if err != nil {
		return fmt.Errorf("insert proactive item: %w", err)
	}
	return nil
}

// DueItems returns all items with trigger_at <= now, ordered by trigger_at ascending.
func (s *ProactiveStore) DueItems(ctx context.Context, now time.Time) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_at, prompt, created_at
		   FROM proactive_items
		  WHERE trigger_at <= ?
		  ORDER BY trigger_at ASC`,
		now.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("query due items: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// Delete removes the item with the given id. Returns nil if the id does not exist.
func (s *ProactiveStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proactive_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete proactive item %d: %w", id, err)
	}
	return nil
}

// List returns all pending items ordered by trigger_at ascending.
func (s *ProactiveStore) List(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_at, prompt, created_at
		   FROM proactive_items
		  ORDER BY trigger_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list proactive items: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// scanItems scans a *sql.Rows into a slice of Item.
func scanItems(rows *sql.Rows) ([]Item, error) {
	var items []Item
	for rows.Next() {
		var it Item
		var trigStr, createdStr string
		if err := rows.Scan(&it.ID, &trigStr, &it.Prompt, &createdStr); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		it.TriggerAt, _ = time.Parse("2006-01-02 15:04:05", trigStr)
		it.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		items = append(items, it)
	}
	return items, rows.Err()
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/proactive/... -v
```

Expected: `TestStoreInsertAndQuery`、`TestStoreDelete`、`TestStoreDeleteIdempotent`、`TestStoreList`、`TestStoreFutureItemNotDue` 全 PASS（`engine_test.go` 此时可能编译失败，因为引用了 `MarkFired` — 下一个 Task 修复）

- [ ] **Step 5: 提交**

```bash
git add internal/proactive/store.go internal/proactive/store_test.go
git commit -m "feat(proactive): replace MarkFired with Delete, add List to Store"
```

---

### Task 3: Engine — 删晨晚问候，Poll 改用 Delete，Fire 返回 error，新增 Store() 访问器

**Files:**
- Modify: `internal/proactive/engine.go`
- Modify: `internal/proactive/engine_test.go`

#### 背景

`engine.go` 当前：
- `Start()` 注册了两个晨晚问候 cron job（`greetingMorningPrompt` / `greetingEveningPrompt`）
- `Poll()` 调用 `e.store.MarkFired(ctx, item.ID)`
- `Fire()` 不返回 error

需要：
1. 彻底删除两个问候常量及其 cron 注册
2. `Poll()` 改为先调 `Delete`，Fire 失败则 emit notification:show
3. `Fire()` 改为返回 `error`
4. 新增 `Store() Store` 访问器

`engine_test.go` 中 `TestPollFiresDueItems` 验证 `MarkFired` 行为，需要同步更新。

- [ ] **Step 1: 在 `engine_test.go` 写失败测试**

完整替换 `engine_test.go`：

```go
package proactive_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"aiko/internal/db"
	"aiko/internal/proactive"
)

// mockApp implements AppInterface for testing.
type mockApp struct {
	mu              sync.Mutex
	chatDirectCalls []string
	collectCalls    []string
	emittedEvents   []string
	chatVisible     bool
	collectReturn   string
	chatDirectErr   error
}

func (m *mockApp) ChatDirect(_ context.Context, prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatDirectCalls = append(m.chatDirectCalls, prompt)
	return m.chatDirectErr
}

func (m *mockApp) ChatDirectCollect(_ context.Context, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectCalls = append(m.collectCalls, prompt)
	return m.collectReturn, nil
}

func (m *mockApp) IsChatVisible() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chatVisible
}

func (m *mockApp) EmitEvent(name string, _ any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emittedEvents = append(m.emittedEvents, name)
}

// TestFireChatOpen verifies Fire emits chat:proactive:start and calls ChatDirect when chat is open.
func TestFireChatOpen(t *testing.T) {
	app := &mockApp{chatVisible: true}
	eng := proactive.NewEngine(app, nil)

	if err := eng.Fire(context.Background(), "good morning"); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.chatDirectCalls) != 1 || app.chatDirectCalls[0] != "good morning" {
		t.Errorf("expected ChatDirect called with prompt, got %v", app.chatDirectCalls)
	}
	if len(app.emittedEvents) == 0 || app.emittedEvents[0] != "chat:proactive:start" {
		t.Errorf("expected chat:proactive:start emitted, got %v", app.emittedEvents)
	}
}

// TestFireChatClosed verifies Fire uses ChatDirectCollect and emits notification:show when chat is closed.
func TestFireChatClosed(t *testing.T) {
	app := &mockApp{chatVisible: false, collectReturn: "evening greeting text"}
	eng := proactive.NewEngine(app, nil)

	if err := eng.Fire(context.Background(), "good evening"); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.collectCalls) != 1 {
		t.Errorf("expected ChatDirectCollect called, got %v", app.collectCalls)
	}
	found := false
	for _, e := range app.emittedEvents {
		if e == "notification:show" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected notification:show emitted, got %v", app.emittedEvents)
	}
}

// TestFireChatClosedTruncates verifies long responses are truncated to 80 runes.
func TestFireChatClosedTruncates(t *testing.T) {
	long := "A very long proactive message that exceeds eighty characters in total length for testing truncation behavior here"
	app := &mockApp{chatVisible: false, collectReturn: long}
	eng := proactive.NewEngine(app, nil)
	if err := eng.Fire(context.Background(), "prompt"); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	found := false
	for _, e := range app.emittedEvents {
		if e == "notification:show" {
			found = true
		}
	}
	if !found {
		t.Error("expected notification:show emitted for long text")
	}
}

// TestPollDeletesAfterFire verifies Poll deletes the row after successful Fire.
func TestPollDeletesAfterFire(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := proactive.NewStore(database)

	if err := store.Insert(context.Background(), time.Now().Add(-time.Second), "follow up"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	app := &mockApp{chatVisible: false, collectReturn: "reminder text"}
	eng := proactive.NewEngine(app, store)
	eng.Poll(context.Background())

	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.collectCalls) != 1 {
		t.Errorf("expected 1 collect call, got %d", len(app.collectCalls))
	}

	// Verify item is deleted (not just marked fired).
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected item deleted, got %d items remaining", len(items))
	}
}

// TestPollDeletesOnFireFailure verifies Poll deletes row and emits notification:show when Fire fails.
func TestPollDeletesOnFireFailure(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := proactive.NewStore(database)

	if err := store.Insert(context.Background(), time.Now().Add(-time.Second), "fail prompt"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// ChatDirect returns error to simulate Fire failure.
	app := &mockApp{chatVisible: true, chatDirectErr: errors.New("agent unavailable")}
	eng := proactive.NewEngine(app, store)
	eng.Poll(context.Background())

	// Row must be deleted even on failure.
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected item deleted on Fire failure, got %d items", len(items))
	}

	// notification:show must be emitted.
	app.mu.Lock()
	defer app.mu.Unlock()
	found := false
	for _, e := range app.emittedEvents {
		if e == "notification:show" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected notification:show emitted on Fire failure, got %v", app.emittedEvents)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/proactive/... -run "TestPollDeletesAfterFire|TestPollDeletesOnFireFailure" -v
```

Expected: FAIL（`Fire` 不返回 error，`Poll` 还在用 `MarkFired`）

- [ ] **Step 3: 更新 `engine.go`**

完整替换 `engine.go`：

```go
package proactive

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
)

const (
	// notifMaxRunes is the max rune length for notification messages.
	notifMaxRunes = 80
)

// AppInterface is the subset of *app.App that ProactiveEngine needs.
// Defined here to break the import cycle (proactive → app would be circular).
type AppInterface interface {
	// ChatDirect streams tokens to the frontend via chat:token / chat:done events.
	ChatDirect(ctx context.Context, prompt string) error
	// ChatDirectCollect runs the agent and returns the full response text with no events emitted.
	ChatDirectCollect(ctx context.Context, prompt string) (string, error)
	// IsChatVisible reports whether the chat bubble is currently open.
	IsChatVisible() bool
	// EmitEvent emits a Wails event to the frontend.
	EmitEvent(name string, data any)
}

// ProactiveEngine drives scheduled follow-up proactive messages.
type ProactiveEngine struct {
	app   AppInterface
	store Store
	cron  *cron.Cron
}

// NewEngine creates a ProactiveEngine. store may be nil (engine skips poll jobs).
func NewEngine(app AppInterface, store Store) *ProactiveEngine {
	return &ProactiveEngine{
		app:   app,
		store: store,
		cron:  cron.New(),
	}
}

// Store returns the underlying Store. Used by app.go to expose List/Delete to the frontend.
func (e *ProactiveEngine) Store() Store {
	return e.store
}

// Start registers cron jobs and begins the scheduler.
// ctx is used as a base context for all fired messages.
func (e *ProactiveEngine) Start(ctx context.Context) {
	// Poll for due follow-up items every minute.
	if e.store != nil {
		_, _ = e.cron.AddFunc("* * * * *", func() {
			e.Poll(ctx)
		})
	}
	e.cron.Start()
}

// Stop stops the cron scheduler.
func (e *ProactiveEngine) Stop() {
	e.cron.Stop()
}

// Fire delivers a proactive message using the given prompt.
// If chat is open, it streams tokens to the frontend.
// If chat is closed, it collects the response and shows a notification.
// Returns an error if the underlying chat call fails.
func (e *ProactiveEngine) Fire(ctx context.Context, prompt string) error {
	if e.app.IsChatVisible() {
		e.app.EmitEvent("chat:proactive:start", nil)
		if err := e.app.ChatDirect(ctx, prompt); err != nil {
			return fmt.Errorf("ChatDirect: %w", err)
		}
		return nil
	}
	// Chat is closed: collect and deliver via notification bubble.
	text, err := e.app.ChatDirectCollect(ctx, prompt)
	if err != nil {
		return fmt.Errorf("ChatDirectCollect: %w", err)
	}
	if utf8.RuneCountInString(text) > notifMaxRunes {
		runes := []rune(text)
		text = string(runes[:notifMaxRunes]) + "…"
	}
	e.app.EmitEvent("notification:show", map[string]any{
		"title":   "✨ (=^･ω･^=)",
		"message": text,
	})
	return nil
}

// Poll queries the store for due items and fires each one.
// The row is deleted before Fire is called to avoid double-firing.
// If Fire fails, a failure notification is emitted.
// Exported for testing.
func (e *ProactiveEngine) Poll(ctx context.Context) {
	if e.store == nil {
		return
	}
	items, err := e.store.DueItems(ctx, time.Now().UTC())
	if err != nil {
		slog.Warn("proactive poll: query due items", "err", err)
		return
	}
	for _, item := range items {
		// Delete before Fire to prevent double-firing if Fire is slow.
		if err := e.store.Delete(ctx, item.ID); err != nil {
			slog.Warn("proactive poll: delete item", "id", item.ID, "err", err)
			continue
		}
		if err := e.Fire(ctx, item.Prompt); err != nil {
			slog.Warn("proactive poll: fire failed", "id", item.ID, "err", err)
			e.app.EmitEvent("notification:show", map[string]any{
				"title":   "提醒触发失败",
				"message": truncate(item.Prompt, 30),
			})
		}
	}
}

// truncate returns the first n runes of s. If s is longer, it appends "…".
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
```

- [ ] **Step 4: 运行所有 proactive 测试确认通过**

```bash
go test ./internal/proactive/... -v
```

Expected: 全部 PASS

- [ ] **Step 5: 确认整体编译通过**

```bash
go build ./...
```

Expected: 无报错

- [ ] **Step 6: 提交**

```bash
git add internal/proactive/engine.go internal/proactive/engine_test.go
git commit -m "feat(proactive): delete-on-fire, remove greeting crons, Fire returns error"
```

---

### Task 4: app.go — 新增 ListProactiveItems 和 DeleteProactiveItem

**Files:**
- Modify: `app.go`

#### 背景

`app.go` 已有 `proactiveEngine *proactive.ProactiveEngine` 字段和 `a.mu` 互斥锁。需要新增两个 Wails 绑定方法。

无单独单元测试（Wails 绑定测试需要运行时），通过 `go build` 验证编译。

- [ ] **Step 1: 在 `app.go` 中新增两个方法**

找到 `IsChatVisible()` 方法附近，在其后追加：

```go
// ListProactiveItems returns all pending proactive reminders ordered by trigger time.
func (a *App) ListProactiveItems() ([]proactive.Item, error) {
	a.mu.RLock()
	pe := a.proactiveEngine
	a.mu.RUnlock()
	if pe == nil {
		return nil, nil
	}
	return pe.Store().List(context.Background())
}

// DeleteProactiveItem cancels a pending proactive reminder by ID.
func (a *App) DeleteProactiveItem(id int64) error {
	a.mu.RLock()
	pe := a.proactiveEngine
	a.mu.RUnlock()
	if pe == nil {
		return nil
	}
	return pe.Store().Delete(context.Background(), id)
}
```

- [ ] **Step 2: 确认编译通过**

```bash
go build ./...
```

Expected: 无报错

- [ ] **Step 3: 重新生成 Wails bindings**

```bash
wails generate module
```

Expected: `frontend/src/wailsjs/go/main/App.js` 和 `App.d.ts` 中出现 `ListProactiveItems` 和 `DeleteProactiveItem`

- [ ] **Step 4: 提交**

```bash
git add app.go frontend/src/wailsjs/
git commit -m "feat(app): add ListProactiveItems and DeleteProactiveItem Wails bindings"
```

---

### Task 5: 前端 — SettingsWindow.vue 新增「提醒事项」tab

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

#### 背景

`SettingsWindow.vue` 当前最后一个 tab 是 `sms`（`activeTab === 'sms'`）。需要在 sms tab 按钮之后添加新 tab 按钮，在 sms tab pane 之后添加新 tab pane，并在 `<script setup>` 中添加数据和方法，在 `<style>` 中添加 CSS。

- [ ] **Step 1: 在 tab 按钮列表末尾添加「提醒事项」按钮**

找到以下内容：
```html
<button :class="{ active: activeTab === 'sms' }" @click="activeTab = 'sms'">短信</button>
```

在其后追加：
```html
<button :class="{ active: activeTab === 'proactive' }" @click="activeTab = 'proactive'">提醒事项</button>
```

- [ ] **Step 2: 在 sms tab pane 之后添加新 tab pane**

找到 `<div v-if="activeTab === 'sms'" class="tab-pane">` 对应的闭合 `</div>` 之后，添加：

```html
<div v-if="activeTab === 'proactive'" class="tab-pane">
  <div class="section-header">
    <h3>提醒事项</h3>
    <button class="btn-small" @click="loadProactiveItems">刷新</button>
  </div>

  <div v-if="proactiveError" class="form-error">{{ proactiveError }}</div>

  <div v-if="proactiveItems.length === 0 && !proactiveError" class="empty-hint">
    暂无待触发的提醒事项
  </div>

  <div v-for="item in proactiveItems" :key="item.ID" class="proactive-row">
    <div class="proactive-info">
      <span class="proactive-time">{{ formatProactiveTime(item.TriggerAt) }}</span>
      <span class="proactive-prompt">{{ truncatePrompt(item.Prompt, 60) }}</span>
    </div>
    <button class="btn-small btn-danger" @click="deleteProactiveItem(item.ID)">删除</button>
  </div>
</div>
```

- [ ] **Step 3: 在 `<script setup>` 中添加逻辑**

找到 `<script setup>` 中现有的 import 行，补充：
```js
import { ListProactiveItems, DeleteProactiveItem } from '../../wailsjs/go/main/App'
```

然后在 `<script setup>` 末尾（`</script>` 之前）追加：

```js
// ── 提醒事项 ──────────────────────────────────────────────
const proactiveItems = ref([])
const proactiveError = ref('')

/** loadProactiveItems fetches all pending reminders from the backend. */
async function loadProactiveItems() {
  try {
    proactiveError.value = ''
    proactiveItems.value = await ListProactiveItems() ?? []
  } catch (e) {
    proactiveError.value = '加载失败'
  }
}

/** deleteProactiveItem removes a reminder optimistically, rolls back on error. */
async function deleteProactiveItem(id) {
  proactiveItems.value = proactiveItems.value.filter(i => i.ID !== id)
  try {
    await DeleteProactiveItem(id)
  } catch (e) {
    await loadProactiveItems()
  }
}

/** formatProactiveTime formats a UTC time string to local M/D HH:mm. */
function formatProactiveTime(t) {
  return new Date(t).toLocaleString('zh-CN', {
    month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit'
  })
}

/** truncatePrompt truncates a prompt string to n characters. */
function truncatePrompt(s, n) {
  return s.length > n ? s.slice(0, n) + '…' : s
}

watch(activeTab, v => { if (v === 'proactive') loadProactiveItems() })
```

- [ ] **Step 4: 在 `<style>` 中追加 CSS**

在 `</style>` 之前追加：

```css
/* ── 提醒事项 tab ───────────────────────────────────────── */
.proactive-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  background: rgba(255,255,255,0.04);
  border-radius: 8px;
  margin-bottom: 8px;
}
.proactive-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
  min-width: 0;
}
.proactive-time {
  font-size: 12px;
  color: #a5b4fc;
  font-variant-numeric: tabular-nums;
}
.proactive-prompt {
  font-size: 13px;
  color: rgba(255,255,255,0.8);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.btn-danger {
  color: #f87171;
  border-color: rgba(248,113,113,0.3);
}
.btn-danger:hover {
  background: rgba(248,113,113,0.15);
}
```

- [ ] **Step 5: 构建前端确认无报错**

```bash
cd frontend && yarn build
```

Expected: 构建成功，无 error（chunk size advisory 可忽略）

- [ ] **Step 6: 提交**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(frontend): add 提醒事项 tab to SettingsWindow"
```

---

### Task 6: 验收 — 全量测试 + 构建

**Files:** 无新文件

- [ ] **Step 1: 运行全量 Go 测试**

```bash
go test ./... 2>&1
```

Expected: 全部 PASS，0 failures

- [ ] **Step 2: Go 编译**

```bash
go build ./...
```

Expected: 无报错

- [ ] **Step 3: 前端构建**

```bash
cd frontend && yarn build
```

Expected: 构建成功
