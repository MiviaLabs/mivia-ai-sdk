package agentloop_test

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// scriptedCompleter answers Chat with one response per call, in
// order, from responses; a matching non-nil entry in errs makes that
// call fail instead. No concrete model client ships in this SDK, so
// every test scripts its own.
type scriptedCompleter struct {
	mu        sync.Mutex
	responses []provider.Response
	errs      []error
	calls     int
	reqs      []provider.Request
}

func (s *scriptedCompleter) Name() string { return "scripted" }

func (s *scriptedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.calls
	s.calls++
	s.reqs = append(s.reqs, req)
	if idx < len(s.errs) && s.errs[idx] != nil {
		return provider.Response{}, s.errs[idx]
	}
	if idx >= len(s.responses) {
		return provider.Response{}, fmt.Errorf("scriptedCompleter: no response scripted for call %d", idx)
	}
	return s.responses[idx], nil
}

func (s *scriptedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("scriptedCompleter: ChatStream not supported")
}

func (s *scriptedCompleter) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *scriptedCompleter) lastRequest() provider.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs[len(s.reqs)-1]
}

// errBoom is a sentinel a fixture tool returns to prove an error
// propagates unchanged.
var errBoom = errors.New("agentloop_test: boom")

// schemaEchoTool implements tools.SchemaTool and tools.Tool. It
// decodes its raw argument bytes into a string InOut, records every
// call, and returns a fixed result or error.
type schemaEchoTool struct {
	mu        sync.Mutex
	name      string
	schema    []byte
	result    any
	runErr    error
	decodeErr error
	calls     int
}

func (t *schemaEchoTool) Name() string { return t.name }

func (t *schemaEchoTool) ParameterSchema() []byte { return t.schema }

func (t *schemaEchoTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	if t.decodeErr != nil {
		return tools.InOut{}, t.decodeErr
	}
	return tools.InOut{Value: string(raw)}, nil
}

func (t *schemaEchoTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	if t.runErr != nil {
		return tools.Out{}, t.runErr
	}
	return tools.Out{Value: t.result}, nil
}

func (t *schemaEchoTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// noSchemaTool implements only tools.Tool, no tools.SchemaTool.
type noSchemaTool struct{ name string }

func (t *noSchemaTool) Name() string { return t.name }

func (t *noSchemaTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ran"}, nil
}

// budgetedSchemaTool adds tools.ResultBudgetTool to schemaEchoTool.
type budgetedSchemaTool struct {
	schemaEchoTool
	maxBytes int
}

func (t *budgetedSchemaTool) MaxResultBytes() int { return t.maxBytes }

// isZeroMessage reports whether m is the zero provider.Message.
func isZeroMessage(m provider.Message) bool {
	return m.Role == "" && m.Content == "" && m.ToolCallID == "" && len(m.ToolCalls) == 0
}

// isZeroResult reports whether res is the zero agentloop.Result:
// hardFail's shape for a hard-fail cause that trips before any
// iteration completes.
func isZeroResult(res agentloop.Result) bool {
	return isZeroMessage(res.Final) && len(res.History) == 0 && res.Iterations == 0 &&
		res.Usage == (provider.Usage{}) && res.Stop == ""
}

// textMessage builds a provider.Message with the given role and
// content, and no tool-call fields.
func textMessage(role provider.Role, content string) provider.Message {
	return provider.Message{Role: role, Content: content}
}

// assistantToolCallMessage builds the assistant turn that requests
// calls.
func assistantToolCallMessage(calls ...provider.ToolCall) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: calls}
}

// toolCallResponse builds a provider.Response whose Message and
// top-level ToolCalls both carry calls: Run reads Response.ToolCalls
// to decide whether the turn requests a tool, and Message.ToolCalls
// to satisfy provider.Message.Validate on the stored history entry.
func toolCallResponse(calls ...provider.ToolCall) provider.Response {
	return provider.Response{Message: assistantToolCallMessage(calls...), ToolCalls: calls}
}
