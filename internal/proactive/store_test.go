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
