package db_test

import (
	"testing"

	"aiko/internal/db"
)

// TestMigrateProactiveItems verifies that the proactive_items table is
// created by the migration and has the expected columns.
func TestMigrateProactiveItems(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// Insert a row and read it back to confirm schema.
	_, err = database.Exec(
		`INSERT INTO proactive_items (trigger_at, prompt) VALUES (datetime('now'), 'hello')`,
	)
	if err != nil {
		t.Fatalf("insert proactive_items: %v", err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM proactive_items`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}
