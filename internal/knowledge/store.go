package knowledge

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"

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
	return s.col.Delete(ctx, map[string]string{"source": source}, nil)
}

// ListSources returns all unique source filenames in the knowledge collection.
func (s *Store) ListSources(ctx context.Context) ([]string, error) {
	count := s.col.Count()
	if count == 0 {
		return nil, nil
	}
	results, err := s.col.Query(ctx, "a", count, nil, nil)
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
