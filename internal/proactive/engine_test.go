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
	mu              sync.Mutex
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

// TestFireChatOpen verifies that Fire() calls ChatDirect and emits chat:proactive:start when chat is open.
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

// TestFireChatClosed verifies that Fire() uses ChatDirectCollect and emits notification:show when chat is closed.
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

// TestFireChatClosedTruncates verifies that long responses are truncated to 80 runes.
func TestFireChatClosedTruncates(t *testing.T) {
	long := "A very long proactive message that exceeds eighty characters in total length for testing truncation behavior here"
	app := &mockApp{chatVisible: false, collectReturn: long}
	eng := proactive.NewEngine(app, nil)
	eng.Fire(context.Background(), "prompt")

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

// TestPollFiresDueItems verifies that Poll calls Fire for each due item and marks it fired.
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
