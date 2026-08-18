package flow_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestRetryIntegrationLinearGraphSucceedsOnThirdAttempt runs a
// three-step linear graph where the middle step's guard fails on its
// first two calls and succeeds on its third. It asserts every step's
// outcome, the final status, the final record, and the recorded sleep
// durations in order.
func TestRetryIntegrationLinearGraphSucceedsOnThirdAttempt(t *testing.T) {
	t.Parallel()
	var guardCalls int32
	var sleeps []time.Duration
	var mu sync.Mutex
	policy := &flow.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Second,
		Sleep: func(ctx context.Context, d time.Duration) {
			mu.Lock()
			sleeps = append(sleeps, d)
			mu.Unlock()
		},
	}
	d, err := flow.New([]flow.Step{
		{ID: "first", To: "s1"},
		{ID: "middle", Needs: []string{"first"}, To: "s2", Retry: policy},
		{ID: "last", Needs: []string{"middle"}, To: "s3"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("s1"), Trigger: machine.Trigger("goS1")},
		machine.Transition{From: machine.Status("s1"), To: machine.Status("s2"), Trigger: machine.Trigger("goS2"),
			Guard: func(ctx context.Context) (bool, error) {
				n := atomic.AddInt32(&guardCalls, 1)
				if n < 3 {
					return false, errors.New("middle boom")
				}
				return true, nil
			}},
		machine.Transition{From: machine.Status("s2"), To: machine.Status("s3"), Trigger: machine.Trigger("goS3")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "first", flow.OutcomeSucceeded)
	mustOutcome(t, report, "middle", flow.OutcomeSucceeded)
	mustOutcome(t, report, "last", flow.OutcomeSucceeded)
	if report.Status() != machine.Status("s3") {
		t.Fatalf("status = %q, want %q", report.Status(), "s3")
	}
	want := []time.Duration{policy.NextDelay(1), policy.NextDelay(2)}
	mu.Lock()
	got := append([]time.Duration(nil), sleeps...)
	mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sleeps[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestRetryIntegrationExhaustedFallbackWrapsLastError runs the same
// linear graph with a guard that never succeeds, and a fallback step
// that catches the exhausted retry. It asserts the fallback's
// Failure.Err wraps the last guard error.
func TestRetryIntegrationExhaustedFallbackWrapsLastError(t *testing.T) {
	t.Parallel()
	middleErr := errors.New("middle always boom")
	var gotErr error
	policy := &flow.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Second,
		Sleep:       func(context.Context, time.Duration) {},
	}
	d, err := flow.New([]flow.Step{
		{ID: "first", To: "s1"},
		{ID: "middle", Needs: []string{"first"}, To: "s2", Retry: policy},
		{ID: "fallback", Needs: []string{"middle"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("s1"), Trigger: machine.Trigger("goS1")},
		machine.Transition{From: machine.Status("s1"), To: machine.Status("s2"), Trigger: machine.Trigger("goS2"),
			Guard: rejectingGuard(middleErr)},
		machine.Transition{From: machine.Status("s1"), To: machine.Status("f"), Trigger: machine.Trigger("goF"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				fail, _ := flow.FailureFrom(ctx)
				gotErr = fail.Err
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "middle", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	if !errors.Is(gotErr, middleErr) {
		t.Fatalf("Failure.Err = %v, does not wrap %v", gotErr, middleErr)
	}
}
