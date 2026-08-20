package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestDecodeAndRunToolRegisteredAfterNewReportsUnderReportPolicy
// builds a Loop over an empty registry, registers a schema-bearing
// tool named "late" on the same *tools.Registry after New returns,
// then scripts a call to "late". Under the default ErrorPolicyReport,
// Run must not panic: it reports ErrToolNotOffered as the tool's
// AuditRecord.Err and as a ToolErrorPrefix-marked RoleTool message.
func TestDecodeAndRunToolRegisteredAfterNewReportsUnderReportPolicy(t *testing.T) {
	reg := tools.New()
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "late", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	late := &schemaEchoTool{name: "late", schema: []byte(`{}`), result: "x"}
	mustAdd(t, reg, late)

	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}

	rec := findToolCallRecord(t, auditor.snapshot(), "call-1")
	if !errors.Is(rec.Err, agentloop.ErrToolNotOffered) {
		t.Fatalf("rec.Err = %v, want it to wrap ErrToolNotOffered", rec.Err)
	}
	if !strings.HasPrefix(rec.ToolResult.Content, agentloop.ToolErrorPrefix) {
		t.Fatalf("rec.ToolResult.Content = %q, want it to start with %q", rec.ToolResult.Content, agentloop.ToolErrorPrefix)
	}
}

// TestDecodeAndRunToolRemovedAfterNewReportsUnknownName covers the
// opposite registry-drift direction from the two tests above: a tool
// present at New time, and so carrying a l.schemas entry, gets
// removed from the shared *tools.Registry before Run sees the model's
// call for it. decodeAndRun's first step is reg.Get, which reads the
// live registry, so the removal surfaces as tools.ErrUnknownName, not
// agentloop.ErrToolNotOffered: the l.schemas hit never gets a chance
// to matter, because Get already fails first.
func TestDecodeAndRunToolRemovedAfterNewReportsUnknownName(t *testing.T) {
	reg := tools.New()
	gone := &schemaEchoTool{name: "gone", schema: []byte(`{}`), result: "x"}
	mustAdd(t, reg, gone)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "gone", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	if ok := reg.Remove("gone"); !ok {
		t.Fatalf("Remove(gone) = false, want true")
	}

	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}

	rec := findToolCallRecord(t, auditor.snapshot(), "call-1")
	if !errors.Is(rec.Err, tools.ErrUnknownName) {
		t.Fatalf("rec.Err = %v, want it to wrap tools.ErrUnknownName", rec.Err)
	}
	if errors.Is(rec.Err, agentloop.ErrToolNotOffered) {
		t.Fatalf("rec.Err = %v, must not wrap ErrToolNotOffered: reg.Get fails before l.schemas is ever consulted", rec.Err)
	}
	if !strings.HasPrefix(rec.ToolResult.Content, agentloop.ToolErrorPrefix) {
		t.Fatalf("rec.ToolResult.Content = %q, want it to start with %q", rec.ToolResult.Content, agentloop.ToolErrorPrefix)
	}
}

// TestDecodeAndRunToolRemovedBetweenTwoRunCallsReportsUnknownName
// proves the same removal-drift surfaces identically on a second Run
// call over the same *Loop: the first Run call succeeds while the
// tool is still registered, then the caller removes it, and a second
// Run call's request for the same tool name fails with
// tools.ErrUnknownName, not agentloop.ErrToolNotOffered.
func TestDecodeAndRunToolRemovedBetweenTwoRunCallsReportsUnknownName(t *testing.T) {
	reg := tools.New()
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	mustAdd(t, reg, tool)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final-1")},
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final-2")},
	}}
	auditor := &recordingAuditor{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Audit: auditor.Audit})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("first Run() error = %v, want nil", err)
	}
	first := findToolCallRecord(t, auditor.snapshot(), "call-1")
	if first.Err != nil {
		t.Fatalf("first Run() call-1 rec.Err = %v, want nil while the tool is still registered", first.Err)
	}

	if ok := reg.Remove("echo"); !ok {
		t.Fatalf("Remove(echo) = false, want true")
	}

	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi again")}); err != nil {
		t.Fatalf("second Run() error = %v, want nil", err)
	}
	second := findToolCallRecord(t, auditor.snapshot(), "call-2")
	if !errors.Is(second.Err, tools.ErrUnknownName) {
		t.Fatalf("second Run() call-2 rec.Err = %v, want it to wrap tools.ErrUnknownName", second.Err)
	}
}

// TestDecodeAndRunToolRegisteredAfterNewFailsUnderFailPolicy repeats
// the same registry-drift setup under ErrorPolicyFail: Run itself
// must fail with ErrToolNotOffered, and the returned Result must
// carry the accumulated History and Iterations at the point of
// failure, matching this plan's Result-shape rule.
func TestDecodeAndRunToolRegisteredAfterNewFailsUnderFailPolicy(t *testing.T) {
	reg := tools.New()
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "late", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	late := &schemaEchoTool{name: "late", schema: []byte(`{}`), result: "x"}
	mustAdd(t, reg, late)

	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrToolNotOffered) {
		t.Fatalf("Run() error = %v, want ErrToolNotOffered", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the assistant turn that requested the call already completed", res.Iterations)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the accumulated state at the point of failure")
	}
}
