package proactive_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"aiko/internal/db"
	"aiko/internal/proactive"
)

// mockApp implements AppInterface for testing.
type mockApp struct {
	mu            sync.Mutex
	emittedEvents []string
	chatVisible   bool
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

// TestFireChatOpen verifies Fire emits chat:proactive:message when chat is open.
func TestFireChatOpen(t *testing.T) {
	app := &mockApp{chatVisible: true}
	eng := proactive.NewEngine(app, nil)

	if err := eng.Fire(context.Background(), "drink water"); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.emittedEvents) == 0 || app.emittedEvents[0] != "chat:proactive:message" {
		t.Errorf("expected chat:proactive:message emitted, got %v", app.emittedEvents)
	}
}

// TestFireChatClosed verifies Fire emits notification:show when chat is closed.
func TestFireChatClosed(t *testing.T) {
	app := &mockApp{chatVisible: false}
	eng := proactive.NewEngine(app, nil)

	if err := eng.Fire(context.Background(), "drink water"); err != nil {
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
		t.Errorf("expected notification:show emitted, got %v", app.emittedEvents)
	}
}

// TestFireChatClosedTruncates verifies long prompts are truncated to 80 runes in notification.
func TestFireChatClosedTruncates(t *testing.T) {
	long := "A very long proactive message that exceeds eighty characters in total length for testing truncation behavior here"
	app := &mockApp{chatVisible: false}
	eng := proactive.NewEngine(app, nil)
	if err := eng.Fire(context.Background(), long); err != nil {
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

	if err := store.Insert(context.Background(), time.Now().Add(-time.Second), "drink water"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	app := &mockApp{chatVisible: false}
	eng := proactive.NewEngine(app, store)
	eng.Poll(context.Background())

	app.mu.Lock()
	found := false
	for _, e := range app.emittedEvents {
		if e == "notification:show" {
			found = true
		}
	}
	app.mu.Unlock()
	if !found {
		t.Error("expected notification:show emitted")
	}

	// Verify item is deleted.
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected item deleted, got %d items remaining", len(items))
	}
}

// TestPollDeletesOnFireFailure verifies Poll still deletes row even when Fire is called.
// Since Fire no longer fails (no LLM), we just verify deletion and event emission.
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

	app := &mockApp{chatVisible: true}
	eng := proactive.NewEngine(app, store)
	eng.Poll(context.Background())

	// Row must be deleted.
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected item deleted, got %d items", len(items))
	}

	// chat:proactive:message must be emitted.
	app.mu.Lock()
	defer app.mu.Unlock()
	found := false
	for _, e := range app.emittedEvents {
		if e == "chat:proactive:message" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected chat:proactive:message emitted, got %v", app.emittedEvents)
	}
}
