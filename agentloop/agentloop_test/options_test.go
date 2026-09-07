package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/context/budget"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
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
// Options under test. wantErr, when non-nil, is checked with
// errors.Is. wantField, when non-empty, asserts the error message
// names that field. wantOK true means Validate must return nil.
type validateCase struct {
	name      string
	mutate    func(agentloop.Options) agentloop.Options
	wantErr   error
	wantField string
	wantOK    bool
}

// runValidateCases runs every case in cases against validOptions,
// mutated by c.mutate, and asserts the case's wantOK/wantErr/wantField
// contract.
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
			}
			if c.wantField != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantField) {
					t.Fatalf("Validate() error = %v, want it to name %q", err, c.wantField)
				}
			}
			if c.wantErr == nil && c.wantField == "" && err == nil {
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
		}, agentloop.ErrInvalidOptions, "Completer", false},
		{"nil Tools", func(o agentloop.Options) agentloop.Options {
			o.Tools = nil
			return o
		}, agentloop.ErrInvalidOptions, "Tools", false},
		{"zero MaxIterations passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxIterations: 0}
			return o
		}, nil, "", true},
		{"negative MaxIterations", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxIterations: -1}
			return o
		}, agentloop.ErrInvalidOptions, "MaxIterations", false},
		{"zero MaxCallsPerTurn passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxCallsPerTurn: 0}
			return o
		}, nil, "", true},
		{"zero MaxTotalTokens passes (unbounded)", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxTotalTokens: 0}
			return o
		}, nil, "", true},
		{"negative MaxTotalTokens fails", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxTotalTokens: -1}
			return o
		}, agentloop.ErrInvalidOptions, "MaxTotalTokens", false},
		{"negative MaxCallsPerTurn fails", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxCallsPerTurn: -1}
			return o
		}, agentloop.ErrInvalidOptions, "MaxCallsPerTurn", false},
		{"zero MaxCallsPerTurn passes", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxCallsPerTurn: 0}
			return o
		}, nil, "", true},
		{"negative Budget field fails", func(o agentloop.Options) agentloop.Options {
			o.Budget = &budget.Limits{MaxBytes: -1}
			return o
		}, nil, "", false},
		{"valid Budget passes", func(o agentloop.Options) agentloop.Options {
			o.Budget = &budget.Limits{MaxBytes: 100}
			return o
		}, nil, "", true},
		{"Usage without SessionID fails", func(o agentloop.Options) agentloop.Options {
			o.Usage = provider.NewAccumulator()
			return o
		}, agentloop.ErrInvalidOptions, "SessionID", false},
		{"Usage with SessionID passes", func(o agentloop.Options) agentloop.Options {
			o.Usage = provider.NewAccumulator()
			o.SessionID = "sess-1"
			return o
		}, nil, "", true},
		{"fully valid options pass", func(o agentloop.Options) agentloop.Options {
			return o
		}, nil, "", true},
	}
	runValidateCases(t, cases)
}

func testOptionsValidateConclude(t *testing.T) {
	cases := []validateCase{
		{"negative ConcludeMargin fails", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Margin: -1}}
			return o
		}, agentloop.ErrInvalidOptions, "Margin", false},
		{"zero ConcludeMargin passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Margin: 0}}
			return o
		}, nil, "", true},
		{"positive ConcludeMargin passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Margin: 3}}
			return o
		}, nil, "", true},
		{"negative ConcludeDeadline fails", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: -time.Second}}
			return o
		}, agentloop.ErrInvalidOptions, "Deadline", false},
		{"zero ConcludeDeadline passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: 0}}
			return o
		}, nil, "", true},
		{"positive ConcludeDeadline passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: time.Minute}}
			return o
		}, nil, "", true},
	}
	runValidateCases(t, cases)
}

// TestConcludeValidate covers one case per invariant Conclude.Validate
// claims. It calls the grouped method directly on a Conclude value, not
// through Options.Validate, and pins the unchanged sentinels with
// errors.Is.
func TestConcludeValidate(t *testing.T) {
	cases := []struct {
		name      string
		concl     agentloop.Conclude
		wantField string
		wantOK    bool
	}{
		{"negative Margin", agentloop.Conclude{Margin: -1}, "Margin", false},
		{"zero Margin passes", agentloop.Conclude{Margin: 0}, "", true},
		{"negative Deadline", agentloop.Conclude{Deadline: -time.Second}, "Deadline", false},
		{"zero Deadline passes", agentloop.Conclude{Deadline: 0}, "", true},
		{"valid group passes", agentloop.Conclude{Margin: 2, Deadline: time.Minute, Notice: "wrap up"}, "", true},
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
			if !errors.Is(err, agentloop.ErrInvalidOptions) {
				t.Fatalf("Validate() error = %v, want ErrInvalidOptions", err)
			}
			if !strings.Contains(err.Error(), c.wantField) {
				t.Fatalf("Validate() error = %v, want it to name %q", err, c.wantField)
			}
		})
	}
}

func testOptionsValidateBudgetsAndLimits(t *testing.T) {
	cases := []validateCase{
		{"negative MaxConcurrentTools fails", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxConcurrentTools: -1}
			return o
		}, agentloop.ErrInvalidOptions, "MaxConcurrentTools", false},
		{"zero MaxConcurrentTools passes", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxConcurrentTools: 0}
			return o
		}, nil, "", true},
		{"positive MaxConcurrentTools passes", func(o agentloop.Options) agentloop.Options {
			o.Bounds = agentloop.Bounds{MaxConcurrentTools: 4}
			return o
		}, nil, "", true},
		{"incomplete ToolBudget fails", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{ToolBudget: &agentloop.ToolBudget{Reserve: nil}}
			return o
		}, agentloop.ErrIncompleteToolBudget, "", false},
		{"valid ToolBudget passes", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{ToolBudget: &agentloop.ToolBudget{Reserve: func(ctx context.Context, calls int) error { return nil }}}
			return o
		}, nil, "", true},
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
		}, agentloop.ErrInvalidOptions, "HeartbeatInterval", false},
		{"positive HeartbeatInterval with Bus passes", func(o agentloop.Options) agentloop.Options {
			o.HeartbeatInterval = 5 * time.Millisecond
			o.Bus = events.New()
			return o
		}, nil, "", true},
		{"zero HeartbeatInterval with nil Bus passes", func(o agentloop.Options) agentloop.Options {
			o.HeartbeatInterval = 0
			return o
		}, nil, "", true},
		{"zero HeartbeatInterval with Bus set passes", func(o agentloop.Options) agentloop.Options {
			o.HeartbeatInterval = 0
			o.Bus = events.New()
			return o
		}, nil, "", true},
	}
	runValidateCases(t, cases)
}

// TestOptionsValidateHeartbeatOrder proves the Completer check runs
// before the HeartbeatInterval check: the earlier invalid field's name
// wins, even when HeartbeatInterval is also positive with a nil Bus.
func TestOptionsValidateHeartbeatOrder(t *testing.T) {
	o := validOptions()
	o.Completer = nil
	o.HeartbeatInterval = 5 * time.Millisecond
	err := o.Validate()
	if !errors.Is(err, agentloop.ErrInvalidOptions) {
		t.Fatalf("Validate() error = %v, want ErrInvalidOptions", err)
	}
	if !strings.Contains(err.Error(), "Completer") {
		t.Fatalf("Validate() error = %v, want it to name Completer (earlier in the fixed order)", err)
	}
	if strings.Contains(err.Error(), "HeartbeatInterval") {
		t.Fatalf("Validate() error = %v, want the earlier check to win over HeartbeatInterval", err)
	}
}

// TestOptionsValidateCompleterBeforeConclude proves nil Completer wins
// over negative ConcludeDeadline.
func TestOptionsValidateCompleterBeforeConclude(t *testing.T) {
	o := validOptions()
	o.Completer = nil
	o.Extensions = &agentloop.Extensions{Conclude: agentloop.Conclude{Deadline: -time.Second}}
	err := o.Validate()
	if !errors.Is(err, agentloop.ErrInvalidOptions) {
		t.Fatalf("Validate() error = %v, want ErrInvalidOptions", err)
	}
	if !strings.Contains(err.Error(), "Completer") {
		t.Fatalf("Validate() error = %v, want it to name Completer", err)
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
	err := withBadConclude.Validate()
	if !errors.Is(err, agentloop.ErrInvalidOptions) {
		t.Fatalf("Validate() error = %v, want ErrInvalidOptions once Extensions is set", err)
	}
	if !strings.Contains(err.Error(), "Margin") {
		t.Fatalf("Validate() error = %v, want it to name Margin", err)
	}
}

// TestOptionsValidateWrapsNestedSentinels proves the four checks that
// delegate to another package's Validate still answer to the package
// sentinel. Options.Validate documents one sentinel for every check,
// and these four returned only the inner one.
func TestOptionsValidateWrapsNestedSentinels(t *testing.T) {
	cases := []struct {
		name  string
		build func(agentloop.Options) agentloop.Options
		inner error
	}{
		{"negative Budget.MaxBytes", func(o agentloop.Options) agentloop.Options {
			o.Budget = &budget.Limits{MaxBytes: -1}
			return o
		}, budget.ErrInvalidOptions},
		{"zero Window.MaxTokens", func(o agentloop.Options) agentloop.Options {
			o.Compaction = agentloop.Compaction{Window: &plan.Window{MaxTokens: 0}}
			return o
		}, plan.ErrMaxTokensNotPositive},
		{"WorkBudget without Refund", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{WorkBudget: &agentloop.WorkBudget{
				Reserve: func(ctx context.Context, req provider.Request) error { return nil },
			}}
			return o
		}, agentloop.ErrIncompleteWorkBudget},
		{"ToolBudget without Reserve", func(o agentloop.Options) agentloop.Options {
			o.Extensions = &agentloop.Extensions{ToolBudget: &agentloop.ToolBudget{}}
			return o
		}, agentloop.ErrIncompleteToolBudget},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.build(validOptions()).Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want an error")
			}
			if !errors.Is(err, agentloop.ErrInvalidOptions) {
				t.Fatalf("Validate() = %v, want errors.Is ErrInvalidOptions", err)
			}
			if !errors.Is(err, c.inner) {
				t.Fatalf("Validate() = %v, want errors.Is the inner sentinel too", err)
			}
		})
	}
}
