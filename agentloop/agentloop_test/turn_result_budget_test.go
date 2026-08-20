package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// toolResultContent returns the RoleTool message content for callID
// out of history, failing the test when no such message exists.
func toolResultContent(t *testing.T, history []provider.Message, callID string) string {
	t.Helper()
	for _, m := range history {
		if m.Role == provider.RoleTool && m.ToolCallID == callID {
			return m.Content
		}
	}
	t.Fatalf("no RoleTool message for call %s in history: %+v", callID, history)
	return ""
}

// TestTurnResultBudgetKeepsFirstReplacesSecond proves a two-call turn,
// with TurnResultBudget sized to fit only the first call's content,
// keeps the first call's content whole and replaces the second call's
// content with BatchTruncationNotice.
func TestTurnResultBudgetKeepsFirstReplacesSecond(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "wxyz"}         // 4 bytes
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "0123456789"} // 10 bytes
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 1, Name: "second", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, TurnResultBudget: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := toolResultContent(t, res.History, "call-1"); got != "wxyz" {
		t.Fatalf("call-1 content = %q, want wxyz (whole)", got)
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.BatchTruncationNotice {
		t.Fatalf("call-2 content = %q, want BatchTruncationNotice", got)
	}
}

// TestTurnResultBudgetZeroPassesThroughWhole proves the same two-call
// setup with TurnResultBudget zero appends both calls' content whole,
// unchanged from the base plan.
func TestTurnResultBudgetZeroPassesThroughWhole(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "wxyz"}
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "0123456789"}
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 1, Name: "second", Arguments: []byte("{}")},
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
	if got := toolResultContent(t, res.History, "call-1"); got != "wxyz" {
		t.Fatalf("call-1 content = %q, want wxyz (whole)", got)
	}
	if got := toolResultContent(t, res.History, "call-2"); got != "0123456789" {
		t.Fatalf("call-2 content = %q, want 0123456789 (whole)", got)
	}
}

// TestTurnResultBudgetAppliesAfterPerCallBudget proves the per-call
// tools.ResultBudgetOf bound applies before TurnResultBudget's batch
// shaping: the already-truncated content, not the original unbounded
// result, is what the batch check measures.
func TestTurnResultBudgetAppliesAfterPerCallBudget(t *testing.T) {
	tool := &budgetedSchemaTool{
		schemaEchoTool: schemaEchoTool{name: "t", schema: []byte(`{}`), result: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}, // 29 x's
		maxBytes:       5,
	}
	reg := tools.New()
	mustAdd(t, reg, tool)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Index: 0, Name: "t", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, TurnResultBudget: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	got := toolResultContent(t, res.History, "call-1")
	if got != "xxxxx" {
		t.Fatalf("content = %q, want xxxxx: TurnResultBudget must measure the already per-call-truncated content, not the original 29 bytes", got)
	}
}

// TestTurnResultBudgetExactBoundary pins the plan's four-call
// boundary case: TurnResultBudget 10, calls of 6/5/5/4 bytes. Call 1
// fits (total 6). Call 2 and call 3 each straddle over budget and are
// replaced, without growing the running total. Call 4's sum lands
// exactly on TurnResultBudget and must stay whole, proving the rule
// uses <=, not <.
func TestTurnResultBudgetExactBoundary(t *testing.T) {
	c1 := &schemaEchoTool{name: "c1", schema: []byte(`{}`), result: "AAAAAA"} // 6
	c2 := &schemaEchoTool{name: "c2", schema: []byte(`{}`), result: "BBBBB"}  // 5
	c3 := &schemaEchoTool{name: "c3", schema: []byte(`{}`), result: "CCCCC"}  // 5
	c4 := &schemaEchoTool{name: "c4", schema: []byte(`{}`), result: "DDDD"}   // 4
	reg := tools.New()
	mustAdd(t, reg, c1)
	mustAdd(t, reg, c2)
	mustAdd(t, reg, c3)
	mustAdd(t, reg, c4)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "c1", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 1, Name: "c2", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-3", Index: 2, Name: "c3", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-4", Index: 3, Name: "c4", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, TurnResultBudget: 10,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := toolResultContent(t, res.History, "call-1"); got != "AAAAAA" {
		t.Fatalf("call-1 content = %q, want AAAAAA (whole)", got)
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.BatchTruncationNotice {
		t.Fatalf("call-2 content = %q, want BatchTruncationNotice", got)
	}
	if got := toolResultContent(t, res.History, "call-3"); got != agentloop.BatchTruncationNotice {
		t.Fatalf("call-3 content = %q, want BatchTruncationNotice", got)
	}
	if got := toolResultContent(t, res.History, "call-4"); got != "DDDD" {
		t.Fatalf("call-4 content = %q, want DDDD (whole): the running total 6+4 == budget 10 must stay whole", got)
	}
}

// TestTurnResultBudgetErrorPolicyReportTruncated proves a reported
// tool-run error's ToolErrorPrefix-marked content counts toward the
// running total exactly like a successful result's content, and gets
// replaced with BatchTruncationNotice when the batch budget is
// exhausted, while AuditRecord.Err still carries the true error.
func TestTurnResultBudgetErrorPolicyReportTruncated(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "AAAAAA"} // 6 bytes
	failing := &schemaEchoTool{name: "failing", schema: []byte(`{}`), decodeErr: errBoom}
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, failing)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 1, Name: "failing", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, TurnResultBudget: 6, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.BatchTruncationNotice {
		t.Fatalf("call-2 content = %q, want BatchTruncationNotice", got)
	}
	rec := findToolCallRecord(t, auditor.snapshot(), "call-2")
	if !errors.Is(rec.Err, errBoom) {
		t.Fatalf("rec.Err = %v, want it to wrap errBoom, unchanged by batch shaping", rec.Err)
	}
	if rec.ToolResult.Content != agentloop.BatchTruncationNotice {
		t.Fatalf("rec.ToolResult.Content = %q, want BatchTruncationNotice", rec.ToolResult.Content)
	}
}

// TestTurnResultBudgetTruncatedSuccessKeepsNilErr proves a
// batch-truncated, otherwise-successful call's AuditRecord.Err stays
// nil, matching a normal successful call.
func TestTurnResultBudgetTruncatedSuccessKeepsNilErr(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "AAAAAA"}       // 6 bytes
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "0123456789"} // 10 bytes
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 1, Name: "second", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, TurnResultBudget: 6, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	rec := findToolCallRecord(t, auditor.snapshot(), "call-2")
	if rec.Err != nil {
		t.Fatalf("rec.Err = %v, want nil: a batch-truncated but successful call must not report an error", rec.Err)
	}
	if rec.ToolResult.Content != agentloop.BatchTruncationNotice {
		t.Fatalf("rec.ToolResult.Content = %q, want BatchTruncationNotice", rec.ToolResult.Content)
	}
}

// TestTurnResultBudgetVetoStopsShapingConsideration proves a
// PointPreTool veto partway through a turn stops later calls from
// running, unchanged from the base plan; the shaping pass only
// considers the calls that ran before the veto.
func TestTurnResultBudgetVetoStopsShapingConsideration(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "AAAAAA"}
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "BBBBBB"}
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)

	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "veto-second", func(ctx context.Context, payload any) (bool, error) {
		call, ok := payload.(provider.ToolCall)
		if ok && call.Name == "second" {
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 1, Name: "second", Arguments: []byte("{}")},
		),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg, TurnResultBudget: 100,
	})
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
	if got := toolResultContent(t, res.History, "call-1"); got != "AAAAAA" {
		t.Fatalf("call-1 content = %q, want AAAAAA (whole)", got)
	}
	for _, m := range res.History {
		if m.ToolCallID == "call-2" {
			t.Fatalf("found a RoleTool message for the vetoed call-2: %+v", m)
		}
	}
}

// TestTurnResultBudgetStableSortTieBreak proves runToolCalls's
// sort.SliceStable switch keeps two same-Index ToolCall values in
// input-slice order, so the first one in slice order is judged
// against the smaller running total and keeps its content whole.
func TestTurnResultBudgetStableSortTieBreak(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "AAAAAA"}       // 6 bytes
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "0123456789"} // 10 bytes
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)

	// Both calls share Index 0; input-slice order is call-1 (first) then
	// call-2 (second).
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 0, Name: "second", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, TurnResultBudget: 6,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := toolResultContent(t, res.History, "call-1"); got != "AAAAAA" {
		t.Fatalf("call-1 content = %q, want AAAAAA (whole): input-slice order must win the tie", got)
	}
	if got := toolResultContent(t, res.History, "call-2"); got != agentloop.BatchTruncationNotice {
		t.Fatalf("call-2 content = %q, want BatchTruncationNotice", got)
	}
}
