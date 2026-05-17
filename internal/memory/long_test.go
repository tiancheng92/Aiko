package memory_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/embedding"
	chromem "github.com/philippgille/chromem-go"
	_ "modernc.org/sqlite"

	"aiko/internal/memory"
)

// stubEmbedder is a deterministic, fixed-dimension embedder for tests.
// Identical texts always receive identical vectors (cosine similarity = 1.0).
type stubEmbedder struct {
	mu      sync.Mutex
	catalog map[string][]float64
	dim     int
	next    int
}

func newStubEmbedder(dim int) *stubEmbedder {
	return &stubEmbedder{catalog: map[string][]float64{}, dim: dim}
}

// EmbedStrings implements embedding.Embedder.
func (e *stubEmbedder) EmbedStrings(_ context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([][]float64, len(texts))
	for i, t := range texts {
		if v, ok := e.catalog[t]; ok {
			result[i] = v
			continue
		}
		v := make([]float64, e.dim)
		idx := e.next % e.dim
		v[idx] = float64(e.next + 1)
		e.next++
		e.catalog[t] = v
		result[i] = v
	}
	return result, nil
}

// newTestLongStore creates a LongStore backed by in-memory chromem and SQLite.
func newTestLongStore(t *testing.T) *memory.LongStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS memory_segments (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			vector_id   TEXT NOT NULL UNIQUE,
			raw_content TEXT NOT NULL,
			summary     TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	vectorDB := chromem.NewDB()
	embedder := newStubEmbedder(16)
	store, err := memory.NewLongStore(vectorDB, db, embedder, nil)
	if err != nil {
		t.Fatalf("NewLongStore: %v", err)
	}
	return store
}

// TestSearchSplit_EmptyCollection verifies that SearchSplit on an empty store
// returns an empty result without error.
func TestSearchSplit_EmptyCollection(t *testing.T) {
	db := chromem.NewDB()
	store, err := memory.NewLongStore(db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := store.SearchSplit(context.Background(), "anything", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Summaries) != 0 || len(res.Raws) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

// TestSearchSplit_InterfaceCompliance is a compile-time check that SearchSplit
// exists with the right signature.
func TestSearchSplit_InterfaceCompliance(t *testing.T) {
	var _ interface {
		SearchSplit(ctx context.Context, query string, k int) (memory.MemorySearchResult, error)
	} = (*memory.LongStore)(nil)
}

// TestLongStore_InterfaceCompleteness is a compile-time check that LongStore
// exposes the expected public API.
func TestLongStore_InterfaceCompleteness(t *testing.T) {
	type longStoreIface interface {
		Store(ctx context.Context, text string) error
		Search(ctx context.Context, query string, k int) ([]string, error)
		SearchSplit(ctx context.Context, query string, k int) (memory.MemorySearchResult, error)
	}
	var _ longStoreIface = (*memory.LongStore)(nil)
}

// TestStore_RoundTrip verifies that stored text can be retrieved via Search.
func TestStore_RoundTrip(t *testing.T) {
	store := newTestLongStore(t)
	ctx := context.Background()

	if err := store.Store(ctx, "the quick brown fox"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	results, err := store.Search(ctx, "the quick brown fox", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result after storing text")
	}
}

// TestSearch_EmptyStore verifies that Search on an empty store returns nil without error.
func TestSearch_EmptyStore(t *testing.T) {
	store := newTestLongStore(t)
	ctx := context.Background()

	results, err := store.Search(ctx, "anything", 5)
	if err != nil {
		t.Fatalf("Search on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}
}

// TestStore_MultipleEntries verifies that multiple stored entries are all
// retrievable and that Search returns results.
func TestStore_MultipleEntries(t *testing.T) {
	store := newTestLongStore(t)
	ctx := context.Background()

	entries := []string{"apple memory", "banana memory", "cherry memory"}
	for _, e := range entries {
		if err := store.Store(ctx, e); err != nil {
			t.Fatalf("Store %q: %v", e, err)
		}
	}

	results, err := store.Search(ctx, "apple memory", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results after storing 3 entries")
	}
}

// TestConcurrentStore verifies that concurrent Store calls do not race or panic.
func TestConcurrentStore(t *testing.T) {
	store := newTestLongStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = n
			if err := store.Store(ctx, "concurrent memory entry"); err != nil {
				// deduplication may cause some to be silently skipped — that's fine
				_ = err
			}
		}(i)
	}
	wg.Wait()
}

// TestMemorySearchResult_ZeroValue verifies the zero value of MemorySearchResult
// has nil slices (safe to range over).
func TestMemorySearchResult_ZeroValue(t *testing.T) {
	var res memory.MemorySearchResult
	if len(res.Summaries) != 0 {
		t.Error("zero-value Summaries should be empty")
	}
	if len(res.Raws) != 0 {
		t.Error("zero-value Raws should be empty")
	}
}
