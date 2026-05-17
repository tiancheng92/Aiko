package mcp_test

import (
	"context"
	"database/sql"
	"io"
	"testing"
	"time"

	internalmcp "aiko/internal/mcp"

	"github.com/cloudwego/eino/components/tool"
	_ "modernc.org/sqlite"
)

// newTestMCPDB creates an in-memory SQLite DB with the mcp_servers table.
// SetMaxOpenConns(1) ensures all connections share the same in-memory database.
func newTestMCPDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Restrict to one connection so all goroutines share the same in-memory DB
	// (modernc.org/sqlite opens a separate database per connection for ":memory:").
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS mcp_servers (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL UNIQUE,
			transport   TEXT NOT NULL,
			command     TEXT,
			args        TEXT,
			url         TEXT,
			headers     TEXT,
			enabled     INTEGER NOT NULL DEFAULT 1,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestLoadToolsAsync_NoServers verifies that LoadToolsAsync calls done promptly
// when there are no servers configured.
func TestLoadToolsAsync_NoServers(t *testing.T) {
	db := newTestMCPDB(t)
	store := internalmcp.NewServerStore(db)

	done := make(chan struct{})
	internalmcp.LoadToolsAsync(context.Background(), store, 5*time.Second,
		func(tools []tool.BaseTool, closers []io.Closer) {
			close(done)
		})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("LoadToolsAsync did not call done within 3s with no servers")
	}
}

// TestLoadToolsAsync_CancelledContext verifies that cancelling the context
// causes LoadToolsAsync to complete rather than hang indefinitely.
func TestLoadToolsAsync_CancelledContext(t *testing.T) {
	db := newTestMCPDB(t)
	store := internalmcp.NewServerStore(db)

	_, err := db.Exec(`INSERT INTO mcp_servers(name, transport, url, enabled) VALUES ('test', 'sse', 'http://127.0.0.1:19999/sse', 1)`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	internalmcp.LoadToolsAsync(ctx, store, 30*time.Second,
		func(_ []tool.BaseTool, closers []io.Closer) {
			for _, c := range closers {
				_ = c.Close()
			}
			close(done)
		})

	// Cancel immediately so the per-server goroutine hits a context error.
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("LoadToolsAsync did not call done after context cancel")
	}
}

// TestLoadToolsAsync_ConnectionRefused verifies that a server at an unreachable
// address is silently skipped and done is still called with zero tools.
func TestLoadToolsAsync_ConnectionRefused(t *testing.T) {
	db := newTestMCPDB(t)
	store := internalmcp.NewServerStore(db)

	_, err := db.Exec(`INSERT INTO mcp_servers(name, transport, url, enabled) VALUES ('refused', 'sse', 'http://127.0.0.1:19998/sse', 1)`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}

	type result struct{ count int }
	doneCh := make(chan result, 1)
	internalmcp.LoadToolsAsync(context.Background(), store, 2*time.Second,
		func(tools []tool.BaseTool, closers []io.Closer) {
			for _, c := range closers {
				_ = c.Close()
			}
			doneCh <- result{len(tools)}
		})

	select {
	case r := <-doneCh:
		if r.count != 0 {
			t.Errorf("expected 0 tools for refused connection, got %d", r.count)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("LoadToolsAsync did not call done within timeout for refused connection")
	}
}

// TestLoadToolsAsync_DisabledServersSkipped verifies that disabled servers are
// not contacted and done is called promptly.
func TestLoadToolsAsync_DisabledServersSkipped(t *testing.T) {
	db := newTestMCPDB(t)
	store := internalmcp.NewServerStore(db)

	_, err := db.Exec(`INSERT INTO mcp_servers(name, transport, url, enabled) VALUES ('disabled', 'sse', 'http://127.0.0.1:19997/sse', 0)`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}

	done := make(chan struct{})
	internalmcp.LoadToolsAsync(context.Background(), store, 5*time.Second,
		func(_ []tool.BaseTool, _ []io.Closer) {
			close(done)
		})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("LoadToolsAsync did not call done promptly with only disabled servers")
	}
}
