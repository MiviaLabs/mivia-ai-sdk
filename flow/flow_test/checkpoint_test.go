package flow_test

// Red step: before phase 25, this file did not compile, because
// flow.Checkpoint, flow.Decode, and flow.Resume did not exist, and
// flow.Run's call sites were missing the trailing onCheckpoint
// parameter. Checkpoint and Validate landed in flow/checkpoint.go,
// Encode and Decode in flow/wire.go, and the onCheckpoint parameter
// and Resume landed in flow/runner.go and flow/resume.go; the cases
// below then passed.

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestCheckpointValidateRejectsEmptyStatus proves Validate rejects a
// Checkpoint whose Status is the zero value.
func TestCheckpointValidateRejectsEmptyStatus(t *testing.T) {
	t.Parallel()
	c := flow.Checkpoint{Status: machine.Status(""), Done: []string{"a"}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for empty status, got nil")
	}
}

// TestCheckpointEncodeDecodeRoundTrips proves Encode then Decode
// round-trips Status, Record, Done, and Skipped.
func TestCheckpointEncodeDecodeRoundTrips(t *testing.T) {
	t.Parallel()
	c := flow.Checkpoint{
		Status:  statusDone,
		Record:  machine.InOut{Input: "in", Output: "out"},
		Done:    []string{"a", "b"},
		Skipped: []string{"c", "d"},
	}
	data, err := c.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := flow.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Status != c.Status {
		t.Fatalf("Status = %q, want %q", got.Status, c.Status)
	}
	if got.Record.Input != c.Record.Input || got.Record.Output != c.Record.Output {
		t.Fatalf("Record = %+v, want %+v", got.Record, c.Record)
	}
	if len(got.Done) != len(c.Done) || got.Done[0] != c.Done[0] || got.Done[1] != c.Done[1] {
		t.Fatalf("Done = %v, want %v", got.Done, c.Done)
	}
	if len(got.Skipped) != len(c.Skipped) || got.Skipped[0] != c.Skipped[0] || got.Skipped[1] != c.Skipped[1] {
		t.Fatalf("Skipped = %v, want %v", got.Skipped, c.Skipped)
	}
}

// TestCheckpointValidateRejectsDoneSkippedOverlap proves Validate
// rejects a Checkpoint whose Done and Skipped both name the same step
// ID: a step cannot have resolved both OutcomeSucceeded and
// OutcomeSkipped.
func TestCheckpointValidateRejectsDoneSkippedOverlap(t *testing.T) {
	t.Parallel()
	c := flow.Checkpoint{Status: statusDone, Done: []string{"a"}, Skipped: []string{"a"}}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for a step named in both Done and Skipped, got nil")
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Fatalf("error %q should name the overlapping step ID", err.Error())
	}
}

// TestCheckpointDecodeRejectsMalformedJSON proves Decode fails on
// bytes that are not valid JSON.
func TestCheckpointDecodeRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	_, err := flow.Decode([]byte("{not json"))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// TestCheckpointDecodeValidatesResult proves Decode runs Validate on
// the parsed result and rejects an empty Status read from the wire.
func TestCheckpointDecodeValidatesResult(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(flow.Checkpoint{Done: []string{"a"}})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	_, err = flow.Decode(data)
	if err == nil {
		t.Fatal("expected error for an empty status on the wire, got nil")
	}
}

// checkpointFixtureInput is a concrete struct used to prove any type
// identity does not survive a Checkpoint round-trip.
type checkpointFixtureInput struct {
	Name string
}

// TestCheckpointEncodeDecodeLosesConcreteAnyType proves a concrete
// struct stored in Record.Input decodes back as a
// map[string]interface{}, not the original type. A caller must
// convert the map itself to recover the value.
func TestCheckpointEncodeDecodeLosesConcreteAnyType(t *testing.T) {
	t.Parallel()
	c := flow.Checkpoint{
		Status: statusDone,
		Record: machine.InOut{Input: checkpointFixtureInput{Name: "x"}},
	}
	data, err := c.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := flow.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := got.Record.Input.(checkpointFixtureInput); ok {
		t.Fatal("decoded Input asserted to the original concrete type; round-trip should lose it")
	}
	m, ok := got.Record.Input.(map[string]interface{})
	if !ok {
		t.Fatalf("decoded Input = %T, want map[string]interface{}", got.Record.Input)
	}
	if m["Name"] != "x" {
		t.Fatalf("decoded Input[Name] = %v, want %q", m["Name"], "x")
	}
}

// TestRunEmptyGraphNeverCheckpoints proves Run on a zero-step
// Definition with a non-nil onCheckpoint never calls it.
func TestRunEmptyGraphNeverCheckpoints(t *testing.T) {
	t.Parallel()
	d, err := flow.New(nil, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	onCheckpoint := func(c flow.Checkpoint) {
		t.Fatal("onCheckpoint ran on an empty step graph")
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, onCheckpoint)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// checkpointSortFixture builds a two-step chain whose declaration and
// completion order (z then a) is the reverse of Done's required
// sorted order (a then z). A Done that lists completion order instead
// of sorted order fails this fixture.
func checkpointSortFixture(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	const midZ = machine.Status("midZ")
	const final = machine.Status("final")
	d, err := flow.New([]flow.Step{
		{ID: "z", To: string(midZ)},
		{ID: "a", Needs: []string{"z"}, To: string(final)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: midZ, Trigger: triggerGo},
		machine.Transition{From: midZ, To: final, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// TestRunCheckpointFiresOncePerSingletonSorted proves onCheckpoint
// fires once per singleton step, with Done holding exactly the sorted
// IDs of the steps completed so far, after each call.
func TestRunCheckpointFiresOncePerSingletonSorted(t *testing.T) {
	t.Parallel()
	d, m := checkpointSortFixture(t)
	var calls [][]string
	onCheckpoint := func(c flow.Checkpoint) {
		calls = append(calls, append([]string(nil), c.Done...))
	}
	_, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, onCheckpoint)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("onCheckpoint called %d times, want 2", len(calls))
	}
	wantFirst := []string{"z"}
	wantSecond := []string{"a", "z"}
	if len(calls[0]) != 1 || calls[0][0] != wantFirst[0] {
		t.Fatalf("first Done = %v, want %v", calls[0], wantFirst)
	}
	if len(calls[1]) != 2 || calls[1][0] != wantSecond[0] || calls[1][1] != wantSecond[1] {
		t.Fatalf("second Done = %v, want %v (sorted, not completion order z,a)", calls[1], wantSecond)
	}
}

// TestRunCheckpointFiresOncePerWave proves onCheckpoint fires once
// per wave, with every wave member present in Done after that one
// call.
func TestRunCheckpointFiresOncePerWave(t *testing.T) {
	t.Parallel()
	const target = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "left", To: string(target)},
		{ID: "right", To: string(target)},
	}, []flow.Panel{{"left", "right"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: target, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var calls int
	var lastDone []string
	var mu sync.Mutex
	onCheckpoint := func(c flow.Checkpoint) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		lastDone = append([]string(nil), c.Done...)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, onCheckpoint)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("onCheckpoint called %d times, want 1", calls)
	}
	sort.Strings(lastDone)
	if len(lastDone) != 2 || lastDone[0] != "left" || lastDone[1] != "right" {
		t.Fatalf("Done = %v, want [left right]", lastDone)
	}
}

// TestRunNilOnCheckpointBehavesAsBefore proves Run with a nil
// onCheckpoint reaches the same Report and error as before this
// phase.
func TestRunNilOnCheckpointBehavesAsBefore(t *testing.T) {
	t.Parallel()
	d, m := checkpointSortFixture(t)
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != machine.Status("final") {
		t.Fatalf("status = %q, want %q", status, "final")
	}
}

// TestRunPausesWhenContextAlreadyCanceled proves Run returns the
// pinned pause error when ctx is already canceled before the loop's
// first iteration.
func TestRunPausesWhenContextAlreadyCanceled(t *testing.T) {
	t.Parallel()
	d, m := checkpointSortFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var confirmed int
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmed++
		return nil
	}
	report, err := flow.Run(ctx, d, m, machine.InOut{}, confirm, nil, nil)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if !strings.Contains(err.Error(), "run paused") {
		t.Fatalf("error %q should contain the pause message", err.Error())
	}
	if confirmed != 0 {
		t.Fatalf("confirm called %d times, want 0", confirmed)
	}
	if report.Status() != statusStart {
		t.Fatalf("status = %q, want the initial status %q", report.Status(), statusStart)
	}
}

// TestRunSingleStepChecksCanceledCtx proves Run checks ctx for
// cancellation before starting a one-step Definition's only step, the
// same as it does before each step of a multi-step graph. The
// one-step branch is a separate code path from the loop the
// multi-step case exercises, so a multi-step-only pause test cannot
// catch a missing check here.
func TestRunSingleStepChecksCanceledCtx(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var confirmed int
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmed++
		return nil
	}
	report, err := flow.Run(ctx, d, m, machine.InOut{}, confirm, nil, nil)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if !strings.Contains(err.Error(), "run paused") {
		t.Fatalf("error %q should contain the pause message", err.Error())
	}
	if confirmed != 0 {
		t.Fatalf("confirm called %d times, want 0", confirmed)
	}
	if report.Status() != statusStart {
		t.Fatalf("status = %q, want the initial status %q", report.Status(), statusStart)
	}
}

// TestRunSingleStepFiresCheckpointOnce proves Run fires onCheckpoint
// once, with Done holding the single step's ID, when a one-step
// Definition completes. The one-step branch builds its Checkpoint on
// a different return path than the multi-step loop; a multi-step-only
// checkpoint test cannot catch a missing call here.
func TestRunSingleStepFiresCheckpointOnce(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	var calls int
	var lastDone []string
	onCheckpoint := func(c flow.Checkpoint) {
		calls++
		lastDone = append([]string(nil), c.Done...)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, onCheckpoint)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status() != statusDone {
		t.Fatalf("status = %q, want %q", report.Status(), statusDone)
	}
	if calls != 1 {
		t.Fatalf("onCheckpoint called %d times, want 1", calls)
	}
	if len(lastDone) != 1 || lastDone[0] != "a" {
		t.Fatalf("Done = %v, want [a]", lastDone)
	}
}

// TestCheckpointEncodeRejectsInvalidStatus proves Encode runs Validate
// before it marshals, returning the Validate error for a Checkpoint
// whose Status is empty, instead of a marshaled empty-status payload.
func TestCheckpointEncodeRejectsInvalidStatus(t *testing.T) {
	t.Parallel()
	c := flow.Checkpoint{Status: machine.Status("")}
	data, err := c.Encode()
	if err == nil {
		t.Fatal("expected error for empty status, got nil")
	}
	if !strings.Contains(err.Error(), "status must not be empty") {
		t.Fatalf("error %q should contain the Validate message", err.Error())
	}
	if data != nil {
		t.Fatalf("data = %v, want nil on a rejected Encode", data)
	}
}

// TestRunPausesMidGraph proves Run returns the pinned pause error
// mid-graph, after at least one step completed and its checkpoint
// fired, when ctx cancels before the next step starts.
func TestRunPausesMidGraph(t *testing.T) {
	t.Parallel()
	d, m := checkpointSortFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoints int
	onCheckpoint := func(c flow.Checkpoint) {
		checkpoints++
		cancel()
	}
	report, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if !strings.Contains(err.Error(), "run paused") {
		t.Fatalf("error %q should contain the pause message", err.Error())
	}
	if checkpoints != 1 {
		t.Fatalf("onCheckpoint called %d times, want 1", checkpoints)
	}
	if outcome, ok := report.Outcome("z"); !ok || outcome != flow.OutcomeSucceeded {
		t.Fatalf("Outcome(z) = %v, %v, want %v, true", outcome, ok, flow.OutcomeSucceeded)
	}
	if _, ok := report.Outcome("a"); ok {
		t.Fatal("Outcome(a) resolved, want unresolved: the pause must stop before the next step")
	}
}
