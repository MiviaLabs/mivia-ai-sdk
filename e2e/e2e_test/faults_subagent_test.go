package e2e_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// stubCompleter is the concrete provider.Completer the fault kit needs
// to wrap. The faulting call never reaches it.
type stubCompleter struct{}

// Name returns a fixed provider label.
func (stubCompleter) Name() string { return "stub" }

// Chat returns one assistant reply.
func (stubCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: "ok"},
	}, nil
}

// ChatStream emits one done chunk over a closed channel.
func (stubCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Delta: "ok", Done: true}
	close(ch)
	return ch, nil
}

// trackedFanoutTool is one orchestrator step tool that runs every spec
// through RunAll and records every result. A member failure fails the
// step, so the run reports the failing spec.
type trackedFanoutTool struct {
	specs   []subagent.Spec
	results *[]subagent.Result
}

// Name returns the registry name.
func (f *trackedFanoutTool) Name() string { return "fanout" }

// Run spawns all specs and records the joined results.
func (f *trackedFanoutTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	results := subagent.RunAll(ctx, f.specs)
	*f.results = results
	for _, r := range results {
		if r.Err != nil {
			return tools.Out{}, r.Err
		}
	}
	return tools.Out{Value: "all-done"}, nil
}

// faultMemberRunner builds one one-step subagent runner over tool.
func faultMemberRunner(t *testing.T, name string, tool tools.Tool) *agentrun.Runner {
	t.Helper()
	plan, err := flow.New([]flow.Step{{ID: tool.Name(), To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New member %s: %v", name, err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New member %s: %v", name, err)
	}
	reg := tools.New()
	if err := reg.Add(tool); err != nil {
		t.Fatalf("registry.Add member %s: %v", name, err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, name, plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New member %s: %v", name, err)
	}
	return runner
}

// TestFaultMidFanoutReportsFailingSpecAndKeepsSiblings proves one of
// three subagents that dies mid-fanout reports its Err, while the two
// siblings still land.
func TestFaultMidFanoutReportsFailingSpecAndKeepsSiblings(t *testing.T) {
	ctx := context.Background()
	// beta is a model subagent whose provider faults on the first call.
	specs := []subagent.Spec{
		{Name: "alpha", Runner: faultMemberRunner(t, "alpha",
			e2e.PrefixTool{ToolName: "work", Prefix: "alpha"})},
		{Name: "beta", Runner: faultMemberRunner(t, "beta",
			subagent.ProviderTool("chat", &e2e.FaultCompleter{
				Completer: stubCompleter{}, FaultOn: 1,
			}))},
		{Name: "gamma", Runner: faultMemberRunner(t, "gamma",
			e2e.PrefixTool{ToolName: "work", Prefix: "gamma"})},
	}
	var results []subagent.Result
	plan, err := flow.New([]flow.Step{{ID: "fanout", To: "spawned", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New orchestrator: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "spawned", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New orchestrator: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(&trackedFanoutTool{specs: specs, results: &results}); err != nil {
		t.Fatalf("registry.Add fanout: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "fault-fanout-orchestrator", plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New orchestrator: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-fanout-fault", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the failing subagent's error")
	}
	if !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("Run error = %v, want e2e.ErrFault", err)
	}
	if len(results) != 3 {
		t.Fatalf("RunAll results = %d, want 3", len(results))
	}
	byName := map[string]subagent.Result{}
	for _, r := range results {
		byName[r.Name] = r
	}
	for _, name := range []string{"alpha", "gamma"} {
		r := byName[name]
		if r.Err != nil {
			t.Fatalf("sibling %s err = %v, want nil", name, r.Err)
		}
		if r.Status != "done" {
			t.Fatalf("sibling %s status = %q, want done", name, r.Status)
		}
	}
	if !errors.Is(byName["beta"].Err, e2e.ErrFault) {
		t.Fatalf("failing spec beta err = %v, want e2e.ErrFault", byName["beta"].Err)
	}
}

// TestFaultWaitAckResolverFailsRun proves the agentrun Wait decorator
// faults on the Nth ack and the run reports the fault.
func TestFaultWaitAckResolverFailsRun(t *testing.T) {
	ctx := context.Background()
	plan, err := flow.New([]flow.Step{{ID: "work", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	res := &e2e.FaultWait{FaultOn: 1, Inner: func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		return envelope.Ack{}, errors.New("underlying wait must not run")
	}}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "fault-wait-agent", plan), Machine: m, Wait: res.Wait,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-wait-fault", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the wait fault")
	}
	if !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("Run error = %v, want e2e.ErrFault", err)
	}
	if !strings.Contains(err.Error(), "wait") {
		t.Fatalf("Run error %q does not name the wait fault", err)
	}
}

// TestFaultCompleterSurfacesFaultOnNthCall covers the Name and stream
// paths the fanout scenario does not reach.
func TestFaultCompleterSurfacesFaultOnNthCall(t *testing.T) {
	ctx := context.Background()
	fc := &e2e.FaultCompleter{Completer: stubCompleter{}, FaultOn: 2}
	if got := fc.Name(); got != "fault-completer" {
		t.Fatalf("Name = %q, want fault-completer", got)
	}
	// Call 1 passes through Chat.
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}}
	if _, err := fc.Chat(ctx, req); err != nil {
		t.Fatalf("call 1 Chat = %v, want pass-through", err)
	}
	// Call 2 faults on ChatStream.
	if _, err := fc.ChatStream(ctx, req); !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("call 2 ChatStream = %v, want the fault", err)
	}
}
