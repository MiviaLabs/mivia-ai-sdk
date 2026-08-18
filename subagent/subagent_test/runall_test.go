package subagent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// gateRunner builds a runner whose tool signals it started, then
// waits for the release channel: RunAll members must truly overlap.
func gateRunner(t *testing.T, name string, started chan<- string, release <-chan struct{}) *agentrun.Runner {
	t.Helper()
	plan, err := flow.New([]flow.Step{{ID: "work", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(gateTool{name: "work", label: name, started: started, release: release}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	return runnerOver(t, plan, m, reg)
}

// gateTool signals started and blocks until release closes.
type gateTool struct {
	name    string
	label   string
	started chan<- string
	release <-chan struct{}
}

// Name returns the registry name.
func (g gateTool) Name() string { return g.name }

// Run signals start, waits for release, then succeeds.
func (g gateTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	g.started <- g.label
	<-g.release
	return tools.Out{Value: "ok"}, nil
}

// TestRunAllRunsMembersConcurrently proves every member overlaps:
// none can finish before all three started, so the join is real
// parallelism, not a serialized loop.
func TestRunAllRunsMembersConcurrently(t *testing.T) {
	ctx := context.Background()
	started := make(chan string, 3)
	release := make(chan struct{})
	specs := []subagent.Spec{
		{Name: "a", Runner: gateRunner(t, "a", started, release)},
		{Name: "b", Runner: gateRunner(t, "b", started, release)},
		{Name: "c", Runner: gateRunner(t, "c", started, release)},
	}
	done := make(chan []subagent.Result, 1)
	go func() {
		done <- subagent.RunAll(ctx, specs)
	}()
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		name := <-started
		seen[name] = true
	}
	close(release)
	results := <-done
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("result %d (%s): %v", i, r.Name, r.Err)
		}
		if r.Name != specs[i].Name || r.Status != "done" {
			t.Fatalf("result %d = %s,%q want %s,done", i, r.Name, r.Status, specs[i].Name)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("started set = %v, want all three", seen)
	}
}

// TestRunAllMemberErrorDoesNotCancelOthers proves one failing member
// leaves its siblings' results intact.
func TestRunAllMemberErrorDoesNotCancelOthers(t *testing.T) {
	ctx := context.Background()
	specs := []subagent.Spec{
		{Name: "ok1", Runner: prefixRunner(t, "one:", &agentrun.Artifacts{})},
		{Name: "bad", Runner: failingRunner(t, "member broke")},
		{Name: "ok2", Runner: prefixRunner(t, "two:", &agentrun.Artifacts{})},
	}
	results := subagent.RunAll(ctx, specs)
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].Err != nil || results[2].Err != nil {
		t.Fatalf("healthy members failed: %v, %v", results[0].Err, results[2].Err)
	}
	if results[1].Err == nil || !strings.Contains(results[1].Err.Error(), "member broke") {
		t.Fatalf("failing member err = %v, want its own error", results[1].Err)
	}
}
