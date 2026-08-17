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

// newCase holds one table-driven New case.
type newCase struct {
	name        string
	initial     machine.Status
	transitions []machine.Transition
	wantErr     bool
	errSubstr   string
}

// rejectCases lists New cases that must return an error.
var rejectCases = []newCase{
	{
		name:    "rejects self loop",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "idle", Trigger: "reset"},
		},
		wantErr:   true,
		errSubstr: "self loop",
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
		// An unreachable To implies an unreachable From, so the From
		// check covers both. New rejects only the From.
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
	},
}

// acceptCases lists New cases that must return nil.
var acceptCases = []newCase{
	{
		name:    "accepts nil Guard",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "running", Trigger: "start", Guard: nil},
		},
	},
	{
		name:    "accepts valid table with guard",
		initial: "idle",
		transitions: []machine.Transition{
			{From: "idle", To: "running", Trigger: "start", Guard: guardTrue},
		},
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

// runNewCase executes one New test case.
func runNewCase(t *testing.T, tt newCase) {
	t.Helper()
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
}

func TestNewRejects(t *testing.T) {
	t.Parallel()
	for _, tt := range rejectCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runNewCase(t, tt)
		})
	}
}

func TestNewAccepts(t *testing.T) {
	t.Parallel()
	for _, tt := range acceptCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runNewCase(t, tt)
		})
	}
}

// TestValidateRejectsZeroValue proves a zero-value Definition fails Validate.
// New returns early on an empty list, so this branch is reachable only here.
func TestValidateRejectsZeroValue(t *testing.T) {
	t.Parallel()
	var d machine.Definition
	err := d.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("error %q should contain %q", err.Error(), "must not be empty")
	}
}

// TestDefinitionFields reads the state through the accessors.
func TestDefinitionFields(t *testing.T) {
	t.Parallel()
	d, err := machine.New(
		"idle",
		machine.Transition{From: "idle", To: "running", Trigger: "start"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Initial() != "idle" {
		t.Errorf("Initial() = %q, want %q", d.Initial(), "idle")
	}
	if len(d.Transitions()) != 1 {
		t.Fatalf("len(Transitions()) = %d, want 1", len(d.Transitions()))
	}
	if d.Transitions()[0].Trigger != "start" {
		t.Errorf("Trigger = %q, want %q", d.Transitions()[0].Trigger, "start")
	}
}

// TestNewCopiesInputSlice proves a caller write to the input slice after
// New does not leak into the internal table.
func TestNewCopiesInputSlice(t *testing.T) {
	t.Parallel()
	ts := []machine.Transition{
		{From: "idle", To: "running", Trigger: "start"},
	}
	d, err := machine.New("idle", ts...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts[0].To = "evil"
	got, _, err := d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "running" {
		t.Fatalf("Fire status = %q, want %q", got, "running")
	}
}

// TestTransitionsReturnsCopy proves Transitions returns a fresh copy.
func TestTransitionsReturnsCopy(t *testing.T) {
	t.Parallel()
	d, err := machine.New(
		"idle",
		machine.Transition{From: "idle", To: "running", Trigger: "start"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := d.Transitions()
	got[0].To = "evil"
	second := d.Transitions()
	if second[0].To != "running" {
		t.Fatalf("second Transitions read To = %q, want %q", second[0].To, "running")
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

// threeStep builds a three-status machine for query tests.
func threeStep(t *testing.T) *machine.Definition {
	t.Helper()
	d, err := machine.New(
		"idle",
		machine.Transition{From: "idle", To: "running", Trigger: "start"},
		machine.Transition{From: "running", To: "done", Trigger: "finish"},
		machine.Transition{From: "running", To: "idle", Trigger: "cancel"},
	)
	if err != nil {
		t.Fatalf("threeStep: %v", err)
	}
	return d
}

func TestAllowedTransitions(t *testing.T) {
	t.Parallel()
	d := threeStep(t)
	t.Run("returns matching rows", func(t *testing.T) {
		t.Parallel()
		got := d.AllowedTransitions("running")
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].Trigger != "finish" {
			t.Fatalf("got[0].Trigger = %q, want %q (declaration order)", got[0].Trigger, "finish")
		}
		if got[1].Trigger != "cancel" {
			t.Fatalf("got[1].Trigger = %q, want %q (declaration order)", got[1].Trigger, "cancel")
		}
	})
	t.Run("returns empty for unknown status", func(t *testing.T) {
		t.Parallel()
		got := d.AllowedTransitions("absent")
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})
	t.Run("returns a copy", func(t *testing.T) {
		t.Parallel()
		got := d.AllowedTransitions("idle")
		got[0].To = "evil"
		second := d.AllowedTransitions("idle")
		if second[0].To != "running" {
			t.Fatalf("mutation leaked: To = %q, want %q", second[0].To, "running")
		}
	})
}

func TestAllowedTriggers(t *testing.T) {
	t.Parallel()
	d := threeStep(t)
	t.Run("returns distinct triggers", func(t *testing.T) {
		t.Parallel()
		got := d.AllowedTriggers("running")
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		set := map[machine.Trigger]bool{}
		for _, trig := range got {
			set[trig] = true
		}
		if !set["finish"] || !set["cancel"] {
			t.Fatalf("missing triggers: %v", set)
		}
	})
	t.Run("returns empty for unknown status", func(t *testing.T) {
		t.Parallel()
		got := d.AllowedTriggers("absent")
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})
	t.Run("single trigger from leaf status", func(t *testing.T) {
		t.Parallel()
		got := d.AllowedTriggers("idle")
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0] != "start" {
			t.Fatalf("trigger = %q, want %q", got[0], "start")
		}
	})
}
