package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// wiredCompleter answers one fixed reply and can fail. It backs the
// registry wiring scenarios.
type wiredCompleter struct {
	name  string
	reply string
	fail  bool
	calls *int
}

// Name returns the stub's name.
func (c *wiredCompleter) Name() string { return c.name }

// Chat answers the reply or fails.
func (c *wiredCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	if c.calls != nil {
		*c.calls++
	}
	if c.fail {
		return provider.Response{}, context.DeadlineExceeded
	}
	return provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: c.reply},
		Usage:   provider.Usage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12},
	}, nil
}

// ChatStream is unused by these scenarios.
func (c *wiredCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, fmt.Errorf("wired: streaming unused")
}

// wiredSubRunner builds the spawned runner: a one-step plan whose tool
// is ProviderRegistryTool over a primary that fails and a usage-wrapped
// backup that answers, both traced by tr.
func wiredSubRunner(t *testing.T, tr *trace.Tracer, acc *usage.Accumulator, session string, primaryCalls *int) *agentrun.Runner {
	t.Helper()
	wrapped, err := usage.WrapCompleter(session, acc, &wiredCompleter{name: "backup", reply: "backup says hi"})
	if err != nil {
		t.Fatalf("WrapCompleter: %v", err)
	}
	reg := providerregistry.New()
	if err := reg.Register("primary", &wiredCompleter{
		name: "primary", fail: true, calls: primaryCalls,
	}); err != nil {
		t.Fatalf("Register(primary): %v", err)
	}
	if err := reg.Register("backup", wrapped); err != nil {
		t.Fatalf("Register(backup): %v", err)
	}
	toolReg := tools.New()
	addTools(t, toolReg, subagent.ProviderRegistryTool("work", reg,
		[]string{"primary", "backup"}, func(error) bool { return true }))
	plan, err := flow.New([]flow.Step{{ID: "work", To: "done", Payload: "ask the model"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "wired-sub", plan), Machine: m,
		Tools: toolReg, Tracer: tr,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return runner
}

// TestWiredStackComposesAllFour proves the four newest blocks compose
// at the top: a traced orchestrator run whose hook observes its step,
// whose step spawns a traced subagent, whose internal registry tool
// falls back to a usage-wrapped provider. The span tree, the hook
// order, the fallback, and the session total all answer.
func TestWiredStackComposesAllFour(t *testing.T) {
	tr := trace.New()
	acc := usage.New()
	primaryCalls := 0

	hookReg := hooks.New()
	var order []string
	for point, name := range map[hooks.Point]string{
		hooks.PointPreTool: "pre", hooks.PointPostTool: "post", hooks.PointStop: "stop",
	} {
		if err := hookReg.Add(point, name, func(ctx context.Context, payload any) (bool, error) {
			order = append(order, name)
			return true, nil
		}); err != nil {
			t.Fatalf("hooks.Add(%s): %v", name, err)
		}
	}

	spawn := subagent.AsTool("dispatch",
		wiredSubRunner(t, tr, acc, "session-main", &primaryCalls),
		subagent.ToolOptions{Tracer: tr})
	reg := tools.New()
	addTools(t, reg, spawn)
	plan, err := flow.New([]flow.Step{{ID: "dispatch", To: "dispatched", Payload: "delegate"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "dispatched", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "wired-orchestrator", plan), Machine: m,
		Tools: reg, Hooks: hookReg, Tracer: tr,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-wired", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "dispatched" {
		t.Fatalf("status = %q, want %q", status, "dispatched")
	}
	assertWiredTree(t, tr)
	if len(order) != 3 || order[0] != "pre" || order[1] != "post" || order[2] != "stop" {
		t.Fatalf("hook order = %v, want pre, post, stop", order)
	}
	if primaryCalls != 1 {
		t.Fatalf("primary calls = %d, want 1 before the fallback", primaryCalls)
	}
	total, ok := acc.Total("session-main")
	if !ok || total.TotalTokens != 12 {
		t.Fatalf("usage total = %+v,%v, want the backup turn's 12 tokens", total, ok)
	}
}

// assertWiredTree pins the four-level span tree: the orchestrator's
// run root, its tool span, the spawn span, the sub-run's root, and
// the sub-run's model-tool span, each nested under the last.
func assertWiredTree(t *testing.T, tr *trace.Tracer) {
	t.Helper()
	spans := tr.Spans()
	if len(spans) != 5 {
		t.Fatalf("spans = %d, want 5: %+v", len(spans), spans)
	}
	// Disambiguate duplicated names by shape: the orchestrator's run
	// root parents nothing, the sub-run's root parents the spawn, and
	// each tool span parents its own run root.
	var root, subRoot, orchTool, spawn *trace.Span
	for _, s := range spans {
		switch {
		case s.Name == "agentrun.run" && s.ParentID == 0:
			root = s
		case s.Name == "agentrun.run":
			subRoot = s
		case s.Name == "subagent.spawn":
			spawn = s
		case s.Name == "agentrun.tool" && root != nil && s.ParentID == root.ID:
			orchTool = s
		}
	}
	if root == nil || subRoot == nil || orchTool == nil || spawn == nil {
		t.Fatalf("tree shapes missing: %+v", spans)
	}
	for _, link := range []struct{ child, parent *trace.Span }{
		{orchTool, root}, {spawn, orchTool}, {subRoot, spawn},
	} {
		if link.child.ParentID != link.parent.ID {
			t.Errorf("%s parent = %d, want %d (%s)", link.child.Name, link.child.ParentID, link.parent.ID, link.parent.Name)
		}
		if link.child.EndTime().IsZero() {
			t.Errorf("%s span never ended", link.child.Name)
		}
	}
	if attr, ok := root.Attributes()["thread"]; !ok || attr != "thread-wired" {
		t.Errorf("root thread attribute = %q,%v", attr, ok)
	}
	if !strings.HasPrefix(subRoot.Attributes()["thread"], "dispatch-") {
		t.Errorf("sub-run thread attribute = %q, want the spawn thread", subRoot.Attributes()["thread"])
	}
}
