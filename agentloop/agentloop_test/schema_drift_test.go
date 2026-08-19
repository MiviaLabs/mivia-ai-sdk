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
