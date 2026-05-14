package memory_test

import (
	"database/sql"
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
