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
