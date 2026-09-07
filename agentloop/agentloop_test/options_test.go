package agentloop_test

import (
	"context"
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
		Completer: &scriptedCompleter{},
		Tools:     tools.New(),
		Bounds:    agentloop.Bounds{MaxIterations: 1},
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
	testOptionsValidateBasics(t)
	testOptionsValidateConclude(t)
	testOptionsValidateBudgetsAndLimits(t)
}

func testOptionsValidateBasics(t *testing.T) {
	cases := []validateCase{
		{"nil Completer", func(o agentloop.Options) agentloop.Options {
			o.Completer = nil
			return o
		}, agentloop.ErrNoCompleter, false},
		{"nil Tools", func(o agentloop.Options) agentloop.Options {
			o.Tools = nil
			return o
		}, agentloop.ErrNoTools, false},
		{"zero MaxIterations passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxIterations: 0}
			return o
		}, nil, true},
		{"negative MaxIterations", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxIterations: -1}
			return o
		}, agentloop.ErrMaxIterations, false},
		{"zero MaxCallsPerTurn passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxCallsPerTurn: 0}
			return o
		}, nil, true},
		{"zero MaxTotalTokens passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxTotalTokens: 0}
			return o
		}, nil, true},
		{"negative MaxTotalTokens fails", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxTotalTokens: -1}
			return o
		}, agentloop.ErrMaxTotalTokens, false},
		{"negative MaxCallsPerTurn fails", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxCallsPerTurn: -1}
			return o
		}, agentloop.ErrMaxCallsPerTurn, false},
		{"zero MaxCallsPerTurn passes", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxCallsPerTurn: 0}
			return o
		}, nil, true},
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
		}, agentloop.ErrSessionIDRequired, false},
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

func testOptionsValidateConclude(t *testing.T) {
	cases := []validateCase{
		{"negative ConcludeMargin fails", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Margin: -1}}
			return o
		}, agentloop.ErrConcludeMargin, false},
		{"zero ConcludeMargin passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Margin: 0}}
			return o
		}, nil, true},
		{"positive ConcludeMargin passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Margin: 3}}
			return o
		}, nil, true},
		{"negative ConcludeDeadline fails", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: -time.Second}}
			return o
		}, agentloop.ErrConcludeDeadline, false},
		{"zero ConcludeDeadline passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: 0}}
			return o
		}, nil, true},
		{"positive ConcludeDeadline passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: time.Minute}}
			return o
		}, nil, true},
	}
	runValidateCases(t, cases)
}

// TestConcludeValidate covers one case per invariant Conclude.Validate
// claims. It calls the grouped method directly on a Conclude value, not
// through Options.Validate, and pins the unchanged sentinels with
// errors.Is.
func TestConcludeValidate(t *testing.T) {
	cases := []struct {
		name    string
		concl   agentloop.Conclude
		wantErr error
		wantOK  bool
	}{
		{"negative Margin", agentloop.Conclude{Margin: -1}, agentloop.ErrConcludeMargin, false},
		{"zero Margin passes", agentloop.Conclude{Margin: 0}, nil, true},
		{"negative Deadline", agentloop.Conclude{Deadline: -time.Second}, agentloop.ErrConcludeDeadline, false},
		{"zero Deadline passes", agentloop.Conclude{Deadline: 0}, nil, true},
		{"valid group passes", agentloop.Conclude{Margin: 2, Deadline: time.Minute, Notice: "wrap up"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.concl.Validate()
			if c.wantOK {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, c.wantErr)
			}
		})
	}
}

func testOptionsValidateBudgetsAndLimits(t *testing.T) {
	cases := []validateCase{
		{"negative MaxConcurrentTools fails", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxConcurrentTools: -1}
			return o
		}, agentloop.ErrMaxConcurrentTools, false},
		{"zero MaxConcurrentTools passes", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxConcurrentTools: 0}
			return o
		}, nil, true},
		{"positive MaxConcurrentTools passes", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxConcurrentTools: 4}
			return o
		}, nil, true},
		{"incomplete ToolBudget fails", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{ToolBudget: &agentloop.ToolBudget{Reserve: nil}}
			return o
		}, agentloop.ErrIncompleteToolBudget, false},
		{"valid ToolBudget passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{ToolBudget: &agentloop.ToolBudget{Reserve: func(ctx context.Context, calls int) error { return nil }}}
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
// runs before the WorkBudget and ToolBudget checks: an earlier invalid
// field's error wins over ErrHeartbeatRequiresBus, even when
// HeartbeatInterval is also positive with a nil Bus.
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

// TestOptionsValidateCompleterBeforeConclude proves nil Completer wins
// over negative ConcludeDeadline.
func TestOptionsValidateCompleterBeforeConclude(t *testing.T) {
	o := validOptions()
	o.Completer = nil
	o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: -time.Second}}
	err := o.Validate()
	if !errors.Is(err, agentloop.ErrNoCompleter) {
		t.Fatalf("Validate() error = %v, want ErrNoCompleter", err)
	}
}

// TestExtensionsNilBehavesAsZero proves a nil Options.Extensions
// behaves as the zero Extensions: the Options pass Validate and build
// through New, while the same Options with a bad Conclude inside
// Extensions fail, proving the Validate walk reads the pointer.
func TestExtensionsNilBehavesAsZero(t *testing.T) {
	valid := agentloop.Options{
		Completer: &scriptedCompleter{},
		Tools:     tools.New(),
		Bounds:    agentloop.Bounds{MaxIterations: 1},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil without Extensions", err)
	}
	if _, err := agentloop.New(valid); err != nil {
		t.Fatalf("New() error = %v, want nil without Extensions", err)
	}
	withBadConclude := valid
	withBadConclude.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Margin: -1}}
	if err := withBadConclude.Validate(); !errors.Is(err, agentloop.ErrConcludeMargin) {
		t.Fatalf("Validate() error = %v, want ErrConcludeMargin once Extensions is set", err)
	}
}
