package runconfig_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
	"github.com/MiviaLabs/mivia-ai-sdk/secretpath"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
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
	t.Run("negative budget via json", func(t *testing.T) {
		doc := replace(t, `"tools": ["grep"]`, `"options": {"budget": {"max_events": -1}}, "tools": ["grep"]`)
		d, err := runconfig.Load([]byte(doc))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if err := d.External.Add(stubTool{name: "grep"}); err != nil {
			t.Fatalf("External.Add: %v", err)
		}
		d.Options.Agent = agentOver(t, d)
		_, err = d.Runner()
		if err == nil || !strings.Contains(err.Error(), "budget") {
			t.Fatalf("err = %v, want a forwarded budget error", err)
		}
	})
}

// TestRunnerResolvesNewKindsStub checks each new Kind resolves its
// bound step to a stub tool without error, proving the binding, not
// the underlying tool's own behavior.
func TestRunnerResolvesNewKindsStub(t *testing.T) {
	cases := []struct {
		name string
		kind runconfig.Kind
	}{
		{"workspaceread", runconfig.WorkspaceReadKind},
		{"workspacewrite", runconfig.WorkspaceWriteKind},
		{"workspacelist", runconfig.WorkspaceListKind},
		{"workspacestat", runconfig.WorkspaceStatKind},
		{"diff", runconfig.DiffKind},
		{"astool", runconfig.AsToolKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadDoc(t, internalStepDoc(tc.name))
			blocks := runconfig.NewBlocks()
			blocks.Set(tc.kind, stubTool{name: "stub"})
			d.Blocks = blocks
			d.Options.Agent = agentOver(t, d)
			runner, err := d.Runner()
			if err != nil {
				t.Fatalf("Runner: %v", err)
			}
			if runner == nil {
				t.Fatal("Runner = nil, want a built Runner")
			}
		})
	}
}

// workspaceReadStepDoc names one step bound to WorkspaceReadKind, with
// a payload holding the JSON-string-encoded subagent.WorkspaceReadArgs
// form.
const workspaceReadStepDoc = `{
	"machine": {"initial": "queued", "transitions": [
		{"from": "queued", "to": "done", "trigger": "run"}
	]},
	"plan": {"steps": [{"id": "s1", "to": "done", "internal": "workspaceread",
		"payload": "{\"path\":\"notes.txt\"}"}]},
	"tools": []
}`

// TestRunnerResolvesWorkspaceReadReal proves WorkspaceReadKind composes
// with the real subagent and workspace types, driven end to end
// through a real Runner.Run: agentrun's chain decodes the step's
// JSON-string payload through the tool's own DecodeArguments before
// it calls Run, so the resolved WorkspaceReadTool reads the seeded
// file and the run reaches status done.
func TestRunnerResolvesWorkspaceReadReal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello workspace"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	deny, err := secretpath.NewMatcher([]string{"*.env", "*.pem"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	ft, err := subagent.OpenFileTools(subagent.FileToolOptions{Root: dir, Deny: deny})
	if err != nil {
		t.Fatalf("OpenFileTools: %v", err)
	}
	defer ft.Close()
	tool := subagent.WorkspaceReadTool("s1", ft, 65536)

	d := loadDoc(t, workspaceReadStepDoc)
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.WorkspaceReadKind, tool)
	d.Blocks = blocks
	d.Options.Agent = agentOver(t, d)
	art := &agentrun.Artifacts{}
	d.Options.Artifacts = art

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	if runner == nil {
		t.Fatal("Runner = nil, want a built Runner")
	}

	status, _, err := runner.Run(context.Background(), "thread-workspaceread", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
	artifact, ok := art.Get("s1")
	if !ok || artifact != "hello workspace" {
		t.Fatalf("artifact = %q, ok = %v, want the seeded file content", artifact, ok)
	}
}

// innerRunner builds a minimal *agentrun.Runner: a two-status machine,
// a one-step plan, and one registered tool matching the step ID.
func innerRunner(t *testing.T) *agentrun.Runner {
	t.Helper()
	m, err := machine.New("start", machine.Transition{From: "start", To: "done", Trigger: "go"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	plan, err := flow.New([]flow.Step{{ID: "only", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	a, err := agent.New(id, discovery.Card{Name: "inner", Capabilities: []string{"cap"}}, plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(stubTool{name: "only"}); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}
	r, err := agentrun.New(agentrun.Options{Agent: a, Machine: m, Tools: reg})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return r
}

// TestRunnerResolvesAsToolReal proves AsToolKind composes with a real
// nested *agentrun.Runner, driving it to completion through the full
// agent chain. Unlike the five file/diff tools' typed-argument
// contract, AsTool.Run accepts a plain string input, matching
// subagent.FlowTool, so it runs unchanged through agentrun's chain.
func TestRunnerResolvesAsToolReal(t *testing.T) {
	nested := innerRunner(t)
	asTool := subagent.AsTool("s1", nested, subagent.ToolOptions{})

	doc := `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "s1", "to": "done", "payload": "outer", "internal": "astool"}]},
		"tools": []
	}`
	d := loadDoc(t, doc)
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.AsToolKind, asTool)
	d.Blocks = blocks
	d.Options.Agent = agentOver(t, d)

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-astool", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
}
