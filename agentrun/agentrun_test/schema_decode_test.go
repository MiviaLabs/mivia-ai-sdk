package agentrun_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// schemaArgs is the typed argument shape schemaStubTool decodes into.
type schemaArgs struct {
	Path string `json:"path"`
}

// schemaStubTool implements tools.SchemaTool. Run asserts its input is
// the decoded schemaArgs value, not a raw string, and fails the call
// loudly when it is not.
type schemaStubTool struct{ name string }

// Name returns the tool's registry name.
func (t schemaStubTool) Name() string { return t.name }

// ParameterSchema returns a placeholder schema; its content does not
// matter to these tests.
func (t schemaStubTool) ParameterSchema() []byte { return []byte(`{"type":"object"}`) }

// DecodeArguments parses raw as JSON into schemaArgs.
func (t schemaStubTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	var a schemaArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: a}, nil
}

// Run requires in.Value to already be the decoded schemaArgs; a raw
// string input means chain skipped the decode step and fails the
// call.
func (t schemaStubTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	a, ok := in.Value.(schemaArgs)
	if !ok {
		return tools.Out{}, errors.New("schemaStubTool: input is not decoded schemaArgs")
	}
	return tools.Out{Value: "read:" + a.Path}, nil
}

// TestChainDecodesSchemaToolPayload proves chain decodes a
// tools.SchemaTool tool's payload through DecodeArguments before
// calling RunScoped, so Run receives the typed value, not the raw
// string payload.
func TestChainDecodesSchemaToolPayload(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "s1", To: "done", Payload: `{"path":"notes.txt"}`}}, nil)
	reg := tools.New()
	addTools(t, reg, schemaStubTool{name: "s1"})
	m := mustMachine(t, "queued", tr("queued", "done", "run"))

	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: m,
		Tools:   reg,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	status, _, err := runner.Run(ctx, "thread-schema-decode", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
}

// TestChainDecodeFailureWrapsErrArgumentDecode proves a malformed
// payload for a tools.SchemaTool tool surfaces a clean error
// satisfying errors.Is(err, agentrun.ErrArgumentDecode), not a panic
// and not the tool's own opaque error.
func TestChainDecodeFailureWrapsErrArgumentDecode(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "s1", To: "done", Payload: `not valid json`}}, nil)
	reg := tools.New()
	addTools(t, reg, schemaStubTool{name: "s1"})
	m := mustMachine(t, "queued", tr("queued", "done", "run"))

	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: m,
		Tools:   reg,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-schema-decode-fail", machine.InOut{})
	if !errors.Is(err, agentrun.ErrArgumentDecode) {
		t.Fatalf("Run error = %v, want ErrArgumentDecode", err)
	}
}

// countingSchemaTool implements tools.SchemaTool and records each
// decoded schemaArgs.Path it receives, in call order. It fails the
// call when its input is not the decoded schemaArgs, matching
// schemaStubTool's own contract.
type countingSchemaTool struct {
	name string
	got  *[]string
}

// Name returns the tool's registry name.
func (t countingSchemaTool) Name() string { return t.name }

// ParameterSchema returns a placeholder schema.
func (t countingSchemaTool) ParameterSchema() []byte { return []byte(`{"type":"object"}`) }

// DecodeArguments parses raw as JSON into schemaArgs.
func (t countingSchemaTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	var a schemaArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: a}, nil
}

// Run records the decoded path and fails loudly on an undecoded input.
func (t countingSchemaTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	a, ok := in.Value.(schemaArgs)
	if !ok {
		return tools.Out{}, errors.New("countingSchemaTool: input is not decoded schemaArgs")
	}
	*t.got = append(*t.got, a.Path)
	return tools.Out{Value: "read:" + a.Path}, nil
}

// TestChainDecodesSchemaToolPayloadEveryLoopIteration proves chain
// resolves the SchemaTool lookup by the tool's plain registry name,
// not the raw, "#N"-suffixed message ID a repeated step's later
// iterations carry. toolNameFor already strips the suffix for
// RunScoped; a chain that instead looked the SchemaTool up by the raw
// message ID would find nothing in the registry past the first
// iteration and silently skip the decode, which this test would catch
// through countingSchemaTool's undecoded-input failure.
func TestChainDecodesSchemaToolPayloadEveryLoopIteration(t *testing.T) {
	ctx := context.Background()
	child, err := flow.New([]flow.Step{
		{ID: "work", To: "done", Payload: `{"path":"notes.txt"}`},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	twice := func(ctx context.Context) (bool, error) {
		st, _ := flow.LoopStateFrom(ctx)
		return st.Iteration == 0, nil
	}
	plan := mustFlow(t, []flow.Step{
		{ID: "looper", Payload: "p", Sub: child, Loop: &flow.LoopPolicy{Guard: twice}},
	}, nil)
	m := mustMachine(t, "start", machine.Transition{From: "start", To: "done", Trigger: "t1"})

	var got []string
	reg := tools.New()
	addTools(t, reg, countingSchemaTool{name: "work", got: &got}, echoTool{name: "looper"})
	artifacts := &agentrun.Artifacts{}
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m, Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	status, _, err := runner.Run(ctx, "thread-schema-decode-loop", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
	if len(got) != 2 || got[0] != "notes.txt" || got[1] != "notes.txt" {
		t.Fatalf("decoded paths = %+v, want two decoded runs", got)
	}
	if v, ok := artifacts.Get("work"); !ok || v != "read:notes.txt" {
		t.Fatalf("artifact = %q, ok = %v, want read:notes.txt", v, ok)
	}
}

// TestChainLeavesNonSchemaToolPayloadUnchanged confirms a non-
// tools.SchemaTool tool still receives the plain string payload
// unchanged; the new decode branch never fires for it.
func TestChainLeavesNonSchemaToolPayloadUnchanged(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "t1", To: "resolved", Payload: "seed"}}, nil)
	reg := oneStepRegistry(t)
	m := oneStepMachine(t)

	art := &agentrun.Artifacts{}
	runner, err := agentrun.New(agentrun.Options{
		Agent:     mustAgent(t, plan),
		Machine:   m,
		Tools:     reg,
		Artifacts: art,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	status, _, err := runner.Run(ctx, "thread-plain-payload", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("status = %q, want resolved", status)
	}
	got, ok := art.Get("t1")
	if !ok {
		t.Fatal("Artifacts.Get(t1) = false, want a recorded run")
	}
	if got != "out:seed" {
		t.Fatalf("Artifacts.Get(t1) = %q, want out:seed", got)
	}
}
