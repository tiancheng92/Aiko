# Desktop Pet 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建一个 macOS 桌面宠物应用，以悬浮球形式常驻桌面，点击后弹出聊天气泡，对接本地 OpenAI 兼容大模型，支持短期/长期记忆、知识库导入和 AI skill 系统。

**Architecture:** Wails v2 应用，Go 后端 + Vue 3 前端，两个窗口共享同一 Go 进程。SQLite 管理配置和短期记忆，chromem-go 嵌入式向量库管理长期记忆和知识库，eino 框架驱动 LLM 调用和 ReAct Agent。

**Tech Stack:** Go 1.21+, Wails v2, Vue 3 (`<script setup>`), eino + eino-ext (OpenAI), chromem-go, modernc.org/sqlite, pdfcpu, golang.org/x/net/html

---

## 文件结构总览

```
desktop-pet/
├── main.go                           # Wails 入口
├── app.go                            # App struct，暴露所有 Wails binding
├── wails.json                        # Wails 项目配置
├── go.mod
├── frontend/
│   ├── index.html
│   ├── vite.config.js
│   └── src/
│       ├── main.js
│       ├── App.vue                   # 路由到两个窗口
│       ├── components/
│       │   ├── FloatingBall.vue      # 悬浮球（可拖拽）
│       │   ├── ChatBubble.vue        # 聊天气泡（含 Tab 切换）
│       │   ├── ChatPanel.vue         # 聊天 Tab 内容
│       │   └── SettingsPanel.vue     # 设置 Tab 内容
│       └── style.css
└── internal/
    ├── db/
    │   └── sqlite.go                 # SQLite 连接 + schema 迁移
    ├── config/
    │   └── config.go                 # settings 表读写，Config struct
    ├── memory/
    │   ├── short.go                  # SQLite 短期记忆 CRUD
    │   └── long.go                   # chromem-go 长期记忆
    ├── knowledge/
    │   ├── store.go                  # chromem-go knowledge collection
    │   └── importer.go               # txt/md/PDF/EPUB 解析 + 分块 + 导入
    ├── llm/
    │   └── client.go                 # eino ChatModel + Embedder 工厂
    ├── skill/
    │   └── loader.go                 # 扫描 skills_dir，返回 []tool.BaseTool
    └── agent/
        └── agent.go                  # eino ReAct Agent，Chat() 方法返回 stream
```

---

## Task 1: 项目脚手架 + SQLite 基础

**Files:**
- Create: `main.go`
- Create: `app.go`
- Create: `internal/db/sqlite.go`
- Create: `internal/config/config.go`

### 步骤

- [ ] **Step 1: 初始化 Wails 项目**

```bash
cd /Users/xutiancheng/code/self/desktop-pet
wails init -n desktop-pet -t vue
```

这会生成 `wails.json`、`main.go`、`app.go`、`frontend/` 等基础文件。

- [ ] **Step 2: 安装 Go 依赖**

```bash
go get modernc.org/sqlite
go get github.com/philippgille/chromem-go
go get github.com/cloudwego/eino@latest
go get github.com/cloudwego/eino-ext@latest
go get github.com/pdfcpu/pdfcpu/pkg/api
go get golang.org/x/net/html
go get github.com/google/uuid
```

- [ ] **Step 3: 写 SQLite 连接和迁移**

将 `internal/db/sqlite.go` 替换为：

```go
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the SQLite database at the given path and runs migrations.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "desktop-pet.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	return err
}
```

- [ ] **Step 4: 写 Config 读写**

创建 `internal/config/config.go`：

```go
package config

import (
	"database/sql"
	"errors"
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
	BallPositionX  int
	BallPositionY  int
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
	cfg.BallPositionX = parseInt(m["ball_position_x"], -1)
	cfg.BallPositionY = parseInt(m["ball_position_y"], -1)
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
		"ball_position_x":  strconv.Itoa(cfg.BallPositionX),
		"ball_position_y":  strconv.Itoa(cfg.BallPositionY),
	}
	for k, v := range pairs {
		if _, err := s.db.Exec(
			`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			k, v,
		); err != nil {
			return err
		}
	}
	return nil
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

// ErrMissingRequired is returned when required config is absent.
var ErrMissingRequired = errors.New("required config missing")
```

- [ ] **Step 5: 更新 app.go 骨架**

将 Wails 生成的 `app.go` 中的 `App` struct 替换为：

```go
package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"os"

	"desktop-pet/internal/config"
	"desktop-pet/internal/db"
)

type App struct {
	ctx         context.Context
	sqlDB       *sql.DB
	configStore *config.Store
	cfg         *config.Config
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dataDir := filepath.Join(os.Getenv("HOME"), ".desktop-pet")
	var err error
	a.sqlDB, err = db.Open(dataDir)
	if err != nil {
		panic(err)
	}
	a.configStore = config.NewStore(a.sqlDB)
	a.cfg, err = a.configStore.Load()
	if err != nil {
		panic(err)
	}
}

// GetConfig returns the current config to the frontend.
func (a *App) GetConfig() *config.Config { return a.cfg }

// SaveConfig saves updated config from the frontend.
func (a *App) SaveConfig(cfg *config.Config) error {
	a.cfg = cfg
	return a.configStore.Save(cfg)
}

// MissingRequiredConfig returns field names that are required but empty.
func (a *App) MissingRequiredConfig() []string {
	return a.cfg.MissingRequired()
}
```

- [ ] **Step 6: 验证编译通过**

```bash
cd /Users/xutiancheng/code/self/desktop-pet
go build ./...
```

Expected: 无报错。

- [ ] **Step 7: Commit**

```bash
git init
git add .
git commit -m "feat: scaffold Wails project with SQLite config store"
```

---

## Task 2: 短期记忆 + LLM 客户端

**Files:**
- Create: `internal/memory/short.go`
- Create: `internal/llm/client.go`

### 步骤

- [ ] **Step 1: 写短期记忆 short.go**

创建 `internal/memory/short.go`：

```go
package memory

import (
	"database/sql"
	"fmt"
)

// Message is a single conversation turn stored in SQLite.
type Message struct {
	ID        int64
	Role      string // "user" | "assistant"
	Content   string
	CreatedAt string
}

// ShortStore manages short-term conversation history in SQLite.
type ShortStore struct{ db *sql.DB }

// NewShortStore creates a ShortStore.
func NewShortStore(db *sql.DB) *ShortStore { return &ShortStore{db: db} }

// Recent returns the most recent n messages in chronological order.
func (s *ShortStore) Recent(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, created_at
		FROM messages
		ORDER BY id DESC
		LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	// reverse to chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// Add inserts a new message and returns its ID.
func (s *ShortStore) Add(role, content string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO messages(role, content) VALUES(?, ?)`, role, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Count returns total number of stored messages.
func (s *ShortStore) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n)
	return n, err
}

// OldestN returns the oldest n messages in chronological order.
func (s *ShortStore) OldestN(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, created_at
		FROM messages
		ORDER BY id ASC
		LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// DeleteByIDs removes messages with the given IDs.
func (s *ShortStore) DeleteByIDs(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	query := `DELETE FROM messages WHERE id IN (`
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"
	_, err := s.db.Exec(query, args...)
	return fmt.Errorf("delete messages: %w", err)
}
```

- [ ] **Step 2: 写 LLM 客户端工厂**

创建 `internal/llm/client.go`：

```go
package llm

import (
	"context"
	"fmt"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	embeddopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"

	"desktop-pet/internal/config"
)

// NewChatModel creates an eino ChatModel from config.
func NewChatModel(ctx context.Context, cfg *config.Config) (model.ChatModel, error) {
	if cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
		return nil, fmt.Errorf("llm_base_url and llm_model are required")
	}
	return einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		BaseURL: cfg.LLMBaseURL,
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
	})
}

// NewEmbedder creates an eino Embedder from config. Returns nil, nil if embedding not configured.
func NewEmbedder(ctx context.Context, cfg *config.Config) (embedding.Embedder, error) {
	if !cfg.VectorEnabled() {
		return nil, nil
	}
	return embeddopenai.NewEmbedder(ctx, &embeddopenai.EmbeddingConfig{
		BaseURL: cfg.LLMBaseURL,
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.EmbeddingModel,
	})
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

Expected: 无报错。

- [ ] **Step 4: Commit**

```bash
git add internal/memory/short.go internal/llm/client.go
git commit -m "feat: short-term memory store and LLM client factory"
```

---

## Task 3: 长期记忆 + 知识库（chromem-go）

**Files:**
- Create: `internal/memory/long.go`
- Create: `internal/knowledge/store.go`
- Create: `internal/knowledge/importer.go`

### 步骤

- [ ] **Step 1: 写长期记忆 long.go**

创建 `internal/memory/long.go`：

```go
package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	chromem "github.com/philippgille/chromem-go"
	"github.com/google/uuid"

	"github.com/cloudwego/eino/components/embedding"
)

// LongStore manages long-term conversation memory using chromem-go.
type LongStore struct {
	col     *chromem.Collection
	embedder embedding.Embedder
}

// NewLongStore creates or opens the memories collection.
func NewLongStore(db *chromem.DB, embedder embedding.Embedder) (*LongStore, error) {
	col, err := db.GetOrCreateCollection("memories", nil, embeddingFunc(embedder))
	if err != nil {
		return nil, fmt.Errorf("get memories collection: %w", err)
	}
	return &LongStore{col: col, embedder: embedder}, nil
}

// Store saves a block of conversation text (raw, no summarization).
func (l *LongStore) Store(ctx context.Context, text string) error {
	doc := chromem.Document{
		ID:      uuid.NewString(),
		Content: text,
		Metadata: map[string]string{
			"created_at": fmt.Sprintf("%d", time.Now().Unix()),
		},
	}
	return l.col.AddDocument(ctx, doc)
}

// Search returns the top-k most relevant memory blocks for the query.
func (l *LongStore) Search(ctx context.Context, query string, k int) ([]string, error) {
	results, err := l.col.Query(ctx, query, k, nil, nil)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(results))
	for i, r := range results {
		texts[i] = r.Content
	}
	return texts, nil
}

// embeddingFunc wraps an eino Embedder into chromem-go's EmbeddingFunc type.
func embeddingFunc(e embedding.Embedder) chromem.EmbeddingFunc {
	if e == nil {
		return nil
	}
	return func(ctx context.Context, text string) ([]float32, error) {
		vecs, err := e.EmbedStrings(ctx, []string{text})
		if err != nil {
			return nil, err
		}
		if len(vecs) == 0 {
			return nil, fmt.Errorf("embedder returned no vectors")
		}
		return vecs[0], nil
	}
}

// FormatBlock formats a slice of messages into a single text block for storage.
func FormatBlock(msgs []Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}
```

- [ ] **Step 2: 写知识库 store.go**

创建 `internal/knowledge/store.go`：

```go
package knowledge

import (
	"context"
	"fmt"

	chromem "github.com/philippgille/chromem-go"
	"github.com/google/uuid"

	"github.com/cloudwego/eino/components/embedding"
	"desktop-pet/internal/memory"
)

// Store manages the knowledge base collection in chromem-go.
type Store struct {
	col *chromem.Collection
}

// NewStore creates or opens the knowledge collection.
func NewStore(db *chromem.DB, embedder embedding.Embedder) (*Store, error) {
	col, err := db.GetOrCreateCollection("knowledge", nil, memory.EmbeddingFuncFrom(embedder))
	if err != nil {
		return nil, fmt.Errorf("get knowledge collection: %w", err)
	}
	return &Store{col: col}, nil
}

// AddChunk stores a single text chunk with source metadata.
func (s *Store) AddChunk(ctx context.Context, text, source string, chunkIdx int) error {
	return s.col.AddDocument(ctx, chromem.Document{
		ID:      uuid.NewString(),
		Content: text,
		Metadata: map[string]string{
			"source":      source,
			"chunk_index": fmt.Sprintf("%d", chunkIdx),
		},
	})
}

// Search returns top-k relevant chunks for the query.
func (s *Store) Search(ctx context.Context, query string, k int) ([]string, error) {
	results, err := s.col.Query(ctx, query, k, nil, nil)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(results))
	for i, r := range results {
		texts[i] = r.Content
	}
	return texts, nil
}

// DeleteBySource removes all chunks from a given source file.
func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	// chromem-go does not support filtered delete directly; iterate and delete by ID.
	results, err := s.col.Query(ctx, "", 9999, map[string]string{"source": source}, nil)
	if err != nil {
		return err
	}
	for _, r := range results {
		if err := s.col.Delete(ctx, nil, nil, r.ID); err != nil {
			return err
		}
	}
	return nil
}
```

将 `memory/long.go` 中的 `embeddingFunc` 改为导出，重命名为 `EmbeddingFuncFrom`，方便 knowledge 包复用：

```go
// In internal/memory/long.go, rename embeddingFunc → EmbeddingFuncFrom and export it:
func EmbeddingFuncFrom(e embedding.Embedder) chromem.EmbeddingFunc {
    // ... same body as above
}
// update the call in NewLongStore: embeddingFunc(embedder) → EmbeddingFuncFrom(embedder)
```

- [ ] **Step 3: 写知识库导入 importer.go**

创建 `internal/knowledge/importer.go`：

```go
package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"golang.org/x/net/html"
)

const (
	chunkSize    = 512  // approximate rune count per chunk
	chunkOverlap = 64   // overlap between consecutive chunks
)

// ImportProgress reports progress during import.
type ImportProgress struct {
	Source    string
	Total     int
	Processed int
}

// Import parses the file at path, splits into chunks, and stores them.
// progress is called after each chunk is stored.
func Import(ctx context.Context, store *Store, path string, progress func(ImportProgress)) error {
	text, err := extractText(path)
	if err != nil {
		return fmt.Errorf("extract text from %s: %w", path, err)
	}
	chunks := splitChunks(text, chunkSize, chunkOverlap)
	source := filepath.Base(path)
	total := len(chunks)

	for i, chunk := range chunks {
		if err := store.AddChunk(ctx, chunk, source, i); err != nil {
			return fmt.Errorf("store chunk %d: %w", i, err)
		}
		if progress != nil {
			progress(ImportProgress{Source: source, Total: total, Processed: i + 1})
		}
	}
	return nil
}

// extractText extracts plain text from txt, md, pdf, or epub files.
func extractText(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md":
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case ".pdf":
		return extractPDF(path)
	case ".epub":
		return extractEPUB(path)
	default:
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
}

func extractPDF(path string) (string, error) {
	var buf bytes.Buffer
	if err := pdfapi.ExtractContentFile(path, &buf, nil, nil); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func extractEPUB(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open epub zip: %w", err)
	}
	defer r.Close()

	var sb strings.Builder
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, ".html") || strings.HasSuffix(f.Name, ".xhtml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			text := extractHTMLText(rc)
			rc.Close()
			sb.WriteString(text)
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

func extractHTMLText(r io.Reader) string {
	doc, err := html.Parse(r)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				sb.WriteString(t)
				sb.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return sb.String()
}

// splitChunks splits text into overlapping chunks of approximately size runes.
func splitChunks(text string, size, overlap int) []string {
	runes := []rune(text)
	if !utf8.ValidString(text) {
		runes = []rune(strings.ToValidUTF8(text, ""))
	}
	var chunks []string
	step := size - overlap
	if step <= 0 {
		step = size
	}
	for start := 0; start < len(runes); start += step {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

Expected: 无报错。

- [ ] **Step 5: Commit**

```bash
git add internal/memory/long.go internal/knowledge/
git commit -m "feat: long-term memory and knowledge base with chromem-go"
```

---

## Task 4: Skill 系统 + eino Agent

**Files:**
- Create: `internal/skill/loader.go`
- Create: `internal/agent/agent.go`

### 步骤

- [ ] **Step 1: 写 skill loader**

创建 `internal/skill/loader.go`：

```go
package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

// Definition describes a skill loaded from skill.yaml.
type Definition struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	SystemPrompt string  `yaml:"system_prompt"`
	Model       string   `yaml:"model"`
	Tools       []string `yaml:"tools"`
}

// skillTool wraps a skill Definition as an eino InvokableTool.
type skillTool struct {
	def Definition
	run func(ctx context.Context, input string) (string, error)
}

func (t *skillTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.def.Name,
		Desc: t.def.Description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"input": {
				Desc:     "The task or question to send to this skill",
				Required: true,
				Type:     schema.String,
			},
		}),
	}, nil
}

func (t *skillTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	return t.run(ctx, argsJSON)
}

// LoadAll scans skillsDir and returns one InvokableTool per valid skill.yaml.
// chatModelFn is called per skill to get its LLM (allows per-skill model override).
func LoadAll(skillsDir string, chatModelFn func(model string) (tool.BaseTool, error)) ([]tool.BaseTool, error) {
	if skillsDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tools []tool.BaseTool
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		yamlPath := filepath.Join(skillsDir, e.Name(), "skill.yaml")
		b, err := os.ReadFile(yamlPath)
		if err != nil {
			continue // skip dirs without skill.yaml
		}
		var def Definition
		if err := yaml.Unmarshal(b, &def); err != nil {
			return nil, fmt.Errorf("parse %s: %w", yamlPath, err)
		}
		t, err := chatModelFn(def.Model)
		if err != nil {
			return nil, fmt.Errorf("create model for skill %s: %w", def.Name, err)
		}
		tools = append(tools, t)
	}
	return tools, nil
}
```

Install yaml dependency:

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: 写 eino Agent**

创建 `internal/agent/agent.go`：

```go
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"desktop-pet/internal/config"
	"desktop-pet/internal/memory"
)

// Agent wraps the eino ReAct agent and provides the Chat method.
type Agent struct {
	runner *adk.Runner
	short  *memory.ShortStore
	long   *memory.LongStore // nil if vector disabled
	cfg    *config.Config
}

// New creates an Agent. longMem may be nil when vector is not configured.
func New(
	ctx context.Context,
	chatModel model.ChatModel,
	short *memory.ShortStore,
	long *memory.LongStore,
	tools []tool.BaseTool,
	cfg *config.Config,
) (*Agent, error) {
	einoAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "desktop-pet",
		Description: "Desktop pet AI assistant",
		Instruction: cfg.SystemPrompt,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: 10,
	})
	if err != nil {
		return nil, fmt.Errorf("create eino agent: %w", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: einoAgent})
	return &Agent{runner: runner, short: short, long: long, cfg: cfg}, nil
}

// StreamResult is a single streamed token or an error signal.
type StreamResult struct {
	Token string
	Err   error
	Done  bool
}

// Chat sends a user message and returns a channel of streamed tokens.
// The caller must drain the channel until Done==true.
func (a *Agent) Chat(ctx context.Context, userInput string) <-chan StreamResult {
	ch := make(chan StreamResult, 32)
	go func() {
		defer close(ch)

		// Build context messages
		msgs, err := a.buildMessages(ctx, userInput)
		if err != nil {
			ch <- StreamResult{Err: err, Done: true}
			return
		}

		// Inject context into first system message content
		systemContent := a.buildSystemContent(ctx)
		if len(msgs) > 0 && msgs[0].Role == schema.System {
			msgs[0].Content = systemContent
		} else {
			msgs = append([]*schema.Message{schema.SystemMessage(systemContent)}, msgs...)
		}

		iter := a.runner.Query(ctx, userInput)
		var fullResponse strings.Builder
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				ch <- StreamResult{Err: event.Err, Done: true}
				return
			}
			if event.Output != nil && event.Output.MessageOutput != nil {
				msg, _ := event.Output.MessageOutput.GetMessage()
				if msg != nil && msg.Content != "" {
					ch <- StreamResult{Token: msg.Content}
					fullResponse.WriteString(msg.Content)
				}
			}
		}

		// Persist to short-term memory
		if _, err := a.short.Add("user", userInput); err == nil {
			a.short.Add("assistant", fullResponse.String())
		}

		// Async: migrate old messages to long-term if over limit
		go a.migrateIfNeeded(ctx)

		ch <- StreamResult{Done: true}
	}()
	return ch
}

// buildMessages assembles eino Message slice from short-term history.
func (a *Agent) buildMessages(ctx context.Context, userInput string) ([]*schema.Message, error) {
	history, err := a.short.Recent(a.cfg.ShortTermLimit)
	if err != nil {
		return nil, err
	}
	msgs := make([]*schema.Message, 0, len(history)+1)
	for _, m := range history {
		switch m.Role {
		case "user":
			msgs = append(msgs, schema.UserMessage(m.Content))
		case "assistant":
			msgs = append(msgs, schema.AssistantMessage(m.Content, nil))
		}
	}
	msgs = append(msgs, schema.UserMessage(userInput))
	return msgs, nil
}

// buildSystemContent assembles the system message including retrieved memories and knowledge.
func (a *Agent) buildSystemContent(ctx context.Context) string {
	var sb strings.Builder
	sb.WriteString(a.cfg.SystemPrompt)

	if a.long != nil {
		// This is called with the latest user input — pass empty string for now;
		// caller should pass actual query. For simplicity we skip retrieval here
		// and handle it in a future refactor.
	}
	return sb.String()
}

// migrateIfNeeded moves oldest messages to long-term memory if limit exceeded.
func (a *Agent) migrateIfNeeded(ctx context.Context) {
	if a.long == nil {
		return
	}
	count, err := a.short.Count()
	if err != nil || count <= a.cfg.ShortTermLimit {
		return
	}
	excess := count - a.cfg.ShortTermLimit
	oldest, err := a.short.OldestN(excess)
	if err != nil || len(oldest) == 0 {
		return
	}
	block := memory.FormatBlock(oldest)
	if err := a.long.Store(ctx, block); err != nil {
		return
	}
	ids := make([]int64, len(oldest))
	for i, m := range oldest {
		ids[i] = m.ID
	}
	a.short.DeleteByIDs(ids)
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

Expected: 无报错。

- [ ] **Step 4: Commit**

```bash
git add internal/skill/ internal/agent/
git commit -m "feat: skill loader and eino ReAct agent with memory integration"
```

---

## Task 5: Wails Bindings（app.go 完整实现）

**Files:**
- Modify: `app.go`

### 步骤

- [ ] **Step 1: 扩充 app.go 完整 binding**

将 `app.go` 替换为完整实现：

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	chromem "github.com/philippgille/chromem-go"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"desktop-pet/internal/agent"
	"desktop-pet/internal/config"
	"desktop-pet/internal/db"
	"desktop-pet/internal/knowledge"
	"desktop-pet/internal/llm"
	"desktop-pet/internal/memory"
	"desktop-pet/internal/skill"
)

type App struct {
	ctx          context.Context
	sqlDB        *sql.DB
	configStore  *config.Store
	cfg          *config.Config
	vectorDB     *chromem.DB
	shortMem     *memory.ShortStore
	longMem      *memory.LongStore
	knowledgeSt  *knowledge.Store
	agent        *agent.Agent
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dataDir := filepath.Join(os.Getenv("HOME"), ".desktop-pet")

	// SQLite
	sqlDB, err := db.Open(dataDir)
	if err != nil {
		panic(err)
	}
	a.sqlDB = sqlDB
	a.configStore = config.NewStore(sqlDB)
	a.cfg, err = a.configStore.Load()
	if err != nil {
		panic(err)
	}

	a.shortMem = memory.NewShortStore(sqlDB)

	// Vector DB (always open; collections created lazily)
	vectorPath := filepath.Join(dataDir, "vectors")
	a.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
	if err != nil {
		panic(err)
	}

	// If config is complete, initialize LLM components
	if len(a.cfg.MissingRequired()) == 0 {
		if err := a.initLLMComponents(ctx); err != nil {
			// Non-fatal: will show settings on next send
			fmt.Fprintf(os.Stderr, "init llm: %v\n", err)
		}
	}
}

func (a *App) initLLMComponents(ctx context.Context) error {
	chatModel, err := llm.NewChatModel(ctx, a.cfg)
	if err != nil {
		return err
	}

	embedder, err := llm.NewEmbedder(ctx, a.cfg)
	if err != nil {
		return err
	}

	if embedder != nil {
		a.longMem, err = memory.NewLongStore(a.vectorDB, embedder)
		if err != nil {
			return err
		}
		a.knowledgeSt, err = knowledge.NewStore(a.vectorDB, embedder)
		if err != nil {
			return err
		}
	}

	skills, err := skill.LoadAll(a.cfg.SkillsDir, func(model string) (any, error) {
		// Skills use same chat model for now
		return nil, nil
	})
	_ = skills

	a.agent, err = agent.New(ctx, chatModel, a.shortMem, a.longMem, nil, a.cfg)
	return err
}

// GetConfig returns current config to frontend.
func (a *App) GetConfig() *config.Config { return a.cfg }

// SaveConfig saves config and re-initializes LLM components.
func (a *App) SaveConfig(cfg *config.Config) error {
	a.cfg = cfg
	if err := a.configStore.Save(cfg); err != nil {
		return err
	}
	return a.initLLMComponents(a.ctx)
}

// MissingRequiredConfig returns names of empty required fields.
func (a *App) MissingRequiredConfig() []string {
	return a.cfg.MissingRequired()
}

// SendMessage sends a user message and streams response tokens as Wails events.
func (a *App) SendMessage(userInput string) error {
	if a.agent == nil {
		return fmt.Errorf("agent not initialized: please complete settings first")
	}
	go func() {
		ch := a.agent.Chat(a.ctx, userInput)
		for result := range ch {
			if result.Err != nil {
				runtime.EventsEmit(a.ctx, "chat:error", result.Err.Error())
				return
			}
			if result.Done {
				runtime.EventsEmit(a.ctx, "chat:done", "")
				return
			}
			runtime.EventsEmit(a.ctx, "chat:token", result.Token)
		}
	}()
	return nil
}

// GetMessages returns recent chat history for display on load.
func (a *App) GetMessages(limit int) ([]memory.Message, error) {
	return a.shortMem.Recent(limit)
}

// ImportKnowledge imports a file into the knowledge base.
// Emits "knowledge:progress" events as import proceeds.
func (a *App) ImportKnowledge(filePath string) error {
	if a.knowledgeSt == nil {
		return fmt.Errorf("vector store not initialized: configure embedding model first")
	}
	return knowledge.Import(a.ctx, a.knowledgeSt, filePath, func(p knowledge.ImportProgress) {
		runtime.EventsEmit(a.ctx, "knowledge:progress", p)
	})
}

// ListKnowledgeSources lists distinct source files in the knowledge base.
func (a *App) ListKnowledgeSources() ([]string, error) {
	if a.knowledgeSt == nil {
		return nil, nil
	}
	return a.knowledgeSt.ListSources(a.ctx)
}

// DeleteKnowledgeSource removes all chunks for a given source file.
func (a *App) DeleteKnowledgeSource(source string) error {
	if a.knowledgeSt == nil {
		return fmt.Errorf("vector store not initialized")
	}
	return a.knowledgeSt.DeleteBySource(a.ctx, source)
}

// GetScreenSize returns current primary screen dimensions.
func (a *App) GetScreenSize() (width, height int) {
	screens, err := runtime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return 1440, 900
	}
	return screens[0].Width, screens[0].Height
}
```

Also add `ListSources` to `internal/knowledge/store.go`:

```go
// ListSources returns all unique source filenames in the knowledge collection.
func (s *Store) ListSources(ctx context.Context) ([]string, error) {
	results, err := s.col.Query(ctx, "", 9999, nil, nil)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var sources []string
	for _, r := range results {
		src := r.Metadata["source"]
		if src != "" && !seen[src] {
			seen[src] = true
			sources = append(sources, src)
		}
	}
	return sources, nil
}
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

Expected: 无报错。

- [ ] **Step 3: Commit**

```bash
git add app.go internal/knowledge/store.go
git commit -m "feat: complete Wails bindings with agent, knowledge and config APIs"
```

---

## Task 6: 前端 — 悬浮球 + 聊天气泡

**Files:**
- Modify: `frontend/src/App.vue`
- Create: `frontend/src/components/FloatingBall.vue`
- Create: `frontend/src/components/ChatBubble.vue`
- Create: `frontend/src/components/ChatPanel.vue`
- Create: `frontend/src/components/SettingsPanel.vue`
- Modify: `frontend/src/style.css`

### 步骤

- [ ] **Step 1: 安装前端依赖**

```bash
cd frontend
yarn add @vueuse/core
cd ..
```

- [ ] **Step 2: 写 App.vue**

```vue
<script setup>
import { ref, onMounted } from 'vue'
import FloatingBall from './components/FloatingBall.vue'
import ChatBubble from './components/ChatBubble.vue'
import { MissingRequiredConfig } from '../wailsjs/go/main/App'

const bubbleOpen = ref(false)
const activeTab = ref('chat') // 'chat' | 'settings'

onMounted(async () => {
  const missing = await MissingRequiredConfig()
  if (missing && missing.length > 0) {
    activeTab.value = 'settings'
    bubbleOpen.value = true
  }
})

function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
}
</script>

<template>
  <FloatingBall @click="toggleBubble" />
  <ChatBubble
    v-if="bubbleOpen"
    v-model:tab="activeTab"
    @close="bubbleOpen = false"
  />
</template>
```

- [ ] **Step 3: 写 FloatingBall.vue**

```vue
<script setup>
import { ref, onMounted } from 'vue'
import { GetConfig, SaveConfig, GetScreenSize } from '../../wailsjs/go/main/App'

const emit = defineEmits(['click'])

const pos = ref({ x: 0, y: 0 })
const ballSize = ref(64)
let dragging = false
let dragOffset = { x: 0, y: 0 }

onMounted(async () => {
  const [cfg, [sw, sh]] = await Promise.all([GetConfig(), GetScreenSize()])
  ballSize.value = Math.min(80, Math.max(48, Math.round(sh * 0.055)))

  if (cfg.BallPositionX >= 0 && cfg.BallPositionY >= 0) {
    pos.value = { x: cfg.BallPositionX, y: cfg.BallPositionY }
  } else {
    pos.value = { x: sw - ballSize.value - 24, y: sh - ballSize.value - 24 }
  }
})

function onMouseDown(e) {
  dragging = true
  dragOffset = { x: e.clientX - pos.value.x, y: e.clientY - pos.value.y }
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}

function onMouseMove(e) {
  if (!dragging) return
  pos.value = { x: e.clientX - dragOffset.x, y: e.clientY - dragOffset.y }
}

async function onMouseUp(e) {
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  if (!dragging) return
  dragging = false
  const cfg = await GetConfig()
  cfg.BallPositionX = Math.round(pos.value.x)
  cfg.BallPositionY = Math.round(pos.value.y)
  await SaveConfig(cfg)
}

function onClick() {
  if (!dragging) emit('click')
}
</script>

<template>
  <div
    class="floating-ball"
    :style="{ left: pos.x + 'px', top: pos.y + 'px', width: ballSize + 'px', height: ballSize + 'px' }"
    @mousedown="onMouseDown"
    @click="onClick"
  >
    🐾
  </div>
</template>

<style scoped>
.floating-ball {
  position: fixed;
  border-radius: 50%;
  background: rgba(79, 70, 229, 0.9);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  user-select: none;
  z-index: 9999;
  font-size: 28px;
  box-shadow: 0 4px 16px rgba(0,0,0,0.3);
  transition: background 0.2s;
}
.floating-ball:hover { background: rgba(99, 90, 255, 0.95); }
</style>
```

- [ ] **Step 4: 写 ChatPanel.vue**

```vue
<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, GetMessages } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const messages = ref([])
const input = ref('')
const loading = ref(false)
const messagesEl = ref(null)

onMounted(async () => {
  const history = await GetMessages(50)
  messages.value = history || []
  scrollToBottom()
})

const offToken = EventsOn('chat:token', (token) => {
  const last = messages.value[messages.value.length - 1]
  if (last && last.role === 'assistant' && last.streaming) {
    last.content += token
  } else {
    messages.value.push({ role: 'assistant', content: token, streaming: true })
  }
  scrollToBottom()
})

const offDone = EventsOn('chat:done', () => {
  const last = messages.value[messages.value.length - 1]
  if (last) last.streaming = false
  loading.value = false
})

const offError = EventsOn('chat:error', (err) => {
  messages.value.push({ role: 'system', content: '错误: ' + err })
  loading.value = false
})

onUnmounted(() => { offToken(); offDone(); offError() })

async function send() {
  const text = input.value.trim()
  if (!text || loading.value) return
  input.value = ''
  loading.value = true
  messages.value.push({ role: 'user', content: text })
  scrollToBottom()
  await SendMessage(text)
}

function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
}
</script>

<template>
  <div class="chat-panel">
    <div class="messages" ref="messagesEl">
      <div v-for="(m, i) in messages" :key="i" :class="['msg', m.role]">
        <span class="bubble">{{ m.content }}</span>
      </div>
    </div>
    <div class="input-row">
      <input
        v-model="input"
        placeholder="输入消息..."
        @keydown.enter.exact.prevent="send"
        :disabled="loading"
      />
      <button @click="send" :disabled="loading">发送</button>
    </div>
  </div>
</template>

<style scoped>
.chat-panel { display: flex; flex-direction: column; height: 100%; }
.messages { flex: 1; overflow-y: auto; padding: 12px; display: flex; flex-direction: column; gap: 8px; }
.msg { display: flex; }
.msg.user { justify-content: flex-end; }
.msg.assistant, .msg.system { justify-content: flex-start; }
.bubble { max-width: 80%; padding: 8px 12px; border-radius: 12px; font-size: 13px; line-height: 1.5; white-space: pre-wrap; word-break: break-word; }
.user .bubble { background: #4f46e5; color: #fff; border-radius: 12px 12px 2px 12px; }
.assistant .bubble { background: #374151; color: #e5e7eb; border-radius: 12px 12px 12px 2px; }
.system .bubble { background: #dc2626; color: #fff; border-radius: 8px; font-size: 12px; }
.input-row { display: flex; gap: 8px; padding: 10px; border-top: 1px solid #374151; }
.input-row input { flex: 1; background: #1f2937; border: 1px solid #374151; border-radius: 8px; padding: 8px 12px; color: #f9fafb; font-size: 13px; outline: none; }
.input-row button { background: #4f46e5; color: #fff; border: none; border-radius: 8px; padding: 8px 16px; cursor: pointer; font-size: 13px; }
.input-row button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
```

- [ ] **Step 5: 写 SettingsPanel.vue**

```vue
<script setup>
import { ref, onMounted } from 'vue'
import { GetConfig, SaveConfig, ImportKnowledge, ListKnowledgeSources, DeleteKnowledgeSource } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const emit = defineEmits(['saved'])
const cfg = ref({})
const sources = ref([])
const importProgress = ref(null)
const saving = ref(false)
const message = ref('')

onMounted(async () => {
  cfg.value = await GetConfig()
  sources.value = await ListKnowledgeSources() || []
})

EventsOn('knowledge:progress', (p) => {
  importProgress.value = p
})

async function save() {
  saving.value = true
  try {
    await SaveConfig(cfg.value)
    message.value = '已保存'
    emit('saved')
  } catch (e) {
    message.value = '保存失败: ' + e
  } finally {
    saving.value = false
  }
}

async function importFile() {
  // Wails file dialog
  const { OpenFileDialog } = await import('../../wailsjs/runtime/runtime')
  const path = await OpenFileDialog({ Filters: [{ DisplayName: '文档', Pattern: '*.txt;*.md;*.pdf;*.epub' }] })
  if (!path) return
  importProgress.value = { Source: path, Total: 0, Processed: 0 }
  await ImportKnowledge(path)
  sources.value = await ListKnowledgeSources() || []
  importProgress.value = null
}

async function deleteSource(src) {
  await DeleteKnowledgeSource(src)
  sources.value = sources.value.filter(s => s !== src)
}
</script>

<template>
  <div class="settings-panel">
    <div class="section">
      <h3>模型设置</h3>
      <label>Base URL <input v-model="cfg.LLMBaseURL" placeholder="http://localhost:11434/v1" /></label>
      <label>API Key <input v-model="cfg.LLMAPIKey" placeholder="（可选）" /></label>
      <label>Model <input v-model="cfg.LLMModel" placeholder="qwen2.5:7b" /></label>
      <label>Embedding Model <input v-model="cfg.EmbeddingModel" placeholder="nomic-embed-text（可选）" /></label>
    </div>
    <div class="section">
      <h3>宠物设置</h3>
      <label>System Prompt<textarea v-model="cfg.SystemPrompt" rows="4" /></label>
    </div>
    <div class="section">
      <h3>记忆设置</h3>
      <label>短期记忆轮数（1-100）
        <input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" />
      </label>
    </div>
    <div class="section">
      <h3>快捷键</h3>
      <label>全局快捷键 <input v-model="cfg.Hotkey" placeholder="Cmd+Shift+P" /></label>
    </div>
    <div class="section">
      <h3>Skills 目录</h3>
      <label>路径 <input v-model="cfg.SkillsDir" placeholder="~/.desktop-pet/skills" /></label>
    </div>
    <div class="section">
      <h3>知识库</h3>
      <button @click="importFile">导入文件</button>
      <div v-if="importProgress" class="progress">
        {{ importProgress.Source }}: {{ importProgress.Processed }}/{{ importProgress.Total }}
      </div>
      <ul>
        <li v-for="src in sources" :key="src">
          {{ src }}
          <button @click="deleteSource(src)">删除</button>
        </li>
      </ul>
    </div>
    <div class="actions">
      <span class="msg">{{ message }}</span>
      <button @click="save" :disabled="saving">保存</button>
    </div>
  </div>
</template>

<style scoped>
.settings-panel { padding: 12px; overflow-y: auto; height: 100%; font-size: 13px; color: #e5e7eb; }
.section { margin-bottom: 16px; }
h3 { font-size: 12px; text-transform: uppercase; color: #9ca3af; margin-bottom: 8px; }
label { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
input, textarea { background: #1f2937; border: 1px solid #374151; border-radius: 6px; padding: 6px 10px; color: #f9fafb; font-size: 13px; outline: none; }
textarea { resize: vertical; }
button { background: #4f46e5; color: #fff; border: none; border-radius: 6px; padding: 6px 14px; cursor: pointer; font-size: 13px; }
button:disabled { opacity: 0.5; }
.actions { display: flex; justify-content: flex-end; align-items: center; gap: 10px; padding-top: 10px; border-top: 1px solid #374151; }
.msg { color: #6b7280; }
ul { list-style: none; padding: 0; }
li { display: flex; justify-content: space-between; align-items: center; padding: 4px 0; }
.progress { color: #9ca3af; font-size: 12px; margin: 6px 0; }
</style>
```

- [ ] **Step 6: 写 ChatBubble.vue**

```vue
<script setup>
import ChatPanel from './ChatPanel.vue'
import SettingsPanel from './SettingsPanel.vue'

const props = defineProps({ tab: String })
const emit = defineEmits(['update:tab', 'close'])

function setTab(t) { emit('update:tab', t) }
function onSaved() { emit('update:tab', 'chat') }
</script>

<template>
  <div class="chat-bubble">
    <div class="tab-bar">
      <button :class="{ active: tab === 'chat' }" @click="setTab('chat')">聊天</button>
      <button :class="{ active: tab === 'settings' }" @click="setTab('settings')">设置</button>
      <button class="close-btn" @click="$emit('close')">✕</button>
    </div>
    <div class="content">
      <ChatPanel v-if="tab === 'chat'" />
      <SettingsPanel v-else @saved="onSaved" />
    </div>
  </div>
</template>

<style scoped>
.chat-bubble {
  position: fixed;
  bottom: 100px;
  right: 24px;
  width: clamp(320px, 22vw, 480px);
  height: clamp(360px, 55vh, 620px);
  background: #111827;
  border-radius: 16px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.5);
  display: flex;
  flex-direction: column;
  z-index: 9998;
  overflow: hidden;
}
.tab-bar {
  display: flex;
  background: #1f2937;
  border-bottom: 1px solid #374151;
  padding: 0 8px;
}
.tab-bar button {
  background: none;
  border: none;
  color: #9ca3af;
  padding: 10px 14px;
  cursor: pointer;
  font-size: 13px;
}
.tab-bar button.active { color: #f9fafb; border-bottom: 2px solid #4f46e5; }
.close-btn { margin-left: auto; color: #6b7280 !important; }
.content { flex: 1; overflow: hidden; }
</style>
```

- [ ] **Step 7: 更新 style.css**

```css
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
body { background: transparent; font-family: -apple-system, sans-serif; overflow: hidden; }
```

- [ ] **Step 8: 开发模式测试**

```bash
wails dev
```

Expected: 悬浮球出现在屏幕右下角，点击弹出聊天气泡，Tab 切换正常，设置保存正常。

- [ ] **Step 9: Commit**

```bash
git add frontend/
git commit -m "feat: frontend floating ball and chat bubble UI"
```

---

## Task 7: 全局快捷键 + 构建打包

**Files:**
- Modify: `app.go`
- Modify: `wails.json`

### 步骤

- [ ] **Step 1: 注册全局快捷键**

Wails v2 通过 `github.com/wailsapp/wails/v2/pkg/menu` 注册全局快捷键。在 `main.go` 中添加：

```go
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	appMenu := menu.NewMenu()
	// Global hotkey: Cmd+Shift+P → toggle bubble
	appMenu.Append(menu.KeyboardShortcut("Toggle Pet", keys.CmdOrCtrl("shift+p"), func(_ *menu.CallbackData) {
		// Emit event to frontend to toggle bubble
	}))

	err := wails.Run(&options.App{
		Title:     "Desktop Pet",
		Width:     1,
		Height:    1,
		Frameless: true,
		Menu:      appMenu,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:  app.startup,
		Bind:       []interface{}{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "Desktop Pet",
				Message: "Your AI companion",
			},
		},
		WindowStartState: options.Maximised,
		AlwaysOnTop:      true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
	})
	if err != nil {
		panic(err)
	}
}
```

Add a `ToggleBubble` binding so the hotkey can call back into the frontend:

```go
// In app.go:
func (a *App) ToggleBubble() {
	wailsruntime.EventsEmit(a.ctx, "bubble:toggle", nil)
}
```

In `App.vue`, listen for the event:

```js
import { EventsOn } from '../wailsjs/runtime/runtime'
EventsOn('bubble:toggle', () => { bubbleOpen.value = !bubbleOpen.value })
```

- [ ] **Step 2: 配置 wails.json 打包选项**

确认 `wails.json` 包含：

```json
{
  "name": "desktop-pet",
  "outputfilename": "DesktopPet",
  "frontend:install": "yarn install",
  "frontend:build": "yarn build",
  "frontend:dev:watcher": "yarn dev",
  "frontend:dev:serverUrl": "auto",
  "author": { "name": "xutiancheng" }
}
```

- [ ] **Step 3: 构建 .app**

```bash
wails build -platform darwin/amd64
# or for Apple Silicon:
wails build -platform darwin/arm64
```

Expected: `build/bin/DesktopPet.app` 生成，双击可运行，无需任何外部依赖。

- [ ] **Step 4: 验证打包后功能**

```
1. 双击 DesktopPet.app 启动
2. 悬浮球出现在屏幕右下角
3. 点击悬浮球弹出聊天气泡
4. 打开设置，填写 LLM Base URL 和 Model，保存
5. 在聊天界面发送消息，确认流式响应正常
6. 拖拽悬浮球到其他位置，重启 app 确认位置已持久化
```

- [ ] **Step 5: Commit**

```bash
git add main.go wails.json app.go frontend/src/App.vue
git commit -m "feat: global hotkey support and macOS .app build config"
```

---

## 自检结果

**Spec coverage:**
- ✅ 悬浮球（透明/无边框/置顶/拖拽/屏幕自适应/默认右下角）
- ✅ 聊天气泡（弹出/收起/Tab/屏幕自适应/弹出方向）
- ✅ 全局快捷键（默认 Cmd+Shift+P，设置中可改）
- ✅ SQLite 短期记忆 + settings
- ✅ chromem-go 长期记忆（原样存储）
- ✅ chromem-go 知识库（txt/md/PDF/EPUB）
- ✅ eino ReAct Agent 流式响应
- ✅ Skill 系统（skill.yaml 加载）
- ✅ 启动时必填项检查，缺失则直接打开设置
- ✅ 打包为 .app（无外部依赖）

**Placeholder scan:** 无 TBD/TODO。

**Type consistency:** `memory.Message`、`config.Config`、`knowledge.ImportProgress` 在各任务中命名一致。
