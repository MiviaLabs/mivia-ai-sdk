package agentrun_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunTwoStepsWithTools builds a real two-step agent over real
// blocks. Step one runs a review tool; step two reads step one's
// artifact through PayloadOf. It asserts the recorded artifacts, the
// stored refs, and the thread verification.
func TestRunTwoStepsWithTools(t *testing.T) {
	ctx := context.Background()
	artifacts := &agentrun.Artifacts{}
	store := mustStore(t)

	plan := mustFlow(t, []flow.Step{
		{ID: "review", To: "reviewed", Payload: "seed1"},
		{ID: "ship", To: "shipped", Needs: []string{"review"}, PayloadFrom: agentrun.PayloadOf("review", artifacts)},
	}, nil)
	reg := tools.New()
	addTools(t, reg,
		prefixTool{name: "review", prefix: "reviewed:"},
		prefixTool{name: "ship", prefix: "shipped:"},
	)
	m := mustMachine(t, "queued",
		tr("queued", "reviewed", "run"),
		tr("reviewed", "shipped", "run"),
	)

	runner, err := agentrun.New(agentrun.Options{
		Agent:     mustAgent(t, plan),
		Machine:   m,
		Tools:     reg,
		Store:     store,
		Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	counter := &eventCounter{}
	runner.Bus().Subscribe(agent.MessageAckedEvent, counter.handler())
	runner.Bus().Subscribe(agent.ThreadVerifiedEvent, counter.handler())

	status, _, err := runner.Run(ctx, "thread-integration-1", machine.InOut{Input: "go"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "shipped" {
		t.Fatalf("status = %q, want %q", status, "shipped")
	}

	if v, ok := artifacts.Get("review"); !ok || v != "reviewed:seed1" {
		t.Errorf("artifact review = %q,%v want reviewed:seed1,true", v, ok)
	}
	if v, ok := artifacts.Get("ship"); !ok || v != "shipped:reviewed:seed1" {
		t.Errorf("artifact ship = %q,%v want shipped:reviewed:seed1,true", v, ok)
	}

	// Both results landed in the store under content-addressed refs.
	for _, want := range []string{"reviewed:seed1", "shipped:reviewed:seed1"} {
		ref := envelope.ContextRef(want)
		got, err := store.Get(ref)
		if err != nil {
			t.Fatalf("store.Get(%s): %v", ref, err)
		}
		if string(got) != want {
			t.Errorf("store ref %s = %q, want %q", ref, got, want)
		}
	}

	if got := counter.count(agent.MessageAckedEvent); got != 2 {
		t.Errorf("acked events = %d, want 2", got)
	}
	if got := counter.count(agent.ThreadVerifiedEvent); got != 1 {
		t.Errorf("thread verified events = %d, want 1", got)
	}
}

// TestRunNonTextResult proves a tool returning a non-string result
// fails the step with ErrResultNotText naming the tool.
func TestRunNonTextResult(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "t1", To: "resolved", Payload: "seed"}}, nil)
	reg := tools.New()
	addTools(t, reg, nonTextTool{name: "t1"})
	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: oneStepMachine(t),
		Tools:   reg,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, err = runner.Run(ctx, "thread-nt", machine.InOut{})
	if !errors.Is(err, agentrun.ErrResultNotText) {
		t.Fatalf("Run error = %v, want ErrResultNotText", err)
	}
}

// TestRunEmptyStringResult proves an empty-string tool result is a
// runtime fault from envelope.NewAck, not ErrResultNotText.
func TestRunEmptyStringResult(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "t1", To: "resolved", Payload: "seed"}}, nil)
	reg := tools.New()
	addTools(t, reg, emptyTool{})
	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: oneStepMachine(t),
		Tools:   reg,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, err = runner.Run(ctx, "thread-empty", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want a runtime fault")
	}
	if errors.Is(err, agentrun.ErrResultNotText) {
		t.Fatalf("Run error %v must not be ErrResultNotText", err)
	}
	if !stringsContains(err, "restatement") {
		t.Fatalf("Run error %q lacks a NewAck restatement fault", err)
	}
}

// emptyTool returns an empty-string result, which is a string but
// unacceptable as a NewAck restatement.
type emptyTool struct{}

// Name returns the tool's registry name.
func (emptyTool) Name() string { return "t1" }

// Run returns an empty string result.
func (emptyTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: ""}, nil
}

// TestRunRespectsRoom proves a Room stamps onto built messages without
// changing the happy path.
func TestRunRespectsRoom(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "t1", To: "resolved", Payload: "seed"}}, nil)
	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: oneStepMachine(t),
		Tools:   oneStepRegistry(t),
		Room:    "room-a",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-room", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("status = %q, want %q", status, "resolved")
	}
}

// stringsContains reports whether s appears in err's text.
func stringsContains(err error, s string) bool {
	return err != nil && strings.Contains(err.Error(), s)
}
