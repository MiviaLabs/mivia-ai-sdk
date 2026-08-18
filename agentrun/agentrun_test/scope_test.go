package agentrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// privilegedDeniedTool returns success when RunScoped allows it, or
// ErrScopeDenied when the Scope denies it. It implements
// tools.PrivilegedTool, which makes RequireAllowlisted scope always
// deny unless the name appears in Allowlist.
type privilegedDeniedTool struct{ name string }

func (t privilegedDeniedTool) Name() string { return t.name }
func (t privilegedDeniedTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ok"}, nil
}
func (privilegedDeniedTool) Privileged() bool { return true }

// TestRunPrivilegedDeny proves a privileged tool that does not appear
// in any Scope Allowlist fails the step with ErrScopeDenied instead of
// running it.
func TestRunPrivilegedDeny(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "priv", To: "denied", Payload: "go"}}, nil)
	reg := tools.New()
	addTools(t, reg, privilegedDeniedTool{name: "priv"})
	m := mustMachine(t, "queued", tr("queued", "denied", "run"))

	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: m,
		Tools:   reg,
		Scope:   tools.NewScope(tools.ScopeOptions{}), // empty Allowlist, no names permitted
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-priv-deny", machine.InOut{})
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("Run error = %v, want ErrScopeDenied", err)
	}
}

// TestRunWithExtraDenylist proves an ExtraDenylist entry blocks a
// tool by name even when Allowlist is empty.
func TestRunWithExtraDenylist(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "deny", To: "done", Payload: "go"}}, nil)
	reg := tools.New()
	addTools(t, reg, privilegedDeniedTool{name: "deny"})
	m := mustMachine(t, "queued", tr("queued", "done", "run"))

	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: m,
		Tools:   reg,
		Scope: tools.NewScope(tools.ScopeOptions{
			ExtraDenylist: []string{"deny"},
		}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-extra-deny", machine.InOut{})
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("Run error = %v, want ErrScopeDenied", err)
	}
}

// TestRunPrivilegedAllowsPermitted proves a privileged tool whose name
// appears in Scope Allowlist succeeds through the chain.
func TestRunPrivilegedAllowsPermitted(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "priv", To: "done", Payload: "go"}}, nil)
	reg := tools.New()
	addTools(t, reg, privilegedDeniedTool{name: "priv"})
	m := mustMachine(t, "queued", tr("queued", "done", "run"))

	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: m,
		Tools:   reg,
		Scope:   tools.NewScope(tools.ScopeOptions{Allowlist: []string{"priv"}}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	status, _, err := runner.Run(ctx, "thread-priv-ok", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want %q", status, "done")
	}
}
