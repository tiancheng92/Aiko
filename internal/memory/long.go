package memory

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"
)

// LongStore manages long-term conversation memory using chromem-go.
type LongStore struct {
	mu  sync.RWMutex
	col *chromem.Collection
}

// NewLongStore creates or opens the memories collection.
func NewLongStore(vectorDB *chromem.DB, embedder embedding.Embedder) (*LongStore, error) {
	col, err := vectorDB.GetOrCreateCollection("memories", nil, EmbeddingFuncFrom(embedder))
	if err != nil {
		return nil, fmt.Errorf("get memories collection: %w", err)
	}
	return &LongStore{col: col}, nil
}

// Store saves a conversation segment as a raw vector.
// Skips near-duplicate entries (cosine similarity > 0.92) to prevent fact accumulation.
func (l *LongStore) Store(ctx context.Context, text string) error {
	l.mu.RLock()
	col := l.col
	l.mu.RUnlock()

	if col.Count() > 0 {
		dupes, err := col.Query(ctx, text, 1, nil, nil)
		if err == nil && len(dupes) > 0 && dupes[0].Similarity > 0.92 {
			return nil
		}
	}

	id := uuid.NewString()
	now := time.Now()

	if err := col.AddDocument(ctx, chromem.Document{
		ID:      id,
		Content: text,
		Metadata: map[string]string{
			"created_at": strconv.FormatInt(now.Unix(), 10),
		},
	}); err != nil {
		return fmt.Errorf("store raw vector: %w", err)
	}

	return nil
}

// timeDecayScore blends a chromem similarity score with a 30-day half-life
// time-decay factor: 70% semantic + 30% recency.
func timeDecayScore(r chromem.Result, now float64) float32 {
	const halfLifeSecs = 30.0 * 86400
	var createdAt float64
	if ts := r.Metadata["created_at"]; ts != "" {
		if v, err := strconv.ParseFloat(ts, 64); err == nil {
			createdAt = v
		}
	}
	decay := 1.0
	if createdAt > 0 {
		if delta := now - createdAt; delta > 0 {
			decay = math.Exp(-0.693147 * delta / halfLifeSecs)
		}
	}
	return float32(float64(r.Similarity)*0.7 + decay*0.3)
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
	candidates := make([]scored, 0, len(results))
	for _, r := range results {
		candidates = append(candidates, scored{content: r.Content, score: timeDecayScore(r, now)})
	}

	slices.SortFunc(candidates, func(a, b scored) int { return cmp.Compare(b.score, a.score) })

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
		f64 := vecs[0]
		f32 := make([]float32, len(f64))
		for i, v := range f64 {
			f32[i] = float32(v)
		}
		return f32, nil
	}
}
