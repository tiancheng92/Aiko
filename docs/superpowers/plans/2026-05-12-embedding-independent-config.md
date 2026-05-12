# Embedding Independent Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow the embedding model to use a separate Base URL and API Key from the chat model, with a per-profile "inherit" toggle defaulting to true.

**Architecture:** Add three fields (`EmbeddingInherit`, `EmbeddingBaseURL`, `EmbeddingAPIKey`) to `ModelProfile` and `Config`; `ApplyProfile` resolves which values to use; `NewEmbedder` reads the resolved `Config` fields; frontend profile form shows the independent fields conditionally.

**Tech Stack:** Go (config, llm packages), SQLite migrations, Vue 3 `<script setup>`

---

### Task 1: DB migration — add three columns to model_profiles

**Files:**
- Modify: `internal/db/sqlite.go`
- Test: `internal/db/sqlite_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/db/sqlite_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/db/... -run TestMigrateModelProfilesEmbeddingColumns -v
```

Expected: FAIL — column `embedding_inherit` does not exist.

- [ ] **Step 3: Add migration patches to sqlite.go**

In `internal/db/sqlite.go`, append three entries to the `patches` slice inside `migrate()`:

```go
// v6: embedding model may use a separate base_url and api_key.
`ALTER TABLE model_profiles ADD COLUMN embedding_inherit  INTEGER NOT NULL DEFAULT 1`,
`ALTER TABLE model_profiles ADD COLUMN embedding_base_url TEXT    NOT NULL DEFAULT ''`,
`ALTER TABLE model_profiles ADD COLUMN embedding_api_key  TEXT    NOT NULL DEFAULT ''`,
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/db/... -run TestMigrateModelProfilesEmbeddingColumns -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/db/sqlite.go internal/db/sqlite_test.go
git commit -m "feat(db): add embedding_inherit/base_url/api_key columns to model_profiles"
```

---

### Task 2: ModelProfile struct + SQL (profile.go)

**Files:**
- Modify: `internal/config/profile.go`

- [ ] **Step 1: Write the failing test**

Add a new file `internal/config/profile_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/... -run "TestProfile" -v
```

Expected: FAIL — `config.ModelProfile` has no field `EmbeddingInherit`.

- [ ] **Step 3: Add fields to ModelProfile struct**

In `internal/config/profile.go`, add to `ModelProfile`:

```go
EmbeddingInherit bool   `json:"embedding_inherit"`
EmbeddingBaseURL string `json:"embedding_base_url"`
EmbeddingAPIKey  string `json:"embedding_api_key"`
```

Full updated struct:

```go
type ModelProfile struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Provider         Provider `json:"provider"`
	BaseURL          string   `json:"base_url"`
	APIKey           string   `json:"api_key"`
	Model            string   `json:"model"`
	EmbeddingModel   string   `json:"embedding_model"`
	EmbeddingDim     int      `json:"embedding_dim"`
	EmbeddingInherit bool     `json:"embedding_inherit"`
	EmbeddingBaseURL string   `json:"embedding_base_url"`
	EmbeddingAPIKey  string   `json:"embedding_api_key"`
	TTSModelDir      string   `json:"tts_model_dir"`
	TTSVoice         string   `json:"tts_voice"`
	TTSSpeed         float64  `json:"tts_speed"`
	TTSBackend       string   `json:"tts_backend"`
}
```

- [ ] **Step 4: Update Save() to default EmbeddingInherit=true on new profiles**

In `Save()`, after `if p.Provider == ""` block, add:

```go
if p.ID == 0 && !p.EmbeddingInherit {
    // Only override default when explicitly set false on insert.
    // For new profiles with zero value (false), we want true as the default.
    // We detect "not explicitly set" by checking if both EmbeddingBaseURL and
    // EmbeddingAPIKey are also empty — treat as inherit=true.
    if p.EmbeddingBaseURL == "" && p.EmbeddingAPIKey == "" {
        p.EmbeddingInherit = true
    }
}
```

- [ ] **Step 5: Update List() SQL and Scan**

Replace the `List()` method:

```go
// List returns all profiles ordered by id.
func (s *ProfileStore) List() ([]ModelProfile, error) {
	rows, err := s.db.Query(`
		SELECT id, name, provider, base_url, api_key, model, embedding_model, embedding_dim,
		       embedding_inherit, embedding_base_url, embedding_api_key,
		       tts_model, tts_voice, tts_speed, tts_backend
		FROM model_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelProfile
	for rows.Next() {
		var p ModelProfile
		var inheritInt int
		if err := rows.Scan(&p.ID, &p.Name, &p.Provider, &p.BaseURL, &p.APIKey,
			&p.Model, &p.EmbeddingModel, &p.EmbeddingDim,
			&inheritInt, &p.EmbeddingBaseURL, &p.EmbeddingAPIKey,
			&p.TTSModelDir, &p.TTSVoice, &p.TTSSpeed, &p.TTSBackend); err != nil {
			return nil, err
		}
		p.EmbeddingInherit = inheritInt != 0
		out = append(out, p)
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Update Get() SQL and Scan**

Replace the `Get()` method:

```go
// Get returns a single profile by id.
func (s *ProfileStore) Get(id int64) (*ModelProfile, error) {
	var p ModelProfile
	var inheritInt int
	err := s.db.QueryRow(`
		SELECT id, name, provider, base_url, api_key, model, embedding_model, embedding_dim,
		       embedding_inherit, embedding_base_url, embedding_api_key,
		       tts_model, tts_voice, tts_speed, tts_backend
		FROM model_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Provider, &p.BaseURL, &p.APIKey,
			&p.Model, &p.EmbeddingModel, &p.EmbeddingDim,
			&inheritInt, &p.EmbeddingBaseURL, &p.EmbeddingAPIKey,
			&p.TTSModelDir, &p.TTSVoice, &p.TTSSpeed, &p.TTSBackend)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("profile %d not found", id)
	}
	p.EmbeddingInherit = inheritInt != 0
	return &p, err
}
```

- [ ] **Step 7: Update Save() INSERT and UPDATE SQL**

Replace `Save()`:

```go
// Save inserts or updates a profile. Sets p.ID on insert.
func (s *ProfileStore) Save(p *ModelProfile) error {
	if p.EmbeddingDim == 0 {
		p.EmbeddingDim = 1536
	}
	if p.Provider == "" {
		p.Provider = ProviderOpenAI
	}
	// New profiles with no explicit independent embedding config default to inherit=true.
	if p.ID == 0 && !p.EmbeddingInherit && p.EmbeddingBaseURL == "" && p.EmbeddingAPIKey == "" {
		p.EmbeddingInherit = true
	}
	inheritInt := 0
	if p.EmbeddingInherit {
		inheritInt = 1
	}
	if p.ID == 0 {
		res, err := s.db.Exec(`
			INSERT INTO model_profiles(name, provider, base_url, api_key, model,
			  embedding_model, embedding_dim, embedding_inherit, embedding_base_url, embedding_api_key,
			  tts_model, tts_voice, tts_speed, tts_backend)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Name, p.Provider, p.BaseURL, p.APIKey, p.Model,
			p.EmbeddingModel, p.EmbeddingDim, inheritInt, p.EmbeddingBaseURL, p.EmbeddingAPIKey,
			p.TTSModelDir, p.TTSVoice, p.TTSSpeed, p.TTSBackend)
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.Exec(`
		UPDATE model_profiles SET name=?, provider=?, base_url=?, api_key=?, model=?,
		  embedding_model=?, embedding_dim=?, embedding_inherit=?, embedding_base_url=?, embedding_api_key=?,
		  tts_model=?, tts_voice=?, tts_speed=?, tts_backend=?
		WHERE id=?`,
		p.Name, p.Provider, p.BaseURL, p.APIKey, p.Model,
		p.EmbeddingModel, p.EmbeddingDim, inheritInt, p.EmbeddingBaseURL, p.EmbeddingAPIKey,
		p.TTSModelDir, p.TTSVoice, p.TTSSpeed, p.TTSBackend, p.ID)
	return err
}
```

- [ ] **Step 8: Run tests to verify they pass**

```bash
go test ./internal/config/... -run "TestProfile" -v
```

Expected: all three `TestProfile*` tests PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/config/profile.go internal/config/profile_test.go
git commit -m "feat(config): add EmbeddingInherit/BaseURL/APIKey to ModelProfile"
```

---

### Task 3: Config struct + ApplyProfile + VectorEnabled

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
// newProfilesTestDB creates an in-memory SQLite DB with both settings and model_profiles tables.
func newProfilesTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = database.Exec(`
		CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
		CREATE TABLE IF NOT EXISTS model_profiles (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			provider        TEXT NOT NULL DEFAULT 'openai',
			base_url        TEXT NOT NULL DEFAULT '',
			api_key         TEXT NOT NULL DEFAULT '',
			model           TEXT NOT NULL DEFAULT '',
			embedding_model TEXT NOT NULL DEFAULT '',
			embedding_dim   INTEGER NOT NULL DEFAULT 1536,
			embedding_inherit  INTEGER NOT NULL DEFAULT 1,
			embedding_base_url TEXT NOT NULL DEFAULT '',
			embedding_api_key  TEXT NOT NULL DEFAULT '',
			tts_model       TEXT NOT NULL DEFAULT '',
			tts_voice       TEXT NOT NULL DEFAULT '',
			tts_speed       REAL NOT NULL DEFAULT 1.0,
			tts_backend     TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
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
	// LLM fields must not be overwritten.
	if cfg.LLMBaseURL != "http://llm.local/v1" {
		t.Errorf("LLMBaseURL: got %q, want %q", cfg.LLMBaseURL, "http://llm.local/v1")
	}
}

// TestVectorEnabled_InheritEmptyModel verifies VectorEnabled=false when EmbeddingModel is empty.
func TestVectorEnabled_InheritEmptyModel(t *testing.T) {
	cfg := &config.Config{
		EmbeddingBaseURL: "http://llm.local/v1",
		EmbeddingModel:   "",
	}
	if cfg.VectorEnabled() {
		t.Error("VectorEnabled: expected false when EmbeddingModel is empty")
	}
}

// TestVectorEnabled_IndependentEmptyURL verifies VectorEnabled=false when EmbeddingBaseURL is empty.
func TestVectorEnabled_IndependentEmptyURL(t *testing.T) {
	cfg := &config.Config{
		EmbeddingModel:   "text-embedding-3-small",
		EmbeddingBaseURL: "",
	}
	if cfg.VectorEnabled() {
		t.Error("VectorEnabled: expected false when EmbeddingBaseURL is empty")
	}
}

// TestVectorEnabled_IndependentConfigured verifies VectorEnabled=true when both fields are set.
func TestVectorEnabled_IndependentConfigured(t *testing.T) {
	cfg := &config.Config{
		EmbeddingModel:   "text-embedding-3-small",
		EmbeddingBaseURL: "https://api.openai.com/v1",
	}
	if !cfg.VectorEnabled() {
		t.Error("VectorEnabled: expected true when model and base_url are set")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -run "TestApplyProfile|TestVectorEnabled" -v
```

Expected: FAIL — `config.Config` has no field `EmbeddingBaseURL`.

- [ ] **Step 3: Add fields to Config struct**

In `internal/config/config.go`, add after `EmbeddingDim int`:

```go
EmbeddingBaseURL string
EmbeddingAPIKey  string
```

- [ ] **Step 4: Update ApplyProfile()**

Replace the `ApplyProfile` method body:

```go
// ApplyProfile overwrites LLM-related fields from the given profile.
func (c *Config) ApplyProfile(p *ModelProfile) {
	c.LLMBaseURL = p.BaseURL
	c.LLMAPIKey = p.APIKey
	c.LLMModel = p.Model
	c.LLMProvider = string(p.Provider)
	c.EmbeddingModel = p.EmbeddingModel
	c.EmbeddingDim = p.EmbeddingDim
	c.ActiveProfileID = p.ID
	// Apply defaults and write back to profile so it persists.
	if c.LLMBaseURL == "" && c.LLMProvider == "openrouter" {
		c.LLMBaseURL = "https://openrouter.ai/api/v1"
		p.BaseURL = c.LLMBaseURL
	}
	// Resolve embedding endpoint: inherit from LLM config or use dedicated values.
	if p.EmbeddingInherit {
		c.EmbeddingBaseURL = c.LLMBaseURL
		c.EmbeddingAPIKey = c.LLMAPIKey
	} else {
		c.EmbeddingBaseURL = p.EmbeddingBaseURL
		c.EmbeddingAPIKey = p.EmbeddingAPIKey
	}
	c.TTSModelDir = p.TTSModelDir
	c.TTSVoice = p.TTSVoice
	if p.TTSSpeed == 0 {
		c.TTSSpeed = 1.0
	} else {
		c.TTSSpeed = p.TTSSpeed
	}
	c.TTSBackend = p.TTSBackend
}
```

- [ ] **Step 5: Update VectorEnabled()**

Replace the `VectorEnabled` method:

```go
// VectorEnabled reports whether embedding is configured.
func (c *Config) VectorEnabled() bool {
	return c.EmbeddingModel != "" && c.EmbeddingBaseURL != ""
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/config/... -run "TestApplyProfile|TestVectorEnabled" -v
```

Expected: all five tests PASS.

- [ ] **Step 7: Run full config test suite**

```bash
go test ./internal/config/... -v
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add EmbeddingBaseURL/APIKey to Config, update ApplyProfile and VectorEnabled"
```

---

### Task 4: Update NewEmbedder to use dedicated Config fields

**Files:**
- Modify: `internal/llm/client.go`

- [ ] **Step 1: Update NewEmbedder**

Replace the `NewEmbedder` function in `internal/llm/client.go`:

```go
// NewEmbedder creates an eino Embedder from config. Returns nil, nil if embedding not configured.
// Uses cfg.EmbeddingBaseURL and cfg.EmbeddingAPIKey (resolved by ApplyProfile from the active
// ModelProfile — either inherited from LLM config or independently set).
func NewEmbedder(ctx context.Context, cfg *config.Config) (embedding.Embedder, error) {
	if !cfg.VectorEnabled() {
		return nil, nil
	}
	return embeddopenai.NewEmbedder(ctx, &embeddopenai.EmbeddingConfig{
		BaseURL: cfg.EmbeddingBaseURL,
		APIKey:  cfg.EmbeddingAPIKey,
		Model:   cfg.EmbeddingModel,
	})
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```

Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/llm/client.go
git commit -m "feat(llm): NewEmbedder uses dedicated EmbeddingBaseURL/APIKey from Config"
```

---

### Task 5: Frontend — profile form UI + JS

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: Update profileForm default value**

Find line 85 in `SettingsWindow.vue`:

```js
const profileForm = ref({ id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '' })
```

Replace with:

```js
const profileForm = ref({ id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, embedding_inherit: true, embedding_base_url: '', embedding_api_key: '', tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '' })
```

- [ ] **Step 2: Update openProfileForm() default**

Find in `openProfileForm()` (around line 355):

```js
profileForm.value = { id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '' }
```

Replace with:

```js
profileForm.value = { id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, embedding_inherit: true, embedding_base_url: '', embedding_api_key: '', tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '' }
```

- [ ] **Step 3: Update editProfile() to preserve new fields**

Find in `editProfile()` (around line 363):

```js
profileForm.value = { ...p, tts_backend: p.tts_backend || '' }
```

Replace with:

```js
profileForm.value = { ...p, embedding_inherit: p.embedding_inherit ?? true, tts_backend: p.tts_backend || '' }
```

- [ ] **Step 4: Add UI in the template**

Find in the template (around line 1165) the `向量维度` label:

```html
<label>向量维度<span class="field-hint">与所选向量模型保持一致，默认 1536</span><input type="number" v-model.number="profileForm.embedding_dim" min="256" max="4096" /></label>
```

After that line, insert:

```html
<label class="checkbox-label" style="display:flex;align-items:center;gap:8px;margin-top:8px;font-size:13px">
  <input type="checkbox" v-model="profileForm.embedding_inherit" style="width:14px;height:14px;flex-shrink:0" />
  向量模型继承 Chat 模型配置（使用相同的 Base URL 和 API Key）
</label>
<template v-if="!profileForm.embedding_inherit">
  <label style="margin-top:8px">向量模型 Base URL
    <input
      v-model="profileForm.embedding_base_url"
      placeholder="https://api.openai.com/v1"
      spellcheck="false" autocorrect="off" autocomplete="off"
    />
  </label>
  <label>向量模型 API Key
    <input v-model="profileForm.embedding_api_key" type="password" placeholder="（可选）" spellcheck="false" autocorrect="off" autocomplete="off" />
  </label>
</template>
```

- [ ] **Step 5: Run frontend build to check for errors**

```bash
cd frontend && yarn build 2>&1 | tail -20
```

Expected: build succeeds with no errors.

- [ ] **Step 6: Run Go build to confirm Wails bindings still compile**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(frontend): add embedding inherit toggle and independent base_url/api_key fields to profile form"
```

---

### Task 6: Smoke test end-to-end

**Files:** none modified

- [ ] **Step 1: Run full test suite**

```bash
go test ./... 2>&1 | grep -E "FAIL|ok|---"
```

Expected: all packages report `ok`, no `FAIL`.

- [ ] **Step 2: Build and run the app**

```bash
make run
```

Open Settings → Model → edit an existing profile.
Verify:
1. "向量模型继承 Chat 模型配置" checkbox is checked by default
2. Unchecking reveals "向量模型 Base URL" and "向量模型 API Key" inputs
3. Re-checking hides the independent fields
4. Save + reopen the profile: independent URL/key persist correctly

- [ ] **Step 3: Test VectorEnabled path**

In Settings → Model → edit a profile:
1. Uncheck inherit, leave Base URL empty, save
2. Restart app (or trigger agent init via config change)
3. Confirm in logs that embedder is nil (knowledge base disabled, no crash)

- [ ] **Step 4: Final commit if any fixes needed**

If smoke test required any fixes, commit them:

```bash
git add -p
git commit -m "fix: address smoke test findings for embedding independent config"
```
