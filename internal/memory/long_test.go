package memory_test

import (
	"context"
	"testing"

	chromem "github.com/philippgille/chromem-go"

	"aiko/internal/memory"
)

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

func TestSearchSplit_InterfaceCompliance(t *testing.T) {
	// Compile-time check that SearchSplit exists with the right signature.
	var _ interface {
		SearchSplit(ctx context.Context, query string, k int) (memory.MemorySearchResult, error)
	} = (*memory.LongStore)(nil)
}
