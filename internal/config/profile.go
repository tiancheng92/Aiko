package config

import (
	"database/sql"
	"errors"
	"fmt"
)

// Provider identifies which LLM backend to use.
type Provider string

const (
	ProviderOpenAI     Provider = "openai"     // OpenAI-compatible endpoints
	ProviderOpenRouter Provider = "openrouter" // OpenRouter
)

// ModelProfile holds a named set of LLM credentials and model selection.
type ModelProfile struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Provider       Provider `json:"provider"`
	BaseURL        string   `json:"base_url"`
	APIKey         string   `json:"api_key"`
	Model          string   `json:"model"`
	EmbeddingModel   string   `json:"embedding_model"`
	EmbeddingDim     int      `json:"embedding_dim"`
	EmbeddingInherit   bool     `json:"embedding_inherit"`
	EmbeddingProvider  string   `json:"embedding_provider"`
	EmbeddingBaseURL   string   `json:"embedding_base_url"`
	EmbeddingAPIKey    string   `json:"embedding_api_key"`
	TTSModelDir      string   `json:"tts_model_dir"` // kokoro 模型目录（含 onnx/voices bin）
	TTSVoice       string   `json:"tts_voice"`
	TTSSpeed       float64  `json:"tts_speed"`
	TTSBackend     string   `json:"tts_backend"` // "kokoro" | ""（系统 say）
}

// ProfileStore manages model_profiles rows.
type ProfileStore struct{ db *sql.DB }

// NewProfileStore creates a ProfileStore backed by db.
func NewProfileStore(db *sql.DB) *ProfileStore { return &ProfileStore{db: db} }

// List returns all profiles ordered by id.
func (s *ProfileStore) List() ([]ModelProfile, error) {
	rows, err := s.db.Query(`
		SELECT id, name, provider, base_url, api_key, model, embedding_model, embedding_dim,
		       embedding_inherit, embedding_provider, embedding_base_url, embedding_api_key,
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
			&inheritInt, &p.EmbeddingProvider, &p.EmbeddingBaseURL, &p.EmbeddingAPIKey,
			&p.TTSModelDir, &p.TTSVoice, &p.TTSSpeed, &p.TTSBackend); err != nil {
			return nil, err
		}
		p.EmbeddingInherit = inheritInt != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get returns a single profile by id.
func (s *ProfileStore) Get(id int64) (*ModelProfile, error) {
	var p ModelProfile
	var inheritInt int
	err := s.db.QueryRow(`
		SELECT id, name, provider, base_url, api_key, model, embedding_model, embedding_dim,
		       embedding_inherit, embedding_provider, embedding_base_url, embedding_api_key,
		       tts_model, tts_voice, tts_speed, tts_backend
		FROM model_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Provider, &p.BaseURL, &p.APIKey,
			&p.Model, &p.EmbeddingModel, &p.EmbeddingDim,
			&inheritInt, &p.EmbeddingProvider, &p.EmbeddingBaseURL, &p.EmbeddingAPIKey,
			&p.TTSModelDir, &p.TTSVoice, &p.TTSSpeed, &p.TTSBackend)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("profile %d not found", id)
		}
		return nil, err
	}
	p.EmbeddingInherit = inheritInt != 0
	return &p, nil
}

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
	if p.EmbeddingProvider == "" {
		p.EmbeddingProvider = "openai"
	}
	if p.ID == 0 {
		res, err := s.db.Exec(`
			INSERT INTO model_profiles(name, provider, base_url, api_key, model,
			  embedding_model, embedding_dim, embedding_inherit, embedding_provider, embedding_base_url, embedding_api_key,
			  tts_model, tts_voice, tts_speed, tts_backend)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Name, p.Provider, p.BaseURL, p.APIKey, p.Model,
			p.EmbeddingModel, p.EmbeddingDim, inheritInt, p.EmbeddingProvider, p.EmbeddingBaseURL, p.EmbeddingAPIKey,
			p.TTSModelDir, p.TTSVoice, p.TTSSpeed, p.TTSBackend)
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.Exec(`
		UPDATE model_profiles SET name=?, provider=?, base_url=?, api_key=?, model=?,
		  embedding_model=?, embedding_dim=?, embedding_inherit=?, embedding_provider=?, embedding_base_url=?, embedding_api_key=?,
		  tts_model=?, tts_voice=?, tts_speed=?, tts_backend=?
		WHERE id=?`,
		p.Name, p.Provider, p.BaseURL, p.APIKey, p.Model,
		p.EmbeddingModel, p.EmbeddingDim, inheritInt, p.EmbeddingProvider, p.EmbeddingBaseURL, p.EmbeddingAPIKey,
		p.TTSModelDir, p.TTSVoice, p.TTSSpeed, p.TTSBackend, p.ID)
	return err
}

// Delete removes a profile by id.
func (s *ProfileStore) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM model_profiles WHERE id=?`, id)
	return err
}
