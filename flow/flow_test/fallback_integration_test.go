package flow_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestFallbackEndToEndGraphCompletesAndRecordsFailure runs a graph end
// to end: a step with a rejecting Guard, a fallback that records its
// Failure, and a final join. It asserts every step's Outcome, the
// final status, and that the fallback read the failed step's ID.
func TestFallbackEndToEndGraphCompletesAndRecordsFailure(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	var recordedStep string
	var recordedErr error
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "join", Needs: []string{"fallback"}, To: "j"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: func(ctx context.Context) (bool, error) { return false, riskyErr }},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				fail, ok := flow.FailureFrom(ctx)
				if !ok {
					t.Error("FailureFrom reported false inside the fallback's OnEntry")
				}
				recordedStep, recordedErr = fail.Step, fail.Err
				return nil
			}},
		machine.Transition{From: machine.Status("f"), To: machine.Status("j"), Trigger: machine.Trigger("goJ")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status() != machine.Status("j") {
		t.Fatalf("status = %q, want %q", report.Status(), "j")
	}
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	mustOutcome(t, report, "join", flow.OutcomeSucceeded)
	if recordedStep != "risky" {
		t.Fatalf("fallback read Failure.Step = %q, want %q", recordedStep, "risky")
	}
	if !errors.Is(recordedErr, riskyErr) {
		t.Fatalf("fallback read Failure.Err = %v, does not wrap %v", recordedErr, riskyErr)
	}
}

// TestFallbackConfirmRejectionAbortsEndToEnd runs the confirm-rejection
// case end to end and asserts the run aborts even though a fallback
// is declared for the rejected step.
func TestFallbackConfirmRejectionAbortsEndToEnd(t *testing.T) {
	t.Parallel()
	confirmErr := errors.New("confirm rejected")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "join", Needs: []string{"fallback"}, To: "j"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR")},
		machine.Transition{From: machine.Status("r"), To: machine.Status("f"), Trigger: machine.Trigger("goF")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	confirm := func(ctx context.Context, step flow.Step) error {
		if step.ID == "risky" {
			return confirmErr
		}
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, confirmErr) {
		t.Fatalf("error does not wrap the confirm error: %v", err)
	}
	if _, ok := report.Outcome("fallback"); ok {
		t.Fatal("\"fallback\" resolved despite the uncatchable confirm rejection")
	}
	if _, ok := report.Outcome("join"); ok {
		t.Fatal("\"join\" resolved despite the aborted run")
	}
}

// TestFallbackPanelFailureRace runs a panel failure with a fallback
// for every failed member under the race detector: run this test with
// go test -race. It proves resolvePanelFailure's outcome marking and
// pending bookkeeping are race-free against the wave's own concurrent
// goroutines.
func TestFallbackPanelFailureRace(t *testing.T) {
	t.Parallel()
	const panelTo = machine.Status("panel-done")
	waveErr := errors.New("wave boom")
	var calls int64
	d, err := flow.New([]flow.Step{
		{ID: "m1", To: string(panelTo)},
		{ID: "m2", To: string(panelTo)},
		{ID: "m3", To: string(panelTo)},
		{ID: "m4", To: string(panelTo)},
		{ID: "fb1", Needs: []string{"m1"}, When: flow.AdmissionOnFailed, To: "fb1"},
		{ID: "fb2", Needs: []string{"m2"}, When: flow.AdmissionOnFailed, To: "fb2"},
		{ID: "fb3", Needs: []string{"m3"}, When: flow.AdmissionOnFailed, To: "fb3"},
		{ID: "fb4", Needs: []string{"m4"}, When: flow.AdmissionOnFailed, To: "fb4"},
	}, []flow.Panel{{"m1", "m2", "m3", "m4"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				atomic.AddInt64(&calls, 1)
				return false, waveErr
			}},
		machine.Transition{From: statusStart, To: machine.Status("fb1"), Trigger: machine.Trigger("goFB1")},
		machine.Transition{From: machine.Status("fb1"), To: machine.Status("fb2"), Trigger: machine.Trigger("goFB2")},
		machine.Transition{From: machine.Status("fb2"), To: machine.Status("fb3"), Trigger: machine.Trigger("goFB3")},
		machine.Transition{From: machine.Status("fb3"), To: machine.Status("fb4"), Trigger: machine.Trigger("goFB4")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, id := range []string{"m1", "m2", "m3", "m4"} {
		mustOutcome(t, report, id, flow.OutcomeFailed)
	}
	for _, id := range []string{"fb1", "fb2", "fb3", "fb4"} {
		mustOutcome(t, report, id, flow.OutcomeSucceeded)
	}
	if got := atomic.LoadInt64(&calls); got != 4 {
		t.Fatalf("Guard ran %d times, want 4: one per panel member", got)
	}
}
