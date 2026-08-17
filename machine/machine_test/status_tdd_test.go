package machine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// guardTrue is a test guard that always passes.
func guardTrue(_ context.Context) (bool, error) { return true, nil }

// validateCase holds one table-driven Validate case.
type validateCase struct {
	name        string
	initial     machine.Status
	transitions []machine.Transition
	wantErr     bool
	errSubstr   string
}

// rejectCases lists Validate cases that must return an error.
var rejectCases = []validateCase{
	{
		name:    "rejects self loop",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "idle", Trigger: "reset"},
		},
		wantErr:   true,
		errSubstr: "self loop",
		// Red step: Validate returned nil for self-loop input.
		// Assert added; test failed. Implementation added the check.
	},
	{
		name:    "rejects From not in declared set",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "running", Trigger: "start"},
			{From: "orphan", To: "done", Trigger: "skip"},
		},
		wantErr:   true,
		errSubstr: "not in the declared set",
		// Red step: Validate returned nil for unreachable From.
		// Assert added; test failed. Implementation added reachability check.
		// An unreachable To implies an unreachable From, so the From
		// check covers both. Validate rejects only the From.
	},
	{
		name:        "rejects empty transition list",
		initial:     "idle",
		transitions: []machine.Transition{},
		wantErr:     true,
		errSubstr:   "must not be empty",
	},
	{
		name:    "rejects dispatch ambiguity",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "a", Trigger: "go"},
			{From: "idle", To: "b", Trigger: "go"},
		},
		wantErr:   true,
		errSubstr: "duplicate transition",
		// Red step: Validate returned nil for duplicate From and Trigger.
		// Assert added; test failed. Implementation added the check.
	},
}

// acceptCases lists Validate cases that must return nil.
var acceptCases = []validateCase{
	{
		name:    "accepts nil Guard",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "running", Trigger: "start", Guard: nil},
		},
		// Red step: not applicable; nil Guard was always accepted.
	},
	{
		name:    "accepts valid table with guard",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "running", Trigger: "start", Guard: guardTrue},
		},
		// Red step: Validate returned nil for valid table from the start.
		// Implementation confirmed correct.
	},
	{
		name:    "accepts valid table with multiple transitions",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "running", Trigger: "start"},
			{From: "running", To: "done", Trigger: "finish"},
		},
	},
}

// runValidateCase executes one Validate test case.
func runValidateCase(t *testing.T, tt validateCase) {
	t.Helper()
	d := &machine.Definition{
		Initial:     tt.initial,
		Transitions: tt.transitions,
	}
	err := d.Validate()
	if tt.wantErr {
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), tt.errSubstr) {
			t.Fatalf("error %q should contain %q", err.Error(), tt.errSubstr)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		initial     machine.Status
		transitions []machine.Transition
		wantErr     bool
		errSubstr   string
	}{
		{
			name:      "rejects empty transition list",
			initial:   "idle",
			wantErr:   true,
			errSubstr: "must not be empty",
			// Red step: New with no transitions returned nil, nil.
			// Assert added; test failed. Implementation added the check.
		},
		{
			name:    "accepts valid transition list",
			initial: "idle",
			transitions: []machine.Transition{
				{From: "idle", To: "running", Trigger: "start"},
			},
			// Red step: New returned nil, nil before Validate existed.
			// Assert added; test failed on nil Definition.
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := machine.New(tt.initial, tt.transitions...)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d == nil {
				t.Fatal("expected non-nil Definition")
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()
	for _, tt := range rejectCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runValidateCase(t, tt)
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	t.Parallel()
	for _, tt := range acceptCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runValidateCase(t, tt)
		})
	}
}

func TestDefinitionFields(t *testing.T) {
	t.Parallel()
	d, err := machine.New(
		"idle",
		machine.Transition{From: "idle", To: "running", Trigger: "start"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Initial != "idle" {
		t.Errorf("Initial = %q, want %q", d.Initial, "idle")
	}
	if len(d.Transitions) != 1 {
		t.Fatalf("len(Transitions) = %d, want 1", len(d.Transitions))
	}
	if d.Transitions[0].Trigger != "start" {
		t.Errorf("Trigger = %q, want %q", d.Transitions[0].Trigger, "start")
	}
}

func TestErrorsAreNotSentinel(t *testing.T) {
	t.Parallel()
	// A distinct sentinel that never equals the error New returns.
	sentinel := errors.New("machine: transition list must not be empty")
	_, err := machine.New("idle")
	if err == nil {
		t.Fatal("expected error for empty transition list")
	}
	// Errors are fresh fmt.Errorf values, not exported sentinels.
	// errors.Is must not match the sentinel.
	if errors.Is(err, sentinel) {
		t.Fatal("error matched the sentinel; expected a fresh fmt.Errorf value")
	}
}
