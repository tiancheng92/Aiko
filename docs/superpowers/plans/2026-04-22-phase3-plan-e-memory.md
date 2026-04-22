# 三期记忆架构优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 借鉴 MemPalace 的理念，将对话历史从"平铺向量检索"升级为"分层结构化存储"，并引入可选的小模型摘要机制，提升长期记忆检索的准确性和相关性。

**Architecture:**
MemPalace 的核心理念：verbatim（原文存储） + 结构化索引（主题/人物/项目分层）+ 混合检索（语义 + 关键词 + 时间衰减）。我们不直接引入 MemPalace（Python），而是在 Go 内实现等价理念：
1. **Segment 层**：每次对话以完整 session 为粒度存储（而非拆散的消息块），保留时间戳和可选摘要。
2. **摘要器**：可选的小模型对每个 session 生成一句话摘要，同时存储原文和摘要的向量。
3. **混合检索**：语义相似度 × 时间衰减权重，近期记忆得分更高。
4. **结构保留**：每条长期记忆记录包含 `raw_content`、`summary`、`created_at`，检索时两者都参与向量计算。

原有 `LongStore` API（`Store`、`Search`、`DeleteAll`）保持不变，内部实现升级，对 `agent.go` 无需改动。

**Tech Stack:** Go `chromem-go`（已有）、SQLite（元数据层）、可选 OpenAI-compatible embedder（已有）

---

## 文件结构

| 操作 | 文件 | 说明 |
|---|---|---|
| Modify | `internal/memory/long.go` | 升级 LongStore：分层存储、时间衰减检索、摘要字段 |
| Modify | `internal/db/sqlite.go` | 添加 memory_segments 表（元数据层） |
| Modify | `internal/llm/client.go` | 添加 NewSummarizer 工厂（可选小模型） |
| Modify | `app.go` | initLLMComponents 中传入可选 summarizer |

---

### Task 1: DB migration — memory_segments 表

**Files:**
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: 在 `migrate()` 中追加 memory_segments 表**

在 `migrate` 函数的 SQL 末尾追加（在最后一个 `);` 之前）：

```sql
CREATE TABLE IF NOT EXISTS memory_segments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    vector_id   TEXT NOT NULL UNIQUE,   -- chromem document ID
    raw_content TEXT NOT NULL,
    summary     TEXT,                   -- optional one-line summary
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memory_segments_created ON memory_segments(created_at DESC);
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/db/sqlite.go
git commit -m "feat: add memory_segments metadata table"
```

---

### Task 2: 可选摘要器

**Files:**
- Modify: `internal/llm/client.go`

- [ ] **Step 1: 添加 `Summarizer` 接口和 `NewSummarizer` 工厂**

在 `client.go` 末尾追加：

```go
// Summarizer generates a one-sentence summary of a text block.
type Summarizer interface {
    Summarize(ctx context.Context, text string) (string, error)
}

// llmSummarizer calls the chat model with a fixed summarization prompt.
type llmSummarizer struct {
    model model.ChatModel
}

// NewSummarizer creates a Summarizer backed by the chat model.
// Returns nil if cfg has no LLM configured (so caller can skip summarization).
func NewSummarizer(ctx context.Context, cfg *config.Config) (Summarizer, error) {
    if cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
        return nil, nil
    }
    m, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
        BaseURL: cfg.LLMBaseURL,
        APIKey:  cfg.LLMAPIKey,
        Model:   cfg.LLMModel,
    })
    if err != nil {
        return nil, fmt.Errorf("new summarizer model: %w", err)
    }
    return &llmSummarizer{model: m}, nil
}

// Summarize generates a one-sentence summary of text using the chat model.
func (s *llmSummarizer) Summarize(ctx context.Context, text string) (string, error) {
    prompt := "请用一句话总结以下对话内容的核心主题，不超过30个字：\n\n" + text
    msgs := []*schema.Message{
        {Role: schema.User, Content: prompt},
    }
    resp, err := s.model.Generate(ctx, msgs)
    if err != nil {
        return "", fmt.Errorf("summarize: %w", err)
    }
    if len(resp.Choices) == 0 {
        return "", nil
    }
    return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
```

在 `client.go` import 中加入：

```go
"strings"
"github.com/cloudwego/eino/schema"
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/llm/client.go
git commit -m "feat: add optional Summarizer backed by chat model"
```

---

### Task 3: 升级 LongStore — 分层存储 + 时间衰减检索

**Files:**
- Modify: `internal/memory/long.go`

- [ ] **Step 1: 更新 `LongStore` struct 和 `NewLongStore`**

将现有 `LongStore` 定义替换为：

```go
// LongStore manages long-term conversation memory using chromem-go and SQLite metadata.
type LongStore struct {
    mu         sync.RWMutex
    col        *chromem.Collection
    db         *sql.DB
    summarizer llm.Summarizer // optional; nil means no summarization
}

// NewLongStore creates or opens the memories collection.
// db is the SQLite database for metadata; summarizer may be nil.
func NewLongStore(vectorDB *chromem.DB, sqlDB *sql.DB, embedder embedding.Embedder, summarizer llm.Summarizer) (*LongStore, error) {
    col, err := vectorDB.GetOrCreateCollection("memories", nil, EmbeddingFuncFrom(embedder))
    if err != nil {
        return nil, fmt.Errorf("get memories collection: %w", err)
    }
    return &LongStore{col: col, db: sqlDB, summarizer: summarizer}, nil
}
```

在 `long.go` import 中加入：

```go
"database/sql"
"desktop-pet/internal/llm"
```

- [ ] **Step 2: 升级 `Store` 方法——存储原文 + 可选摘要 + SQLite 元数据**

替换现有 `Store` 方法：

```go
// Store saves a conversation segment. If a summarizer is configured, a one-sentence
// summary is also generated and stored as a second vector for better retrieval coverage.
func (l *LongStore) Store(ctx context.Context, text string) error {
    l.mu.RLock()
    col := l.col
    l.mu.RUnlock()

    id := uuid.NewString()
    now := time.Now()

    // Generate optional summary.
    var summary string
    if l.summarizer != nil {
        if s, err := l.summarizer.Summarize(ctx, text); err == nil {
            summary = s
        }
    }

    // Store the raw text vector.
    if err := col.AddDocument(ctx, chromem.Document{
        ID:      id,
        Content: text,
        Metadata: map[string]string{
            "created_at": fmt.Sprintf("%d", now.Unix()),
            "type":       "raw",
        },
    }); err != nil {
        return fmt.Errorf("store raw vector: %w", err)
    }

    // Store the summary vector (if available) with a separate ID.
    if summary != "" {
        summaryID := uuid.NewString()
        _ = col.AddDocument(ctx, chromem.Document{
            ID:      summaryID,
            Content: summary,
            Metadata: map[string]string{
                "created_at": fmt.Sprintf("%d", now.Unix()),
                "type":       "summary",
                "raw_id":     id,
            },
        })
    }

    // Persist metadata to SQLite.
    if l.db != nil {
        _, err := l.db.ExecContext(ctx,
            `INSERT INTO memory_segments(vector_id, raw_content, summary, created_at) VALUES(?,?,?,?)`,
            id, text, summary, now)
        if err != nil {
            // Non-fatal: vector is already stored.
            return nil
        }
    }
    return nil
}
```

- [ ] **Step 3: 升级 `Search` 方法——时间衰减重排序**

替换现有 `Search` 方法：

```go
// Search returns the top-k most relevant memory blocks for the query,
// re-ranked by a time-decay factor that boosts recent memories.
func (l *LongStore) Search(ctx context.Context, query string, k int) ([]string, error) {
    l.mu.RLock()
    col := l.col
    l.mu.RUnlock()

    if col.Count() == 0 {
        return nil, nil
    }
    // Fetch more candidates to allow re-ranking.
    fetch := min(k*3, col.Count())
    results, err := col.Query(ctx, query, fetch, nil, nil)
    if err != nil {
        return nil, err
    }

    type scored struct {
        content string
        score   float32
    }

    now := float64(time.Now().Unix())
    const halfLifeDays = 30.0
    halfLifeSecs := halfLifeDays * 86400

    var candidates []scored
    seen := make(map[string]bool) // deduplicate by raw_id
    for _, r := range results {
        // Skip duplicate summary entries that point to a raw we already have.
        if rawID := r.Metadata["raw_id"]; rawID != "" {
            if seen[rawID] {
                continue
            }
            seen[rawID] = true
        }

        // Parse stored timestamp for time-decay.
        var createdAt float64
        if ts := r.Metadata["created_at"]; ts != "" {
            if v, err := strconv.ParseFloat(ts, 64); err == nil {
                createdAt = v
            }
        }

        // Time-decay: e^(-λ·Δt), λ = ln2 / halfLife
        var decay float64 = 1.0
        if createdAt > 0 {
            delta := now - createdAt
            if delta > 0 {
                decay = math.Exp(-0.693147 * delta / halfLifeSecs)
            }
        }
        // Blend: 70% semantic + 30% recency.
        blended := float32(float64(r.Similarity)*0.7 + decay*0.3)
        candidates = append(candidates, scored{content: r.Content, score: blended})
    }

    // Sort by blended score descending.
    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].score > candidates[j].score
    })

    // Return top-k content strings.
    out := make([]string, 0, k)
    for i, c := range candidates {
        if i >= k {
            break
        }
        out = append(out, c.content)
    }
    return out, nil
}
```

在 `long.go` import 中加入：

```go
"math"
"sort"
"strconv"
```

- [ ] **Step 4: 更新 `DeleteAll` 同时清理 SQLite 元数据**

替换现有 `DeleteAll`：

```go
// DeleteAll removes all documents from the long-term memory collection and
// clears the SQLite metadata table.
func (l *LongStore) DeleteAll(db *chromem.DB, embedder embedding.Embedder) error {
    if err := db.DeleteCollection("memories"); err != nil {
        return fmt.Errorf("delete memories collection: %w", err)
    }
    col, err := db.GetOrCreateCollection("memories", nil, EmbeddingFuncFrom(embedder))
    if err != nil {
        return fmt.Errorf("recreate memories collection: %w", err)
    }
    l.mu.Lock()
    l.col = col
    l.mu.Unlock()

    if l.db != nil {
        if _, err := l.db.Exec(`DELETE FROM memory_segments`); err != nil {
            return fmt.Errorf("clear memory_segments: %w", err)
        }
    }
    return nil
}
```

- [ ] **Step 5: 验证编译**

```bash
go build ./...
```

Expected: 编译失败，`NewLongStore` 签名改变，`app.go` 和 `ClearChatHistory` 需要更新 — 预期，下一步修复。

- [ ] **Step 6: 更新 `app.go` 中所有 `NewLongStore` 调用**

在 `initLLMComponents` 中，先创建 summarizer，再传入 `NewLongStore`：

```go
// Create optional summarizer.
summarizer, err := llm.NewSummarizer(ctx, a.cfg)
if err != nil {
    // Non-fatal: proceed without summarization.
    slog.Warn("summarizer init failed, continuing without summarization", "err", err)
    summarizer = nil
}

// ...（现有 embedder 创建代码）...

if embedder != nil {
    longMem, err = memory.NewLongStore(a.vectorDB, a.sqlDB, embedder, summarizer)
    // ...
}
```

在 `ClearChatHistory` 中，`longMem.DeleteAll(a.vectorDB, embedder)` 调用不需要改变（签名未变）。

- [ ] **Step 7: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 8: Commit**

```bash
git add internal/memory/long.go internal/llm/client.go app.go
git commit -m "feat: upgrade LongStore with segmented storage, optional summarization, and time-decay retrieval"
```

---

## Self-Review

**Spec coverage:**
- ✅ #8 基于 mempalace 理念优化记忆存储 — 分层（raw + summary 双向量）、verbatim 原文保留、混合检索（语义 + 时间衰减）
- ✅ 可选小模型用于总结 — Task 2 (Summarizer, NewSummarizer)
- ✅ API 兼容 — `Store`/`Search`/`DeleteAll` 签名不变，agent.go 无需修改

**Placeholder scan:** 无 TBD / TODO。时间衰减公式（半衰期 30 天）和混合权重（70/30）为具体数值，可在后续调优。

**Type consistency:** `NewLongStore(vectorDB, sqlDB, embedder, summarizer)` 在 Task 3 Step 1 定义，Task 3 Step 6 中 `app.go` 同步更新；`llm.Summarizer` 接口在 Task 2 中定义，`long.go` 通过 import 引用，类型一致。
