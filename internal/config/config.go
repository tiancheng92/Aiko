package config

import (
	"database/sql"
	"strconv"
	"strings"
)

// Config holds all application settings.
type Config struct {
	// LLM fields are populated from the active ModelProfile at startup.
	LLMBaseURL     string
	LLMAPIKey      string
	LLMModel       string
	LLMProvider    string // "openai" or "openrouter"
	EmbeddingModel string
	Live2DModel    string // 模型目录名，默认 "hiyori"
	EmbeddingDim   int
	SystemPrompt   string
	ShortTermLimit int
	NudgeInterval  int      // 每隔多少轮触发一次 self-growth nudge，0 表示使用默认值 5
	SkillsDirs     []string // skills 目录列表，支持多个路径
	PetSize        int // 宠物显示尺寸（像素），0 表示自动根据屏幕高度计算
	ChatWidth      int // 聊天框宽度（像素），0 表示使用默认值
	ChatHeight     int // 聊天框高度（像素），0 表示使用默认值
	ActiveProfileID int64 // 当前激活的 ModelProfile ID，0 表示未设置
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
		LLMBaseURL:      m["llm_base_url"],
		LLMAPIKey:       m["llm_api_key"],
		LLMModel:        m["llm_model"],
		LLMProvider:     m["llm_provider"],
		EmbeddingModel:  m["embedding_model"],
		Live2DModel:     orDefault(m["live2d_model"], "hiyori"),
		SystemPrompt:    m["system_prompt"],
		SkillsDirs:      splitLines(m["skills_dirs"]),
	}
	cfg.EmbeddingDim = parseInt(m["embedding_dim"], 1536)
	cfg.ShortTermLimit = parseInt(m["short_term_limit"], 30)
	cfg.NudgeInterval = parseInt(m["nudge_interval"], 5)
	if cfg.NudgeInterval <= 0 {
		cfg.NudgeInterval = 5
	}
	cfg.PetSize = parseInt(m["pet_size"], 0)
	cfg.ChatWidth = parseInt(m["chat_width"], 0)
	cfg.ChatHeight = parseInt(m["chat_height"], 0)
	cfg.ActiveProfileID = int64(parseInt(m["active_profile_id"], 0))
	return cfg, nil
}

// Save writes all settings to the database.
func (s *Store) Save(cfg *Config) error {
	// Apply defaults for empty required fields.
	if cfg.LLMBaseURL == "" && cfg.LLMProvider == "openrouter" {
		cfg.LLMBaseURL = "https://openrouter.ai/api/v1"
	}
	pairs := map[string]string{
		"llm_base_url":      cfg.LLMBaseURL,
		"llm_api_key":       cfg.LLMAPIKey,
		"llm_model":         cfg.LLMModel,
		"llm_provider":      cfg.LLMProvider,
		"embedding_model":   cfg.EmbeddingModel,
		"embedding_dim":     strconv.Itoa(cfg.EmbeddingDim),
		"system_prompt":     cfg.SystemPrompt,
		"short_term_limit":  strconv.Itoa(cfg.ShortTermLimit),
		"nudge_interval":    strconv.Itoa(cfg.NudgeInterval),
		"skills_dirs":       joinLines(cfg.SkillsDirs),
		"live2d_model":      cfg.Live2DModel,
		"pet_size":          strconv.Itoa(cfg.PetSize),
		"chat_width":        strconv.Itoa(cfg.ChatWidth),
		"chat_height":       strconv.Itoa(cfg.ChatHeight),
		"active_profile_id": strconv.FormatInt(cfg.ActiveProfileID, 10),
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
}

// MissingRequired returns names of required fields that are empty.
func (c *Config) MissingRequired() []string {
	var missing []string
	// OpenRouter has a built-in default base URL, so it's not required for that provider.
	if c.LLMBaseURL == "" && c.LLMProvider != string(ProviderOpenRouter) {
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

