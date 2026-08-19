package longtermmemory_test

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

func TestStoreConcurrentSaveSearchPromote(t *testing.T) {
	s := longtermmemory.New(0)
	shared := validEntry("Shared entry", "shared summary")
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.Background()
			// Every goroutine re-saves the same entry: idempotency
			// must survive the race.
			if _, err := s.Save(ctx, shared); err != nil {
				t.Errorf("Save shared: %v", err)
			}
			for j := 0; j < 5; j++ {
				e := distinct("Goroutine entry", "2026-01-01")
				e.Detail = "unique detail"
				e.Summary = e.Summary + " " + string(rune('a'+i)) + string(rune('0'+j))
				if _, err := s.Save(ctx, e); err != nil {
					t.Errorf("Save own: %v", err)
				}
			}
			if _, err := s.Search(ctx, longtermmemory.Query{Text: "shared", Scope: "proj"}); err != nil {
				t.Errorf("Search: %v", err)
			}
			hits, err := s.Search(ctx, longtermmemory.Query{Text: "goroutine", Scope: "proj"})
			if err != nil {
				t.Errorf("Search own: %v", err)
			}
			for _, hit := range hits {
				if err := s.PromoteToCore(ctx, hit.ID); err != nil {
					t.Errorf("PromoteToCore: %v", err)
				}
			}
		}(g)
	}
	wg.Wait()
	n, err := s.Count(context.Background(), "proj")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	// 8 goroutines times 5 own entries, plus the one shared entry.
	if n != 41 {
		t.Fatalf("Count after the race = %d, want 41: no lost idempotency", n)
	}
}
