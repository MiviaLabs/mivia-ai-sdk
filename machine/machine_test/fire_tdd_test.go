package machine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// newIdleToRunning builds a one-row table from idle to running.
func newIdleToRunning(guard machine.Guard) (*machine.Definition, error) {
	return machine.New(
		"idle",
		machine.Transition{
			From:    "idle",
			To:      "running",
			Trigger: "start",
			Guard:   guard,
		},
	)
}

// TestFireResolvesRow proves Fire returns the row To status.
func TestFireResolvesRow(t *testing.T) {
	t.Parallel()
	d, err := newIdleToRunning(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _, err := d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "running" {
		t.Fatalf("Fire status = %q, want %q", got, "running")
	}
	// Red step: Fire did not exist, so the test did not compile.
	// Fire added; the test passed.
}

// TestFireActionWritesOutput proves an action fills the returned Output.
func TestFireActionWritesOutput(t *testing.T) {
	t.Parallel()
	d, err := machine.New(
		"idle",
		machine.Transition{
			From:    "idle",
			To:      "running",
			Trigger: "start",
			OnEntry: func(_ context.Context, rec *machine.InOut) error {
				rec.Output = "produced"
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, out, err := d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Output != "produced" {
		t.Fatalf("Fire Output = %q, want %q", out.Output, "produced")
	}
	// Red step: Action received no record, so it could not write Output.
	// The returned Output stayed nil. The signature change fixed it.
}

// TestFireUnknownFrom proves Fire returns an error on an unknown status.
func TestFireUnknownFrom(t *testing.T) {
	t.Parallel()
	d, err := newIdleToRunning(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = d.Fire(context.Background(), "absent", "start", machine.InOut{})
	if err == nil {
		t.Fatal("expected error for unknown from status, got nil")
	}
	if !strings.Contains(err.Error(), "no transition") {
		t.Fatalf("error %q should mention no transition", err.Error())
	}
	// Red step: Fire did not exist on the empty phase.
	// The unknown-from path returned nil before the check was added.
}

// TestFireUnknownTrigger proves Fire returns an error on an unknown trigger.
func TestFireUnknownTrigger(t *testing.T) {
	t.Parallel()
	d, err := newIdleToRunning(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = d.Fire(context.Background(), "idle", "bogus", machine.InOut{})
	if err == nil {
		t.Fatal("expected error for unknown trigger, got nil")
	}
	if !strings.Contains(err.Error(), "no transition") {
		t.Fatalf("error %q should mention no transition", err.Error())
	}
	// Red step: Fire did not exist. The unknown-trigger path returned
	// nil before the check was added.
}

// TestFireGuardBlocksMove proves a failing guard moves nothing.
func TestFireGuardBlocksMove(t *testing.T) {
	t.Parallel()
	guard := func(_ context.Context) (bool, error) { return false, nil }
	d, err := newIdleToRunning(guard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _, err := d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err == nil {
		t.Fatal("expected error for rejected guard, got nil")
	}
	if !strings.Contains(err.Error(), "guard rejected") {
		t.Fatalf("error %q should mention guard rejected", err.Error())
	}
	if got != "idle" {
		t.Fatalf("Fire kept status %q, want %q", got, "idle")
	}
	// Red step: Fire did not exist. The guard-rejection path returned
	// the target status before the guard check was added.
}

// TestFireGuardErrorPropagates proves a guard error bubbles up.
func TestFireGuardErrorPropagates(t *testing.T) {
	t.Parallel()
	guardErr := "guard exploded"
	guard := func(_ context.Context) (bool, error) { return false, errForged(guardErr) }
	d, err := newIdleToRunning(guard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err == nil {
		t.Fatal("expected guard error, got nil")
	}
	if !strings.Contains(err.Error(), guardErr) {
		t.Fatalf("error %q should mention %q", err.Error(), guardErr)
	}
	// Red step: Fire did not exist. The guard-error path returned nil
	// before the error propagation was added.
}

// TestFireOnExitSkipsWhenGuardFails proves OnExit does not run on a fail.
func TestFireOnExitSkipsWhenGuardFails(t *testing.T) {
	t.Parallel()
	exitRan := false
	guard := func(_ context.Context) (bool, error) { return false, nil }
	d, err := machine.New(
		"idle",
		machine.Transition{
			From:    "idle",
			To:      "running",
			Trigger: "start",
			Guard:   guard,
			OnExit: func(_ context.Context, _ *machine.InOut) error {
				exitRan = true
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err == nil {
		t.Fatal("expected guard rejection error, got nil")
	}
	if exitRan {
		t.Fatal("OnExit ran when the guard failed; it must stay silent")
	}
	// Red step: Fire did not exist. Before the guard-before-OnExit
	// ordering was added, OnExit ran even on a failed guard.
}

// TestFireActionErrorPropagates proves an action error bubbles up.
func TestFireActionErrorPropagates(t *testing.T) {
	t.Parallel()
	actionErr := "action exploded"
	d, err := machine.New(
		"idle",
		machine.Transition{
			From:    "idle",
			To:      "running",
			Trigger: "start",
			OnEntry: func(_ context.Context, _ *machine.InOut) error {
				return errForged(actionErr)
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err == nil {
		t.Fatal("expected action error, got nil")
	}
	if !strings.Contains(err.Error(), actionErr) {
		t.Fatalf("error %q should mention %q", err.Error(), actionErr)
	}
	// Red step: Fire did not exist. The action-error path returned nil
	// before the error propagation was added.
}

// TestFireOnExitErrorSkipsOnEntry proves an OnExit error stops the move.
func TestFireOnExitErrorSkipsOnEntry(t *testing.T) {
	t.Parallel()
	exitErr := "exit exploded"
	entryRan := false
	d, err := machine.New(
		"idle",
		machine.Transition{
			From:    "idle",
			To:      "running",
			Trigger: "start",
			OnExit: func(_ context.Context, _ *machine.InOut) error {
				return errForged(exitErr)
			},
			OnEntry: func(_ context.Context, _ *machine.InOut) error {
				entryRan = true
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err == nil {
		t.Fatal("expected OnExit error, got nil")
	}
	if !strings.Contains(err.Error(), exitErr) {
		t.Fatalf("error %q should mention %q", err.Error(), exitErr)
	}
	if entryRan {
		t.Fatal("OnEntry ran after OnExit failed; it must stay silent")
	}
}

// errForged builds a fresh error for a test.
func errForged(msg string) error {
	return errorsForge{msg: msg}
}

type errorsForge struct{ msg string }

func (e errorsForge) Error() string { return e.msg }
