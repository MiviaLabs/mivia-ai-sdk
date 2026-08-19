//go:build ledger_sqlite

package ledger_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// sqliteStoreT opens a file-backed SQLiteStore and registers Close as a
// test cleanup. A file path, not ":memory:", forces the WAL and
// busy_timeout path the race tests exercise on the real backend.
func sqliteStoreT(t *testing.T) *ledger.SQLiteStore {
	t.Helper()
	store, err := ledger.NewSQLiteStore(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return store
}

// sqliteLedgerT builds a Ledger over a file-backed SQLiteStore.
func sqliteLedgerT(t *testing.T) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(sqliteStoreT(t), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

// TestSQLiteLedgerClaimRaceExactlyOneWinner races two Claims against
// the same key over a file-backed SQLiteStore. Exactly one wins; the
// loser observes ErrLeaseActive after SQLite serializes the writers.
// Run under go test -tags ledger_sqlite -race ./ledger/...
func TestSQLiteLedgerClaimRaceExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	l := sqliteLedgerT(t)
	mustAdmit(t, l, ctx, "k1", 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := ledger.OwnerID("owner-a")
			if i == 1 {
				owner = "owner-b"
			}
			_, errs[i] = l.Claim(ctx, testActor, "k1", owner, fixedLease, fixedNow)
		}(i)
	}
	wg.Wait()

	successes, activeErrors := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ledger.ErrLeaseActive):
			activeErrors++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want 1 (errs: %v)", successes, errs)
	}
	if activeErrors != 1 {
		t.Fatalf("ErrLeaseActive count = %d, want 1 (errs: %v)", activeErrors, errs)
	}
}

// TestSQLiteLedgerCompleteRaceExactlyOneWinner races two Completes,
// one StatusCompleted and one StatusFailed, against the same fence.
// Exactly one lands; the loser observes ErrNotClaimed. Run under
// go test -tags ledger_sqlite -race ./ledger/...
func TestSQLiteLedgerCompleteRaceExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	l := sqliteLedgerT(t)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")

	var wg sync.WaitGroup
	var completedErr, failedErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		completedErr = l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusCompleted, fixedNow)
	}()
	go func() {
		defer wg.Done()
		failedErr = l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusFailed, fixedNow)
	}()
	wg.Wait()

	completedWon := completedErr == nil
	failedWon := failedErr == nil
	if completedWon == failedWon {
		t.Fatalf("want exactly one winner: completedErr=%v failedErr=%v", completedErr, failedErr)
	}
	if !completedWon && !errors.Is(completedErr, ledger.ErrNotClaimed) {
		t.Fatalf("losing StatusCompleted call = %v, want ErrNotClaimed", completedErr)
	}
	if !failedWon && !errors.Is(failedErr, ledger.ErrNotClaimed) {
		t.Fatalf("losing StatusFailed call = %v, want ErrNotClaimed", failedErr)
	}

	st, found, err := l.State(ctx, "k1")
	if err != nil || !found {
		t.Fatalf("State: found=%v err=%v", found, err)
	}
	want := ledger.StatusFailed
	if completedWon {
		want = ledger.StatusCompleted
	}
	if st.Status != want {
		t.Fatalf("final Status = %q, want %q", st.Status, want)
	}
}

// TestSQLiteLedgerStressLinearizable runs the shared mixed-operation
// storm over a file-backed SQLiteStore and checks the recorded history
// linearizes against the same independent model the MemStore storm
// uses. Run under go test -tags ledger_sqlite -race ./ledger/...
func TestSQLiteLedgerStressLinearizable(t *testing.T) {
	ctx := context.Background()
	l := sqliteLedgerT(t)
	keys := []ledger.IdempotencyKey{"k0", "k1", "k2"}
	for _, k := range keys {
		mustAdmit(t, l, ctx, k, 1)
	}
	ops := runStorm(ctx, l, keys, 4, 4, 3)
	for i := range ops {
		if !isSentinel(ops[i].err) {
			t.Fatalf("op %d (%d on %s) returned unexpected error %v", i, ops[i].kind, ops[i].key, ops[i].err)
		}
	}
	if !linearizable(ops, initialModel(keys)) {
		t.Fatalf("recorded history of %d ops is not linearizable over SQLiteStore", len(ops))
	}
}
