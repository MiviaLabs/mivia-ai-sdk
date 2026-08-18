// Package agent_test also holds the context-budget cases for Run: a
// nil budget, a generous budget, an invalid budget, a MaxEvents cap
// that trips mid-plan, a cumulative MaxBytes cap that trips on the
// second of two steps, a MaxBytes cap the very first step alone
// exceeds, and the Fits-before-hb.Beat ordering proof.
package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestRunNilBudgetUnchanged proves a nil budget reproduces today's
// unbounded behavior exactly: same status, same nil error.
func TestRunNilBudgetUnchanged(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}
}

// TestRunGenerousBudgetSucceeds proves a valid budget with caps well
// above the plan's payload size and step count succeeds identically
// to the nil case.
func TestRunGenerousBudgetSucceeds(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	budget := &contextbudget.Limits{MaxBytes: 1_000_000, MaxEvents: 1_000}
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", budget)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}
}

// TestRunInvalidBudgetSurfacesValidateError proves an invalid budget
// (a negative MaxBytes) returns machine.Status(""), in unchanged, and
// an error containing "MaxBytes", asserted before wait is ever
// called.
func TestRunInvalidBudgetSurfacesValidateError(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	calls := 0
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		calls++
		return confirmingWait(ctx, msg)
	}
	in := machine.InOut{Input: "keep me"}
	budget := &contextbudget.Limits{MaxBytes: -1}
	status, rec, err := a.Run(context.Background(), "thread-1", m, in, wait, bus, nil, "", budget)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for an invalid budget")
	}
	if !strings.Contains(err.Error(), "MaxBytes") {
		t.Fatalf("Run() error = %q, want it to contain %q", err.Error(), "MaxBytes")
	}
	if status != machine.Status("") {
		t.Fatalf("Run() status = %q, want empty", status)
	}
	if rec != in {
		t.Fatalf("Run() record = %+v, want unchanged %+v", rec, in)
	}
	if calls != 0 {
		t.Fatalf("wait called %d times, want 0: Validate must fail before wait runs", calls)
	}
}

// TestRunMaxEventsExceededMidPlan proves a MaxEvents cap smaller than
// the plan's step count returns ErrOverBudget on the step that
// exceeds it, and the bus's recorded MessageAckedEvent count equals
// the number of steps built before the failing one; no
// ThreadVerifiedEvent fires.
func TestRunMaxEventsExceededMidPlan(t *testing.T) {
	a, m := twoStepFixture(t)
	bus := events.New()
	delivered, acked, threadVerified := 0, 0, 0
	if err := bus.Subscribe(agent.MessageDeliveredEvent, func(ctx context.Context, e events.Event) error {
		delivered++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(MessageDeliveredEvent) unexpected error: %v", err)
	}
	if err := bus.Subscribe(agent.MessageAckedEvent, func(ctx context.Context, e events.Event) error {
		acked++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(MessageAckedEvent) unexpected error: %v", err)
	}
	if err := bus.Subscribe(agent.ThreadVerifiedEvent, func(ctx context.Context, e events.Event) error {
		threadVerified++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(ThreadVerifiedEvent) unexpected error: %v", err)
	}

	budget := &contextbudget.Limits{MaxEvents: 1}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", budget)
	if !errors.Is(err, agent.ErrOverBudget) {
		t.Fatalf("Run() error = %v, want errors.Is match for ErrOverBudget", err)
	}
	if delivered != 2 {
		t.Fatalf("MessageDeliveredEvent fired %d times, want 2: the failing step still delivers", delivered)
	}
	if acked != 1 {
		t.Fatalf("MessageAckedEvent fired %d times, want 1: only the step before the failing one commits", acked)
	}
	if threadVerified != 0 {
		t.Fatalf("ThreadVerifiedEvent fired %d times, want 0", threadVerified)
	}
}

// TestRunMaxBytesExceededCumulative proves Fits is checked against
// runningBytes, the cumulative sum of every payload built so far plus
// the step about to run, not against the current step's payload
// alone: a MaxBytes cap no single step's payload exceeds alone, but
// that the sum of both steps' payloads does exceed, succeeds through
// step one and fails on step two.
func TestRunMaxBytesExceededCumulative(t *testing.T) {
	a, m := twoStepFixture(t)
	bus := newRunBus(t)
	// Each step's payload ("step a payload" / "step b payload") is 14
	// bytes; one step alone fits under 20, but the cumulative sum of
	// both (28) does not.
	budget := &contextbudget.Limits{MaxEvents: 1_000, MaxBytes: 20}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", budget)
	if !errors.Is(err, agent.ErrOverBudget) {
		t.Fatalf("Run() error = %v, want errors.Is match for ErrOverBudget", err)
	}
}

// TestRunMaxBytesExceededFirstStepAlone proves Fits catches a single
// step that alone exceeds the cap, not only a cumulative sum across
// steps: a MaxBytes cap set below the very first step's own payload
// size returns ErrOverBudget on step one, with zero
// MessageAckedEvent.
func TestRunMaxBytesExceededFirstStepAlone(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := events.New()
	acked := 0
	if err := bus.Subscribe(agent.MessageDeliveredEvent, func(ctx context.Context, e events.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe(MessageDeliveredEvent) unexpected error: %v", err)
	}
	if err := bus.Subscribe(agent.MessageAckedEvent, func(ctx context.Context, e events.Event) error {
		acked++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(MessageAckedEvent) unexpected error: %v", err)
	}

	// "do the thing" is 12 bytes; a cap of 5 is below it.
	budget := &contextbudget.Limits{MaxBytes: 5}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", budget)
	if !errors.Is(err, agent.ErrOverBudget) {
		t.Fatalf("Run() error = %v, want errors.Is match for ErrOverBudget", err)
	}
	if acked != 0 {
		t.Fatalf("MessageAckedEvent fired %d times, want 0: step one never commits", acked)
	}
}

// TestRunBudgetFitsFailureNeverBeats proves confirmStep checks Fits
// before hb.Beat: with a non-nil hb and a budget whose Fits check
// fails on the run's single gated step, hb.Alive for the run's
// identity-plus-thread beat id reads false after Run returns
// ErrOverBudget.
func TestRunBudgetFitsFailureNeverBeats(t *testing.T) {
	a, id, m := oneStepFixtureWithIdentity(t)
	bus := newRunBus(t)
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	wantID := id.Signer() + ":thread-1"

	// "do the thing" is 12 bytes; a cap of 1 is below it.
	budget := &contextbudget.Limits{MaxBytes: 1}
	_, _, runErr := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, hb, "", budget)
	if !errors.Is(runErr, agent.ErrOverBudget) {
		t.Fatalf("Run() error = %v, want errors.Is match for ErrOverBudget", runErr)
	}
	if hb.Alive(wantID, time.Now()) {
		t.Fatal("hb.Alive(id) = true after Run returns ErrOverBudget, want false: Fits must run before hb.Beat")
	}
}
