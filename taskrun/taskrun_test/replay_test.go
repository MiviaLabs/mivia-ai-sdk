package taskrun_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestReplaySentinels proves a key already completed or failed in the
// ledger returns its sentinel without running work.
func TestReplaySentinels(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	finish(t, l, ctx, "done", ledger.StatusCompleted)
	finish(t, l, ctx, "broke", ledger.StatusFailed)

	tests := []struct {
		name string
		key  ledger.IdempotencyKey
		want error
	}{
		{name: "completed", key: "done", want: taskrun.ErrTaskDone},
		{name: "failed", key: "broke", want: taskrun.ErrTaskFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			err := taskrun.Run(ctx, runOpts(l), taskrun.Task{Key: tc.key, Seq: 2}, func(context.Context) error {
				called++
				return nil
			})
			if err != tc.want {
				t.Fatalf("Run = %v, want %v", err, tc.want)
			}
			if called != 0 {
				t.Fatalf("work ran on a terminal key (%d calls)", called)
			}
		})
	}
}

// finish admits, claims, and completes key as status at fixedNow.
func finish(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, status machine.Status) {
	t.Helper()
	if _, err := l.Admit(ctx, "actor", key, 1, "boot", fixedNow); err != nil {
		t.Fatalf("Admit %s: %v", key, err)
	}
	fence, err := l.Claim(ctx, "actor", key, "owner", fixedLease, fixedNow)
	if err != nil {
		t.Fatalf("Claim %s: %v", key, err)
	}
	if err := l.Complete(ctx, "actor", key, "owner", fence, status, fixedNow); err != nil {
		t.Fatalf("Complete %s: %v", key, err)
	}
}
