package knowledge

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"

	"aiko/internal/memory"
)

const minSimilarity = 0.3

// SearchResult holds a matched chunk together with its source file and score.
type SearchResult struct {
	Content    string
	Source     string
	Similarity float32
}

// Store manages the knowledge base collection in chromem-go, with source
// names tracked in SQLite to avoid querying the vector index for metadata.
type Store struct {
	col *chromem.Collection
	db  *sql.DB
}

// NewStore creates or opens the knowledge collection.
func NewStore(db *chromem.DB, sqlDB *sql.DB, embedder embedding.Embedder) (*Store, error) {
	col, err := db.GetOrCreateCollection("knowledge", nil, memory.EmbeddingFuncFrom(embedder))
	if err != nil {
		return nil, fmt.Errorf("get knowledge collection: %w", err)
	}
	return &Store{col: col, db: sqlDB}, nil
}

// AddChunk stores a single text chunk with source metadata and records the
// source in the knowledge_sources table.
func (s *Store) AddChunk(ctx context.Context, text, source string, chunkIdx int) error {
	if err := s.col.AddDocument(ctx, chromem.Document{
		ID:      uuid.NewString(),
		Content: text,
		Metadata: map[string]string{
			"source":      source,
			"chunk_index": fmt.Sprintf("%d", chunkIdx),
		},
	}); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO knowledge_sources(source) VALUES(?)`, source)
	return err
}

// Search returns top-k relevant chunks for the query.
// It fetches up to 2×k candidates from the vector index, filters those below
// minSimilarity, and returns at most k results ordered by descending similarity.
// Returns nil if the knowledge base is empty or no result meets the threshold.
func (s *Store) Search(ctx context.Context, query string, k int) ([]SearchResult, error) {
	total := s.col.Count()
	if total == 0 {
		return nil, nil
	}

	// Over-fetch so that threshold filtering still yields up to k results.
	fetch := min(k*2, total)
	raw, err := s.col.Query(ctx, query, fetch, nil, nil)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, k)
	for _, r := range raw {
		if r.Similarity < minSimilarity {
			continue
		}
		results = append(results, SearchResult{
			Content:    r.Content,
			Source:     r.Metadata["source"],
			Similarity: r.Similarity,
		})
		if len(results) == k {
			break
		}
	}
	return results, nil
}

// DeleteBySource removes all chunks from a given source file and deletes
// the source record from the SQLite index.
func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	if err := s.col.Delete(ctx, map[string]string{"source": source}, nil); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM knowledge_sources WHERE source = ?`, source)
	return err
}

// ListSources returns all unique source filenames recorded in the SQLite index.
func (s *Store) ListSources(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source FROM knowledge_sources ORDER BY added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}
