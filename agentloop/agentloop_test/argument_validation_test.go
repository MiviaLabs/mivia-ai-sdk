package agentloop_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// requiredFieldSchema requires an "x" property, so an empty-object
// call fails schema validation and a call carrying "x" passes.
const requiredFieldSchema = `{"type":"object","required":["x"],"properties":{"x":{"type":"string"}}}`

// malformedSchema is not a legal JSON Schema: "type" must be a string
// or array of strings, not a number.
const malformedSchema = `{"type":1}`

// TestDecodeAndRunArgumentValidationFailFails proves a call missing a
// schema-required field fails with ErrArgumentValidation under
// ErrorPolicyFail, before the tool's DecodeArguments ever runs.
func TestDecodeAndRunArgumentValidationFailFails(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(requiredFieldSchema), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrArgumentValidation) {
		t.Fatalf("Run() error = %v, want ErrArgumentValidation", err)
	}
	if tool.decodeCallCount() != 0 {
		t.Fatalf("decodeCallCount = %d, want 0: an argument-validation failure must never reach DecodeArguments", tool.decodeCallCount())
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the assistant turn that requested the call already completed", res.Iterations)
	}
}

// TestDecodeAndRunArgumentValidationReportsCorrective proves a call
// missing a schema-required field reports a ToolErrorPrefix-marked,
// schema.Corrective-shaped message under ErrorPolicyReport, not a raw
// Go error string.
func TestDecodeAndRunArgumentValidationReportsCorrective(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(requiredFieldSchema), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	content := findToolContent(t, res.History, "call-1")
	if !strings.HasPrefix(content, agentloop.ToolErrorPrefix) {
		t.Fatalf("content = %q, want it to start with %q", content, agentloop.ToolErrorPrefix)
	}
	corrective := strings.TrimPrefix(content, agentloop.ToolErrorPrefix)
	if corrective == "" || strings.Contains(corrective, "agentloop:") {
		t.Fatalf("content = %q, want a bounded schema.Corrective message, not a raw Go error string", content)
	}
	if tool.decodeCallCount() != 0 {
		t.Fatalf("decodeCallCount = %d, want 0: an argument-validation failure must never reach DecodeArguments", tool.decodeCallCount())
	}
}

// TestDecodeAndRunArgumentValidationPassesReachesDecode proves a call
// whose Arguments satisfy the schema reaches DecodeArguments and runs
// the tool normally.
func TestDecodeAndRunArgumentValidationPassesReachesDecode(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(requiredFieldSchema), result: "ran"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"x":"y"}`)}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if tool.decodeCallCount() != 1 {
		t.Fatalf("decodeCallCount = %d, want 1: a schema-passing call must reach DecodeArguments", tool.decodeCallCount())
	}
	if tool.callCount() != 1 {
		t.Fatalf("callCount = %d, want 1: a schema-passing call must run the tool", tool.callCount())
	}
	content := findToolContent(t, res.History, "call-1")
	if content != "ran" {
		t.Fatalf("content = %q, want ran", content)
	}
}

// TestNewMalformedSchemaFails proves a SchemaTool whose
// ParameterSchema() does not compile fails New with ErrInvalidSchema,
// before any Run call.
func TestNewMalformedSchemaFails(t *testing.T) {
	tool := &schemaEchoTool{name: "bad", schema: []byte(malformedSchema), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{}
	_, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if !errors.Is(err, agentloop.ErrInvalidSchema) {
		t.Fatalf("New() error = %v, want ErrInvalidSchema", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("Chat call count = %d, want 0: New must fail before any Run call", completer.callCount())
	}
}

// TestNewCompileLoopIsScoped proves the compile loop that fails New
// with ErrInvalidSchema for a wide (unscoped) Loop over a registry
// carrying a malformed schema does not fail a second, narrower-scoped
// Loop built over the same shared *tools.Registry once that Scope
// excludes the malformed tool.
func TestNewCompileLoopIsScoped(t *testing.T) {
	good := &schemaEchoTool{name: "good", schema: []byte(`{}`), result: "x"}
	bad := &schemaEchoTool{name: "bad", schema: []byte(malformedSchema), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, good)
	mustAdd(t, reg, bad)

	_, err := agentloop.New(agentloop.Options{Completer: &scriptedCompleter{}, Tools: reg, MaxIterations: 5})
	if !errors.Is(err, agentloop.ErrInvalidSchema) {
		t.Fatalf("wide New() error = %v, want ErrInvalidSchema", err)
	}

	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"good"}})
	loop, err := agentloop.New(agentloop.Options{Completer: &scriptedCompleter{}, Tools: reg, Scope: scope, MaxIterations: 5})
	if err != nil {
		t.Fatalf("narrow New() error = %v, want nil: the compile loop must be scoped, not registry-wide", err)
	}
	if loop == nil {
		t.Fatalf("narrow New() loop = nil, want a *Loop")
	}
}

// TestDecodeAndRunScopeDeniedBeforeSchemaValidate proves a call naming
// a tool outside Scope but present in the registry fails with
// tools.ErrScopeDenied, before DecodeArguments or schema.Validate ever
// run: decodeAndRun's own Scope check is the defense-in-depth gate for
// a compile loop that no longer covers the full registry.
func TestDecodeAndRunScopeDeniedBeforeSchemaValidate(t *testing.T) {
	allowed := &schemaEchoTool{name: "allowed", schema: []byte(`{}`), result: "x"}
	denied := &schemaEchoTool{name: "denied", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, allowed)
	mustAdd(t, reg, denied)
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"allowed"}})
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "denied", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, Scope: scope, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("Run() error = %v, want tools.ErrScopeDenied", err)
	}
	if denied.decodeCallCount() != 0 {
		t.Fatalf("denied.decodeCallCount() = %d, want 0: a scope-denied tool's decoder must never see model-supplied bytes", denied.decodeCallCount())
	}
}

// TestDecodeAndRunOversizedArgumentsFailsAdmission proves Arguments
// over schema.MaxPayloadBytes fails ErrArgumentValidation wrapping
// schema.ErrAdmission, without ever calling DecodeArguments.
func TestDecodeAndRunOversizedArgumentsFailsAdmission(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	oversized := bytes.Repeat([]byte("1"), schema.MaxPayloadBytes+1)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: oversized}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrArgumentValidation) {
		t.Fatalf("Run() error = %v, want ErrArgumentValidation", err)
	}
	if !errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Run() error = %v, want it to wrap schema.ErrAdmission", err)
	}
	if tool.decodeCallCount() != 0 {
		t.Fatalf("decodeCallCount = %d, want 0: an oversized payload must never reach DecodeArguments", tool.decodeCallCount())
	}
}

// TestRunConcurrentSchemaValidatedCalls proves N goroutines calling
// Run concurrently on one shared *Loop, each driving a shared,
// concurrency-safe scripted Completer through several
// schema-validated tool calls, resolve without a panic or a race: the
// immutable, New-time schemas cache and schema.Compiled.Validate's own
// documented concurrent-use safety hold under concurrent Run calls.
// Every scripted response is identical and always requests a
// schema-passing tool call, so the run always ends at
// StopMaxIterations regardless of which goroutine consumes which
// response. Run with -race.
func TestRunConcurrentSchemaValidatedCalls(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(requiredFieldSchema), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)

	const goroutines = 8
	const iterations = 3
	responses := make([]provider.Response, 0, goroutines*iterations)
	for i := 0; i < goroutines*iterations; i++ {
		responses = append(responses, toolCallResponse(provider.ToolCall{ID: "call", Name: "echo", Arguments: []byte(`{"x":"y"}`)}))
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: iterations})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
			if err != nil {
				t.Errorf("Run() error = %v, want nil", err)
				return
			}
			if res.Stop != agentloop.StopMaxIterations {
				t.Errorf("Stop = %v, want StopMaxIterations", res.Stop)
			}
		}()
	}
	wg.Wait()
}

// findToolContent returns the RoleTool message Content for
// toolCallID, failing the test when none exists.
func findToolContent(t *testing.T, history []provider.Message, toolCallID string) string {
	t.Helper()
	for _, m := range history {
		if m.Role == provider.RoleTool && m.ToolCallID == toolCallID {
			return m.Content
		}
	}
	t.Fatalf("no RoleTool message with ToolCallID %s in history: %+v", toolCallID, history)
	return ""
}
