package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"
)

// LongStore manages long-term conversation memory using chromem-go.
type LongStore struct {
	col      *chromem.Collection
	embedder embedding.Embedder
}

// NewLongStore creates or opens the memories collection.
func NewLongStore(db *chromem.DB, embedder embedding.Embedder) (*LongStore, error) {
	col, err := db.GetOrCreateCollection("memories", nil, EmbeddingFuncFrom(embedder))
	if err != nil {
		return nil, fmt.Errorf("get memories collection: %w", err)
	}
	return &LongStore{col: col, embedder: embedder}, nil
}

// Store saves a block of conversation text (raw, no summarization).
func (l *LongStore) Store(ctx context.Context, text string) error {
	return l.col.AddDocument(ctx, chromem.Document{
		ID:      uuid.NewString(),
		Content: text,
		Metadata: map[string]string{
			"created_at": fmt.Sprintf("%d", time.Now().Unix()),
		},
	})
}

// Search returns the top-k most relevant memory blocks for the query.
// Returns nil if no memories exist yet.
func (l *LongStore) Search(ctx context.Context, query string, k int) ([]string, error) {
	if l.col.Count() == 0 {
		return nil, nil
	}
	n := k
	if n > l.col.Count() {
		n = l.col.Count()
	}
	results, err := l.col.Query(ctx, query, n, nil, nil)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(results))
	for i, r := range results {
		texts[i] = r.Content
	}
	return texts, nil
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
