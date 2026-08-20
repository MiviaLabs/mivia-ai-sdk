package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunIterationStartEndOrder proves EventIterationStart and
// EventIterationEnd fire once per iteration, in order, bracketing
// every other event that iteration emits. HeartbeatInterval is set
// large enough that no tick ever fires, isolating the Start/End/
// tool-call-event sequence from heartbeat timing.
func TestRunIterationStartEndOrder(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	bus := events.New()
	rec := &eventRecorder{}
	subscribeEvents(t, bus, rec.handle,
		agentloop.EventIterationStart, agentloop.EventIterationEnd,
		agentloop.EventToolCallStart, agentloop.EventToolCallEnd,
		agentloop.EventCompletionHeartbeat, agentloop.EventToolCallHeartbeat,
	)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		Bus: bus, HeartbeatInterval: time.Hour,
	})
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
	want := []events.Name{
		agentloop.EventIterationStart,
		agentloop.EventToolCallStart,
		agentloop.EventToolCallEnd,
		agentloop.EventIterationEnd,
		agentloop.EventIterationStart,
		agentloop.EventIterationEnd,
	}
	got := rec.names()
	if len(got) != len(want) {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event sequence = %v, want %v", got, want)
		}
	}

	// Data content: iteration 1's bracket and tool-call bracket must
	// name iteration 1 and call-1/echo; iteration 2's bracket must
	// name iteration 2. A label that dropped the iteration number or
	// the call ID/name would still pass the Name-only sequence check
	// above, so this pins the content too.
	evts := rec.events()
	wantData := []string{
		"iteration 1",
		"iteration 1: tool call call-1 (echo)",
		"iteration 1: tool call call-1 (echo)",
		"iteration 1",
		"iteration 2",
		"iteration 2",
	}
	for i, w := range wantData {
		if evts[i].Data != w {
			t.Fatalf("event[%d] Data = %q, want %q", i, evts[i].Data, w)
		}
	}
}

// TestRunCtxCanceledBeforeFirstIterationEmitsNoEvents proves Run emits
// zero EventIterationStart/EventIterationEnd events when ctx is
// already canceled before the first iteration begins: the top-of-loop
// ctx check in run returns before runIteration, the sole emitter of
// those two events, ever runs. This is a different shape than
// TestRunIterationEndHardFailPaths' "ctx cancellation mid-call" case,
// which cancels ctx only after one iteration's Start already fired,
// so that table always expects exactly one End. A build that emitted
// Start/End around the top-of-loop check too, or that swallowed the
// ctx.Err() check and let one runIteration slip through, would still
// pass that table but would fail this test's zero-event assertion.
func TestRunCtxCanceledBeforeFirstIterationEmitsNoEvents(t *testing.T) {
	bus := events.New()
	rec := &eventRecorder{}
	subscribeEvents(t, bus, rec.handle, agentloop.EventIterationStart, agentloop.EventIterationEnd)
	completer := &scriptedCompleter{responses: []provider.Response{{Message: textMessage(provider.RoleAssistant, "hi")}}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := loop.Run(ctx, []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err == nil {
		t.Fatalf("Run() error = nil, want context.Canceled")
	}
	if !isZeroResult(res) {
		t.Fatalf("Result = %+v, want zero: no iteration ever completed", res)
	}
	if got := rec.names(); len(got) != 0 {
		t.Fatalf("events = %v, want none: runIteration must never run when ctx is already canceled", got)
	}
}

// hardFailBuild constructs one hard-fail scenario's Loop, ctx, and
// starting messages, wired to bus.
type hardFailBuild func(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message)

// buildHardFailCtxCancelMidCall triggers the hard-fail cause where a
// canceled ctx stops tool-call dispatch mid-turn, inside runIteration.
func buildHardFailCtxCancelMidCall(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "x"}
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)
	ctx, cancel := context.WithCancel(context.Background())
	completer := &cancelingCompleter{
		cancel: cancel,
		resp: toolCallResponse(
			provider.ToolCall{Index: 0, ID: "call-1", Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{Index: 1, ID: "call-2", Name: "second", Arguments: []byte("{}")},
		),
	}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, ctx, []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// buildHardFailTrimError triggers a Trim error.
func buildHardFailTrimError(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	completer := &scriptedCompleter{responses: []provider.Response{{Message: textMessage(provider.RoleAssistant, "hi")}}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) { return nil, errBoom },
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// buildHardFailBudgetError triggers a Budget rejection on the first
// iteration.
func buildHardFailBudgetError(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	completer := &scriptedCompleter{responses: []provider.Response{{Message: textMessage(provider.RoleAssistant, "hi")}}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour,
		Budget: &contextbudget.Limits{MaxBytes: 1},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// buildHardFailPlanHistoryError triggers ErrPlanFailed through a
// Window whose Calibrated estimator always errors.
func buildHardFailPlanHistoryError(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{{Message: textMessage(provider.RoleAssistant, "hi")}}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour,
		Window:     &contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{TriggerPercent: 50}},
		Summarizer: sum,
		Calibrated: contextplan.Calibrate(errEstimator{}, 1.0),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// buildHardFailAuditError triggers an Audit rejection.
func buildHardFailAuditError(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	completer := &scriptedCompleter{responses: []provider.Response{{Message: textMessage(provider.RoleAssistant, "hi")}}}
	auditor := &recordingAuditor{err: errAudit}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour,
		Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// buildHardFailTokenBudgetExceeded triggers ErrTokenBudgetExceeded.
func buildHardFailTokenBudgetExceeded(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi"), Usage: provider.Usage{TotalTokens: 100}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour,
		MaxTotalTokens: 10,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// buildHardFailToolCallError triggers a non-veto PointPreTool handler
// error, which runToolCalls returns as a tool-call error.
func buildHardFailToolCallError(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "boom", func(ctx context.Context, payload any) (bool, error) {
		return false, errBoom
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg, Bus: bus, HeartbeatInterval: time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// buildHardFailCallsPerTurnExceeded triggers ErrCallsPerTurnExceeded
// by requesting more tool calls in one turn than MaxCallsPerTurn
// allows.
func buildHardFailCallsPerTurnExceeded(t *testing.T, bus *events.Bus) (*agentloop.Loop, context.Context, []provider.Message) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")},
		),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, Bus: bus, HeartbeatInterval: time.Hour,
		MaxCallsPerTurn: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return loop, context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}
}

// TestRunIterationEndHardFailPaths is table-driven over every hard-
// fail cause runIteration can exit through: each case must emit
// exactly one EventIterationEnd, matching the deferred-emission
// contract that covers every exit path.
func TestRunIterationEndHardFailPaths(t *testing.T) {
	cases := []struct {
		name  string
		build hardFailBuild
	}{
		{"ctx cancellation mid-call", buildHardFailCtxCancelMidCall},
		{"trim error", buildHardFailTrimError},
		{"budget error", buildHardFailBudgetError},
		{"planHistory error", buildHardFailPlanHistoryError},
		{"audit error", buildHardFailAuditError},
		{"token-budget-exceeded error", buildHardFailTokenBudgetExceeded},
		{"tool-call error", buildHardFailToolCallError},
		{"calls-per-turn-exceeded error", buildHardFailCallsPerTurnExceeded},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bus := events.New()
			rec := &eventRecorder{}
			subscribeEvents(t, bus, rec.handle, agentloop.EventIterationEnd)
			loop, ctx, msgs := c.build(t, bus)
			_, err := loop.Run(ctx, msgs)
			if err == nil {
				t.Fatalf("Run() error = nil, want a hard-fail error")
			}
			got := rec.names()
			if len(got) != 1 || got[0] != agentloop.EventIterationEnd {
				t.Fatalf("EventIterationEnd events = %v, want exactly one", got)
			}
		})
	}
}

// delayedRecoveryCompleter answers Chat like scriptedCompleter, with
// one call in flight for delay before it answers: the recovery-retry
// equivalent of slowCompleter, letting a heartbeat test observe ticks
// across both of runChat's Chat calls (the rejected original and the
// recovery retry), not only its outer boundary.
type delayedRecoveryCompleter struct {
	delay     time.Duration
	responses []provider.Response
	errs      []error

	mu    sync.Mutex
	calls int
}

func (d *delayedRecoveryCompleter) Name() string { return "delayed-recovery" }

func (d *delayedRecoveryCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	d.mu.Lock()
	idx := d.calls
	d.calls++
	d.mu.Unlock()
	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		return provider.Response{}, ctx.Err()
	}
	if idx < len(d.errs) && d.errs[idx] != nil {
		return provider.Response{}, d.errs[idx]
	}
	return d.responses[idx], nil
}

func (d *delayedRecoveryCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("delayedRecoveryCompleter: ChatStream not supported")
}

func (d *delayedRecoveryCompleter) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// TestRunCompletionHeartbeatSpansPromptTooLongRecovery proves the
// completion-heartbeat ticker startHeartbeat starts around runChat in
// runIteration keeps ticking across runChat's internal
// recoverPromptTooLong retry, a second l.completer.Chat call nested
// inside the same closure the scope-tightening fix (commit 20be851)
// wraps. The outer closure brackets the whole runChat call, so a
// rejected-then-retried iteration is still "one Completer call in
// flight" for heartbeat purposes; a build that only ticked around the
// first physical Chat call would go silent during the retry and still
// pass every other heartbeat test, none of which exercises this path.
func TestRunCompletionHeartbeatSpansPromptTooLongRecovery(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer error = %v, want nil", err)
	}
	completer := &delayedRecoveryCompleter{
		delay:     heartbeatTestBlock,
		errs:      []error{provider.ErrPromptTooLong},
		responses: []provider.Response{{}, {Message: textMessage(provider.RoleAssistant, "done")}},
	}
	bus := events.New()
	ch, handler := eventChan()
	subscribeEvents(t, bus, handler, agentloop.EventCompletionHeartbeat)
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 4,
		Window: &w, Summarizer: sum, Calibrated: contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
		Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := loop.Run(context.Background(), msgs); err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	}()

	timer := time.NewTimer(heartbeatTestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("timed out after %v waiting for Run to return", heartbeatTestTimeout)
	}

	if got := completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want exactly 2: one rejection, one retry", got)
	}
	got := len(drainAllBuffered(ch))
	if got == 0 {
		t.Fatalf("EventCompletionHeartbeat count = 0, want > 0 across both the rejected call and the retry")
	}
}
