package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// TestDedupWithinTurnFalseRunsBothCalls proves DedupWithinTurn false
// runs two identical calls in one turn twice, unchanged from the base
// plan.
func TestDedupWithinTurnFalseRunsBothCalls(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
		),
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
	if tool.callCount() != 2 {
		t.Fatalf("tool.callCount() = %d, want 2: DedupWithinTurn false must run both calls", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got == agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want the real result, not DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnByteEqualArguments proves a second call with the
// same name and byte-equal Arguments is deduped: the underlying tool
// runs once, and the second call's RoleTool message carries
// DuplicateCallNotice with the duplicate call's own ToolCallID.
func TestDedupWithinTurnByteEqualArguments(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
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
		t.Fatalf("tool.callCount() = %d, want 1: the second identical call must never reach RunScoped", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-1"); got != "x" {
		t.Fatalf("call-1 content = %q, want the real result %q", got, "x")
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnKeyOrderIndependent proves two calls whose
// Arguments hold the same JSON object with a different key order both
// compare equal under canonicalization, and the second is deduped.
func TestDedupWithinTurnKeyOrderIndependent(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1,"b":2}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"b":2,"a":1}`)},
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
		t.Fatalf("tool.callCount() = %d, want 1: a different key order must still canonicalize equal", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnDifferentArgumentsBothRun proves two calls with
// the same tool name but genuinely different Arguments both run;
// neither is treated as a duplicate.
func TestDedupWithinTurnDifferentArgumentsBothRun(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
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
	if tool.callCount() != 2 {
		t.Fatalf("tool.callCount() = %d, want 2: genuinely different arguments must never be deduped", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got == agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want the real result, not DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnDifferentToolNamesSameArgumentsBothRun proves two
// calls with byte-equal Arguments but different tool names are never
// deduped against each other: dedupKey carries the tool name, so the
// pair's keys differ even though their canonicalized args match.
func TestDedupWithinTurnDifferentToolNamesSameArgumentsBothRun(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "r1"}
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "r2"}
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "first", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "second", Arguments: []byte(`{"a":1}`)},
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
	if first.callCount() != 1 || second.callCount() != 1 {
		t.Fatalf("call counts = first:%d second:%d, want 1,1: a different tool name must never be deduped against another tool's identical arguments", first.callCount(), second.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got != "r2" {
		t.Fatalf("call-2 content = %q, want the real result %q, not DuplicateCallNotice", got, "r2")
	}
}

// TestDedupWithinTurnCarriesDuplicateCallsOwnToolCallID proves the
// synthesized RoleTool message for a deduped call carries the
// duplicate call's own ToolCallID, not the first call's.
func TestDedupWithinTurnCarriesDuplicateCallsOwnToolCallID(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
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
	found := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.Content == agentloop.DuplicateCallNotice {
			found = true
			if m.ToolCallID != "call-2" {
				t.Fatalf("deduped message ToolCallID = %q, want %q, the duplicate call's own ID", m.ToolCallID, "call-2")
			}
		}
	}
	if !found {
		t.Fatalf("no DuplicateCallNotice message in history: %+v", res.History)
	}
}

// TestDedupWithinTurnHooksFireOnceForPair proves PointPreTool and
// PointPostTool each fire exactly once for a turn with two identical
// calls, on the first call only; the deduped second call never
// triggers either hook point.
func TestDedupWithinTurnHooksFireOnceForPair(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	var preCount, postCount int
	if err := hreg.Add(hooks.PointPreTool, "count", func(ctx context.Context, payload any) (bool, error) {
		preCount++
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	if err := hreg.Add(hooks.PointPostTool, "count", func(ctx context.Context, payload any) (bool, error) {
		postCount++
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if preCount != 1 {
		t.Fatalf("PointPreTool fired %d times, want 1", preCount)
	}
	if postCount != 1 {
		t.Fatalf("PointPostTool fired %d times, want 1", postCount)
	}
}

// TestDedupWithinTurnPreToolVetoStopsBeforeSecondCall proves a
// PointPreTool veto on the first of two identical calls stops the
// turn before the second call is ever reached: the second call is
// never evaluated for dedup, since the veto ends the turn first.
func TestDedupWithinTurnPreToolVetoStopsBeforeSecondCall(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "veto", func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
		),
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg, DedupWithinTurn: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopHookVeto {
		t.Fatalf("Stop = %v, want StopHookVeto", res.Stop)
	}
	if tool.callCount() != 0 {
		t.Fatalf("tool.callCount() = %d, want 0: a veto on the first call must stop the turn", tool.callCount())
	}
	for _, m := range res.History {
		if m.ToolCallID == "call-2" {
			t.Fatalf("history carries a message for call-2: %+v, want the veto to stop the turn first", m)
		}
	}
}

// TestDedupWithinTurnResetsBetweenIterations proves the dedup set
// resets between iterations: a call repeated on a later iteration
// runs again, proving no cross-turn dedup happens.
func TestDedupWithinTurnResetsBetweenIterations(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)}),
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
		t.Fatalf("tool.callCount() = %d, want 2: an identical call on a later iteration must run again", tool.callCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got == agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want the real result, not DuplicateCallNotice: dedup must not cross iterations", got)
	}
}

// TestDedupWithinTurnAuditRecordNilErr proves the AuditKindToolCall
// record for a deduped call carries a nil Err, since a served
// duplicate is not a tool-run error.
func TestDedupWithinTurnAuditRecordNilErr(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	rec := findToolCallRecord(t, auditor.snapshot(), "call-2")
	if rec.Err != nil {
		t.Fatalf("rec.Err = %v, want nil: a served duplicate is not a tool-run error", rec.Err)
	}
	if rec.ToolResult.Content != agentloop.DuplicateCallNotice {
		t.Fatalf("rec.ToolResult.Content = %q, want DuplicateCallNotice", rec.ToolResult.Content)
	}
}

// TestDedupWithinTurnErroredCallSeedsDedupSet proves a first call that
// resolves as an ErrorPolicyReport-appended RoleTool error message,
// without the underlying tool running, still seeds the dedup set: a
// byte-identical retry of that same call later in the same turn is
// deduped and serves DuplicateCallNotice.
func TestDedupWithinTurnErroredCallSeedsDedupSet(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), decodeErr: errBoom}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
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
	if tool.decodeCallCount() != 1 {
		t.Fatalf("tool.decodeCallCount() = %d, want 1: the errored first call must seed the dedup set", tool.decodeCallCount())
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want DuplicateCallNotice", got)
	}
}

// TestDedupWithinTurnAuditErrorOnDuplicateFailsRun proves an Audit
// error on the deduped call's own AuditKindToolCall record fails Run,
// exactly like an audit error on a normally-run call: the
// DuplicateCallNotice message reaches history before the audit call,
// so it survives in the returned partial Result.
func TestDedupWithinTurnAuditErrorOnDuplicateFailsRun(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
		),
	}}
	auditFn := func(ctx context.Context, rec agentloop.AuditRecord) error {
		if rec.Kind == agentloop.AuditKindToolCall && rec.ToolResult.Content == agentloop.DuplicateCallNotice {
			return errAudit
		}
		return nil
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true, Audit: auditFn,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, errAudit) {
		t.Fatalf("Run() error = %v, want errAudit", err)
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.DuplicateCallNotice {
		t.Fatalf("call-2 content = %q, want DuplicateCallNotice already in the partial History", got)
	}
}

// TestDedupWithinTurnSkippedCallOpensNoToolSpan proves a deduped call
// never reaches runOneToolCall's tracer.Start call: a wired Tracer
// opens exactly one "agentloop.tool_call" span for a turn with two
// identical calls, on the first call only, plus the one
// "agentloop.iteration" span for the turn itself.
func TestDedupWithinTurnSkippedCallOpensNoToolSpan(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte(`{"a":1}`)},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte(`{"a":1}`)},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	tracer := trace.New()
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, DedupWithinTurn: true, Tracer: tracer,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	var toolSpans, iterationSpans int
	for _, s := range tracer.Spans() {
		switch s.Name {
		case "agentloop.tool_call":
			toolSpans++
		case "agentloop.iteration":
			iterationSpans++
		}
	}
	if toolSpans != 1 {
		t.Fatalf("tool_call spans = %d, want 1: a deduped call must never open its own span", toolSpans)
	}
	if iterationSpans != 2 {
		t.Fatalf("iteration spans = %d, want 2", iterationSpans)
	}
}

// toolResultContent returns the Content of the RoleTool message in
// history whose ToolCallID matches id, failing the test when none
// exists.
func toolResultContent(t *testing.T, history []provider.Message, id string) string {
	t.Helper()
	for _, m := range history {
		if m.Role == provider.RoleTool && m.ToolCallID == id {
			return m.Content
		}
	}
	t.Fatalf("no RoleTool message with ToolCallID %s: %+v", id, history)
	return ""
}
