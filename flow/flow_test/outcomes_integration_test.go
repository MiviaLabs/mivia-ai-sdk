package flow_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// errMixedWaveMember is the second member's Fire failure in
// TestReportPanelMixedResultLeavesNeitherMemberResolved.
var errMixedWaveMember = errors.New("second member failed")

// TestReportDiamondGraphOutcomesAndTieBreak reruns the phase 5 diamond
// graph through the Report API. It asserts the outcome of every step
// and that the declaration-order tie-break still holds.
func TestReportDiamondGraphOutcomesAndTieBreak(t *testing.T) {
	t.Parallel()
	const (
		root = machine.Status("root")
		mid1 = machine.Status("mid1")
		mid2 = machine.Status("mid2")
		join = machine.Status("join")
	)
	d, err := flow.New([]flow.Step{
		{ID: "root", To: string(root)},
		{ID: "mid-first", Needs: []string{"root"}, To: string(mid1)},
		{ID: "mid-second", Needs: []string{"root"}, To: string(mid2)},
		{ID: "join", Needs: []string{"mid-first", "mid-second"}, To: string(join)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: root, Trigger: triggerGo},
		machine.Transition{From: root, To: mid1, Trigger: triggerGo},
		machine.Transition{From: mid1, To: mid2, Trigger: triggerGo},
		machine.Transition{From: mid2, To: join, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var order []string
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, recordingConfirm(&order), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"root", "mid-first", "mid-second", "join"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	for _, id := range want {
		outcome, ok := report.Outcome(id)
		if !ok || outcome != flow.OutcomeSucceeded {
			t.Fatalf("Outcome(%q) = %v, %v, want %v, true", id, outcome, ok, flow.OutcomeSucceeded)
		}
	}
	if report.Status() != join {
		t.Fatalf("Status() = %q, want %q", report.Status(), join)
	}
}

// TestReportPanelMixedResultLeavesNeitherMemberResolved runs a panel
// with one failing member and one succeeding member. It asserts the
// wave error aborts the run and that Outcomes holds no entry for
// either member.
func TestReportPanelMixedResultLeavesNeitherMemberResolved(t *testing.T) {
	t.Parallel()
	const target = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(target)},
		{ID: "b", To: string(target)},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var calls int64
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: target, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				// The first Fire call to reach here succeeds; the second
				// fails. Which declared member wins the race is not
				// asserted; only that the wave ends mixed.
				if atomic.AddInt64(&calls, 1) == 1 {
					return nil
				}
				return errMixedWaveMember
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := report.Outcome("a"); ok {
		t.Fatal("Outcome(a) resolved true, want false: a mixed wave marks no member")
	}
	if _, ok := report.Outcome("b"); ok {
		t.Fatal("Outcome(b) resolved true, want false: a mixed wave marks no member")
	}
	outcomes := report.Outcomes()
	if len(outcomes) != 0 {
		t.Fatalf("Outcomes() = %v, want empty", outcomes)
	}
}
