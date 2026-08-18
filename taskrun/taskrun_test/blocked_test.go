package taskrun_test

import (
	"context"
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
