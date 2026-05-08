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
