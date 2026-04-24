package memory

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"

	"aiko/internal/llm"
)

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

// Search returns the top-k most relevant memory blocks for the query,
// re-ranked by a time-decay factor that boosts recent memories.
func (l *LongStore) Search(ctx context.Context, query string, k int) ([]string, error) {
	l.mu.RLock()
	col := l.col
	l.mu.RUnlock()

	total := col.Count()
	if total == 0 {
		return nil, nil
	}
	// Fetch more candidates to allow re-ranking.
	fetch := min(k*3, total)
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

// EmbeddingFuncFrom wraps an eino Embedder into chromem-go's EmbeddingFunc type.
// Returns nil if e is nil.
func EmbeddingFuncFrom(e embedding.Embedder) chromem.EmbeddingFunc {
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
		// Convert []float64 to []float32 as required by chromem-go.
		f64 := vecs[0]
		f32 := make([]float32, len(f64))
		for i, v := range f64 {
			f32[i] = float32(v)
		}
		return f32, nil
	}
}
