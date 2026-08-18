//go:build ledger_sqlite

package ledger

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

// TestSQLiteStoreCompareAndSwapRaceExactlyOneWinner races two
// goroutines calling CompareAndSwap against the same freshly admitted
// key on a file-backed SQLiteStore. Exactly one call succeeds; the
// busy_timeout pragma serializes the other writer's statement instead
// of failing it immediately with SQLITE_BUSY. Run under
// go test -tags ledger_sqlite -race ./ledger/....
func TestSQLiteStoreCompareAndSwapRaceExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.db")
	store := newSQLiteStoreT(t, path)

	if ok, err := store.CompareAndSwap(ctx, "k1", TaskState{}, TaskState{
		Key: "k1", Status: StatusPending, Sequence: 1,
	}); err != nil || !ok {
		t.Fatalf("seed insert: ok=%v err=%v", ok, err)
	}
	seed, found, err := store.Load(ctx, "k1")
	if err != nil || !found {
		t.Fatalf("Load: found=%v err=%v", found, err)
	}

	var wg sync.WaitGroup
	oks := make([]bool, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := store.CompareAndSwap(ctx, "k1", seed, TaskState{
				Key:        "k1",
				Status:     StatusClaimed,
				Sequence:   1,
				Owner:      "owner",
				Fence:      FenceToken(i + 1),
				LeaseUntil: fixedSQLiteNow,
			})
			oks[i] = ok
			errs[i] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for i, ok := range oks {
		if errs[i] != nil {
			t.Fatalf("CompareAndSwap[%d]: %v", i, errs[i])
		}
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want 1 (oks: %v)", successes, oks)
	}
}
