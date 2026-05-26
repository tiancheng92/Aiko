package memory_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/schema"
	_ "modernc.org/sqlite"

	"aiko/internal/memory"
)

func newTestShortStore(t *testing.T) *memory.ShortStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		thinking_content TEXT NOT NULL DEFAULT '',
		images TEXT NOT NULL DEFAULT '',
		files TEXT NOT NULL DEFAULT '',
		migrated_to_long INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return memory.NewShortStore(db)
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
	if _, err := s.Add("user", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("assistant", "hi there"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("user", "how are you"); err != nil {
		t.Fatal(err)
	}

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
	for range 5 {
		if _, err := s.Add("user", "msg"); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := s.RecentMessages(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3, got %d", len(msgs))
	}
}

func TestAddFull_WithThinkingContent(t *testing.T) {
	s := newTestShortStore(t)
	_, err := s.AddFull("assistant", "response", "thinking process", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Recent(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ThinkingContent != "thinking process" {
		t.Errorf("expected ThinkingContent %q, got %q", "thinking process", msgs[0].ThinkingContent)
	}
}

func newTestShortStoreWithFTS(t *testing.T) *memory.ShortStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		thinking_content TEXT NOT NULL DEFAULT '',
		images TEXT NOT NULL DEFAULT '',
		files TEXT NOT NULL DEFAULT '',
		migrated_to_long INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE VIRTUAL TABLE messages_fts USING fts5(content, content=messages, content_rowid=id)`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	// modernc.org/sqlite does not auto-create content-sync triggers.
	for _, trig := range []string{
		`CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content) VALUES('delete', old.id, old.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content) VALUES('delete', old.id, old.content);
			INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
		END`,
	} {
		if _, err := db.Exec(trig); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	// Populate the FTS index initially.
	_, err = db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return memory.NewShortStore(db)
}

func TestSearch_FindsMatches(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	s.Add("user", "hello world")
	s.Add("assistant", "hi there")
	s.Add("user", "world tour")

	results, err := s.Search("world")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// newest first
	if results[0].Content != "world tour" {
		t.Errorf("msg[0]: want 'world tour', got %q", results[0].Content)
	}
	if results[1].Content != "hello world" {
		t.Errorf("msg[1]: want 'hello world', got %q", results[1].Content)
	}
}

func TestSearch_NoResults(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	s.Add("user", "hello")

	results, err := s.Search("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	s.Add("user", "hello")

	results, err := s.Search("")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestGetNewestToID_IncludesTarget(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	var ids []int64
	for i := range 25 {
		id, err := s.Add("user", fmt.Sprintf("message %d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// Target is message 5 (the 6th message, early in history)
	msgs, err := s.GetNewestToID(ids[5], 10)
	if err != nil {
		t.Fatal(err)
	}
	// Should include target and everything newer
	if len(msgs) < 20 {
		t.Errorf("expected at least 20 messages, got %d", len(msgs))
	}
	found := false
	for _, m := range msgs {
		if m.ID == ids[5] {
			found = true
			break
		}
	}
	if !found {
		t.Error("target ID not found in results")
	}
}

func TestGetNewestToID_TargetIsRecent(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	var ids []int64
	for i := range 5 {
		id, err := s.Add("user", fmt.Sprintf("msg %d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	// Target is the newest message — should return just one page
	msgs, err := s.GetNewestToID(ids[4], 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}
