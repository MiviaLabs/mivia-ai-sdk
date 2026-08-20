package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// TestRunMaxTotalTokens proves a scripted Completer whose responses'
// summed Usage.TotalTokens crosses a positive MaxTotalTokens on the
// second iteration fails the run with ErrTokenBudgetExceeded, and
// that a paired Options.Usage accumulator still records the tripping
// call's tokens.
func TestRunMaxTotalTokens(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	responses := []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{TotalTokens: 60}
			return r
		}(),
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{TotalTokens: 60}
			return r
		}(),
	}
	completer := &scriptedCompleter{responses: responses}
	acc := usage.New()
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxTotalTokens: 100,
		Usage: acc, SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	if res.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2: the tripping call itself already completed", res.Iterations)
	}
	if len(res.History) != 4 {
		t.Fatalf("History len = %d, want 4 (user, assistant, tool result, assistant): per hardFail's rule, the accumulated state must travel", len(res.History))
	}
	if !isZeroMessage(res.Final) || res.Stop != "" {
		t.Fatalf("Final = %+v, Stop = %v, want both zero: hardFail never sets Final or Stop", res.Final, res.Stop)
	}
	total, ok := acc.Total("sess-1")
	if !ok {
		t.Fatalf("acc.Total(sess-1) ok = false, want true")
	}
	if total.TotalTokens != 120 {
		t.Fatalf("acc.Total(sess-1).TotalTokens = %d, want 120: the tripping call's tokens must still land", total.TotalTokens)
	}
}

// TestRunMaxTotalTokensUnderReportedTotal proves the reproduction: a
// Completer that fills PromptTokens and CompletionTokens but leaves
// TotalTokens at zero must not bypass MaxTotalTokens. Before the
// billedTokens fix, run summed TotalTokens alone, so runningTokens stayed
// 0 across both responses and the cap never tripped.
func TestRunMaxTotalTokensUnderReportedTotal(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	responses := []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{PromptTokens: 30, CompletionTokens: 30}
			return r
		}(),
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{PromptTokens: 30, CompletionTokens: 30}
			return r
		}(),
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxTotalTokens: 100,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	if res.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2: the tripping call itself already completed", res.Iterations)
	}
}

// TestRunMaxTotalTokensUnderReportedTotalUnbounded proves a zero
// MaxTotalTokens with the same under-reported Usage shape runs to its
// normal stop, unaffected: the fix adds no cap where none is configured.
func TestRunMaxTotalTokensUnderReportedTotalUnbounded(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	responses := []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{PromptTokens: 30, CompletionTokens: 30}
			return r
		}(),
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{PromptTokens: 30, CompletionTokens: 30}
			return r
		}(),
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %v, want StopMaxIterations", res.Stop)
	}
}

// TestRunMaxTotalTokensSurchargedTotal proves a response whose
// TotalTokens exceeds PromptTokens plus CompletionTokens still trips
// MaxTotalTokens on that larger reading: billedTokens picks the larger
// of the two figures, not only the under-reporting direction.
func TestRunMaxTotalTokensSurchargedTotal(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{PromptTokens: 20, CompletionTokens: 20, TotalTokens: 50}
			return r
		}(),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxTotalTokens: 40,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the surcharge trips on the first response alone", res.Iterations)
	}
}

// TestRunMaxTotalTokensAtCapPasses pins run.go's MaxTotalTokens
// boundary from the passing side: two responses whose Usage.TotalTokens
// sum to exactly MaxTotalTokens must not trip ErrTokenBudgetExceeded.
func TestRunMaxTotalTokensAtCapPasses(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	responses := []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{TotalTokens: 50}
			return r
		}(),
		{Message: textMessage(provider.RoleAssistant, "done"), Usage: provider.Usage{TotalTokens: 50}},
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxTotalTokens: 100,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil at the exact cap boundary", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
}

// TestRunMaxTotalTokensOneOverCapFails pairs
// TestRunMaxTotalTokensAtCapPasses from the failing side: the same
// cap, with the first response's TotalTokens one token higher, sums to
// MaxTotalTokens+1 and trips ErrTokenBudgetExceeded.
func TestRunMaxTotalTokensOneOverCapFails(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	responses := []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")})
			r.Usage = provider.Usage{TotalTokens: 51}
			return r
		}(),
		{Message: textMessage(provider.RoleAssistant, "done"), Usage: provider.Usage{TotalTokens: 50}},
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxTotalTokens: 100,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded one token over the cap", err)
	}
}

// TestRunZeroMaxTotalTokensUnbounded proves a zero MaxTotalTokens with
// the same scripted responses runs to StopMaxIterations unaffected.
func TestRunZeroMaxTotalTokensUnbounded(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	mk := func(id string) provider.Response {
		r := toolCallResponse(provider.ToolCall{ID: id, Name: "echo", Arguments: []byte("{}")})
		r.Usage = provider.Usage{TotalTokens: 1000}
		return r
	}
	completer := &scriptedCompleter{responses: []provider.Response{mk("call-1"), mk("call-2")}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 2})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %v, want StopMaxIterations", res.Stop)
	}
}
