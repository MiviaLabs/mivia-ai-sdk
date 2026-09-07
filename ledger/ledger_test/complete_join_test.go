package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestCompleteFailureJoins proves a Complete that fails after a failed
// work joins the returned error, and errors.Is matches the work error.
func TestCompleteFailureJoins(t *testing.T) {
	ctx := context.Background()
	// Admit, Claim, then Complete each take one CompareAndSwap, so
	// failing the third forces Run's Complete call to error.
	ps := &probeStore{Store: ledger.NewMemStore(), fail: 3}
	l, err := ledger.New(ps, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workErr := errors.New("work broke")
	task := ledger.Task{Key: "key", Seq: 1}
	got := ledger.Run(ctx, runOpts(l), task, func(context.Context) error { return workErr })
	if !errors.Is(got, workErr) {
		t.Fatalf("Run = %v, want the work error to lead", got)
	}
	if !errors.Is(got, errProbe) {
		t.Fatalf("Run = %v, want the Complete error joined", got)
	}
}
