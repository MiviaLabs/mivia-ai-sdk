package agentloop_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// The fixed timing convention every heartbeat test follows: a short
// interval, scripted blocking work past two intervals, and a
// generous, hard-failing timeout. See
// docs/plans/agentloop.md's heartbeat and progress events addendum.
const (
	heartbeatTestInterval = 5 * time.Millisecond
	heartbeatTestBlock    = 30 * time.Millisecond
	heartbeatTestTimeout  = 200 * time.Millisecond
)

// slowCompleter answers Chat with resp after delay, respecting ctx
// cancellation while it waits.
type slowCompleter struct {
	delay time.Duration
	resp  provider.Response
}

func (s *slowCompleter) Name() string { return "slow" }

func (s *slowCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	select {
	case <-time.After(s.delay):
		return s.resp, nil
	case <-ctx.Done():
		return provider.Response{}, ctx.Err()
	}
}

func (s *slowCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("slowCompleter: ChatStream not supported")
}

// slowTool implements tools.SchemaTool. Run sleeps delay before
// returning result.
type slowTool struct {
	name   string
	delay  time.Duration
	result any
}

func (t *slowTool) Name() string { return t.name }

func (t *slowTool) ParameterSchema() []byte { return []byte(`{}`) }

func (t *slowTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

func (t *slowTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	select {
	case <-time.After(t.delay):
		return tools.Out{Value: t.result}, nil
	case <-ctx.Done():
		return tools.Out{}, ctx.Err()
	}
}

// errEstimator always fails EstimateTokens, used to trigger
// agentloop.ErrPlanFailed from planHistory.
type errEstimator struct{}

func (errEstimator) EstimateTokens(req provider.Request) (int, error) {
	return 0, errBoom
}

// subscribeEvents subscribes handler to every name in names on bus,
// failing the test on a Subscribe error.
func subscribeEvents(t *testing.T, bus *events.Bus, handler events.Handler, names ...events.Name) {
	t.Helper()
	for _, name := range names {
		if err := bus.Subscribe(name, handler); err != nil {
			t.Fatalf("Subscribe(%s) error = %v, want nil", name, err)
		}
	}
}

// eventChan builds a buffered channel and a handler that pushes every
// received Event onto it, for the select-with-timeout convention.
func eventChan() (chan events.Event, events.Handler) {
	ch := make(chan events.Event, 256)
	return ch, func(ctx context.Context, e events.Event) error {
		ch <- e
		return nil
	}
}

// drainAtLeast reads at least n events from ch before deadline elapses
// and returns every event read. A timeout before n events arrive is a
// hard test failure.
func drainAtLeast(t *testing.T, ch <-chan events.Event, n int, deadline time.Duration) []events.Event {
	t.Helper()
	var got []events.Event
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for len(got) < n {
		select {
		case e := <-ch:
			got = append(got, e)
		case <-timer.C:
			t.Fatalf("timed out after %v waiting for %d events, got %d: %+v", deadline, n, len(got), got)
		}
	}
	return got
}

// assertNoEvent asserts no event arrives on ch within window.
func assertNoEvent(t *testing.T, ch <-chan events.Event, window time.Duration) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("received unexpected event %+v, want none", e)
	case <-time.After(window):
	}
}

// eventRecorder collects every Event a handler receives, in arrival
// order, guarded for concurrent use.
type eventRecorder struct {
	mu  sync.Mutex
	got []events.Event
}

func (r *eventRecorder) handle(ctx context.Context, e events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, e)
	return nil
}

func (r *eventRecorder) names() []events.Name {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]events.Name, len(r.got))
	for i, e := range r.got {
		names[i] = e.Name
	}
	return names
}

// events returns a copy of every Event recorded so far, in arrival
// order, so a test can inspect Data content as well as Name.
func (r *eventRecorder) events() []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Event(nil), r.got...)
}

// countNumGoroutine settles briefly and reports runtime.NumGoroutine,
// giving a just-stopped goroutine time to unwind.
func countNumGoroutine(t *testing.T) int {
	t.Helper()
	runtime.Gosched()
	<-time.After(2 * time.Millisecond)
	return runtime.NumGoroutine()
}

// assertNoGoroutineLeak polls runtime.NumGoroutine until it returns to
// at most before, or fails the test once deadline elapses.
func assertNoGoroutineLeak(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(heartbeatTestTimeout)
	for {
		now := runtime.NumGoroutine()
		if now <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: NumGoroutine() = %d, want <= %d", now, before)
		}
		<-time.After(5 * time.Millisecond)
	}
}

// TestRunCompletionHeartbeat proves a Completer call that blocks past
// two heartbeat intervals emits at least two EventCompletionHeartbeat
// events before Run returns, each one labeled for iteration 1. A
// non-empty check alone would still pass a build that labeled the
// heartbeat tick with the wrong iteration number, or any other
// non-empty placeholder distinct from the Start/End events' own
// label; pinning the exact string closes that gap.
func TestRunCompletionHeartbeat(t *testing.T) {
	bus := events.New()
	ch, handler := eventChan()
	subscribeEvents(t, bus, handler, agentloop.EventCompletionHeartbeat)
	completer := &slowCompleter{delay: heartbeatTestBlock, resp: provider.Response{Message: textMessage(provider.RoleAssistant, "done")}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 1,
		Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	}()
	got := drainAtLeast(t, ch, 2, heartbeatTestTimeout)
	for _, e := range got {
		if e.Name != agentloop.EventCompletionHeartbeat {
			t.Fatalf("event Name = %s, want EventCompletionHeartbeat", e.Name)
		}
		if e.Data != "iteration 1" {
			t.Fatalf("event Data = %q, want %q", e.Data, "iteration 1")
		}
	}
	<-done
}

// TestRunCompletionHeartbeatZeroIntervalNone proves a zero
// HeartbeatInterval emits no EventCompletionHeartbeat, even across a
// Completer call that blocks past what two intervals would have been.
func TestRunCompletionHeartbeatZeroIntervalNone(t *testing.T) {
	bus := events.New()
	ch, handler := eventChan()
	subscribeEvents(t, bus, handler, agentloop.EventCompletionHeartbeat)
	completer := &slowCompleter{delay: heartbeatTestBlock, resp: provider.Response{Message: textMessage(provider.RoleAssistant, "done")}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 1, Bus: bus,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	assertNoEvent(t, ch, 1*time.Millisecond)
}

// TestRunHeartbeatEmitSwallowsNoSubscriberError proves the heartbeat
// path swallows events.Bus.Emit's "no subscriber for name" error
// exactly like fireStop already swallows a hooks.Registry.Fire error:
// a Bus with no heartbeat-name subscriber still lets Run complete
// normally.
func TestRunHeartbeatEmitSwallowsNoSubscriberError(t *testing.T) {
	bus := events.New()
	completer := &slowCompleter{delay: heartbeatTestBlock, resp: provider.Response{Message: textMessage(provider.RoleAssistant, "done")}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 1, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
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
}

// TestRunZeroIntervalSuppressesAllEvents proves emitEvent's documented
// coupling: a zero HeartbeatInterval silences every event this
// package defines, not only the ticking heartbeat events. A Bus is
// wired and subscribed to every event name, and the run drives both
// an iteration and a tool call, so a bug that gates only the
// heartbeat ticks (leaving Start/End emitting regardless of
// l.heartbeat) would still fail this test.
func TestRunZeroIntervalSuppressesAllEvents(t *testing.T) {
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
		Completer: completer, Tools: reg, MaxIterations: 5, Bus: bus,
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
	if got := rec.names(); len(got) != 0 {
		t.Fatalf("events = %v, want none: HeartbeatInterval == 0 must gate every event, not only heartbeat ticks", got)
	}
}

// TestRunHeartbeatRace runs the completion heartbeat concurrently with
// the main loop's own state changes and must pass under go test
// -race. It also asserts both heartbeat kinds actually fired at least
// once: a race-clean but silent implementation must still fail this
// test.
func TestRunHeartbeatRace(t *testing.T) {
	tool := &slowTool{name: "slow", delay: heartbeatTestBlock, result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	bus := events.New()
	var completionBeats, toolBeats int
	var mu sync.Mutex
	countBeat := func(ctx context.Context, e events.Event) error {
		mu.Lock()
		switch e.Name {
		case agentloop.EventCompletionHeartbeat:
			completionBeats++
		case agentloop.EventToolCallHeartbeat:
			toolBeats++
		}
		mu.Unlock()
		return nil
	}
	subscribeEvents(t, bus, countBeat, agentloop.EventCompletionHeartbeat, agentloop.EventToolCallHeartbeat)
	completer := &slowCompleter{delay: heartbeatTestBlock, resp: toolCallResponse(provider.ToolCall{ID: "call-1", Name: "slow", Arguments: []byte("{}")})}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 1, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if completionBeats == 0 {
		t.Fatalf("completionBeats = 0, want > 0 (Completer call should tick at least once)")
	}
	if toolBeats == 0 {
		t.Fatalf("toolBeats = 0, want > 0 (tool call should tick at least once)")
	}
}
