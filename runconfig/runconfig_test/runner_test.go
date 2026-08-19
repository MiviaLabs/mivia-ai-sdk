package runconfig_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// stubTool is an external tool the runner tests register by name.
type stubTool struct{ name string }

// Name returns the registry name.
func (s stubTool) Name() string { return s.name }

// Run returns its string input payload unchanged.
func (s stubTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	v, _ := in.Value.(string)
	return tools.Out{Value: v}, nil
}

// loadForRunner loads oneStepDoc with a caller-registered external
// tool named grep.
func loadForRunner(t *testing.T, register bool) *runconfig.Definition {
	t.Helper()
	d := loadDoc(t, oneStepDoc("grep"))
	if register {
		if err := d.External.Add(stubTool{name: "grep"}); err != nil {
			t.Fatalf("External.Add: %v", err)
		}
	}
	return d
}

// agentOver builds an Agent over d's loaded plan.
func agentOver(t *testing.T, d *runconfig.Definition) *agent.Agent {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	a, err := agent.New(id, discovery.Card{Name: "runner-test", Capabilities: []string{"cap"}}, d.Plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	return a
}

// TestRunnerResolves tests Runner's binding resolution.
func TestRunnerResolves(t *testing.T) {
	t.Run("unknown external tool", func(t *testing.T) {
		d := loadForRunner(t, false)
		_, err := d.Runner()
		if !errors.Is(err, runconfig.ErrUnknownTool) {
			t.Fatalf("err = %v, want ErrUnknownTool", err)
		}
	})
	t.Run("unknown internal kind", func(t *testing.T) {
		d := loadDoc(t, `{
			"machine": {"initial": "q", "transitions": [
				{"from": "q", "to": "d", "trigger": "r"}
			]},
			"plan": {"steps": [{"id": "s", "to": "d", "internal": "memory"}]},
			"tools": []
		}`)
		_, err := d.Runner()
		if !errors.Is(err, runconfig.ErrUnknownInternal) {
			t.Fatalf("err = %v, want ErrUnknownInternal", err)
		}
	})
	t.Run("nil agent", func(t *testing.T) {
		d := loadForRunner(t, true)
		_, err := d.Runner()
		if !errors.Is(err, agentrun.ErrNoAgent) {
			t.Fatalf("err = %v, want agentrun.ErrNoAgent", err)
		}
	})
	t.Run("bad budget", func(t *testing.T) {
		d := loadForRunner(t, true)
		d.Options.Agent = agentOver(t, d)
		d.Options.Budget = &contextbudget.Limits{MaxBytes: -1}
		_, err := d.Runner()
		if err == nil || !strings.Contains(err.Error(), "budget") {
			t.Fatalf("err = %v, want a forwarded budget error", err)
		}
	})
}
