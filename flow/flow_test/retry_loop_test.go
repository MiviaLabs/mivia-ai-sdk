package flow_test

// This file holds the retry loop's Run-level cases: hook
// re-invocation, attempt exhaustion, Retryable and MaxAttempts
// isolation, Confirm and Route exclusion, and context cancellation.
// The Validate, NextDelay, and New-validation cases live in
// retry_test.go, to keep each file at or below the 500-line
// structure cap.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// countingCloser tracks call counts across Guard, OnExit, and OnEntry
// closures for the retry loop's hook-tolerance tests.
type countingCloser struct {
	guard   int32
	onExit  int32
	onEntry int32
}

// TestRetrySucceedsOnThirdAttemptReinvokesEveryHook proves a step
// whose transition fails on its first two attempts and succeeds on
// the third ends OutcomeSucceeded, with Guard, OnExit, and OnEntry
// each invoked exactly three times: once per attempt.
func TestRetrySucceedsOnThirdAttemptReinvokesEveryHook(t *testing.T) {
	t.Parallel()
	var c countingCloser
	var sleeps int32
	riskyErr := errors.New("entry boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Sleep: func(ctx context.Context, dur time.Duration) {
				atomic.AddInt32(&sleeps, 1)
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt32(&c.guard, 1)
				return true, nil
			},
			OnExit: func(ctx context.Context, rec *machine.InOut) error {
				atomic.AddInt32(&c.onExit, 1)
				return nil
			},
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				n := atomic.AddInt32(&c.onEntry, 1)
				if n < 3 {
					return riskyErr
				}
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "risky", flow.OutcomeSucceeded)
	if c.guard != 3 {
		t.Fatalf("guard calls = %d, want 3", c.guard)
	}
	if c.onExit != 3 {
		t.Fatalf("onExit calls = %d, want 3", c.onExit)
	}
	if c.onEntry != 3 {
		t.Fatalf("onEntry calls = %d, want 3", c.onEntry)
	}
	if sleeps != 2 {
		t.Fatalf("Sleep calls = %d, want 2", sleeps)
	}
}

// TestRetryExhaustsAttemptsFailsAfterMax proves a step whose guard
// always fails, under MaxAttempts 3, ends OutcomeFailed after exactly
// three guard calls and aborts the run with the last attempt's error.
func TestRetryExhaustsAttemptsFailsAfterMax(t *testing.T) {
	t.Parallel()
	var guardCalls int32
	riskyErr := errors.New("always boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Sleep:       func(context.Context, time.Duration) {},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt32(&guardCalls, 1)
				return false, riskyErr
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, riskyErr) {
		t.Fatalf("error does not wrap the last attempt's error: %v", err)
	}
	if guardCalls != 3 {
		t.Fatalf("guard calls = %d, want 3", guardCalls)
	}
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
}

// TestRetryableFalseStopsAfterFirstFailure proves a Retryable
// predicate that returns false stops the loop after the first
// failure, even when MaxAttempts allows more.
func TestRetryableFalseStopsAfterFirstFailure(t *testing.T) {
	t.Parallel()
	var guardCalls int32
	riskyErr := errors.New("unretryable boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 5,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Retryable:   func(error) bool { return false },
			Sleep:       func(context.Context, time.Duration) {},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt32(&guardCalls, 1)
				return false, riskyErr
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if guardCalls != 1 {
		t.Fatalf("guard calls = %d, want 1", guardCalls)
	}
}

// TestRetryNilKeepsSingleAttemptBehavior proves a step with Retry nil
// keeps today's single-attempt behavior: one guard call, one failure,
// one abort.
func TestRetryNilKeepsSingleAttemptBehavior(t *testing.T) {
	t.Parallel()
	var guardCalls int32
	riskyErr := errors.New("boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt32(&guardCalls, 1)
				return false, riskyErr
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if guardCalls != 1 {
		t.Fatalf("guard calls = %d, want 1", guardCalls)
	}
}

// TestRetryMaxAttemptsOneIsolatesFromNilRetry proves a non-nil Retry
// with MaxAttempts 1 produces the same outcome as a nil Retry, for a
// different reason: the loop runs zero extra iterations and never
// calls Sleep or Retryable.
func TestRetryMaxAttemptsOneIsolatesFromNilRetry(t *testing.T) {
	t.Parallel()
	var guardCalls, sleepCalls, retryableCalls int32
	riskyErr := errors.New("boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 1,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Retryable: func(error) bool {
				atomic.AddInt32(&retryableCalls, 1)
				return true
			},
			Sleep: func(context.Context, time.Duration) {
				atomic.AddInt32(&sleepCalls, 1)
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt32(&guardCalls, 1)
				return false, riskyErr
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if guardCalls != 1 {
		t.Fatalf("guard calls = %d, want 1", guardCalls)
	}
	if sleepCalls != 0 {
		t.Fatalf("Sleep calls = %d, want 0", sleepCalls)
	}
	if retryableCalls != 0 {
		t.Fatalf("Retryable calls = %d, want 0", retryableCalls)
	}
}

// TestRetryNeverWrapsConfirm proves a retried step whose Confirm
// always rejects fires its guard exactly once: fireWithRetry wraps
// only fireStep, and Run calls Confirm after fireWithRetry returns,
// outside the retry loop.
func TestRetryNeverWrapsConfirm(t *testing.T) {
	t.Parallel()
	var guardCalls int32
	confirmErr := errors.New("confirm rejected")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Sleep:       func(context.Context, time.Duration) {},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt32(&guardCalls, 1)
				return true, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	confirm := func(ctx context.Context, step flow.Step) error { return confirmErr }
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, confirmErr) {
		t.Fatalf("error does not wrap the confirm error: %v", err)
	}
	if guardCalls != 1 {
		t.Fatalf("guard calls = %d, want 1", guardCalls)
	}
}

// TestRetryNeverWrapsRoute proves a retried branch step whose Route
// always errors runs Route exactly once: Route runs in the runner,
// after the wave, never inside fireWithRetry.
func TestRetryNeverWrapsRoute(t *testing.T) {
	t.Parallel()
	var routeCalls int32
	routeErr := errors.New("route boom")
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Retry: &flow.RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Sleep:       func(context.Context, time.Duration) {},
		}, Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
			atomic.AddInt32(&routeCalls, 1)
			return nil, routeErr
		}},
		{ID: "after", Needs: []string{"branch"}, To: "a"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("b"), To: machine.Status("a"), Trigger: machine.Trigger("goA")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, routeErr) {
		t.Fatalf("error does not wrap the route error: %v", err)
	}
	if routeCalls != 1 {
		t.Fatalf("Route calls = %d, want 1", routeCalls)
	}
}

// TestRetryCanceledCtxStopsLoopShortOfMaxAttempts proves a ctx
// canceled mid-loop, by a Sleep field that cancels it on its first
// call, aborts the retry loop with the context error and stops the
// loop short of MaxAttempts.
func TestRetryCanceledCtxStopsLoopShortOfMaxAttempts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var guardCalls, sleepCalls int32
	riskyErr := errors.New("boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 5,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Sleep: func(context.Context, time.Duration) {
				atomic.AddInt32(&sleepCalls, 1)
				cancel()
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt32(&guardCalls, 1)
				return false, riskyErr
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	if guardCalls >= 3 {
		t.Fatalf("guard calls = %d, want fewer than 3", guardCalls)
	}
	if sleepCalls != 1 {
		t.Fatalf("Sleep calls = %d, want 1", sleepCalls)
	}
}

// TestDefaultSleepReturnsEarlyOnCtxCancellation proves the default
// Sleep (nil field) returns before its full duration when its ctx is
// canceled mid-wait. A signal channel, not time.Sleep, orders the
// cancellation: the canceling goroutine waits for the guard's first
// call, so it fires only once the default Sleep has started its
// select on an hour-long timer.
func TestDefaultSleepReturnsEarlyOnCtxCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	guardCalled := make(chan struct{}, 1)
	go func() {
		<-guardCalled
		cancel()
	}()
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 5,
			BaseDelay:   time.Hour,
			MaxDelay:    time.Hour,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				guardCalled <- struct{}{}
				return false, errors.New("boom")
			},
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	start := time.Now()
	_, err = flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if elapsed >= time.Hour {
		t.Fatalf("Run took %v, expected an early return well under the full backoff", elapsed)
	}
	if elapsed > time.Second {
		t.Fatalf("Run took %v, expected a return near the cancellation time", elapsed)
	}
}

// TestRetryExhaustedFallbackReceivesLastError proves a step that
// exhausts its retries, with a phase 23 fallback declared, continues
// the run down the fallback path, and FailureFrom returns the last
// attempt's error.
func TestRetryExhaustedFallbackReceivesLastError(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("exhausted boom")
	var gotErr error
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r", Retry: &flow.RetryPolicy{
			MaxAttempts: 2,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Second,
			Sleep:       func(context.Context, time.Duration) {},
		}},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF"),
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
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	if !errors.Is(gotErr, riskyErr) {
		t.Fatalf("Failure.Err = %v, does not wrap %v", gotErr, riskyErr)
	}
}
