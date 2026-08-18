package taskrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestCompleteAlwaysLands proves a work function that fails after the
// claim does not panic, Run still completes, and a second Complete with
// the same fence returns ErrNotClaimed, proving the first landed.
func TestCompleteAlwaysLands(t *testing.T) {
	ctx := context.Background()
	ps := &probeStore{Store: ledger.NewMemStore()}
	l, err := ledger.New(ps, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workErr := errors.New("work broke")
	task := taskrun.Task{Key: "key", Seq: 1}
	got := taskrun.Run(ctx, runOpts(l), task, func(context.Context) error { return workErr })
	if !errors.Is(got, workErr) {
		t.Fatalf("Run = %v, want the unwrapped work error", got)
	}
	// The record now carries the fence Run's Complete used. A second
	// Complete on the terminal record returns ErrNotClaimed.
	replayErr := l.Complete(ctx, "actor", "key", "owner", ps.fence, ledger.StatusFailed, fixedNow)
	if !errors.Is(replayErr, ledger.ErrNotClaimed) {
		t.Fatalf("second Complete = %v, want ErrNotClaimed", replayErr)
	}
}
