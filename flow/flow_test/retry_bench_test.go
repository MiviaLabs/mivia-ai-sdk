package flow_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// benchRetrySingleStep builds a one-step Definition whose guard
// always succeeds on the first attempt, with retry set when retry is
// non-nil.
func benchRetrySingleStep(tb testing.TB, retry *flow.RetryPolicy) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone), Retry: retry},
	}, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart, machine.Transition{
		From: statusStart, To: statusDone, Trigger: triggerGo,
	})
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// BenchmarkRunRetryNil measures Run on a one-step graph with Retry
// nil: the phase 23 baseline this phase must not regress.
//
// Measured baseline (AMD Ryzen 9 9900X, go test -bench): 133.0 ns/op,
// 368 B/op, 4 allocs/op.
func BenchmarkRunRetryNil(b *testing.B) {
	d, m := benchRetrySingleStep(b, nil)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// BenchmarkRunRetryPresentNeverTriggered measures Run on the same
// one-step graph with a RetryPolicy present but never triggered: the
// guard always succeeds on the first attempt, so the loop's guard
// overhead is the only delta from BenchmarkRunRetryNil, not backoff
// timing.
//
// Measured (AMD Ryzen 9 9900X, go test -bench): 134.3 ns/op, 368
// B/op, 4 allocs/op: within noise of the Retry-nil baseline above.
func BenchmarkRunRetryPresentNeverTriggered(b *testing.B) {
	retry := &flow.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	d, m := benchRetrySingleStep(b, retry)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// TestRunRetryPresentAllocBudget asserts the allocation budget for
// Run on the one-step graph with a never-triggered RetryPolicy: no
// more than 6 allocations, a margin over the measured 4-alloc
// baseline in BenchmarkRunRetryPresentNeverTriggered.
func TestRunRetryPresentAllocBudget(t *testing.T) {
	retry := &flow.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	d, m := benchRetrySingleStep(t, retry)
	ctx := context.Background()
	const budget = 6
	got := testing.AllocsPerRun(10, func() {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if got > budget {
		t.Fatalf("AllocsPerRun = %v, want at most %d", got, budget)
	}
}
