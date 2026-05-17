package memory

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/rs/zerolog/log"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"
	"golang.org/x/sync/errgroup"

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

	// Deduplication: skip if a near-identical raw vector already exists.
	// This prevents the same fact from accumulating across conversations.
	if col.Count() > 0 {
		dupes, err := col.Query(ctx, text, 1, map[string]string{"type": "raw"}, nil)
		if err == nil && len(dupes) > 0 && dupes[0].Similarity > 0.92 {
			return nil
		}
	}

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
	// Summary is a retrieval optimization — a failure here should not block
	// persistence of the raw vector, so we log and continue.
	if summary != "" {
		summaryID := uuid.NewString()
		if err := col.AddDocument(ctx, chromem.Document{
			ID:      summaryID,
			Content: summary,
			Metadata: map[string]string{
				"created_at": fmt.Sprintf("%d", now.Unix()),
				"type":       "summary",
				"raw_id":     id,
			},
		}); err != nil {
			log.Warn().Str("id", id).Err(err).Msg("long memory: summary vector add failed")
		}
	}

	// Persist metadata to SQLite. Non-fatal when it fails: the vector is the
	// source of truth for retrieval, and the SQLite table is best-effort
	// metadata used for the settings UI and manual browsing.
	if l.db != nil {
		if _, err := l.db.ExecContext(ctx,
			`INSERT INTO memory_segments(vector_id, raw_content, summary, created_at) VALUES(?,?,?,?)`,
			id, text, summary, now); err != nil {
			log.Warn().Str("id", id).Err(err).Msg("long memory: sqlite metadata insert failed")
		}
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

// decayRank re-ranks a slice of chromem results by timeDecayScore and returns
// the top-k content strings. No deduplication is performed.
func decayRank(results []chromem.Result, k int) []string {
	type scored struct {
		content string
		score   float32
	}
	now := float64(time.Now().Unix())
	candidates := make([]scored, 0, len(results))
	for _, r := range results {
		candidates = append(candidates, scored{content: r.Content, score: timeDecayScore(r, now)})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	out := make([]string, 0, k)
	for i, c := range candidates {
		if i >= k {
			break
		}
		out = append(out, c.content)
	}
	return out
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
		candidates = append(candidates, scored{content: r.Content, score: timeDecayScore(r, now)})
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

// MemorySearchResult holds separately retrieved summaries and raw memory blocks.
type MemorySearchResult struct {
	Summaries []string // one-sentence summaries of past conversations
	Raws      []string // full raw conversation blocks
}

// SearchSplit retrieves the top-k most relevant summaries and raw memory blocks
// separately via metadata filter, so both dimensions contribute top-k slots
// without competing against each other.
func (l *LongStore) SearchSplit(ctx context.Context, query string, k int) (MemorySearchResult, error) {
	l.mu.RLock()
	col := l.col
	l.mu.RUnlock()

	if col.Count() == 0 {
		return MemorySearchResult{}, nil
	}

	var res MemorySearchResult
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)

	// Fetch k*2 candidates per type to allow time-decay re-ranking before
	// taking the final top-k.
	fetch := min(k*2, col.Count())

	g.Go(func() error {
		results, err := col.Query(gctx, query, fetch, map[string]string{"type": "summary"}, nil)
		if err != nil {
			return err
		}
		mu.Lock()
		res.Summaries = decayRank(results, k)
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		results, err := col.Query(gctx, query, fetch, map[string]string{"type": "raw"}, nil)
		if err != nil {
			return err
		}
		mu.Lock()
		res.Raws = decayRank(results, k)
		mu.Unlock()
		return nil
	})

	if err := g.Wait(); err != nil {
		return MemorySearchResult{}, err
	}
	return res, nil
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
