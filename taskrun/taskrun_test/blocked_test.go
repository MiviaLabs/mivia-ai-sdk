package taskrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestBlockedDependencySkipsWork proves a task whose dependency
// completed StatusFailed is StatusBlocked; Run returns ErrTaskBlocked
// and never calls work.
func TestBlockedDependencySkipsWork(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Build "root" and "child" (which needs root), then fail root so
	// the dependent scan blocks child before Run ever sees it.
	if _, err := l.Admit(ctx, "actor", "root", 1, "root", fixedNow); err != nil {
		t.Fatalf("Admit root: %v", err)
	}
	if _, err := l.Admit(ctx, "actor", "child", 1, "child", fixedNow, "root"); err != nil {
		t.Fatalf("Admit child: %v", err)
	}
	rootFence, err := l.Claim(ctx, "actor", "root", "owner", fixedLease, fixedNow)
	if err != nil {
		t.Fatalf("Claim root: %v", err)
	}
	if err := l.Complete(ctx, "actor", "root", "owner", rootFence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete root: %v", err)
	}
	called := 0
	err = taskrun.Run(ctx, runOpts(l), taskrun.Task{Key: "child", Seq: 1}, func(context.Context) error {
		called++
		return nil
	})
	if err != taskrun.ErrTaskBlocked {
		t.Fatalf("Run = %v, want ErrTaskBlocked", err)
	}
	if called != 0 {
		t.Fatalf("work ran %d times on a blocked task", called)
	}
}

// TestBlockedEscapeeReturnsNotClaimedThenBlocked pins the two-call
// shape for a task the failure walk never saw. "child" names "mid",
// which admits already blocked after "root" fails, so nothing walks
// child and it stays StatusPending. Run's State check therefore reads
// StatusPending on the first call, and Claim's own ancestor check
// refuses with ledger.ErrNotClaimed. That refusal blocks child, so the
// second call returns ErrTaskBlocked. Work runs on neither call.
func TestBlockedEscapeeReturnsNotClaimedThenBlocked(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Admit child naming a "mid" that does not exist yet, then fail
	// root while mid is still absent, so the failure walk finds
	// nothing. Admitting mid afterwards inserts it already blocked.
	if _, err := l.Admit(ctx, "actor", "child", 1, "child", fixedNow, "mid"); err != nil {
		t.Fatalf("Admit child: %v", err)
	}
	if _, err := l.Admit(ctx, "actor", "root", 1, "root", fixedNow); err != nil {
		t.Fatalf("Admit root: %v", err)
	}
	rootFence, err := l.Claim(ctx, "actor", "root", "owner", fixedLease, fixedNow)
	if err != nil {
		t.Fatalf("Claim root: %v", err)
	}
	if err := l.Complete(ctx, "actor", "root", "owner", rootFence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete root: %v", err)
	}
	if _, err := l.Admit(ctx, "actor", "mid", 1, "mid", fixedNow, "root"); err != nil {
		t.Fatalf("Admit mid: %v", err)
	}

	called := 0
	task := taskrun.Task{Key: "child", Seq: 1, Needs: []ledger.IdempotencyKey{"mid"}}
	work := func(context.Context) error {
		called++
		return nil
	}
	first := taskrun.Run(ctx, runOpts(l), task, work)
	if !errors.Is(first, ledger.ErrNotClaimed) {
		t.Fatalf("first Run = %v, want ledger.ErrNotClaimed", first)
	}
	if errors.Is(first, taskrun.ErrTaskBlocked) {
		t.Fatalf("first Run = %v, want ledger.ErrNotClaimed, not ErrTaskBlocked", first)
	}
	second := taskrun.Run(ctx, runOpts(l), task, work)
	if !errors.Is(second, taskrun.ErrTaskBlocked) {
		t.Fatalf("second Run = %v, want ErrTaskBlocked", second)
	}
	if called != 0 {
		t.Fatalf("work ran %d times on an escapee", called)
	}
}
