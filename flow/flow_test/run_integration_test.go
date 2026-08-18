package flow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// recordingConfirm returns a Confirm that appends the step ID to
// order and never fails.
func recordingConfirm(order *[]string) flow.Confirm {
	return func(ctx context.Context, step flow.Step) error {
		*order = append(*order, step.ID)
		return nil
	}
}

// TestRunLinearThreeSteps proves the order rule and record threading
// on a linear three-step graph.
func TestRunLinearThreeSteps(t *testing.T) {
	t.Parallel()
	const (
		s1 = machine.Status("s1")
		s2 = machine.Status("s2")
		s3 = machine.Status("s3")
	)
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(s1)},
		{ID: "b", Needs: []string{"a"}, To: string(s2)},
		{ID: "c", Needs: []string{"b"}, To: string(s3)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: s1, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				rec.Output = "a-ran"
				return nil
			}},
		machine.Transition{From: s1, To: s2, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				rec.Output = rec.Output.(string) + ",b-ran"
				return nil
			}},
		machine.Transition{From: s2, To: s3, Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				rec.Output = rec.Output.(string) + ",c-ran"
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var order []string
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, recordingConfirm(&order), nil)
	status := report.Status()
	out := report.Record()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != s3 {
		t.Fatalf("status = %q, want %q", status, s3)
	}
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if order[i] != id {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if out.Output != "a-ran,b-ran,c-ran" {
		t.Fatalf("out.Output = %v, want threaded record", out.Output)
	}
}

// TestRunDiamondTieBreak proves the declaration-order tie-break: the
// mid step declared first runs first when both mid steps are ready
// at the same time.
func TestRunDiamondTieBreak(t *testing.T) {
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
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, recordingConfirm(&order), nil)
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
}

// TestRunReadyBeforeDeclared proves nextReady picks the first ready
// step, not the first declared step. b needs root; a needs x. root
// resolves before x, so b becomes ready before a, even though a is
// declared first.
func TestRunReadyBeforeDeclared(t *testing.T) {
	t.Parallel()
	const (
		sRoot = machine.Status("root")
		sB    = machine.Status("b-done")
		sX    = machine.Status("x-done")
		sA    = machine.Status("a-done")
	)
	d, err := flow.New([]flow.Step{
		{ID: "a", Needs: []string{"x"}, To: string(sA)},
		{ID: "b", Needs: []string{"root"}, To: string(sB)},
		{ID: "root", To: string(sRoot)},
		{ID: "x", Needs: []string{"root"}, To: string(sX)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: sRoot, Trigger: triggerGo},
		machine.Transition{From: sRoot, To: sB, Trigger: triggerGo},
		machine.Transition{From: sB, To: sX, Trigger: triggerGo},
		machine.Transition{From: sX, To: sA, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var order []string
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, recordingConfirm(&order), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"root", "b", "x", "a"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestRunGateFailureStopsRun proves a guard rejection stops the run
// before the failing step's ack, and the ack never fires afterward.
func TestRunGateFailureStopsRun(t *testing.T) {
	t.Parallel()
	const s1 = machine.Status("s1")
	d, err := flow.New([]flow.Step{{ID: "a", To: string(s1)}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart, machine.Transition{
		From: statusStart, To: s1, Trigger: triggerGo,
		Guard: func(ctx context.Context) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	confirmed := false
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmed = true
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if confirmed {
		t.Fatal("confirm ran after a guard rejection")
	}
	if status != statusStart {
		t.Fatalf("status = %q, want the unchanged initial status %q", status, statusStart)
	}
}

// TestRunUnconfirmedAckBlocksNext proves an unconfirmed ack stops the
// walk before the following step fires.
func TestRunUnconfirmedAckBlocksNext(t *testing.T) {
	t.Parallel()
	const (
		s1 = machine.Status("s1")
		s2 = machine.Status("s2")
	)
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(s1)},
		{ID: "b", Needs: []string{"a"}, To: string(s2)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: s1, Trigger: triggerGo},
		machine.Transition{From: s1, To: s2, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	ackErr := errors.New("ack rejected")
	confirm := func(ctx context.Context, step flow.Step) error {
		if step.ID == "a" {
			return ackErr
		}
		t.Fatalf("step %q fired after an unconfirmed ack", step.ID)
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ackErr) {
		t.Fatalf("error %v does not wrap the confirm error", err)
	}
	if status != s1 {
		t.Fatalf("status = %q, want %q", status, s1)
	}
}
