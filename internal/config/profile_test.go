package config_test

import (
	"database/sql"
	"testing"

	"aiko/internal/config"
	"aiko/internal/db"
)

func newProfileTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// TestProfileRoundTrip_EmbeddingInherit verifies that EmbeddingInherit=true
// round-trips through Save/Get.
func TestProfileRoundTrip_EmbeddingInherit(t *testing.T) {
	store := config.NewProfileStore(newProfileTestDB(t))

	p := &config.ModelProfile{
		Name:             "inherit-true",
		Provider:         config.ProviderOpenAI,
		BaseURL:          "http://localhost:11434/v1",
		APIKey:           "key1",
		Model:            "llama3",
		EmbeddingInherit: true,
		EmbeddingBaseURL: "",
		EmbeddingAPIKey:  "",
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.EmbeddingInherit {
		t.Errorf("EmbeddingInherit: got false, want true")
	}
	if got.EmbeddingBaseURL != "" {
		t.Errorf("EmbeddingBaseURL: got %q, want ''", got.EmbeddingBaseURL)
	}
}

// TestProfileRoundTrip_EmbeddingIndependent verifies that EmbeddingInherit=false
// with custom URL/key round-trips correctly.
func TestProfileRoundTrip_EmbeddingIndependent(t *testing.T) {
	store := config.NewProfileStore(newProfileTestDB(t))

	p := &config.ModelProfile{
		Name:             "inherit-false",
		Provider:         config.ProviderOpenAI,
		BaseURL:          "http://llm.local/v1",
		APIKey:           "llm-key",
		Model:            "gpt-4o",
		EmbeddingInherit: false,
		EmbeddingBaseURL: "https://api.openai.com/v1",
		EmbeddingAPIKey:  "emb-key",
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EmbeddingInherit {
		t.Errorf("EmbeddingInherit: got true, want false")
	}
	if got.EmbeddingBaseURL != "https://api.openai.com/v1" {
		t.Errorf("EmbeddingBaseURL: got %q", got.EmbeddingBaseURL)
	}
	if got.EmbeddingAPIKey != "emb-key" {
		t.Errorf("EmbeddingAPIKey: got %q", got.EmbeddingAPIKey)
	}
}

// TestProfileList_EmbeddingColumns verifies that List returns the new fields.
func TestProfileList_EmbeddingColumns(t *testing.T) {
	store := config.NewProfileStore(newProfileTestDB(t))

	p := &config.ModelProfile{
		Name:             "list-test",
		Provider:         config.ProviderOpenAI,
		Model:            "gpt-4o",
		EmbeddingInherit: false,
		EmbeddingBaseURL: "https://emb.example.com/v1",
		EmbeddingAPIKey:  "emb-key2",
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("List len: got %d, want 1", len(profiles))
	}
	got := profiles[0]
	if got.EmbeddingInherit {
		t.Errorf("EmbeddingInherit: got true, want false")
	}
	if got.EmbeddingBaseURL != "https://emb.example.com/v1" {
		t.Errorf("EmbeddingBaseURL: got %q", got.EmbeddingBaseURL)
	}
}
