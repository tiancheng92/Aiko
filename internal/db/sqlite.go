package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

// migrate creates all tables and indexes. CREATE TABLE/INDEX IF NOT EXISTS
// makes every statement a no-op on databases that already have the object.
func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			role             TEXT    NOT NULL,
			content          TEXT    NOT NULL,
			thinking_content TEXT    NOT NULL DEFAULT '',
			images           TEXT    NOT NULL DEFAULT '',
			files            TEXT    NOT NULL DEFAULT '',
			migrated_to_long INTEGER NOT NULL DEFAULT 0,
			created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_messages_id      ON messages(id DESC);
		CREATE INDEX IF NOT EXISTS idx_messages_role    ON messages(role);
		CREATE INDEX IF NOT EXISTS idx_messages_role_id ON messages(role, id DESC);

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

		CREATE TABLE IF NOT EXISTS cron_jobs (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			name          TEXT    NOT NULL,
			description   TEXT    NOT NULL,
			schedule      TEXT    NOT NULL,
			prompt        TEXT    NOT NULL,
			enabled       INTEGER NOT NULL DEFAULT 1,
			save_to_memory INTEGER NOT NULL DEFAULT 0,
			notify        INTEGER NOT NULL DEFAULT 1,
			next_run_at   DATETIME,
			last_run      DATETIME,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_cron_enabled ON cron_jobs(enabled);

		CREATE TABLE IF NOT EXISTS mcp_servers (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT    NOT NULL UNIQUE,
			transport  TEXT    NOT NULL,
			command    TEXT,
			args       TEXT,
			url        TEXT,
			headers    TEXT,
			enabled    INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS model_profiles (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			name                TEXT    NOT NULL UNIQUE,
			provider            TEXT    NOT NULL DEFAULT 'openai',
			base_url            TEXT    NOT NULL DEFAULT '',
			api_key             TEXT    NOT NULL DEFAULT '',
			model               TEXT    NOT NULL DEFAULT '',
			embedding_model     TEXT    NOT NULL DEFAULT '',
			embedding_dim       INTEGER NOT NULL DEFAULT 1536,
			embedding_inherit   INTEGER NOT NULL DEFAULT 1,
			embedding_provider  TEXT    NOT NULL DEFAULT 'openai',
			embedding_base_url  TEXT    NOT NULL DEFAULT '',
			embedding_api_key   TEXT    NOT NULL DEFAULT '',
			tts_model           TEXT    NOT NULL DEFAULT '',
			tts_voice           TEXT    NOT NULL DEFAULT '',
			tts_speed           REAL    NOT NULL DEFAULT 1.0,
			tts_backend         TEXT    NOT NULL DEFAULT '',
			supports_vision     INTEGER NOT NULL DEFAULT 0,
			created_at          DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS proactive_items (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			trigger_at DATETIME NOT NULL,
			prompt     TEXT     NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_proactive_trigger ON proactive_items(trigger_at ASC);

		CREATE TABLE IF NOT EXISTS summary (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			content    TEXT    NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}
