package config

import (
	"database/sql"
	"strconv"
	"strings"
)

// Config holds all application settings.
type Config struct {
	LLMBaseURL     string
	LLMAPIKey      string
	LLMModel       string
	EmbeddingModel string
	Live2DModel    string // 模型目录名，默认 "hiyori"
	EmbeddingDim   int
	SystemPrompt   string
	ShortTermLimit int
	SkillsDirs     []string // skills 目录列表，支持多个路径
	PetSize        int // 宠物显示尺寸（像素），0 表示自动根据屏幕高度计算
	ChatWidth      int // 聊天框宽度（像素），0 表示使用默认值
	ChatHeight     int // 聊天框高度（像素），0 表示使用默认值
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
		Live2DModel:    orDefault(m["live2d_model"], "hiyori"),
		SystemPrompt:   m["system_prompt"],
		SkillsDirs:     splitLines(m["skills_dirs"]),
	}
	cfg.EmbeddingDim = parseInt(m["embedding_dim"], 1536)
	cfg.ShortTermLimit = parseInt(m["short_term_limit"], 30)
	cfg.PetSize = parseInt(m["pet_size"], 0)
	cfg.ChatWidth = parseInt(m["chat_width"], 0)
	cfg.ChatHeight = parseInt(m["chat_height"], 0)
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
		"skills_dirs":      joinLines(cfg.SkillsDirs),
		"live2d_model":     cfg.Live2DModel,
		"pet_size":         strconv.Itoa(cfg.PetSize),
		"chat_width":       strconv.Itoa(cfg.ChatWidth),
		"chat_height":      strconv.Itoa(cfg.ChatHeight),
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

// splitLines splits a newline-separated string into non-empty trimmed lines.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// joinLines joins a slice of strings with newlines.
func joinLines(ss []string) string { return strings.Join(ss, "\n") }

