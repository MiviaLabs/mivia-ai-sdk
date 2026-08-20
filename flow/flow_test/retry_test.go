package flow_test

// Red step: before phase 30 landed, flow.RetryPolicy and Step.Retry
// did not exist. This file did not compile: `go build ./flow/...`
// failed with "undefined: flow.RetryPolicy". RetryPolicy, Validate,
// NextDelay, and fireWithRetry landed in flow/retry.go; the cases
// below then passed.
//
// The retry loop's Run-level cases live in retry_loop_test.go, to
// keep this file at or below the 500-line structure cap.

import (
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// TestRetryPolicyValidateRejectsZeroMaxAttempts pins the exact
// message for a RetryPolicy with MaxAttempts < 1.
func TestRetryPolicyValidateRejectsZeroMaxAttempts(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{MaxAttempts: 0, MaxDelay: time.Second}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "flow: retry: max attempts must be at least 1"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestRetryPolicyValidateRejectsZeroMaxDelay pins the exact message
// for a RetryPolicy with a zero MaxDelay.
func TestRetryPolicyValidateRejectsZeroMaxDelay(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{MaxAttempts: 3, MaxDelay: 0}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "flow: retry: max delay must be positive"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestRetryPolicyRejectsNegativeBaseDelay pins the exact message for
// a RetryPolicy with a negative BaseDelay.
func TestRetryPolicyRejectsNegativeBaseDelay(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{MaxAttempts: 3, BaseDelay: -time.Second, MaxDelay: time.Second}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "flow: retry: base delay must not be negative"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestRetryPolicyAcceptsZeroBaseDelay proves the negative rule does
// not reject a zero BaseDelay, which means an immediate retry.
func TestRetryPolicyAcceptsZeroBaseDelay(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{MaxAttempts: 3, BaseDelay: 0, MaxDelay: time.Second}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if got := p.NextDelay(4); got != 0 {
		t.Fatalf("NextDelay(4) = %v, want 0", got)
	}
}

// TestRetryPolicyValidateAcceptsValidPolicy proves Validate returns
// nil for a policy that meets both rules.
func TestRetryPolicyValidateAcceptsValidPolicy(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestNextDelayDoublesAndClamps proves NextDelay returns BaseDelay
// for attempt 1, doubles for later attempts, and clamps at MaxDelay.
func TestNextDelayDoublesAndClamps(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{
		MaxAttempts: 10,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 10 * time.Millisecond},
		{2, 20 * time.Millisecond},
		{3, 40 * time.Millisecond},
		{4, 80 * time.Millisecond},
		{5, 100 * time.Millisecond},
		{6, 100 * time.Millisecond},
	}
	for _, tt := range cases {
		if got := p.NextDelay(tt.attempt); got != tt.want {
			t.Errorf("NextDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

// TestNextDelayClampsBaseDelayAboveMaxDelay proves the post-loop
// clamp bounds a BaseDelay that already exceeds MaxDelay, even on the
// first attempt where the doubling loop never runs.
func TestNextDelayClampsBaseDelayAboveMaxDelay(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   2 * time.Second,
		MaxDelay:    time.Second,
	}
	if got := p.NextDelay(1); got != time.Second {
		t.Fatalf("NextDelay(1) = %v, want %v", got, time.Second)
	}
}

// TestNextDelayJitterAppliesToClampedResult proves a non-nil Jitter
// perturbs the clamped result, and a nil Jitter leaves it unchanged.
func TestNextDelayJitterAppliesToClampedResult(t *testing.T) {
	t.Parallel()
	base := flow.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    time.Second,
	}
	if got := base.NextDelay(2); got != 20*time.Millisecond {
		t.Fatalf("nil Jitter: NextDelay(2) = %v, want %v", got, 20*time.Millisecond)
	}
	withJitter := base
	withJitter.Jitter = func(d time.Duration) time.Duration { return d + 5*time.Millisecond }
	if got := withJitter.NextDelay(2); got != 25*time.Millisecond {
		t.Fatalf("Jitter: NextDelay(2) = %v, want %v", got, 25*time.Millisecond)
	}
}

// TestNextDelayJitterNotReClamped proves a Jitter closure that
// returns a value above MaxDelay passes through unclamped.
func TestNextDelayJitterNotReClamped(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    time.Second,
		Jitter:      func(time.Duration) time.Duration { return 10 * time.Second },
	}
	if got := p.NextDelay(3); got != 10*time.Second {
		t.Fatalf("NextDelay(3) = %v, want %v (unclamped Jitter output)", got, 10*time.Second)
	}
}

// TestNextDelayNeverOverflows is the red-green overflow proof: a
// naive BaseDelay*2^(attempt-1) computation for this input overflows
// int64 and wraps to an unpredictable value long before attempt 50.
// The clamp-before-double order in NextDelay never performs that
// multiply and returns MaxDelay exactly.
func TestNextDelayNeverOverflows(t *testing.T) {
	t.Parallel()
	p := flow.RetryPolicy{
		MaxAttempts: 100,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Hour,
	}
	got := p.NextDelay(50)
	if got != time.Hour {
		t.Fatalf("NextDelay(50) = %v, want %v (no overflow, no negative wrap)", got, time.Hour)
	}
	if got < 0 {
		t.Fatalf("NextDelay(50) wrapped negative: %v", got)
	}
}

// TestNextDelayAtMaxAttemptsStaysBounded proves NextDelay called with
// attempt equal to MaxAttempts stays at or below MaxDelay for a table
// of BaseDelay/MaxDelay pairs.
func TestNextDelayAtMaxAttemptsStaysBounded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		maxAttempts int
		baseDelay   time.Duration
		maxDelay    time.Duration
	}{
		{"base one nanosecond", 20, time.Nanosecond, time.Minute},
		{"base equals max", 20, time.Minute, time.Minute},
		{"base one millisecond, tight max", 40, time.Millisecond, 500 * time.Millisecond},
		{"base one second, large max", 60, time.Second, 24 * time.Hour},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := flow.RetryPolicy{
				MaxAttempts: tt.maxAttempts,
				BaseDelay:   tt.baseDelay,
				MaxDelay:    tt.maxDelay,
			}
			got := p.NextDelay(tt.maxAttempts)
			if got < 0 || got > tt.maxDelay {
				t.Fatalf("NextDelay(%d) = %v, want in [0, %v]", tt.maxAttempts, got, tt.maxDelay)
			}
		})
	}
}

// TestNewRejectsStepRetryWithZeroMaxAttempts pins the exact message
// New returns when a step's RetryPolicy has MaxAttempts < 1, proving
// New enforces RetryPolicy.Validate's rule itself, not only when a
// caller calls Validate directly.
func TestNewRejectsStepRetryWithZeroMaxAttempts(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "done", Retry: &flow.RetryPolicy{MaxAttempts: 0, MaxDelay: time.Second}},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "a" retry: max attempts must be at least 1`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsStepRetryWithZeroMaxDelay pins the exact message New
// returns when a step's RetryPolicy has a zero MaxDelay, proving New
// enforces RetryPolicy.Validate's rule itself, not only when a caller
// calls Validate directly.
func TestNewRejectsStepRetryWithZeroMaxDelay(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "done", Retry: &flow.RetryPolicy{MaxAttempts: 3, MaxDelay: 0}},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "a" retry: max delay must be positive`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsStepRetryWithNegativeBaseDelay pins the exact message
// New returns when a step's RetryPolicy has a negative BaseDelay,
// proving New enforces the rule itself, not only when a caller calls
// Validate directly.
func TestNewRejectsStepRetryWithNegativeBaseDelay(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "done", Retry: &flow.RetryPolicy{
			MaxAttempts: 3, BaseDelay: -time.Second, MaxDelay: time.Second,
		}},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "a" retry: base delay must not be negative`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsRetryWithSub pins the exact message for a Retry
// policy on a step with a non-nil Sub.
func TestNewRejectsRetryWithSub(t *testing.T) {
	t.Parallel()
	sub, err := flow.New([]flow.Step{{ID: "inner"}}, nil)
	if err != nil {
		t.Fatalf("flow.New(sub): %v", err)
	}
	_, err = flow.New([]flow.Step{
		{ID: "chained", Sub: sub, Retry: &flow.RetryPolicy{MaxAttempts: 3, MaxDelay: time.Second}},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "chained" has a retry policy but a sub-workflow`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsRetryOnPanelMember pins the exact message for a Retry
// policy declared on a panel member.
func TestNewRejectsRetryOnPanelMember(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "done", Retry: &flow.RetryPolicy{MaxAttempts: 3, MaxDelay: time.Second}},
		{ID: "b", To: "done"},
	}, []flow.Panel{{"a", "b"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names retried step "a"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewAcceptsRetriedStepNoSubNoPanel proves a retried singleton
// step with no Sub and no panel builds without error.
func TestNewAcceptsRetriedStepNoSubNoPanel(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: "done", Retry: &flow.RetryPolicy{MaxAttempts: 3, MaxDelay: time.Second}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}

// TestRetryValidateErrorSubstrings is a smoke check that the error
// substrings the pinned messages use stay unique across the two
// RetryPolicy validation rules.
func TestRetryValidateErrorSubstrings(t *testing.T) {
	t.Parallel()
	attemptsErr := flow.RetryPolicy{MaxAttempts: 0, MaxDelay: time.Second}.Validate()
	delayErr := flow.RetryPolicy{MaxAttempts: 1, MaxDelay: 0}.Validate()
	if strings.Contains(attemptsErr.Error(), "max delay") {
		t.Fatalf("attempts error %q unexpectedly mentions max delay", attemptsErr.Error())
	}
	if strings.Contains(delayErr.Error(), "max attempts") {
		t.Fatalf("delay error %q unexpectedly mentions max attempts", delayErr.Error())
	}
}
