package taskrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestAdmitErrorPropagates proves a ledger Admit failure on its
// CompareAndSwap returns the store error, not a taskrun sentinel.
func TestAdmitErrorPropagates(t *testing.T) {
	ctx := context.Background()
	ps := &probeStore{Store: ledger.NewMemStore(), fail: 1}
	l, err := ledger.New(ps, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = taskrun.Run(ctx, runOpts(l), taskrun.Task{Key: "key", Seq: 1}, func(context.Context) error {
		return nil
	})
	if !errors.Is(err, errProbe) {
		t.Fatalf("Run = %v, want the store error", err)
	}
}

// TestStateErrorPropagates proves a ledger State failure after Admit
// returns the store error and never claims or runs work.
func TestStateErrorPropagates(t *testing.T) {
	ctx := context.Background()
	// Admit performs the first Load; Run's State does the second.
	// failLoad=2 fails State's Load, after the record is admitted.
	ps := &probeStore{Store: ledger.NewMemStore(), failLoad: 2}
	l, err := ledger.New(ps, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	called := 0
	err = taskrun.Run(ctx, runOpts(l), taskrun.Task{Key: "key", Seq: 1}, func(context.Context) error {
		called++
		return nil
	})
	if !errors.Is(err, errProbe) {
		t.Fatalf("Run = %v, want the store error", err)
	}
	if called != 0 {
		t.Fatalf("work ran after a State failure (%d calls)", called)
	}
}
