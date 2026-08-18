package flow_test

// Resume-specific cases split out of checkpoint_test.go to keep each
// file at or below the 500-line structure cap. See checkpoint_test.go
// for the Checkpoint, Encode/Decode, and onCheckpoint cases.

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestResumeContinuesFromMidGraphCheckpoint proves Resume seeds
// outcomes, cur, and rec from a checkpoint captured mid-graph and
// completes the remaining steps to the same final Report a single
// uninterrupted Run call would reach.
func TestResumeContinuesFromMidGraphCheckpoint(t *testing.T) {
	t.Parallel()
	d, m := checkpointSortFixture(t)

	dFull, _ := checkpointSortFixture(t)
	want, err := flow.Run(context.Background(), dFull, m, machine.InOut{Input: "seed"}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("uninterrupted Run: %v", err)
	}

	m2, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("midZ"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("midZ"), To: machine.Status("final"), Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoint flow.Checkpoint
	onCheckpoint := func(c flow.Checkpoint) {
		checkpoint = c
		cancel()
	}
	_, err = flow.Run(ctx, d, m2, machine.InOut{Input: "seed"}, noopConfirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}

	m3, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("midZ"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("midZ"), To: machine.Status("final"), Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	dResume, _ := checkpointSortFixture(t)
	resumed, err := flow.Resume(context.Background(), dResume, m3, checkpoint, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status() != want.Status() {
		t.Fatalf("resumed status = %q, want %q", resumed.Status(), want.Status())
	}
	if resumed.Record().Input != want.Record().Input {
		t.Fatalf("resumed record = %+v, want %+v", resumed.Record(), want.Record())
	}
	wantOutcomes := want.Outcomes()
	gotOutcomes := resumed.Outcomes()
	if len(gotOutcomes) != len(wantOutcomes) {
		t.Fatalf("resumed outcomes = %v, want %v", gotOutcomes, wantOutcomes)
	}
	for id, o := range wantOutcomes {
		if gotOutcomes[id] != o {
			t.Fatalf("resumed outcome[%q] = %v, want %v", id, gotOutcomes[id], o)
		}
	}
}

// TestResumeAllDoneCheckpointCallsNothing proves Resume on a
// checkpoint whose Done already covers every step in d returns the
// checkpoint's status and record, and calls neither confirm nor
// onCheckpoint.
func TestResumeAllDoneCheckpointCallsNothing(t *testing.T) {
	t.Parallel()
	d, _ := checkpointSortFixture(t)
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("midZ"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("midZ"), To: machine.Status("final"), Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	checkpoint := flow.Checkpoint{
		Status: machine.Status("final"),
		Record: machine.InOut{Input: "seed"},
		Done:   []string{"a", "z"},
	}
	var confirmCalls, checkpointCalls int
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmCalls++
		return nil
	}
	onCheckpoint := func(c flow.Checkpoint) {
		checkpointCalls++
	}
	report, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, onCheckpoint)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if confirmCalls != 0 {
		t.Fatalf("confirm called %d times, want 0", confirmCalls)
	}
	if checkpointCalls != 0 {
		t.Fatalf("onCheckpoint called %d times, want 0", checkpointCalls)
	}
	if report.Status() != checkpoint.Status {
		t.Fatalf("status = %q, want %q", report.Status(), checkpoint.Status)
	}
	if report.Record().Input != checkpoint.Record.Input {
		t.Fatalf("record = %+v, want %+v", report.Record(), checkpoint.Record)
	}
}

// TestResumeAllDoneOneStepCheckpointCallsNothing proves Resume on a
// checkpoint whose Done already names the single step's ID in a
// one-step Definition returns the checkpoint's status and record
// without calling confirm or onCheckpoint. This pins the one-step
// short-circuit's guard, separate from the multi-step case above.
func TestResumeAllDoneOneStepCheckpointCallsNothing(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{
		Status: statusDone,
		Record: machine.InOut{Input: "seed"},
		Done:   []string{"a"},
	}
	var confirmCalls, checkpointCalls int
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmCalls++
		return nil
	}
	onCheckpoint := func(c flow.Checkpoint) {
		checkpointCalls++
	}
	report, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, onCheckpoint)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if confirmCalls != 0 {
		t.Fatalf("confirm called %d times, want 0", confirmCalls)
	}
	if checkpointCalls != 0 {
		t.Fatalf("onCheckpoint called %d times, want 0", checkpointCalls)
	}
	if report.Status() != checkpoint.Status {
		t.Fatalf("status = %q, want %q", report.Status(), checkpoint.Status)
	}
}

// TestResumeRejectsNilD proves Resume rejects a nil d before it
// touches m or confirm.
func TestResumeRejectsNilD(t *testing.T) {
	t.Parallel()
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{Status: statusDone}
	_, err := flow.Resume(context.Background(), nil, m, checkpoint, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: d must not be nil") {
		t.Fatalf("error %q should contain the d-nil message", err.Error())
	}
}

// TestResumeRejectsNilM proves Resume rejects a nil m before it
// touches confirm.
func TestResumeRejectsNilM(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	checkpoint := flow.Checkpoint{Status: statusDone}
	_, err := flow.Resume(context.Background(), d, nil, checkpoint, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: m must not be nil") {
		t.Fatalf("error %q should contain the m-nil message", err.Error())
	}
}

// TestResumeRejectsNilConfirm proves Resume rejects a nil confirm
// before it validates the checkpoint.
func TestResumeRejectsNilConfirm(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{Status: statusDone}
	_, err := flow.Resume(context.Background(), d, m, checkpoint, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: confirm must not be nil") {
		t.Fatalf("error %q should contain the confirm-nil message", err.Error())
	}
}

// TestResumeRejectsInvalidCheckpoint proves Resume rejects a
// checkpoint that fails Validate, once d, m, and confirm are all
// non-nil.
func TestResumeRejectsInvalidCheckpoint(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{Status: machine.Status("")}
	_, err := flow.Resume(context.Background(), d, m, checkpoint, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: checkpoint: status must not be empty") {
		t.Fatalf("error %q should contain the checkpoint-invalid message", err.Error())
	}
}

// TestResumeRejectsUnknownDoneStepID proves Resume rejects a
// checkpoint whose Done names a step ID absent from d, naming that ID
// before any step runs.
func TestResumeRejectsUnknownDoneStepID(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{Status: statusStart, Done: []string{"ghost"}}
	var confirmCalls, checkpointCalls int
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmCalls++
		return nil
	}
	onCheckpoint := func(c flow.Checkpoint) {
		checkpointCalls++
	}
	_, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("error %q should name the unknown step ID", err.Error())
	}
	if confirmCalls != 0 {
		t.Fatalf("confirm called %d times, want 0", confirmCalls)
	}
	if checkpointCalls != 0 {
		t.Fatalf("onCheckpoint called %d times, want 0", checkpointCalls)
	}
}

// TestResumeRejectsUnknownSkippedStepID proves Resume rejects a
// checkpoint whose Skipped names a step ID absent from d, naming that
// ID before any step runs. Mirrors
// TestResumeRejectsUnknownDoneStepID for the Skipped seeding loop.
func TestResumeRejectsUnknownSkippedStepID(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{Status: statusStart, Skipped: []string{"ghost"}}
	var confirmCalls, checkpointCalls int
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmCalls++
		return nil
	}
	onCheckpoint := func(c flow.Checkpoint) {
		checkpointCalls++
	}
	_, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("error %q should name the unknown step ID", err.Error())
	}
	if confirmCalls != 0 {
		t.Fatalf("confirm called %d times, want 0", confirmCalls)
	}
	if checkpointCalls != 0 {
		t.Fatalf("onCheckpoint called %d times, want 0", checkpointCalls)
	}
}

// TestResumeNilDWinsOverInvalidCheckpoint proves the nil-d error wins
// when both d is nil and the checkpoint fails Validate: checks 1
// through 3 run before check 4.
func TestResumeNilDWinsOverInvalidCheckpoint(t *testing.T) {
	t.Parallel()
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{Status: machine.Status("")}
	_, err := flow.Resume(context.Background(), nil, m, checkpoint, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: d must not be nil") {
		t.Fatalf("error %q should contain the d-nil message, not a Validate error", err.Error())
	}
}

// TestResumeNilMWinsOverNilConfirm proves the m-nil check runs before
// the confirm-nil check, pinning that link in the five-check order.
func TestResumeNilMWinsOverNilConfirm(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	checkpoint := flow.Checkpoint{Status: statusDone}
	_, err := flow.Resume(context.Background(), d, nil, checkpoint, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: m must not be nil") {
		t.Fatalf("error %q should contain the m-nil message, not a confirm-nil error", err.Error())
	}
}

// TestResumeNilConfirmWinsOverInvalidCheckpoint proves the
// confirm-nil check runs before Checkpoint.Validate, pinning that
// link in the five-check order.
func TestResumeNilConfirmWinsOverInvalidCheckpoint(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	checkpoint := flow.Checkpoint{Status: machine.Status("")}
	_, err := flow.Resume(context.Background(), d, m, checkpoint, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: confirm must not be nil") {
		t.Fatalf("error %q should contain the confirm-nil message, not a Validate error", err.Error())
	}
}

// TestResumeNilMWinsOverInvalidCheckpoint proves the m-nil check runs
// before Checkpoint.Validate, pinning that link in the five-check
// order.
func TestResumeNilMWinsOverInvalidCheckpoint(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	checkpoint := flow.Checkpoint{Status: machine.Status("")}
	_, err := flow.Resume(context.Background(), d, nil, checkpoint, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: m must not be nil") {
		t.Fatalf("error %q should contain the m-nil message, not a Validate error", err.Error())
	}
}

// TestResumeRejectsTopologicallyInconsistentDone proves Resume on a
// checkpoint whose Done names a real step in d while that step's own
// Needs entry is absent from Done returns an error rather than a
// Report with a silently wrong Done set. Resume performs no dedicated
// check for this case; pickTransition rejects the resulting
// out-of-order transition attempt instead.
func TestResumeRejectsTopologicallyInconsistentDone(t *testing.T) {
	t.Parallel()
	const midLeft = machine.Status("midLeft")
	const midRight = machine.Status("midRight")
	const joined = machine.Status("joined")
	d, err := flow.New([]flow.Step{
		{ID: "left", To: string(midLeft)},
		{ID: "right", To: string(midRight)},
		{ID: "join", Needs: []string{"left", "right"}, To: string(joined)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: midLeft, Trigger: triggerGo},
		machine.Transition{From: midLeft, To: midRight, Trigger: triggerGo},
		machine.Transition{From: midRight, To: joined, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	checkpoint := flow.Checkpoint{Status: midRight, Done: []string{"join"}}
	_, err = flow.Resume(context.Background(), d, m, checkpoint, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error for a topologically-inconsistent Done set, got nil")
	}
	if !strings.Contains(err.Error(), "no transition to status") {
		t.Fatalf("error %q should be pickTransition's no-transition failure for the re-run of a prerequisite step", err.Error())
	}
}
