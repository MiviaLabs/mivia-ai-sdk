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
// docs/plans/agents/phase83_heartbeat_events.md.
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
// events before Run returns.
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
		if e.Data == "" {
			t.Fatalf("event Data is empty, want a non-empty label")
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

// TestRunHeartbeatRace runs the completion heartbeat concurrently with
// the main loop's own state changes and must pass under go test
// -race.
func TestRunHeartbeatRace(t *testing.T) {
	tool := &slowTool{name: "slow", delay: heartbeatTestBlock, result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	bus := events.New()
	var received int
	var mu sync.Mutex
	countBeat := func(ctx context.Context, e events.Event) error {
		mu.Lock()
		received++
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
}
