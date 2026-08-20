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
