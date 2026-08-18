//go:build ledger_sqlite

package e2e_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// openSQLite opens one durable store over path and its ledger. The
// caller owns Close: the reopen path closes mid-test on purpose.
func openSQLite(t *testing.T, path string) (*ledger.Ledger, *ledger.SQLiteStore) {
	t.Helper()
	store, err := ledger.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q): %v", path, err)
	}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	return l, store
}

// TestCeremonySurvivesStoreReopen drives the full taskrun ceremony
// around one agentrun pipeline over a SQLite file, closes the store,
// reopens it from the same path, and proves the record and the
// replay sentinel survive: durability across a restart.
func TestCeremonySurvivesStoreReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "e2e.db")
	l, store := openSQLite(t, path)
	opts := taskrun.Options{
		Ledger: l, Actor: "e2e-actor", Owner: "e2e-owner", Lease: time.Minute,
	}

	calls := 0
	if err := taskrun.Run(ctx, opts,
		taskrun.Task{Key: "compose", Seq: 1},
		pipelineWork(t, &calls)); err != nil {
		t.Fatalf("taskrun.Run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("work calls = %d, want 1", calls)
	}

	// Reopen the same file: a restarted process sees the completed
	// record, and the replay sentinel holds without re-running work.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, reopenedStore := openSQLite(t, path)
	t.Cleanup(func() {
		if err := reopenedStore.Close(); err != nil {
			t.Errorf("Close reopened: %v", err)
		}
	})
	st, found, err := reopened.State(ctx, "compose")
	if err != nil || !found || st.Status != ledger.StatusCompleted {
		t.Fatalf("reopened state = %q,%v,%v, want completed", st.Status, found, err)
	}
	replayOpts := taskrun.Options{
		Ledger: reopened, Actor: "e2e-actor", Owner: "e2e-owner",
		Lease: time.Minute,
	}
	if err := taskrun.Run(ctx, replayOpts,
		taskrun.Task{Key: "compose", Seq: 2},
		pipelineWork(t, &calls)); !errors.Is(err, taskrun.ErrTaskDone) {
		t.Fatalf("replay = %v, want ErrTaskDone", err)
	}
	if calls != 1 {
		t.Fatalf("work calls after reopen replay = %d, want 1", calls)
	}

	// A failed dependency blocks its dependents on the durable store
	// too, admitted either side of the failure.
	now := time.Now()
	if _, err := reopened.Admit(ctx, "e2e-actor", "dep", 1, nil, now); err != nil {
		t.Fatalf("Admit dep: %v", err)
	}
	fence, err := reopened.Claim(ctx, "e2e-actor", "dep", "e2e-owner", time.Minute, now)
	if err != nil {
		t.Fatalf("Claim dep: %v", err)
	}
	if err := reopened.Complete(ctx, "e2e-actor", "dep", "e2e-owner", fence,
		ledger.StatusFailed, now); err != nil {
		t.Fatalf("Complete dep: %v", err)
	}
	if _, err := reopened.Admit(ctx, "e2e-actor", "late", 1, nil, now, "dep"); err != nil {
		t.Fatalf("Admit late: %v", err)
	}
	blocked := 0
	err = taskrun.Run(ctx, replayOpts,
		taskrun.Task{Key: "late", Seq: 1, Needs: []ledger.IdempotencyKey{"dep"}},
		pipelineWork(t, &blocked))
	if !errors.Is(err, taskrun.ErrTaskBlocked) {
		t.Fatalf("dependent = %v, want ErrTaskBlocked", err)
	}
	if blocked != 0 {
		t.Fatalf("blocked work calls = %d, want 0", blocked)
	}
}
