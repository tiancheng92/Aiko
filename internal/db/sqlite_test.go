package db_test

import (
	"testing"

	"aiko/internal/db"
)

// TestMigrateDropsFiredColumn verifies that after migration proactive_items has no fired column.
func TestMigrateDropsFiredColumn(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// Try inserting with fired column — should fail.
	_, err = database.Exec(`INSERT INTO proactive_items (trigger_at, prompt, fired) VALUES ('2099-01-01 00:00:00', 'test', 1)`)
	if err == nil {
		t.Fatal("expected error inserting fired column, but got none — fired column still exists")
	}
}

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

// TestMigrateModelProfilesEmbeddingColumns verifies that after migration
// model_profiles has the three new embedding columns with expected defaults.
func TestMigrateModelProfilesEmbeddingColumns(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// Insert a minimal row — new columns must accept omission (use DEFAULT).
	_, err = database.Exec(
		`INSERT INTO model_profiles (name, provider, base_url, api_key, model,
		  embedding_model, embedding_dim, tts_model, tts_voice, tts_speed, tts_backend)
		 VALUES ('test', 'openai', '', '', 'gpt-4o', '', 1536, '', '', 1.0, '')`,
	)
	if err != nil {
		t.Fatalf("insert minimal row: %v", err)
	}

	var inherit int
	var embBaseURL, embAPIKey string
	err = database.QueryRow(
		`SELECT embedding_inherit, embedding_base_url, embedding_api_key
		 FROM model_profiles WHERE name = 'test'`,
	).Scan(&inherit, &embBaseURL, &embAPIKey)
	if err != nil {
		t.Fatalf("scan new columns: %v", err)
	}
	if inherit != 1 {
		t.Errorf("embedding_inherit default: got %d, want 1", inherit)
	}
	if embBaseURL != "" {
		t.Errorf("embedding_base_url default: got %q, want ''", embBaseURL)
	}
	if embAPIKey != "" {
		t.Errorf("embedding_api_key default: got %q, want ''", embAPIKey)
	}
}
