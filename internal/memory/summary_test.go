package memory_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"aiko/internal/memory"
)

func newTestSummaryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE summary (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			content    TEXT    NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSummaryStore_GetEmpty(t *testing.T) {
	s := memory.NewSummaryStore(newTestSummaryDB(t))
	got, err := s.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestSummaryStore_SetAndGet(t *testing.T) {
	s := memory.NewSummaryStore(newTestSummaryDB(t))
	if err := s.Set("hello summary"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "hello summary" {
		t.Errorf("expected %q, got %q", "hello summary", got)
	}
}

func TestSummaryStore_SetOverwrites(t *testing.T) {
	s := memory.NewSummaryStore(newTestSummaryDB(t))
	_ = s.Set("first")
	_ = s.Set("second")
	got, err := s.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("expected %q, got %q", "second", got)
	}
}
