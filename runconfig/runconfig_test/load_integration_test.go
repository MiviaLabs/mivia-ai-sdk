package runconfig_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
	"github.com/MiviaLabs/mivia-ai-sdk/secretpath"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// goldenDoc is the golden document: one external tool, one flow
// internal tool, a two-row machine, and string options.
const goldenDoc = `{
	"machine": {"initial": "queued", "transitions": [
		{"from": "queued", "to": "mid", "trigger": "run"},
		{"from": "mid", "to": "done", "trigger": "finish"}
	]},
	"plan": {"steps": [
		{"id": "first", "to": "mid", "payload": "seed", "tool": "grep"},
		{"id": "second", "needs": ["first"], "to": "done", "payload": "p2", "internal": "flow"}
	]},
	"options": {"room": "platform-team"},
	"tools": ["grep"]
}`

// TestGoldenDocumentRuns loads the golden document, composes the
// caller-side agent, and runs it end to end.
func TestGoldenDocumentRuns(t *testing.T) {
	d := loadDoc(t, goldenDoc)
	if d.Options.Room != "platform-team" {
		t.Fatalf("room = %q", d.Options.Room)
	}

	ctx := context.Background()
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.FlowKind, subagent.FlowTool("flow-inner", innerPlan(t), innerMachine(t), nil))
	if err := d.External.Add(stubTool{name: "grep"}); err != nil {
		t.Fatalf("External.Add: %v", err)
	}

	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	card := discovery.Card{Name: "golden-agent", Capabilities: []string{"cap"}}
	a, err := agent.New(id, card, d.Plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	d.Blocks = blocks
	d.Options.Agent = a

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-golden", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
}

// innerPlan builds the child plan the flow internal tool drives.
func innerPlan(t *testing.T) *flow.Definition {
	t.Helper()
	p, err := flow.New([]flow.Step{
		{ID: "inner", To: "inner-done", Payload: "p"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	return p
}

// innerMachine builds the child machine the flow internal tool
// drives.
func innerMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("inner-queued",
		machine.Transition{From: "inner-queued", To: "inner-done", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// workspaceReadGoldenDoc names a document naming a workspace tool: one
// step bound to WorkspaceReadKind, alongside a room option, matching
// goldenDoc's shape.
const workspaceReadGoldenDoc = `{
	"machine": {"initial": "queued", "transitions": [
		{"from": "queued", "to": "done", "trigger": "run"}
	]},
	"plan": {"steps": [{"id": "read-notes", "to": "done", "internal": "workspaceread"}]},
	"options": {"room": "platform-team"},
	"tools": []
}`

// TestGoldenDocumentWorkspaceReadBuildsRunnableResolver loads a
// document naming a workspace tool and proves it builds a runnable
// resolver: Definition.Runner wires the real
// subagent.WorkspaceReadTool, over a real, t.TempDir()-backed
// *subagent.FileTools, into a valid *agentrun.Runner. See
// runner_test.go's TestRunnerResolvesWorkspaceReadReal for the
// end-to-end proof that the bound step also drives through a real
// Runner.Run, decoding its JSON-string payload through the tool's own
// DecodeArguments.
func TestGoldenDocumentWorkspaceReadBuildsRunnableResolver(t *testing.T) {
	d := loadDoc(t, workspaceReadGoldenDoc)
	if d.Options.Room != "platform-team" {
		t.Fatalf("room = %q", d.Options.Room)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("seeded content"), 0o644); err != nil {
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
	tool := subagent.WorkspaceReadTool("read-notes", ft, 65536)

	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.WorkspaceReadKind, tool)
	d.Blocks = blocks
	d.Options.Agent = agentOver(t, d)

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	if runner == nil {
		t.Fatal("Runner = nil, want a built, runnable resolver")
	}

	schemaTool, ok := tool.(tools.SchemaTool)
	if !ok {
		t.Fatal("tool does not implement tools.SchemaTool")
	}
	args, err := schemaTool.DecodeArguments([]byte(`{"path":"notes.txt"}`))
	if err != nil {
		t.Fatalf("DecodeArguments: %v", err)
	}
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "seeded content" {
		t.Fatalf("Value = %q, want the seeded content unchanged", out.Value)
	}
}

// TestBudgetGatesDeclaredPayloadSize proves the true invariant
// options.budget gates: the cumulative byte total of every step's own
// declared payload, checked before the bound tool runs, independent
// of the tool's own output or behavior. See agent/run.go:80-93's doc
// comment and confirmStep (agent/run.go:153-183).
func TestBudgetGatesDeclaredPayloadSize(t *testing.T) {
	t.Run("rejects an oversized declared payload", func(t *testing.T) {
		doc := `{
			"machine": {"initial": "q", "transitions": [
				{"from": "q", "to": "d", "trigger": "r"}
			]},
			"plan": {"steps": [{"id": "s", "to": "d",
				"payload": "this declared payload is deliberately far larger than the budget cap",
				"tool": "stubTool"}]},
			"options": {"budget": {"max_bytes": 5}},
			"tools": ["stubTool"]
		}`
		d := loadDoc(t, doc)
		if err := d.External.Add(stubTool{name: "stubTool"}); err != nil {
			t.Fatalf("External.Add: %v", err)
		}
		d.Options.Agent = agentOver(t, d)

		runner, err := d.Runner()
		if err != nil {
			t.Fatalf("Runner: %v", err)
		}
		_, _, err = runner.Run(context.Background(), "thread-budget", machine.InOut{})
		if !errors.Is(err, agent.ErrOverBudget) {
			t.Fatalf("err = %v, want agent.ErrOverBudget", err)
		}
	})

	// A budget well above the run's total payload size composes with
	// a bound internal Kind without the tool's own output size
	// affecting the outcome either way. AsToolKind is a witness that a
	// no-decode Kind still runs unchanged: AsTool.Run accepts a plain
	// string input, matching subagent.FlowTool, so it runs through
	// agentrun's chain with no schema decode.
	t.Run("kind binding composes with a permissive budget", func(t *testing.T) {
		nested := innerRunner(t)
		asTool := subagent.AsTool("s", nested, subagent.ToolOptions{})

		doc := `{
			"machine": {"initial": "q", "transitions": [
				{"from": "q", "to": "d", "trigger": "r"}
			]},
			"plan": {"steps": [{"id": "s", "to": "d", "payload": "small",
				"internal": "astool"}]},
			"options": {"budget": {"max_bytes": 100000}},
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
		status, _, err := runner.Run(context.Background(), "thread-budget-ok", machine.InOut{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if status != "d" {
			t.Fatalf("status = %q, want d", status)
		}
	})

	// A second subtest proves the budget check and the schema decode
	// compose without conflict, using WorkspaceReadKind now that the
	// schema-decode path drives it through a real Runner.Run.
	t.Run("kind binding composes with a permissive budget and schema decode", budgetGatesWorkspaceReadSubtest)
}

// budgetGatesWorkspaceReadSubtest is
// TestBudgetGatesDeclaredPayloadSize's second subtest, split into its
// own function to stay under the per-function line limit.
func budgetGatesWorkspaceReadSubtest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("budget-ok content"), 0o644); err != nil {
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
	tool := subagent.WorkspaceReadTool("s", ft, 65536)

	doc := `{
		"machine": {"initial": "q", "transitions": [
			{"from": "q", "to": "d", "trigger": "r"}
		]},
		"plan": {"steps": [{"id": "s", "to": "d",
			"payload": "{\"path\":\"notes.txt\"}",
			"internal": "workspaceread"}]},
		"options": {"budget": {"max_bytes": 100000}},
		"tools": []
	}`
	d := loadDoc(t, doc)
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.WorkspaceReadKind, tool)
	d.Blocks = blocks
	d.Options.Agent = agentOver(t, d)

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-budget-schema-ok", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "d" {
		t.Fatalf("status = %q, want d", status)
	}
}
