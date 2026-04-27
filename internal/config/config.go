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
	AllowedPaths  []string // file system path whitelist; empty = deny all
	ShellTimeout  int      // execute_shell timeout in seconds; default 30
	ShellTrustedCommands []string // 免确认的命令前缀列表
	CodeTimeout   int      // execute_code timeout in seconds; default 60
	SMSWatcherEnabled bool   // 是否启用 SMS 短信监听（macOS 仅支持）
	VoiceAutoSend      bool   // 语音识别完成后是否自动发送消息
	SoundsEnabled       bool   // 是否启用聊天音效
	SkillsDirs     []string // skills 目录列表，支持多个路径
	PetSize        int // 宠物显示尺寸（像素），0 表示自动根据屏幕高度计算
	ChatWidth      int // 聊天框宽度（像素），0 表示使用默认值
	ChatHeight     int // 聊天框高度（像素），0 表示使用默认值
	ActiveProfileID int64 // 当前激活的 ModelProfile ID，0 表示未设置
	TTSModelDir           string  // kokoro 模型目录（空则使用默认 ~/aiko-tts-venv/models）
	TTSVoice              string  // 声线名
	TTSSpeed              float64 // 语速，0.5–2.0
	TTSAutoPlay           bool    // 外观与交互：chat:done 后自动朗读
	TTSSummarizeThreshold int     // 摘要字数阈值，默认 200，0 表示禁用摘要
	TTSBackend            string  // "kokoro" | ""（系统 say）
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
	cfg.AllowedPaths = splitLines(m["allowed_paths"])
	cfg.ShellTimeout = parseInt(m["shell_timeout"], 30)
	cfg.ShellTrustedCommands = splitLines(m["shell_trusted_commands"])
	if cfg.ShellTimeout <= 0 {
		cfg.ShellTimeout = 30
	}
	cfg.CodeTimeout = parseInt(m["code_timeout"], 60)
	if cfg.CodeTimeout <= 0 {
		cfg.CodeTimeout = 60
	}
	cfg.PetSize = parseInt(m["pet_size"], 0)
	cfg.ChatWidth = parseInt(m["chat_width"], 0)
	cfg.ChatHeight = parseInt(m["chat_height"], 0)
	cfg.ActiveProfileID = int64(parseInt(m["active_profile_id"], 0))
	cfg.SMSWatcherEnabled = m["sms_watcher_enabled"] == "true"
	cfg.VoiceAutoSend = m["voice_auto_send"] == "true"
	cfg.SoundsEnabled = m["sounds_enabled"] == "true"
	cfg.TTSAutoPlay = m["tts_auto_play"] == "true"
	cfg.TTSSummarizeThreshold = parseInt(m["tts_summarize_threshold"], 200)
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
		"sms_watcher_enabled": strconv.FormatBool(cfg.SMSWatcherEnabled),
		"voice_auto_send": strconv.FormatBool(cfg.VoiceAutoSend),
		"sounds_enabled": strconv.FormatBool(cfg.SoundsEnabled),
		"tts_auto_play":            strconv.FormatBool(cfg.TTSAutoPlay),
		"tts_summarize_threshold":  strconv.Itoa(cfg.TTSSummarizeThreshold),
		"allowed_paths":            joinLines(cfg.AllowedPaths),
		"shell_timeout":            strconv.Itoa(cfg.ShellTimeout),
		"shell_trusted_commands":   joinLines(cfg.ShellTrustedCommands),
		"code_timeout":             strconv.Itoa(cfg.CodeTimeout),
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
	c.TTSModelDir = p.TTSModelDir
	c.TTSVoice = p.TTSVoice
	if p.TTSSpeed == 0 {
		c.TTSSpeed = 1.0
	} else {
		c.TTSSpeed = p.TTSSpeed
	}
	c.TTSBackend = p.TTSBackend
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

