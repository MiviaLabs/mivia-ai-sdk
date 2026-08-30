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

// alwaysContinue returns a decide function that continues the run on
// every invocation.
func alwaysContinue() func(int, agentloop.StopDecision) []provider.Message {
	return func(int, agentloop.StopDecision) []provider.Message {
		return []provider.Message{continuationMessage()}
	}
}

// TestContinueOnStopBoundedByMaxIterations proves an always-continue
// hook ends with StopMaxIterations, not a hang.
func TestContinueOnStopBoundedByMaxIterations(t *testing.T) {
	rec := &stopHookRecorder{decide: alwaysContinue()}
	completer := &constantCompleter{resp: provider.Response{
		Message: textMessage(provider.RoleAssistant, "again"),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 4,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %q, want StopMaxIterations", res.Stop)
	}
	if completer.callCount() != 4 {
		t.Fatalf("completer calls = %d, want 4", completer.callCount())
	}
	if rec.count() != 4 {
		t.Fatalf("hook invocations = %d, want 4", rec.count())
	}
}

// TestContinueOnStopBoundedByMaxTotalTokens proves the same hook ends
// with ErrTokenBudgetExceeded when the ceiling is set. Every response
// bills 10 tokens, so the third call crosses the 25-token ceiling.
func TestContinueOnStopBoundedByMaxTotalTokens(t *testing.T) {
	rec := &stopHookRecorder{decide: alwaysContinue()}
	completer := &constantCompleter{resp: provider.Response{
		Message: textMessage(provider.RoleAssistant, "again"),
		Usage:   provider.Usage{TotalTokens: 10},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 0,
		MaxTotalTokens: 25, ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	if completer.callCount() != 3 {
		t.Fatalf("completer calls = %d, want 3", completer.callCount())
	}
	if rec.count() != 2 {
		t.Fatalf("hook invocations = %d, want 2: the ceiling trips before the third stop", rec.count())
	}
}

// TestContinueOnStopUnboundedWithoutBounds proves the loop adds no
// bound of its own. With MaxIterations and MaxTotalTokens both zero,
// the run ends only because the hook stopped continuing.
func TestContinueOnStopUnboundedWithoutBounds(t *testing.T) {
	const continuations = 3
	rec := &stopHookRecorder{decide: continueTimes(continuations, continuationMessage())}
	completer := &constantCompleter{resp: provider.Response{
		Message: textMessage(provider.RoleAssistant, "again"),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 0,
		MaxTotalTokens: 0, ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if completer.callCount() != continuations+1 {
		t.Fatalf("completer calls = %d, want %d", completer.callCount(), continuations+1)
	}
	if rec.count() != continuations+1 {
		t.Fatalf("hook invocations = %d, want %d", rec.count(), continuations+1)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls: the hook alone ended the run", res.Stop)
	}
	if res.Iterations != continuations+1 {
		t.Fatalf("Iterations = %d, want %d", res.Iterations, continuations+1)
	}
}

// TestContinueOnStopTrimStripsNoticeReportsNoToolCalls proves the
// conditional notice claim: a continued run whose Trim drops the
// ConcludeNotice reports StopNoToolCalls, not StopConcluded. The notice
// is never appended a second time, so noticeSent stays true.
func TestContinueOnStopTrimStripsNoticeReportsNoToolCalls(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "first")},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 3,
		ConcludeMargin: 3,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			kept := make([]provider.Message, 0, len(msgs))
			for _, m := range msgs {
				if m.Content == agentloop.DefaultConcludeNotice {
					continue
				}
				kept = append(kept, m)
			}
			return kept, nil
		},
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := rec.at(t, 0).Stop; got != agentloop.StopConcluded {
		t.Fatalf("first decision Stop = %q, want StopConcluded", got)
	}
	if got := rec.at(t, 1).Stop; got != agentloop.StopNoToolCalls {
		t.Fatalf("second decision Stop = %q, want StopNoToolCalls", got)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if n := countContent(completer.requestAt(1).Messages, agentloop.DefaultConcludeNotice); n != 0 {
		t.Fatalf("request 2 carries %d notices, want 0: noticeSent must stay true", n)
	}
	if n := countContent(res.History, agentloop.DefaultConcludeNotice); n != 0 {
		t.Fatalf("History carries %d notices, want 0: Trim strips it", n)
	}
}

// TestContinueOnStopConcludedKeepsOneNotice proves a continued run
// appends ConcludeNotice exactly once, over at least two Completer
// calls.
func TestContinueOnStopConcludedKeepsOneNotice(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "first")},
		{Message: textMessage(provider.RoleAssistant, "second")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 3,
		ConcludeMargin: 3, ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if completer.callCount() < 2 {
		t.Fatalf("completer calls = %d, want at least 2: the run must continue", completer.callCount())
	}
	if n := countContent(res.History, agentloop.DefaultConcludeNotice); n != 1 {
		t.Fatalf("History carries %d notices, want exactly 1", n)
	}
	if res.Stop != agentloop.StopConcluded {
		t.Fatalf("Stop = %q, want StopConcluded: the notice survives history", res.Stop)
	}
}

// TestContinueOnStopCancelledContextEndsRun proves a canceled context
// ends a continued run at the next iteration top.
func TestContinueOnStopCancelledContextEndsRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &stopHookRecorder{decide: func(int, agentloop.StopDecision) []provider.Message {
		cancel()
		return []provider.Message{continuationMessage()}
	}}
	completer := &constantCompleter{resp: provider.Response{
		Message: textMessage(provider.RoleAssistant, "again"),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(ctx, []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if rec.count() != 1 {
		t.Fatalf("hook invocations = %d, want 1", rec.count())
	}
	if completer.callCount() != 1 {
		t.Fatalf("completer calls = %d, want 1: cancellation must end the continued run", completer.callCount())
	}
}

// TestContinueOnStopSkippedOnSteered proves StopSteered never consults
// the hook.
func TestContinueOnStopSkippedOnSteered(t *testing.T) {
	rec := &stopHookRecorder{decide: alwaysContinue()}
	entered := make(chan struct{})
	c := &blockingCompleter{entered: entered}
	loop, err := agentloop.New(agentloop.Options{
		Completer: c, Tools: tools.New(), MaxIterations: 5,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	steer := agentloop.NewSteer()
	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, rerr := loop.RunSteerable(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}, steer)
		resCh <- res
		errCh <- rerr
	}()
	<-entered
	steer.Trigger()
	res, rerr := <-resCh, <-errCh
	if rerr != nil {
		t.Fatalf("RunSteerable() error = %v, want nil", rerr)
	}
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered", res.Stop)
	}
	if rec.count() != 0 {
		t.Fatalf("hook invocations = %d, want 0 on StopSteered", rec.count())
	}
}

// TestContinueOnStopSkippedOnHookVeto proves StopHookVeto never
// consults the hook.
func TestContinueOnStopSkippedOnHookVeto(t *testing.T) {
	rec := &stopHookRecorder{decide: alwaysContinue()}
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"}
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
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopHookVeto {
		t.Fatalf("Stop = %q, want StopHookVeto", res.Stop)
	}
	if rec.count() != 0 {
		t.Fatalf("hook invocations = %d, want 0 on StopHookVeto", rec.count())
	}
}

// TestContinueOnStopSkippedOnMaxIterations proves StopMaxIterations
// never consults the hook.
func TestContinueOnStopSkippedOnMaxIterations(t *testing.T) {
	rec := &stopHookRecorder{decide: alwaysContinue()}
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 2,
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %q, want StopMaxIterations", res.Stop)
	}
	if rec.count() != 0 {
		t.Fatalf("hook invocations = %d, want 0 on StopMaxIterations", rec.count())
	}
}

// TestContinueOnStopSkippedOnRepeatedToolFailures proves
// StopRepeatedToolFailures never consults the hook.
func TestContinueOnStopSkippedOnRepeatedToolFailures(t *testing.T) {
	rec := &stopHookRecorder{decide: alwaysContinue()}
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "nonexistent_1", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "nonexistent_2", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 10,
		MaxConsecutiveToolFailures: 2, ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopRepeatedToolFailures {
		t.Fatalf("Stop = %q, want StopRepeatedToolFailures", res.Stop)
	}
	if rec.count() != 0 {
		t.Fatalf("hook invocations = %d, want 0 on StopRepeatedToolFailures", rec.count())
	}
}

// TestContinueOnStopSkippedOnTokenBudgetHardFail proves the one
// hardFail that competes with a graceful stop over the same response
// skips the hook. The MaxTotalTokens check in afterChat fires after the
// response is recorded and before runToolStage runs. This case claims
// that path alone; hardFail has other call sites that return before
// runToolStage is reachable.
func TestContinueOnStopSkippedOnTokenBudgetHardFail(t *testing.T) {
	rec := &stopHookRecorder{decide: alwaysContinue()}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "final"), Usage: provider.Usage{TotalTokens: 100}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		MaxTotalTokens: 10, ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	if rec.count() != 0 {
		t.Fatalf("hook invocations = %d, want 0 on a hard fail", rec.count())
	}
	if completer.callCount() != 1 {
		t.Fatalf("completer calls = %d, want 1", completer.callCount())
	}
}
