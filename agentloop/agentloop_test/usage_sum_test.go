package agentloop_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunUsageSumsAllFourFields pins run.go's sumUsage: two scripted
// responses set every provider.Usage field to distinct, nonzero,
// easily-summed numbers, and Result.Usage must equal the field-by-field
// sum. Flipping any one sumUsage addition to a subtraction changes
// exactly one field's expected sum, so this test kills all four sites.
func TestRunUsageSumsAllFourFields(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	responses := []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, CachedTokens: 5}
			return r
		}(),
		{
			Message: textMessage(provider.RoleAssistant, "done"),
			Usage:   provider.Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10, CachedTokens: 2},
		},
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	want := provider.Usage{PromptTokens: 17, CompletionTokens: 23, TotalTokens: 40, CachedTokens: 7}
	if res.Usage != want {
		t.Fatalf("Usage = %+v, want %+v", res.Usage, want)
	}
}
