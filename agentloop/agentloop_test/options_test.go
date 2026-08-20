package agentloop_test

import (
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// validOptions returns a minimal Options that passes Validate, for a
// test to mutate one field at a time.
func validOptions() agentloop.Options {
	return agentloop.Options{
		Completer:     &scriptedCompleter{},
		Tools:         tools.New(),
		MaxIterations: 1,
	}
}

// validateCase names one Options.Validate table row: mutate builds the
// Options under test, wantErr is checked with errors.Is when
// non-nil, and wantOK true means Validate must return nil.
type validateCase struct {
	name    string
	mutate  func(agentloop.Options) agentloop.Options
	wantErr error
	wantOK  bool
}

// runValidateCases runs every case in cases against validOptions,
// mutated by c.mutate, and asserts the case's wantOK/wantErr contract.
func runValidateCases(t *testing.T, cases []validateCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.mutate(validOptions()).Validate()
			if c.wantOK {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("Validate() error = %v, want %v", err, c.wantErr)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want non-nil")
			}
		})
	}
}

// TestOptionsValidate covers one case per invariant Validate claims.
func TestOptionsValidate(t *testing.T) {
	cases := []validateCase{
		{"nil Completer", func(o agentloop.Options) agentloop.Options {
			o.Completer = nil
			return o
		}, agentloop.ErrNoCompleter, false},
		{"nil Tools", func(o agentloop.Options) agentloop.Options {
			o.Tools = nil
			return o
		}, agentloop.ErrNoTools, false},
		{"zero MaxIterations", func(o agentloop.Options) agentloop.Options {
			o.MaxIterations = 0
			return o
		}, agentloop.ErrMaxIterations, false},
		{"negative MaxIterations", func(o agentloop.Options) agentloop.Options {
			o.MaxIterations = -1
			return o
		}, agentloop.ErrMaxIterations, false},
		{"zero MaxCallsPerTurn passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.MaxCallsPerTurn = 0
			return o
		}, nil, true},
		{"zero MaxTotalTokens passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.MaxTotalTokens = 0
			return o
		}, nil, true},
		{"negative MaxTotalTokens fails", func(o agentloop.Options) agentloop.Options {
			o.MaxTotalTokens = -1
			return o
		}, nil, false},
		{"negative Budget field fails", func(o agentloop.Options) agentloop.Options {
			o.Budget = &contextbudget.Limits{MaxBytes: -1}
			return o
		}, nil, false},
		{"valid Budget passes", func(o agentloop.Options) agentloop.Options {
			o.Budget = &contextbudget.Limits{MaxBytes: 100}
			return o
		}, nil, true},
		{"Usage without SessionID fails", func(o agentloop.Options) agentloop.Options {
			o.Usage = usage.New()
			return o
		}, nil, false},
		{"Usage with SessionID passes", func(o agentloop.Options) agentloop.Options {
			o.Usage = usage.New()
			o.SessionID = "sess-1"
			return o
		}, nil, true},
		{"fully valid options pass", func(o agentloop.Options) agentloop.Options {
			return o
		}, nil, true},
	}
	runValidateCases(t, cases)
}

// TestOptionsValidateHeartbeat covers the HeartbeatInterval/Bus
// invariant: a positive HeartbeatInterval requires a non-nil Bus; a
// zero HeartbeatInterval passes regardless of Bus.
func TestOptionsValidateHeartbeat(t *testing.T) {
	cases := []validateCase{
		{"positive HeartbeatInterval with nil Bus fails", func(o agentloop.Options) agentloop.Options {
			o.HeartbeatInterval = 5 * time.Millisecond
			return o
		}, agentloop.ErrHeartbeatRequiresBus, false},
		{"positive HeartbeatInterval with Bus passes", func(o agentloop.Options) agentloop.Options {
			o.HeartbeatInterval = 5 * time.Millisecond
			o.Bus = events.New()
			return o
		}, nil, true},
		{"zero HeartbeatInterval with nil Bus passes", func(o agentloop.Options) agentloop.Options {
			o.HeartbeatInterval = 0
			return o
		}, nil, true},
		{"zero HeartbeatInterval with Bus set passes", func(o agentloop.Options) agentloop.Options {
			o.HeartbeatInterval = 0
			o.Bus = events.New()
			return o
		}, nil, true},
	}
	runValidateCases(t, cases)
}

// TestOptionsValidateHeartbeatOrder proves the HeartbeatInterval check
// runs last: an earlier invalid field's error wins over
// ErrHeartbeatRequiresBus, even when HeartbeatInterval is also
// positive with a nil Bus.
func TestOptionsValidateHeartbeatOrder(t *testing.T) {
	o := validOptions()
	o.Completer = nil
	o.HeartbeatInterval = 5 * time.Millisecond
	err := o.Validate()
	if !errors.Is(err, agentloop.ErrNoCompleter) {
		t.Fatalf("Validate() error = %v, want ErrNoCompleter (earlier in the fixed order)", err)
	}
	if errors.Is(err, agentloop.ErrHeartbeatRequiresBus) {
		t.Fatalf("Validate() error wraps ErrHeartbeatRequiresBus, want the earlier check to win")
	}
}
