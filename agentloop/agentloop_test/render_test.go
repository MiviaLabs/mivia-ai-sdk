package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// renderedContent runs a one-tool, one-iteration Loop and returns the
// resulting RoleTool message's Content, so render_test.go can assert
// the render order without reaching into unexported Loop internals.
func renderedContent(t *testing.T, tool tools.Tool) (string, error) {
	t.Helper()
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{
		responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call-1", Name: tool.Name(), Arguments: []byte("in")}),
			{Message: textMessage(provider.RoleAssistant, "done")},
		},
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		return "", err
	}
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			return m.Content, nil
		}
	}
	t.Fatalf("no RoleTool message found in history: %+v", res.History)
	return "", nil
}

func TestRenderString(t *testing.T) {
	tool := &schemaEchoTool{name: "t", schema: []byte(`{}`), result: "hello"}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if got != "hello" {
		t.Fatalf("content = %q, want hello", got)
	}
}

func TestRenderUTF8Bytes(t *testing.T) {
	tool := &schemaEchoTool{name: "t", schema: []byte(`{}`), result: []byte("bytes-result")}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if got != "bytes-result" {
		t.Fatalf("content = %q, want bytes-result", got)
	}
}

func TestRenderNilValue(t *testing.T) {
	tool := &schemaEchoTool{name: "t", schema: []byte(`{}`), result: nil}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestRenderJSONFallback(t *testing.T) {
	tool := &schemaEchoTool{name: "t", schema: []byte(`{}`), result: map[string]int{"n": 1}}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if got != `{"n":1}` {
		t.Fatalf("content = %q, want {\"n\":1}", got)
	}
}

func TestRenderJSONFallbackForInvalidUTF8Bytes(t *testing.T) {
	invalid := []byte{0xff, 0xfe, 0xfd}
	tool := &schemaEchoTool{name: "t", schema: []byte(`{}`), result: invalid}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	// json.Marshal base64-encodes a []byte value.
	if !strings.HasPrefix(got, `"`) {
		t.Fatalf("content = %q, want a JSON string (base64)", got)
	}
}

func TestRenderUnrenderableResult(t *testing.T) {
	tool := &schemaEchoTool{name: "t", schema: []byte(`{}`), result: make(chan int)}
	content, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil: ErrorPolicyReport reports the error as the tool result", err)
	}
	if !strings.Contains(content, "cannot be rendered") {
		t.Fatalf("content = %q, want it to carry the render error text", content)
	}
	// Re-run under ErrorPolicyFail to observe the sentinel directly.
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{
		responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call-1", Name: tool.Name(), Arguments: []byte("in")}),
		},
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrUnrenderableResult) {
		t.Fatalf("Run() error = %v, want ErrUnrenderableResult", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the assistant turn that requested the call already completed", res.Iterations)
	}
	if len(res.History) != 2 {
		t.Fatalf("History len = %d, want 2 (user + assistant turn): per hardFail's rule, the completed turn's state must travel", len(res.History))
	}
}

// TestRenderTruncatesAtMarkerBoundary covers the truncateContent
// branch where budget is at or under the marker's own length: the
// content is cut to budget bytes with no marker appended, since there
// is no room for one.
func TestRenderTruncatesAtMarkerBoundary(t *testing.T) {
	tool := &budgetedSchemaTool{
		schemaEchoTool: schemaEchoTool{name: "t", schema: []byte(`{}`), result: strings.Repeat("x", 100)},
		maxBytes:       5,
	}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if got != "xxxxx" {
		t.Fatalf("content = %q, want xxxxx (no room for a marker)", got)
	}
}

// TestRenderTruncatesAtExactMarkerLength covers the truncateContent
// boundary where budget equals the marker's own length exactly: the
// budget is large enough to hold the marker alone, so the result is
// the marker with no content bytes ahead of it.
func TestRenderTruncatesAtExactMarkerLength(t *testing.T) {
	tool := &budgetedSchemaTool{
		schemaEchoTool: schemaEchoTool{name: "t", schema: []byte(`{}`), result: strings.Repeat("x", 100)},
		maxBytes:       len("...[truncated]"),
	}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if got != "...[truncated]" {
		t.Fatalf("content = %q, want the marker alone", got)
	}
}

func TestRenderTruncatesOverBudget(t *testing.T) {
	tool := &budgetedSchemaTool{
		schemaEchoTool: schemaEchoTool{name: "t", schema: []byte(`{}`), result: strings.Repeat("x", 100)},
		maxBytes:       30,
	}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if len(got) > 30 {
		t.Fatalf("content len = %d, want at most 30 (the published budget)", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("content = %q, want a truncation marker", got)
	}
}

// TestRenderTruncationStaysValidUTF8 proves a budget that lands
// mid-rune drops the incomplete trailing bytes instead of emitting
// invalid UTF-8 to the model.
func TestRenderTruncationStaysValidUTF8(t *testing.T) {
	tool := &budgetedSchemaTool{
		schemaEchoTool: schemaEchoTool{name: "t", schema: []byte(`{}`), result: strings.Repeat("héllo wörld中文字 ", 20)},
		maxBytes:       9,
	}
	got, err := renderedContent(t, tool)
	if err != nil {
		t.Fatalf("renderedContent error = %v, want nil", err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("content = %q, want valid UTF-8", got)
	}
	if len(got) > 9 {
		t.Fatalf("content len = %d, want at most 9 (the published budget)", len(got))
	}
}
