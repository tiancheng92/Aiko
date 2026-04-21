package config

import (
	"database/sql"
	"strconv"
)

// Config holds all application settings.
type Config struct {
	LLMBaseURL     string
	LLMAPIKey      string
	LLMModel       string
	EmbeddingModel string
	EmbeddingDim   int
	SystemPrompt   string
	ShortTermLimit int
	SkillsDir      string
	Hotkey         string
	BallPositionX    int
	BallPositionY    int
	BubblePositionX  int
	BubblePositionY  int
}

type Store struct{ db *sql.DB }

// NewStore creates a Config store backed by the given SQLite db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Load reads all settings from the database.
func (s *Store) Load() (*Config, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cfg := &Config{
		LLMBaseURL:     m["llm_base_url"],
		LLMAPIKey:      m["llm_api_key"],
		LLMModel:       m["llm_model"],
		EmbeddingModel: m["embedding_model"],
		SystemPrompt:   m["system_prompt"],
		SkillsDir:      m["skills_dir"],
		Hotkey:         orDefault(m["hotkey"], "Cmd+Shift+P"),
	}
	cfg.EmbeddingDim = parseInt(m["embedding_dim"], 1536)
	cfg.ShortTermLimit = parseInt(m["short_term_limit"], 30)
	cfg.BallPositionX   = parseInt(m["ball_position_x"], -1)
	cfg.BallPositionY   = parseInt(m["ball_position_y"], -1)
	cfg.BubblePositionX = parseInt(m["bubble_position_x"], -1)
	cfg.BubblePositionY = parseInt(m["bubble_position_y"], -1)
	return cfg, nil
}

// Save writes all settings to the database.
func (s *Store) Save(cfg *Config) error {
	pairs := map[string]string{
		"llm_base_url":     cfg.LLMBaseURL,
		"llm_api_key":      cfg.LLMAPIKey,
		"llm_model":        cfg.LLMModel,
		"embedding_model":  cfg.EmbeddingModel,
		"embedding_dim":    strconv.Itoa(cfg.EmbeddingDim),
		"system_prompt":    cfg.SystemPrompt,
		"short_term_limit": strconv.Itoa(cfg.ShortTermLimit),
		"skills_dir":       cfg.SkillsDir,
		"hotkey":           cfg.Hotkey,
		"ball_position_x":    strconv.Itoa(cfg.BallPositionX),
		"ball_position_y":    strconv.Itoa(cfg.BallPositionY),
		"bubble_position_x":  strconv.Itoa(cfg.BubblePositionX),
		"bubble_position_y":  strconv.Itoa(cfg.BubblePositionY),
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range pairs {
		if _, err := tx.Exec(
			`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			k, v,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MissingRequired returns names of required fields that are empty.
func (c *Config) MissingRequired() []string {
	var missing []string
	if c.LLMBaseURL == "" {
		missing = append(missing, "llm_base_url")
	}
	if c.LLMModel == "" {
		missing = append(missing, "llm_model")
	}
	return missing
}

// VectorEnabled reports whether embedding is configured.
func (c *Config) VectorEnabled() bool {
	return c.EmbeddingModel != ""
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

