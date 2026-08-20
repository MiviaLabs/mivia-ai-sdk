package agentloop_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
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

// optionsValidateCase is one TestOptionsValidate table row.
type optionsValidateCase struct {
	name    string
	mutate  func(agentloop.Options) agentloop.Options
	wantErr error // checked with errors.Is when non-nil
	wantOK  bool  // when true, Validate must return nil
}

// optionsValidateCases covers one case per invariant Validate claims.
var optionsValidateCases = []optionsValidateCase{
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
	{"negative ConcludeMargin fails", func(o agentloop.Options) agentloop.Options {
		o.ConcludeMargin = -1
		return o
	}, agentloop.ErrConcludeMargin, false},
	{"zero ConcludeMargin passes", func(o agentloop.Options) agentloop.Options {
		o.ConcludeMargin = 0
		return o
	}, nil, true},
	{"positive ConcludeMargin passes", func(o agentloop.Options) agentloop.Options {
		o.ConcludeMargin = 3
		return o
	}, nil, true},
}

// TestOptionsValidate runs optionsValidateCases.
func TestOptionsValidate(t *testing.T) {
	for _, c := range optionsValidateCases {
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
