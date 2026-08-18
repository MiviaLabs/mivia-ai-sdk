package flow_test

// Integration case for flow.Step.PayloadFrom, run end to end through
// flow.Run with a real machine. One chain of three steps threads each
// transition's output into the next step's PayloadFrom, so the final
// record and every resolved payload are observable.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestPayloadIntegrationChainThreadsOutput builds a three-step chain
// where every transition appends a marker to the record's Output and
// every step's PayloadFrom reads it. It asserts the final record, the
// final status, and each step's resolved payload.
func TestPayloadIntegrationChainThreadsOutput(t *testing.T) {
	t.Parallel()
	const (
		statusMid1 = machine.Status("mid1")
		statusMid2 = machine.Status("mid2")
	)
	appendOutput := func(suffix string) func(context.Context, *machine.InOut) error {
		return func(_ context.Context, rec *machine.InOut) error {
			prev, _ := rec.Output.(string)
			rec.Output = prev + suffix
			return nil
		}
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid1, Trigger: triggerGo,
			OnEntry: func(_ context.Context, rec *machine.InOut) error {
				rec.Output = "pA"
				return nil
			}},
		machine.Transition{From: statusMid1, To: statusMid2, Trigger: triggerGo, OnEntry: appendOutput("-pB")},
		machine.Transition{From: statusMid2, To: statusDone, Trigger: triggerGo, OnEntry: appendOutput("-pC")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusMid1), PayloadFrom: outputReader},
		{ID: "b", Needs: []string{"a"}, To: string(statusMid2), PayloadFrom: outputReader},
		{ID: "c", Needs: []string{"b"}, To: string(statusDone), PayloadFrom: outputReader},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	got := map[string]string{}
	confirm := func(ctx context.Context, step flow.Step) error {
		got[step.ID] = step.Payload
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "a", flow.OutcomeSucceeded)
	mustOutcome(t, report, "b", flow.OutcomeSucceeded)
	mustOutcome(t, report, "c", flow.OutcomeSucceeded)
	if report.Status() != statusDone {
		t.Fatalf("final status = %q, want %q", report.Status(), statusDone)
	}
	if report.Record().Output != "pA-pB-pC" {
		t.Fatalf("final record = %v, want %q", report.Record().Output, "pA-pB-pC")
	}
	want := map[string]string{
		"a": "pA",
		"b": "pA-pB",
		"c": "pA-pB-pC",
	}
	for id, wantPayload := range want {
		if got[id] != wantPayload {
			t.Fatalf("step %q Payload = %q, want %q", id, got[id], wantPayload)
		}
	}
}
