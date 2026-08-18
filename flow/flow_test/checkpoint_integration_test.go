package flow_test

// Integration test: a real onCheckpoint hook appends Encoded bytes to
// an in-memory slice, simulating caller-owned storage. A canceled ctx
// pauses Run; Decode reads the last stored checkpoint back, and
// Resume continues the walk to the same final Report an uninterrupted
// Run reaches.

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// checkpointGraphFixture builds a root singleton, a two-member wave,
// and a dependent tail step, plus a matching machine.
func checkpointGraphFixture(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	const (
		midRoot = machine.Status("midRoot")
		midWave = machine.Status("midWave")
		final   = machine.Status("final")
	)
	d, err := flow.New([]flow.Step{
		{ID: "root", To: string(midRoot)},
		{ID: "wa", Needs: []string{"root"}, To: string(midWave)},
		{ID: "wb", Needs: []string{"root"}, To: string(midWave)},
		{ID: "tail", Needs: []string{"wa", "wb"}, To: string(final)},
	}, []flow.Panel{{"wa", "wb"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: midRoot, Trigger: triggerGo},
		machine.Transition{From: midRoot, To: midWave, Trigger: triggerGo},
		machine.Transition{From: midWave, To: final, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// storingConfirm returns a Confirm closure that counts calls per step
// ID, guarded by a mutex for the wave's concurrent members.
func storingConfirm() (flow.Confirm, func(id string) int) {
	var mu sync.Mutex
	counts := map[string]int{}
	confirm := func(ctx context.Context, step flow.Step) error {
		mu.Lock()
		counts[step.ID]++
		mu.Unlock()
		return nil
	}
	get := func(id string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[id]
	}
	return confirm, get
}

// TestCheckpointIntegrationPauseResumeAcrossSingleton pauses right
// after the root singleton's checkpoint lands, stores it as Encoded
// bytes, decodes it back, and resumes to completion. It asserts the
// resumed run reaches the same final Report an uninterrupted Run
// reaches, and that root's confirm ran exactly once across both runs.
func TestCheckpointIntegrationPauseResumeAcrossSingleton(t *testing.T) {
	t.Parallel()
	d, m := checkpointGraphFixture(t)
	confirmWant, _ := storingConfirm()
	want, err := flow.Run(context.Background(), d, m, machine.InOut{Input: "seed"}, confirmWant, nil, nil)
	if err != nil {
		t.Fatalf("uninterrupted Run: %v", err)
	}

	confirm, counts := storingConfirm()
	var stored [][]byte
	ctx, cancel := context.WithCancel(context.Background())
	onCheckpoint := func(c flow.Checkpoint) {
		data, err := c.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		stored = append(stored, data)
		if len(stored) == 1 {
			cancel()
		}
	}
	_, err = flow.Run(ctx, d, m, machine.InOut{Input: "seed"}, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if counts("root") != 1 {
		t.Fatalf("root confirm count = %d, want 1", counts("root"))
	}
	if counts("wa") != 0 || counts("wb") != 0 || counts("tail") != 0 {
		t.Fatal("a step past the pause point ran before Resume")
	}

	checkpoint, err := flow.Decode(stored[len(stored)-1])
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	resumed, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status() != want.Status() {
		t.Fatalf("resumed status = %q, want %q", resumed.Status(), want.Status())
	}
	if resumed.Record().Input != want.Record().Input {
		t.Fatalf("resumed record = %+v, want %+v", resumed.Record(), want.Record())
	}
	if len(resumed.Outcomes()) != len(want.Outcomes()) {
		t.Fatalf("resumed outcomes = %v, want %v", resumed.Outcomes(), want.Outcomes())
	}
	if counts("root") != 1 {
		t.Fatalf("root confirm count after resume = %d, want 1: root must not re-run", counts("root"))
	}
}

// TestCheckpointIntegrationPauseResumeAcrossWave repeats the
// pause-and-resume sequence across a wave boundary: it pauses right
// after the wave's checkpoint lands and resumes from it.
func TestCheckpointIntegrationPauseResumeAcrossWave(t *testing.T) {
	t.Parallel()
	d, m := checkpointGraphFixture(t)
	confirmWant, _ := storingConfirm()
	want, err := flow.Run(context.Background(), d, m, machine.InOut{Input: "seed"}, confirmWant, nil, nil)
	if err != nil {
		t.Fatalf("uninterrupted Run: %v", err)
	}

	confirm, counts := storingConfirm()
	var stored [][]byte
	ctx, cancel := context.WithCancel(context.Background())
	onCheckpoint := func(c flow.Checkpoint) {
		data, err := c.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		stored = append(stored, data)
		if len(stored) == 2 {
			cancel()
		}
	}
	_, err = flow.Run(ctx, d, m, machine.InOut{Input: "seed"}, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if counts("tail") != 0 {
		t.Fatal("tail ran before Resume")
	}

	checkpoint, err := flow.Decode(stored[len(stored)-1])
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(checkpoint.Done) != 3 {
		t.Fatalf("checkpoint.Done = %v, want 3 entries (root, wa, wb)", checkpoint.Done)
	}

	resumed, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status() != want.Status() {
		t.Fatalf("resumed status = %q, want %q", resumed.Status(), want.Status())
	}
	if counts("wa") != 0 || counts("wb") != 0 {
		// wa and wb are wave members: Run never calls confirm for a
		// wave of two or more members.
		t.Fatalf("wa confirm = %d, wb confirm = %d, want 0, 0 (wave members are never confirmed)", counts("wa"), counts("wb"))
	}
	if counts("tail") != 1 {
		t.Fatalf("tail confirm count = %d, want 1", counts("tail"))
	}
}

// TestCheckpointIntegrationChainedStepResumeSkipsChild proves a
// checkpoint captured right after a chained step's parent transition
// fires lists the chained step's ID exactly once in Done, and that a
// subsequent Resume does not re-invoke the child workflow's confirm
// closure.
func TestCheckpointIntegrationChainedStepResumeSkipsChild(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	const statusFinal = machine.Status("final")
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusMid)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	parent, err := flow.New([]flow.Step{
		{ID: "parent-chain", Sub: child},
		{ID: "parent-next", Needs: []string{"parent-chain"}, To: string(statusFinal)},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusFinal, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	confirm, counts := storingConfirm()
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoint flow.Checkpoint
	var checkpointCalls int
	onCheckpoint := func(c flow.Checkpoint) {
		checkpointCalls++
		if checkpointCalls == 1 {
			checkpoint = c
			cancel()
		}
	}
	_, err = flow.Run(ctx, parent, m, machine.InOut{}, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if checkpointCalls != 1 {
		t.Fatalf("onCheckpoint called %d times before pause, want 1", checkpointCalls)
	}
	if len(checkpoint.Done) != 1 || checkpoint.Done[0] != "parent-chain" {
		t.Fatalf("checkpoint.Done = %v, want [parent-chain]", checkpoint.Done)
	}
	if counts("c1") != 1 {
		t.Fatalf("c1 confirm count = %d, want 1", counts("c1"))
	}

	resumed, err := flow.Resume(context.Background(), parent, m, checkpoint, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status() != statusFinal {
		t.Fatalf("resumed status = %q, want %q", resumed.Status(), statusFinal)
	}
	if counts("c1") != 1 {
		t.Fatalf("c1 confirm count after resume = %d, want 1: the child must not re-run", counts("c1"))
	}
	if counts("parent-next") != 1 {
		t.Fatalf("parent-next confirm count = %d, want 1", counts("parent-next"))
	}
}
