package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	_ "modernc.org/sqlite"
)

// Open opens (or creates) the SQLite database at dataDir and runs migrations.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "aiko.db")
	// Enable WAL mode and a 5-second busy timeout via DSN parameters so
	// concurrent goroutines (agent, knowledge import, config save) never see
	// "database is locked" errors.
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Limit to one open connection; SQLite WAL allows concurrent reads but
	// only one writer at a time — serialising through one connection is simplest.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// migrate creates all tables and applies idempotent column patches for DBs
// created before certain schema additions.
func migrate(db *sql.DB) error {
	// Create all tables in one shot with the current complete schema.
	// CREATE TABLE IF NOT EXISTS is a no-op for tables that already exist.
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS knowledge_sources (
			source   TEXT PRIMARY KEY,
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS tool_permissions (
			tool_name        TEXT PRIMARY KEY,
			permission_level TEXT NOT NULL DEFAULT 'public',
			granted          INTEGER NOT NULL DEFAULT 0,
			granted_at       DATETIME,
			last_used        DATETIME
		);
		CREATE TABLE IF NOT EXISTS memory_segments (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			vector_id   TEXT NOT NULL UNIQUE,
			raw_content TEXT NOT NULL,
			summary     TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memory_segments_created ON memory_segments(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_messages_id     ON messages(id DESC);
		CREATE INDEX IF NOT EXISTS idx_messages_role   ON messages(role);
		CREATE TABLE IF NOT EXISTS cron_jobs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			description TEXT NOT NULL,
			schedule    TEXT NOT NULL,
			prompt      TEXT NOT NULL,
			enabled     INTEGER NOT NULL DEFAULT 1,
			last_run    DATETIME,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_cron_enabled    ON cron_jobs(enabled);
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
		);
		CREATE TABLE IF NOT EXISTS model_profiles (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			provider        TEXT NOT NULL DEFAULT 'openai',
			base_url        TEXT NOT NULL DEFAULT '',
			api_key         TEXT NOT NULL DEFAULT '',
			model           TEXT NOT NULL DEFAULT '',
			embedding_model TEXT NOT NULL DEFAULT '',
			embedding_dim   INTEGER NOT NULL DEFAULT 1536,
			tts_model       TEXT NOT NULL DEFAULT '',
			tts_voice       TEXT NOT NULL DEFAULT '',
			tts_speed       REAL NOT NULL DEFAULT 1.0,
			tts_backend     TEXT NOT NULL DEFAULT '',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS proactive_items (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			trigger_at DATETIME NOT NULL,
			prompt     TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_proactive_trigger ON proactive_items(trigger_at ASC);
	`)
	if err != nil {
		return err
	}

	// Idempotent column patches for databases created before schema additions.
	patches := []string{
		// v2: store images as JSON array of data URLs alongside each message.
		`ALTER TABLE messages ADD COLUMN images TEXT NOT NULL DEFAULT ''`,
		// v3: store attached file names as JSON array alongside each message.
		`ALTER TABLE messages ADD COLUMN files TEXT NOT NULL DEFAULT ''`,
	}
	for _, p := range patches {
		if _, err := db.Exec(p); err != nil {
			// SQLite returns "duplicate column name" when the column already
			// exists; treat that as a no-op.
			if !isDuplicateColumnErr(err) {
				return fmt.Errorf("patch %q: %w", p, err)
			}
		}
	}

	return nil
}

// isDuplicateColumnErr reports whether err is the SQLite "duplicate column
// name" error returned when ALTER TABLE ADD COLUMN is run on an existing col.
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column name") ||
		strings.Contains(msg, "already exists")
}
