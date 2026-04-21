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
	dbPath := filepath.Join(dataDir, "desktop-pet.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
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
			source TEXT PRIMARY KEY,
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS tool_permissions (
			tool_name        TEXT PRIMARY KEY,
			permission_level TEXT NOT NULL DEFAULT 'public',
			granted          INTEGER NOT NULL DEFAULT 0,
			granted_at       DATETIME,
			last_used        DATETIME
		);
	`)
	return err
}
