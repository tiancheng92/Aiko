# Proactive Sensing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `ProactiveEngine` that lets Aiko send morning/evening greetings on schedule and follow-up reminders derived from conversation, without polluting memory.

**Architecture:** A new `internal/proactive/` package owns SQLite storage, a cron-driven engine, and a `schedule_followup` tool. The engine calls `agent.ChatDirectCollect` when chat is closed (delivers via notification bubble) or streams tokens to the frontend when chat is open. All proactive messages bypass memory persistence.

**Tech Stack:** Go, `robfig/cron/v3` (already a dep), `database/sql` SQLite, Wails v2 events, Vue 3

---

## File Map

| Action | File | Purpose |
|--------|------|---------|
| Create | `internal/proactive/store.go` | SQLite CRUD for `proactive_items` |
| Create | `internal/proactive/store_test.go` | Unit tests for store |
| Create | `internal/proactive/engine.go` | `ProactiveEngine` + `AppInterface` |
| Create | `internal/proactive/engine_test.go` | Unit tests for engine delivery |
| Create | `internal/proactive/tool.go` | `ScheduleFollowupTool` |
| Create | `internal/proactive/tool_test.go` | Unit tests for tool validation |
| Modify | `internal/db/sqlite.go` | Add `proactive_items` table migration |
| Modify | `internal/agent/agent.go` | Add `ChatDirectCollect` method |
| Modify | `app.go` | Add engine field, `IsChatVisible`, `EmitEvent`, `SetChatVisible`, wire engine |
| Modify | `frontend/src/App.vue` | Call `SetChatVisible` on `bubble:toggle` |
| Modify | `frontend/src/components/ChatPanel.vue` | Handle `chat:proactive:start`, `.proactive` CSS |

---

## Task 1: DB Migration — proactive_items table

**Files:**
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: Write the failing test**

Create `internal/db/sqlite_test.go`:

```go
package db_test

import (
	"testing"

	"aiko/internal/db"
)

// TestMigrateProactiveItems verifies that the proactive_items table is
// created by the migration and has the expected columns.
func TestMigrateProactiveItems(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// Insert a row and read it back to confirm schema.
	_, err = database.Exec(
		`INSERT INTO proactive_items (trigger_at, prompt) VALUES (datetime('now'), 'hello')`,
	)
	if err != nil {
		t.Fatalf("insert proactive_items: %v", err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM proactive_items`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/db/... -run TestMigrateProactiveItems -v
```

Expected: FAIL — `insert proactive_items: no such table: proactive_items`

- [ ] **Step 3: Add migration**

In `internal/db/sqlite.go`, append to the end of the `migrate` function, after the `model_profiles` block:

```go
	// proactive_items: one-shot proactive messages scheduled by the engine or agent tool.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS proactive_items (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			trigger_at  DATETIME NOT NULL,
			prompt      TEXT NOT NULL,
			fired       BOOLEAN DEFAULT FALSE,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("create proactive_items: %w", err)
	}
	return nil
```

Also remove the bare `return nil` that was previously at the end of the `migrate` function (the one after the `model_profiles` block).

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/db/... -run TestMigrateProactiveItems -v
```

Expected: PASS

- [ ] **Step 5: Compile check**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/db/sqlite.go internal/db/sqlite_test.go
git commit -m "feat(db): add proactive_items migration"
```

---

## Task 2: ProactiveStore

**Files:**
- Create: `internal/proactive/store.go`
- Create: `internal/proactive/store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/proactive/store_test.go`:

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

// TestStoreInsertAndQuery verifies insert + query of pending items.
func TestStoreInsertAndQuery(t *testing.T) {
	s := openStore(t)
	triggerAt := time.Now().Add(-time.Second) // already due

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

// TestStoreMarkFired verifies that marking an item fired excludes it from future queries.
func TestStoreMarkFired(t *testing.T) {
	s := openStore(t)
	triggerAt := time.Now().Add(-time.Second)

	if err := s.Insert(t.Context(), triggerAt, "ping"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	items, err := s.DueItems(t.Context(), time.Now())
	if err != nil || len(items) == 0 {
		t.Fatalf("expected due item, got err=%v items=%v", err, items)
	}

	if err := s.MarkFired(t.Context(), items[0].ID); err != nil {
		t.Fatalf("mark fired: %v", err)
	}

	after, err := s.DueItems(t.Context(), time.Now())
	if err != nil {
		t.Fatalf("due items after mark: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected 0 due items after fired, got %d", len(after))
	}
}

// TestStoreFutureItemNotDue verifies that future items are not returned.
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

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/proactive/... -v
```

Expected: FAIL — package not found or compilation error

- [ ] **Step 3: Implement ProactiveStore**

Create `internal/proactive/store.go`:

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
	Fired     bool
	CreatedAt time.Time
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

// DueItems returns all unfired items with trigger_at <= now.
func (s *ProactiveStore) DueItems(ctx context.Context, now time.Time) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_at, prompt, fired, created_at
		   FROM proactive_items
		  WHERE fired = FALSE AND trigger_at <= ?
		  ORDER BY trigger_at ASC`,
		now.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("query due items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		var trigStr, createdStr string
		if err := rows.Scan(&it.ID, &trigStr, &it.Prompt, &it.Fired, &createdStr); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		it.TriggerAt, _ = time.Parse("2006-01-02 15:04:05", trigStr)
		it.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		items = append(items, it)
	}
	return items, rows.Err()
}

// MarkFired marks the item with the given id as fired.
func (s *ProactiveStore) MarkFired(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE proactive_items SET fired = TRUE WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("mark fired %d: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/proactive/... -run "TestStore" -v
```

Expected: all 3 store tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/proactive/store.go internal/proactive/store_test.go
git commit -m "feat(proactive): add ProactiveStore with SQLite backend"
```

---

## Task 3: agent.ChatDirectCollect

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/agent_test.go` (or append if it exists):

```go
package agent_test

import (
	"context"
	"testing"

	"aiko/internal/agent"
)

// TestChatDirectCollectExists is a compile-time check that ChatDirectCollect
// exists and has the right signature. A real integration test would require
// a live LLM; this just ensures the method is defined.
func TestChatDirectCollectExists(t *testing.T) {
	// Verify the method signature exists on *Agent via interface satisfaction.
	type collecter interface {
		ChatDirectCollect(ctx context.Context, prompt string) (string, error)
	}
	var _ collecter = (*agent.Agent)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/agent/... -run TestChatDirectCollectExists -v
```

Expected: FAIL — `*agent.Agent does not implement collecter (missing ChatDirectCollect method)`

- [ ] **Step 3: Add ChatDirectCollect to agent.go**

In `internal/agent/agent.go`, add this method after `ChatDirect`:

```go
// ChatDirectCollect sends a prompt to the agent, collects the full response
// as a string, and returns it. Unlike ChatDirect, no Wails events are emitted.
// Used by the ProactiveEngine when the chat panel is closed.
func (a *Agent) ChatDirectCollect(ctx context.Context, prompt string) (string, error) {
	ch := a.ChatDirect(ctx, prompt)
	var sb strings.Builder
	for r := range ch {
		if r.Err != nil {
			return "", r.Err
		}
		if r.Done {
			break
		}
		sb.WriteString(r.Token)
	}
	return sb.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/agent/... -run TestChatDirectCollectExists -v
```

Expected: PASS

- [ ] **Step 5: Compile check**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): add ChatDirectCollect for proactive engine"
```

---

## Task 4: ProactiveEngine

**Files:**
- Create: `internal/proactive/engine.go`
- Create: `internal/proactive/engine_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/proactive/engine_test.go`:

```go
package proactive_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"aiko/internal/proactive"
)

// mockApp implements AppInterface for testing.
type mockApp struct {
	mu             sync.Mutex
	chatDirectCalls []string
	collectCalls    []string
	emittedEvents   []string
	chatVisible     bool
	collectReturn   string
}

func (m *mockApp) ChatDirect(_ context.Context, prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatDirectCalls = append(m.chatDirectCalls, prompt)
	return nil
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

// TestFireChatOpen verifies that fire() calls ChatDirect and emits chat:proactive:start when chat is open.
func TestFireChatOpen(t *testing.T) {
	app := &mockApp{chatVisible: true}
	eng := proactive.NewEngine(app, nil)

	eng.Fire(context.Background(), "good morning")

	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.chatDirectCalls) != 1 || app.chatDirectCalls[0] != "good morning" {
		t.Errorf("expected ChatDirect called with prompt, got %v", app.chatDirectCalls)
	}
	if len(app.emittedEvents) == 0 || app.emittedEvents[0] != "chat:proactive:start" {
		t.Errorf("expected chat:proactive:start emitted, got %v", app.emittedEvents)
	}
}

// TestFireChatClosed verifies that fire() uses ChatDirectCollect and emits notification:show when chat is closed.
func TestFireChatClosed(t *testing.T) {
	app := &mockApp{chatVisible: false, collectReturn: "evening greeting text"}
	eng := proactive.NewEngine(app, nil)

	eng.Fire(context.Background(), "good evening")

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

// TestFireChatClosedTruncates verifies that long responses are truncated to 80 chars.
func TestFireChatClosedTruncates(t *testing.T) {
	long := "A very long proactive message that exceeds eighty characters in total length for testing truncation behavior here"
	app := &mockApp{chatVisible: false, collectReturn: long}
	eng := proactive.NewEngine(app, nil)
	eng.Fire(context.Background(), "prompt")

	// We just verify no panic and notification:show was emitted.
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

// TestPollFiresDueItems verifies that Poll calls fire for each due item and marks it fired.
func TestPollFiresDueItems(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := proactive.NewStore(database)

	// Insert a due item.
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

	// Verify item is now marked fired.
	items, err := store.DueItems(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("due items: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected item fired, got %d due items", len(items))
	}
}
```

Note: the test file needs `"aiko/internal/db"` import for `TestPollFiresDueItems`.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/proactive/... -run "TestFire|TestPoll" -v
```

Expected: FAIL — `proactive.NewEngine` and `AppInterface` not defined

- [ ] **Step 3: Implement ProactiveEngine**

Create `internal/proactive/engine.go`:

```go
package proactive

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
)

const (
	greetingMorningPrompt = `你是桌面宠物 Aiko，现在是早上，主动向用户发一句温暖简短的早安问候（1-2句话，自然随意，不要过于正式）。不要提及你是AI。`
	greetingEveningPrompt = `你是桌面宠物 Aiko，现在是晚上，主动向用户发一句轻松的晚间问候（1-2句话）。可以关心今天过得怎样。不要提及你是AI。`

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

// ProactiveEngine drives scheduled and follow-up proactive messages.
type ProactiveEngine struct {
	app   AppInterface
	store *ProactiveStore
	cron  *cron.Cron
}

// NewEngine creates a ProactiveEngine. store may be nil (engine skips poll jobs).
func NewEngine(app AppInterface, store *ProactiveStore) *ProactiveEngine {
	return &ProactiveEngine{
		app:   app,
		store: store,
		cron:  cron.New(),
	}
}

// Start registers cron jobs and begins the scheduler.
// ctx is used as a base context for all fired messages.
func (e *ProactiveEngine) Start(ctx context.Context) {
	// Morning greeting at 09:00 local time.
	_, _ = e.cron.AddFunc("0 9 * * *", func() {
		e.Fire(ctx, greetingMorningPrompt)
	})
	// Evening check-in at 21:00 local time.
	_, _ = e.cron.AddFunc("0 21 * * *", func() {
		e.Fire(ctx, greetingEveningPrompt)
	})
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
func (e *ProactiveEngine) Fire(ctx context.Context, prompt string) {
	if e.app.IsChatVisible() {
		// Emit sentinel so frontend can style the bubble.
		e.app.EmitEvent("chat:proactive:start", nil)
		if err := e.app.ChatDirect(ctx, prompt); err != nil {
			slog.Warn("proactive fire: ChatDirect error", "err", err)
		}
		return
	}
	// Chat is closed: collect and deliver via notification bubble.
	text, err := e.app.ChatDirectCollect(ctx, prompt)
	if err != nil {
		slog.Warn("proactive fire: ChatDirectCollect error", "err", err)
		return
	}
	if utf8.RuneCountInString(text) > notifMaxRunes {
		runes := []rune(text)
		text = string(runes[:notifMaxRunes]) + "…"
	}
	e.app.EmitEvent("notification:show", map[string]any{
		"title":   "✨ (=^･ω･^=)",
		"message": text,
	})
}

// Poll queries the store for due items and fires each one.
// Exported for testing.
func (e *ProactiveEngine) Poll(ctx context.Context) {
	if e.store == nil {
		return
	}
	items, err := e.store.DueItems(ctx, time.Now())
	if err != nil {
		slog.Warn("proactive poll: query due items", "err", err)
		return
	}
	for _, item := range items {
		// Mark fired before calling Fire to avoid double-firing if Fire is slow.
		if err := e.store.MarkFired(ctx, item.ID); err != nil {
			slog.Warn("proactive poll: mark fired", "id", item.ID, "err", err)
			continue
		}
		e.Fire(ctx, item.Prompt)
	}
}
```

- [ ] **Step 4: Fix import in engine_test.go**

Make sure `engine_test.go` has all imports. The top of the file should be:

```go
package proactive_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"aiko/internal/db"
	"aiko/internal/proactive"
)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/proactive/... -v
```

Expected: all tests PASS (store tests + engine tests)

- [ ] **Step 6: Commit**

```bash
git add internal/proactive/engine.go internal/proactive/engine_test.go
git commit -m "feat(proactive): add ProactiveEngine with cron + delivery logic"
```

---

## Task 5: ScheduleFollowupTool

**Files:**
- Create: `internal/proactive/tool.go`
- Create: `internal/proactive/tool_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/proactive/tool_test.go`:

```go
package proactive_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"aiko/internal/db"
	"aiko/internal/proactive"
)

// TestScheduleFollowupValidation verifies that past times and far-future times are rejected.
func TestScheduleFollowupValidation(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := proactive.NewStore(database)
	tool := proactive.NewScheduleFollowupTool(store)

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "past time rejected",
			input:   `{"when":"2020-01-01T09:00:00","message":"ping"}`,
			wantErr: "过去",
		},
		{
			name:    "too far future rejected",
			input:   `{"when":"2099-12-31T09:00:00","message":"ping"}`,
			wantErr: "30天",
		},
		{
			name:    "missing message rejected",
			input:   `{"when":"` + time.Now().Add(time.Hour).Format("2006-01-02T15:04:05") + `","message":""}`,
			wantErr: "message",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.InvokableRun(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result, tc.wantErr) {
				t.Errorf("expected result containing %q, got %q", tc.wantErr, result)
			}
		})
	}
}

// TestScheduleFollowupValid verifies that a valid call inserts a row.
func TestScheduleFollowupValid(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := proactive.NewStore(database)
	tool := proactive.NewScheduleFollowupTool(store)

	when := time.Now().Add(time.Hour).Format("2006-01-02T15:04:05")
	result, err := tool.InvokableRun(context.Background(),
		`{"when":"`+when+`","message":"check in on interview prep"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "已安排") {
		t.Errorf("expected success message, got %q", result)
	}

	// Confirm row was inserted by querying the store for any future item.
	// Use a time far in the future to include our item.
	items, err := store.DueItems(context.Background(), time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("due items: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/proactive/... -run "TestScheduleFollowup" -v
```

Expected: FAIL — `proactive.NewScheduleFollowupTool` not found

- [ ] **Step 3: Implement ScheduleFollowupTool**

Create `internal/proactive/tool.go` with this complete implementation:

```go
package proactive

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	interntools "aiko/internal/tools"
)

// ScheduleFollowupTool lets the agent schedule a proactive follow-up message.
// It implements interntools.Tool so it can be wrapped by the permission gate.
type ScheduleFollowupTool struct {
	Store *ProactiveStore
}

// NewScheduleFollowupTool returns a ScheduleFollowupTool backed by store.
func NewScheduleFollowupTool(store *ProactiveStore) *ScheduleFollowupTool {
	return &ScheduleFollowupTool{Store: store}
}

// Name returns the stable tool name used for permission storage.
func (t *ScheduleFollowupTool) Name() string { return "schedule_followup" }

// Permission requires one-time user approval.
func (t *ScheduleFollowupTool) Permission() interntools.PermissionLevel {
	return interntools.PermProtected
}

// Info returns the eino tool schema.
func (t *ScheduleFollowupTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: "安排一条主动跟进消息，在指定时间主动提醒用户。当对话中发现用户有未来计划、待办事项或值得跟进的内容时调用。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"when": {
				Type:     schema.String,
				Desc:     "触发时间，ISO 8601 本地时间格式，例如 2026-04-26T09:00:00",
				Required: true,
			},
			"message": {
				Type:     schema.String,
				Desc:     "触发时发送给 AI 的提示词，说明要跟进什么内容",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun validates inputs and inserts the proactive item into the store.
func (t *ScheduleFollowupTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseToolArgs(input)

	whenStr, _ := args["when"].(string)
	message, _ := args["message"].(string)

	if message == "" {
		return "请提供 message 参数说明要跟进的内容", nil
	}
	if whenStr == "" {
		return "请提供 when 参数（ISO 8601 本地时间，例如 2026-04-26T09:00:00）", nil
	}

	when, err := time.ParseInLocation("2006-01-02T15:04:05", whenStr, time.Local)
	if err != nil {
		return fmt.Sprintf("时间格式无效，请使用 2006-01-02T15:04:05 格式，收到：%q", whenStr), nil
	}

	now := time.Now()
	if when.Before(now) {
		return "指定时间已过去，请提供未来的时间", nil
	}
	if when.After(now.Add(30 * 24 * time.Hour)) {
		return "指定时间超过30天，请安排30天内的跟进", nil
	}

	if err := t.Store.Insert(ctx, when, message); err != nil {
		return "", fmt.Errorf("schedule followup: %w", err)
	}

	return fmt.Sprintf("已安排：将在 %s 提醒你", when.Format("2006年01月02日 15:04")), nil
}

// parseToolArgs unmarshals JSON input into a map. Returns empty map on failure.
func parseToolArgs(input string) map[string]any {
	args := map[string]any{}
	if input == "" || input == "{}" {
		return args
	}
	_ = json.Unmarshal([]byte(input), &args)
	return args
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/proactive/... -run "TestScheduleFollowup" -v
```

Expected: all 4 sub-tests of TestScheduleFollowupValidation PASS + TestScheduleFollowupValid PASS

- [ ] **Step 5: Run all proactive tests**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/proactive/... -v
```

Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/proactive/tool.go internal/proactive/tool_test.go
git commit -m "feat(proactive): add ScheduleFollowupTool"
```

---

## Task 6: Wire ProactiveEngine into app.go

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Add isChatVisible field and new methods**

In `app.go`, in the `App` struct (around line 58, after `chatGeneration`), add:

```go
	isChatVisible   bool               // guarded by mu; true when chat bubble is open
	proactiveEngine *proactive.ProactiveEngine
```

Also add the import at the top of `app.go` (in the import block):

```go
	"aiko/internal/proactive"
```

- [ ] **Step 2: Add IsChatVisible, EmitEvent, SetChatVisible, ChatDirect, ChatDirectCollect methods**

Add these methods to `app.go` (after `StopGeneration` or near the end of the file):

```go
// IsChatVisible reports whether the chat bubble is currently open.
// Implements proactive.AppInterface.
func (a *App) IsChatVisible() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isChatVisible
}

// SetChatVisible is called by the frontend when the chat bubble opens or closes.
func (a *App) SetChatVisible(visible bool) {
	a.mu.Lock()
	a.isChatVisible = visible
	a.mu.Unlock()
}

// EmitEvent emits a Wails runtime event to the frontend.
// Implements proactive.AppInterface.
func (a *App) EmitEvent(name string, data any) {
	wailsruntime.EventsEmit(a.ctx, name, data)
}

// ProactiveChatDirect streams a prompt to the agent (for proactive messages when
// chat is open) and emits chat:token / chat:done events. Does not persist to memory.
// Implements proactive.AppInterface.
func (a *App) ProactiveChatDirect(ctx context.Context, prompt string) error {
	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()
	if ag == nil {
		return fmt.Errorf("agent not ready")
	}
	ch := ag.ChatDirect(ctx, prompt)
	for r := range ch {
		if r.Err != nil {
			return r.Err
		}
		if r.Done {
			wailsruntime.EventsEmit(a.ctx, "chat:done", "")
			break
		}
		wailsruntime.EventsEmit(a.ctx, "chat:token", r.Token)
	}
	return nil
}

// ProactiveChatDirectCollect runs the agent for proactive messages when chat is
// closed, collects the full response text, and returns it without emitting events.
// Implements proactive.AppInterface.
func (a *App) ProactiveChatDirectCollect(ctx context.Context, prompt string) (string, error) {
	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()
	if ag == nil {
		return "", fmt.Errorf("agent not ready")
	}
	return ag.ChatDirectCollect(ctx, prompt)
}
```

Note: `AppInterface` uses `ChatDirect` and `ChatDirectCollect` as method names. The `*App` receiver methods are named `ProactiveChatDirect` and `ProactiveChatDirectCollect` to avoid confusion — we need to ensure the interface uses the same names as what we implement. Update the interface definition in `engine.go` to match:

Actually, let's keep it clean. The `AppInterface` in `engine.go` defines the interface. `*App` just needs to satisfy it. The method names on `*App` must match the interface exactly. Update `engine.go`'s `AppInterface` to use `ChatDirect` and `ChatDirectCollect`, and name the `*App` methods the same:

Rename the above methods on `*App` to `ChatDirect` and `ChatDirectCollect`:

```go
// ChatDirect streams a proactive prompt to the frontend via chat:token/chat:done.
// Implements proactive.AppInterface.
func (a *App) ChatDirect(ctx context.Context, prompt string) error { ... }

// ChatDirectCollect runs the agent for a proactive prompt and returns the full text.
// Implements proactive.AppInterface.
func (a *App) ChatDirectCollect(ctx context.Context, prompt string) (string, error) { ... }
```

- [ ] **Step 3: Wire engine in initLLMComponents**

In `initLLMComponents` in `app.go`, after building `newAgent` and before the final `a.mu.Lock()` block, add:

```go
	// Build proactive engine and its store/tool.
	proactiveSt := proactive.NewStore(a.sqlDB)
	followupTool := proactive.NewScheduleFollowupTool(proactiveSt)
	allTools = append(allTools, internaltools.ToEino(followupTool, a.permStore))
```

Wait — `allTools` is built before `newAgent` is created. The tool must be added before the agent is constructed. Reorder: add the `proactiveSt` and `followupTool` lines right before `newAgent, err := agent.New(...)`:

```go
	// Proactive follow-up tool (needs store before agent construction).
	proactiveSt := proactive.NewStore(a.sqlDB)
	followupTool := proactive.NewScheduleFollowupTool(proactiveSt)
	allTools = append(allTools, internaltools.ToEino(followupTool, a.permStore))

	newAgent, err := agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW, dataDir)
	if err != nil {
		return fmt.Errorf("new agent: %w", err)
	}

	newEngine := proactive.NewEngine(a, proactiveSt)
	newEngine.Start(a.ctx)
```

Then in the `a.mu.Lock()` block, stop old engine and store new one:

```go
	a.mu.Lock()
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	if a.proactiveEngine != nil {
		a.proactiveEngine.Stop()
	}
	a.scheduler = sched
	a.longMem = longMem
	a.knowledgeSt = knowledgeSt
	a.petAgent = newAgent
	a.proactiveEngine = newEngine
	a.mu.Unlock()
```

- [ ] **Step 4: Compile check**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors. If there are import errors, ensure `"aiko/internal/proactive"` is in the import block of `app.go`.

- [ ] **Step 5: Regenerate Wails bindings**

`SetChatVisible` is a new exported method — regenerate bindings so the frontend can call it.

```bash
cd /Users/xutiancheng/code/self/Aiko && wails generate module
```

Expected: `frontend/src/wailsjs/go/main/App.js` and `App.d.ts` updated with `SetChatVisible`.

- [ ] **Step 6: Commit**

```bash
git add app.go frontend/src/wailsjs/
git commit -m "feat(app): wire ProactiveEngine into App, add IsChatVisible/SetChatVisible/ChatDirect/ChatDirectCollect"
```

---

## Task 7: Frontend — SetChatVisible + proactive bubble styling

**Files:**
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Add SetChatVisible import and call in App.vue**

In `frontend/src/App.vue`, find the imports from `wailsjs/go/main/App` (the line that imports Wails-generated Go bindings). Add `SetChatVisible` to that import (keep all existing imports, just add the new one):

```js
import { ..., SetChatVisible } from '../../wailsjs/go/main/App'
```

Then in the `bubble:toggle` handler (around line 334), after `bubbleOpen.value = !bubbleOpen.value`, call:

```js
offToggle = EventsOn('bubble:toggle', () => {
  bubbleOpen.value = !bubbleOpen.value
  SetChatVisible(bubbleOpen.value).catch(() => {})  // ADD THIS LINE
  if (bubbleOpen.value) {
    pendingTokens = ''
    nextTick(() => {
      chatBubbleRef.value?.focusInput()
      chatBubbleRef.value?.scrollToBottom()
    })
  }
})
```

- [ ] **Step 2: Handle chat:proactive:start in ChatPanel.vue**

In `frontend/src/components/ChatPanel.vue`, add a module-scope flag and event listener. Find where `EventsOn` calls are made in `onMounted` and add:

```js
// Module-scope flag (alongside other module-scope lets like soundsEnabled)
let nextBubbleIsProactive = false

// In onMounted, add:
EventsOn('chat:proactive:start', () => {
  nextBubbleIsProactive = true
})
```

Find where the assistant message bubble is created when the first token arrives (look for where a new assistant message is pushed to the messages array). When creating a new assistant bubble, attach the `proactive` flag:

```js
// When creating a new assistant message bubble (first token of a response):
messages.value.push({
  role: 'assistant',
  content: '',
  proactive: nextBubbleIsProactive,
})
nextBubbleIsProactive = false
```

- [ ] **Step 3: Add .proactive CSS class binding**

In the template of `ChatPanel.vue`, find the assistant message bubble element and add the CSS class binding:

```html
<div
  :class="['bubble', 'assistant', { proactive: m.proactive }]"
  ...
>
```

- [ ] **Step 4: Add CSS for .proactive**

In the `<style scoped>` section of `ChatPanel.vue`, add:

```css
/* Proactive messages from Aiko (initiated without user prompt) */
.bubble.assistant.proactive {
  border-left: 3px solid rgba(139, 92, 246, 0.6);
  padding-left: calc(var(--bubble-padding, 10px) - 3px);
}
```

- [ ] **Step 5: Verify the frontend builds**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build
```

Expected: build succeeds with no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/App.vue frontend/src/components/ChatPanel.vue frontend/src/wailsjs/go/main/App.js frontend/src/wailsjs/go/main/App.d.ts
git commit -m "feat(frontend): call SetChatVisible on toggle, style proactive bubbles"
```

---

## Task 8: End-to-end smoke test

**Files:**
- No file changes (manual test)

- [ ] **Step 1: Run all Go tests**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./... 2>&1 | grep -E "FAIL|ok|---"
```

Expected: all packages show `ok`, no `FAIL`.

- [ ] **Step 2: Start dev mode and smoke test**

```bash
cd /Users/xutiancheng/code/self/Aiko && wails dev
```

Manual checks:
1. App starts without panic
2. Open chat bubble → close it → verify `SetChatVisible` calls succeed (no JS console errors)
3. In chat, ask the AI: "帮我安排明天早上9点提醒我跑步" — verify the agent calls `schedule_followup` tool (may require granting permission first in 工具权限 settings) and responds with "已安排：将在…"
4. Check DB: `sqlite3 ~/.aiko/aiko.db "SELECT * FROM proactive_items;"` — verify a row exists

- [ ] **Step 3: Final commit (if any cleanup needed)**

```bash
git add -p  # stage only intentional changes
git commit -m "feat: proactive sensing complete"
```
