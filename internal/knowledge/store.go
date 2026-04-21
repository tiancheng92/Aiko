package knowledge

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"

	"desktop-pet/internal/memory"
)

// Store manages the knowledge base collection in chromem-go.
type Store struct {
	col     *chromem.Collection
	sources sync.Map // tracks known source names (populated by AddChunk)
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
	err := s.col.AddDocument(ctx, chromem.Document{
		ID:      uuid.NewString(),
		Content: text,
		Metadata: map[string]string{
			"source":      source,
			"chunk_index": fmt.Sprintf("%d", chunkIdx),
		},
	})
	if err != nil {
		return err
	}
	s.sources.Store(source, true)
	return nil
}

// Search returns top-k relevant chunks for the query.
// Returns nil if the knowledge base is empty.
func (s *Store) Search(ctx context.Context, query string, k int) ([]string, error) {
	if s.col.Count() == 0 {
		return nil, nil
	}
	n := k
	if n > s.col.Count() {
		n = s.col.Count()
	}
	results, err := s.col.Query(ctx, query, n, nil, nil)
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
	err := s.col.Delete(ctx, map[string]string{"source": source}, nil)
	if err != nil {
		return err
	}
	s.sources.Delete(source)
	return nil
}

// ListSources returns all unique source filenames tracked since the store was created.
// Note: this reflects sources added in the current session; re-importing restores them.
func (s *Store) ListSources(_ context.Context) ([]string, error) {
	var sources []string
	s.sources.Range(func(key, _ any) bool {
		if src, ok := key.(string); ok {
			sources = append(sources, src)
		}
		return true
	})
	return sources, nil
}
