package config_test

import (
	"database/sql"
	"testing"

	"aiko/internal/config"

	_ "modernc.org/sqlite"
)

// newTestDB creates an in-memory SQLite DB with the settings table.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestConfigRenderBackend_RoundTrip tests that RenderBackend and VRMModel round-trip through Save/Load.
func TestConfigRenderBackend_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	cfg := &config.Config{
		RenderBackend: "vrm",
		VRMModel:      "my_model.vrm",
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.RenderBackend != "vrm" {
		t.Errorf("RenderBackend: got %q, want %q", loaded.RenderBackend, "vrm")
	}
	if loaded.VRMModel != "my_model.vrm" {
		t.Errorf("VRMModel: got %q, want %q", loaded.VRMModel, "my_model.vrm")
	}
}

// TestConfigRenderBackend_Defaults tests that RenderBackend defaults to "live2d" and VRMModel to "".
func TestConfigRenderBackend_Defaults(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.RenderBackend != "live2d" {
		t.Errorf("RenderBackend default: got %q, want %q", loaded.RenderBackend, "live2d")
	}
	if loaded.VRMModel != "" {
		t.Errorf("VRMModel default: got %q, want %q", loaded.VRMModel, "")
	}
}

// TestConfigThemeStyle_RoundTrip tests that ThemeStyle round-trips through Save/Load.
func TestConfigThemeStyle_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	cfg := &config.Config{ThemeStyle: "frosted"}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ThemeStyle != "frosted" {
		t.Errorf("ThemeStyle: got %q, want %q", loaded.ThemeStyle, "frosted")
	}
}

// TestConfigThemeStyle_Default tests that ThemeStyle defaults to "frosted".
func TestConfigThemeStyle_Default(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ThemeStyle != "frosted" {
		t.Errorf("ThemeStyle default: got %q, want %q", loaded.ThemeStyle, "frosted")
	}
}

// TestApplyProfile_Inherit verifies that inherit=true copies LLM base_url/api_key to embedding fields.
func TestApplyProfile_Inherit(t *testing.T) {
	p := &config.ModelProfile{
		BaseURL:          "http://llm.local/v1",
		APIKey:           "llm-key",
		Model:            "llama3",
		EmbeddingModel:   "nomic-embed",
		EmbeddingDim:     768,
		EmbeddingInherit: true,
		EmbeddingBaseURL: "",
		EmbeddingAPIKey:  "",
	}
	cfg := &config.Config{}
	cfg.ApplyProfile(p)

	if cfg.EmbeddingBaseURL != "http://llm.local/v1" {
		t.Errorf("EmbeddingBaseURL: got %q, want %q", cfg.EmbeddingBaseURL, "http://llm.local/v1")
	}
	if cfg.EmbeddingAPIKey != "llm-key" {
		t.Errorf("EmbeddingAPIKey: got %q, want %q", cfg.EmbeddingAPIKey, "llm-key")
	}
}

// TestApplyProfile_Independent verifies that inherit=false uses the dedicated embedding fields.
func TestApplyProfile_Independent(t *testing.T) {
	p := &config.ModelProfile{
		BaseURL:          "http://llm.local/v1",
		APIKey:           "llm-key",
		Model:            "llama3",
		EmbeddingModel:   "text-embedding-3-small",
		EmbeddingDim:     1536,
		EmbeddingInherit: false,
		EmbeddingBaseURL: "https://api.openai.com/v1",
		EmbeddingAPIKey:  "emb-key",
	}
	cfg := &config.Config{}
	cfg.ApplyProfile(p)

	if cfg.EmbeddingBaseURL != "https://api.openai.com/v1" {
		t.Errorf("EmbeddingBaseURL: got %q, want %q", cfg.EmbeddingBaseURL, "https://api.openai.com/v1")
	}
	if cfg.EmbeddingAPIKey != "emb-key" {
		t.Errorf("EmbeddingAPIKey: got %q, want %q", cfg.EmbeddingAPIKey, "emb-key")
	}
	// LLM fields must not be affected by this check.
	if cfg.LLMBaseURL != "http://llm.local/v1" {
		t.Errorf("LLMBaseURL: got %q, want %q", cfg.LLMBaseURL, "http://llm.local/v1")
	}
}

// TestVectorEnabled_EmptyModel verifies VectorEnabled=false when EmbeddingModel is empty.
func TestVectorEnabled_EmptyModel(t *testing.T) {
	cfg := &config.Config{
		EmbeddingBaseURL: "http://llm.local/v1",
		EmbeddingModel:   "",
	}
	if cfg.VectorEnabled() {
		t.Error("VectorEnabled: expected false when EmbeddingModel is empty")
	}
}

// TestVectorEnabled_EmptyURL verifies VectorEnabled=false when EmbeddingBaseURL is empty.
func TestVectorEnabled_EmptyURL(t *testing.T) {
	cfg := &config.Config{
		EmbeddingModel:   "text-embedding-3-small",
		EmbeddingBaseURL: "",
	}
	if cfg.VectorEnabled() {
		t.Error("VectorEnabled: expected false when EmbeddingBaseURL is empty")
	}
}

// TestConfigLanguage_RoundTrip tests that Language round-trips through Save/Load.
func TestConfigLanguage_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	cfg := &config.Config{Language: "ja"}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Language != "ja" {
		t.Errorf("Language: got %q, want %q", loaded.Language, "ja")
	}
}

// TestConfigLanguage_Default tests that Language defaults to empty string.
func TestConfigLanguage_Default(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Language != "" {
		t.Errorf("Language default: got %q, want %q", loaded.Language, "")
	}
}

// TestVectorEnabled_Configured verifies VectorEnabled=true when both fields are set.
func TestVectorEnabled_Configured(t *testing.T) {
	cfg := &config.Config{
		EmbeddingModel:   "text-embedding-3-small",
		EmbeddingBaseURL: "https://api.openai.com/v1",
	}
	if !cfg.VectorEnabled() {
		t.Error("VectorEnabled: expected true when model and base_url are set")
	}
}
