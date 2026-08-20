package agentloop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestDedupWithinTurnNumericLiteralFormsNotEqual proves 1 and 1.0 stay
// distinct json.Number strings under UseNumber(), so neither call is
// deduped against the other.
func TestDedupWithinTurnNumericLiteralFormsNotEqual(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"n":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"n":1.0}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if tool.callCount() != 2 {
		t.Fatalf("tool.callCount() = %d, want 2: 1 and 1.0 must not canonicalize equal", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got == agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want the real result, not DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnLargeIntegersNotEqual proves two large distinct
// integers that collide under naive float64 decoding, 2^53 and
// 2^53+1, stay distinct under UseNumber() and both run.
func TestDedupWithinTurnLargeIntegersNotEqual(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"id":9007199254740992}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"id":9007199254740993}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if tool.callCount() != 2 {
		t.Fatalf("tool.callCount() = %d, want 2: distinct large integers must not collide", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got == agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want the real result, not DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnDuplicateJSONKeyUsesLastValue proves raw JSON
// with a duplicate key canonicalizes using encoding/json's last-value
// behavior, and a second call matching that last value is deduped.
func TestDedupWithinTurnDuplicateJSONKeyUsesLastValue(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1,"a":2}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":2}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if tool.callCount() != 1 {
		t.Fatalf("tool.callCount() = %d, want 1: the duplicate-key call must canonicalize to {\"a\":2}", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnUnicodeEscapeFormsEqual proves two string values
// that differ only by Unicode-escape form decode to the same Go
// string, so the second call is deduped.
func TestDedupWithinTurnUnicodeEscapeFormsEqual(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"s":"café"}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"s":"café"}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if tool.callCount() != 1 {
		t.Fatalf("tool.callCount() = %d, want 1: café and caf\\u00e9 must decode to the same string", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnMalformedArgumentsFailOpen proves malformed
// Arguments make canonicalizeArgs fail: the call still runs, is never
// treated as a duplicate, is never recorded, and the turn does not
// fail.
func TestDedupWithinTurnMalformedArgumentsFailOpen(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{not json`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{not json`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: a canonicalization error must never fail the turn", err)
	}
	// Malformed Arguments fail schema.Compiled.Validate before
	// DecodeArguments runs, so each call reports its own
	// ErrArgumentValidation error, independently: neither is ever
	// deduped, since a canonicalization error excludes the call from
	// the dedup set.
	c1 := toolResultContent(t, res.History, "call-1")
	c2 := toolResultContent(t, res.History, "call-2")
	if c1 == agentloop.DuplicateCallNotice || c2 == agentloop.DuplicateCallNotice {
		t.Fatalf("call-1 = %q, call-2 = %q, want neither to be DuplicateCallNotice", c1, c2)
	}
	if !strings.HasPrefix(c1, agentloop.ToolErrorPrefix) || !strings.HasPrefix(c2, agentloop.ToolErrorPrefix) {
		t.Fatalf("call-1 = %q, call-2 = %q, want both to carry ToolErrorPrefix: each call must run and report its own error", c1, c2)
	}
}

// TestDedupWithinTurnTrailingDataFailOpen proves Arguments holding a
// valid JSON value followed by trailing bytes, or two concatenated
// JSON values, fails canonicalization: a second call carrying the
// same leading fragment but different trailing bytes is never falsely
// deduped, and both calls always run.
func TestDedupWithinTurnTrailingDataFailOpen(t *testing.T) {
	tests := []struct {
		name   string
		first  []byte
		second []byte
	}{
		{name: "trailing garbage", first: []byte(`{"a":1}garbage`), second: []byte(`{"a":1}garbage`)},
		{name: "concatenated JSON values", first: []byte(`{"a":1}{"b":2}`), second: []byte(`{"a":1}{"b":2}`)},
		{name: "trailing closing bracket", first: []byte(`{"a":1}]`), second: []byte(`{"a":1}]`)},
		{name: "trailing closing brace", first: []byte(`{"a":1}}`), second: []byte(`{"a":1}}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
			reg := tools.New()
			mustAdd(t, reg, tool)
			completer := &scriptedCompleter{responses: []provider.Response{
				toolCallResponse(
					provider.ToolCall{ID: "call-1", Name: "echo", Arguments: tc.first},
					provider.ToolCall{ID: "call-2", Name: "echo", Arguments: tc.second},
				),
				{Message: textMessage(provider.RoleAssistant, "final")},
			}}
			loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true})
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
			if err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			// Trailing-data Arguments also fail schema.Compiled.Validate
			// (a stricter, independent JSON-well-formedness check), so
			// each call reports its own error, never reaching
			// DecodeArguments; the assertion that matters here is that
			// neither call is falsely deduped against the other.
			c1 := toolResultContent(t, res.History, "call-1")
			c2 := toolResultContent(t, res.History, "call-2")
			if c1 == agentloop.DuplicateCallNotice || c2 == agentloop.DuplicateCallNotice {
				t.Fatalf("call-1 = %q, call-2 = %q, want neither to be DuplicateCallNotice", c1, c2)
			}
		})
	}
}
