package taskrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestWorkErrorUnwrapped proves a failed work is returned unwrapped
// and the record completes StatusFailed, confirmable by State.
func TestWorkErrorUnwrapped(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workErr := errors.New("build exploded")
	task := taskrun.Task{Key: "key", Seq: 1}
	got := taskrun.Run(ctx, runOpts(l), task, func(context.Context) error { return workErr })
	if !errors.Is(got, workErr) {
		t.Fatalf("Run = %v, want the work error", got)
	}
	st, found, err := l.State(ctx, "key")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !found || st.Status != ledger.StatusFailed {
		t.Fatalf("Status = %q (found %v), want StatusFailed", st.Status, found)
	}
}
