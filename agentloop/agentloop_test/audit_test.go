package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// recordingAuditor collects every AuditRecord passed to its Audit
// func, in call order, guarded for concurrent use.
type recordingAuditor struct {
	mu      sync.Mutex
	records []agentloop.AuditRecord
	err     error
}

func (a *recordingAuditor) Audit(ctx context.Context, rec agentloop.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, rec)
	return a.err
}

func (a *recordingAuditor) snapshot() []agentloop.AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agentloop.AuditRecord(nil), a.records...)
}

// errAudit is a sentinel an Audit func returns to prove Run wraps and
// propagates it.
var errAudit = errors.New("agentloop_test: audit rejected")

// TestAuditRecordsOneCompletionAndOneToolCallPerIteration proves a
// two-iteration, two-tool-call run records one AuditKindCompletion
// record per iteration and one AuditKindToolCall record per tool
// call, in order, each carrying the run's own Request/Response/
// ToolCall/ToolResult values.
func TestAuditRecordsOneCompletionAndOneToolCallPerIteration(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "r1"}
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "r2"}
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)

	resp1 := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "first", Arguments: []byte("{}")})
	resp2 := toolCallResponse(provider.ToolCall{ID: "call-2", Name: "second", Arguments: []byte("{}")})
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "done")}
	completer := &scriptedCompleter{responses: []provider.Response{resp1, resp2, final}}

	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3", res.Iterations)
	}

	records := auditor.snapshot()
	wantKinds := []agentloop.AuditKind{
		agentloop.AuditKindCompletion, agentloop.AuditKindToolCall,
		agentloop.AuditKindCompletion, agentloop.AuditKindToolCall,
		agentloop.AuditKindCompletion,
	}
	if len(records) != len(wantKinds) {
		t.Fatalf("records = %d, want %d: %+v", len(records), len(wantKinds), records)
	}
	for i, want := range wantKinds {
		if records[i].Kind != want {
			t.Fatalf("records[%d].Kind = %v, want %v", i, records[i].Kind, want)
		}
	}
	if records[0].Response.Message.ToolCalls[0].ID != "call-1" {
		t.Fatalf("records[0].Response = %+v, want the first turn's own response", records[0].Response)
	}
	if records[1].ToolCall.ID != "call-1" || records[1].ToolResult.Content != "r1" {
		t.Fatalf("records[1] = %+v, want ToolCall call-1 and ToolResult content r1", records[1])
	}
	if records[3].ToolCall.ID != "call-2" || records[3].ToolResult.Content != "r2" {
		t.Fatalf("records[3] = %+v, want ToolCall call-2 and ToolResult content r2", records[3])
	}
	if records[4].Response.Message.Content != "done" {
		t.Fatalf("records[4].Response = %+v, want the final turn's own response", records[4].Response)
	}

	// The first completion's Request carries only the caller's
	// starting message; the third completion's Request carries the
	// accumulated history, including both tool results, proving
	// Request is the iteration's own request, not a shared or
	// zero-value snapshot.
	if len(records[0].Request.Messages) != 1 || records[0].Request.Messages[0].Content != "hi" {
		t.Fatalf("records[0].Request.Messages = %+v, want exactly the caller's starting message", records[0].Request.Messages)
	}
	lastReq := records[4].Request.Messages
	if len(lastReq) != 5 {
		t.Fatalf("records[4].Request.Messages len = %d, want 5 (user, assistant+tool, tool result, assistant+tool, tool result)", len(lastReq))
	}
	if lastReq[2].Role != provider.RoleTool || lastReq[2].Content != "r1" {
		t.Fatalf("records[4].Request.Messages[2] = %+v, want the first tool result r1", lastReq[2])
	}
}

// TestAuditToolCallErrCarriesUnrenderedDecodeError proves an
// AuditKindToolCall record for a reported ErrorPolicyReport
// decodeAndRun failure carries the original, unrendered error in Err,
// not the ToolErrorPrefix-marked Content string.
func TestAuditToolCallErrCarriesUnrenderedDecodeError(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), decodeErr: errBoom}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	rec := findToolCallRecord(t, auditor.snapshot(), "call-1")
	if !errors.Is(rec.Err, errBoom) {
		t.Fatalf("rec.Err = %v, want it to wrap errBoom", rec.Err)
	}
	if strings.Contains(rec.Err.Error(), agentloop.ToolErrorPrefix) {
		t.Fatalf("rec.Err = %v, want the unrendered error, not the ToolErrorPrefix-marked Content string", rec.Err)
	}
	if !strings.HasPrefix(rec.ToolResult.Content, agentloop.ToolErrorPrefix) {
		t.Fatalf("rec.ToolResult.Content = %q, want it to start with %q", rec.ToolResult.Content, agentloop.ToolErrorPrefix)
	}
}

// TestAuditToolCallErrCarriesUnrenderableResultError proves an
// AuditKindToolCall record for a render failure carries an Err
// wrapping ErrUnrenderableResult, not only a decodeAndRun failure.
func TestAuditToolCallErrCarriesUnrenderableResultError(t *testing.T) {
	tool := &schemaEchoTool{name: "t", schema: []byte(`{}`), result: make(chan int)}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "t", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	rec := findToolCallRecord(t, auditor.snapshot(), "call-1")
	if !errors.Is(rec.Err, agentloop.ErrUnrenderableResult) {
		t.Fatalf("rec.Err = %v, want it to wrap ErrUnrenderableResult", rec.Err)
	}
}

// TestAuditNoRecordForVetoedCall proves a PointPreTool veto produces
// no AuditKindToolCall record for the vetoed call.
func TestAuditNoRecordForVetoedCall(t *testing.T) {
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
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	for _, rec := range auditor.snapshot() {
		if rec.Kind == agentloop.AuditKindToolCall {
			t.Fatalf("found an AuditKindToolCall record for a vetoed call: %+v", rec)
		}
	}
}

// TestAuditNoRecordForFailedToolCall proves an ErrorPolicyFail tool
// error produces no AuditKindToolCall record for the failing call.
func TestAuditNoRecordForFailedToolCall(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), runErr: errBoom}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); !errors.Is(err, errBoom) {
		t.Fatalf("Run() error = %v, want errBoom", err)
	}
	for _, rec := range auditor.snapshot() {
		if rec.Kind == agentloop.AuditKindToolCall {
			t.Fatalf("found an AuditKindToolCall record for a failed call: %+v", rec)
		}
	}
}

// TestAuditCompletionRecordedBeforeCallsPerTurnFailure proves a
// scripted Completer whose second iteration's response trips
// ErrCallsPerTurnExceeded still yields an AuditKindCompletion record
// for that second iteration before Run returns the wrapped error.
func TestAuditCompletionRecordedBeforeCallsPerTurnFailure(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")},
		),
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxCallsPerTurn: 1, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrCallsPerTurnExceeded) {
		t.Fatalf("Run() error = %v, want ErrCallsPerTurnExceeded", err)
	}
	records := auditor.snapshot()
	completions := 0
	for _, rec := range records {
		if rec.Kind == agentloop.AuditKindCompletion {
			completions++
		}
	}
	if completions != 2 {
		t.Fatalf("AuditKindCompletion records = %d, want 2: the tripping iteration's completion must still be audited", completions)
	}
}

// TestAuditCompletionRecordedBeforeTokenBudgetFailure proves the same
// audit-before-hard-fail ordering for a response that trips
// ErrTokenBudgetExceeded.
func TestAuditCompletionRecordedBeforeTokenBudgetFailure(t *testing.T) {
	tripping := provider.Response{Message: textMessage(provider.RoleAssistant, "hi"), Usage: provider.Usage{TotalTokens: 200}}
	completer := &scriptedCompleter{responses: []provider.Response{tripping}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, MaxTotalTokens: 100, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	records := auditor.snapshot()
	if len(records) != 1 || records[0].Kind != agentloop.AuditKindCompletion {
		t.Fatalf("records = %+v, want one AuditKindCompletion record for the tripping iteration", records)
	}
}

// TestAuditFuncErrorFailsRun proves an Audit func returning an error
// fails the run with the wrapped error, errors.Is-checkable back to
// the Audit func's own sentinel, and the returned Result carries the
// accumulated History, Iterations, and Usage at the point of failure.
func TestAuditFuncErrorFailsRun(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	auditor := &recordingAuditor{err: errAudit}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: tools.New(), MaxIterations: 5, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, errAudit) {
		t.Fatalf("Run() error = %v, want errAudit", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the completed turn's state must travel", res.Iterations)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the accumulated state at the point of audit failure")
	}
}

// TestAuditFuncErrorOnToolCallFailsRun proves a non-nil Audit error
// from the AuditKindToolCall branch inside runToolCalls fails the
// run, the same way an AuditKindCompletion error already does in
// TestAuditFuncErrorFailsRun. The returned Result's History already
// contains the tool call's RoleTool message, matching runToolCalls's
// documented append-then-audit order.
func TestAuditFuncErrorOnToolCallFailsRun(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
	}}
	auditFn := func(ctx context.Context, rec agentloop.AuditRecord) error {
		if rec.Kind == agentloop.AuditKindToolCall {
			return errAudit
		}
		return nil
	}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Audit: auditFn})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, errAudit) {
		t.Fatalf("Run() error = %v, want errAudit", err)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want it to already contain the tool call's RoleTool message")
	}
	found := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("History = %+v, want a RoleTool message for call-1, appended before the audit error", res.History)
	}
}

// TestAuditNilRunsUnchanged proves a nil Options.Audit runs unchanged
// from the base plan's existing behavior.
func TestAuditNilRunsUnchanged(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: tools.New(), MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
}

// findToolCallRecord returns the AuditKindToolCall record whose
// ToolCall.ID matches id, failing the test when none exists.
func findToolCallRecord(t *testing.T, records []agentloop.AuditRecord, id string) agentloop.AuditRecord {
	t.Helper()
	for _, rec := range records {
		if rec.Kind == agentloop.AuditKindToolCall && rec.ToolCall.ID == id {
			return rec
		}
	}
	t.Fatalf("no AuditKindToolCall record with ToolCall.ID %s: %+v", id, records)
	return agentloop.AuditRecord{}
}
