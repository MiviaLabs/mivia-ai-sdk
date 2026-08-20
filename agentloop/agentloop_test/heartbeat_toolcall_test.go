package agentloop_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunHeartbeatGoroutineLeakOnCtxCancel proves the completion-
// heartbeat ticker goroutine started around a Completer call does not
// leak when ctx is canceled mid-call.
func TestRunHeartbeatGoroutineLeakOnCtxCancel(t *testing.T) {
	bus := events.New()
	completer := &slowCompleter{delay: heartbeatTestBlock, resp: provider.Response{Message: textMessage(provider.RoleAssistant, "hi")}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 1, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	before := countNumGoroutine(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-time.After(heartbeatTestInterval)
		cancel()
	}()
	_, err = loop.Run(ctx, []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	assertNoGoroutineLeak(t, before)
}

// TestRunHeartbeatGoroutineLeakNoTick proves the completion-heartbeat
// ticker goroutine does not leak when the Completer call returns
// before the first tick fires.
func TestRunHeartbeatGoroutineLeakNoTick(t *testing.T) {
	bus := events.New()
	completer := &scriptedCompleter{responses: []provider.Response{{Message: textMessage(provider.RoleAssistant, "hi")}}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 1, Bus: bus, HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	before := countNumGoroutine(t)
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	assertNoGoroutineLeak(t, before)
}

// TestRunOneToolCallVetoEventOrder proves a PointPreTool veto still
// produces EventToolCallStart immediately followed by
// EventToolCallEnd, with no EventToolCallHeartbeat in between and no
// heartbeat ticker started. The event sequence alone cannot prove the
// last part: Run returns in well under one heartbeatTestInterval, so
// a bug that started the ticker before the veto check, instead of
// after, would still emit zero heartbeat events in that window. The
// goroutine-count check below closes that gap: a ticker started but
// never stopped (no deferred stop reachable from the veto's early
// return) leaks its goroutine, which assertNoGoroutineLeak catches
// even though the event assertion would not.
func TestRunOneToolCallVetoEventOrder(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "veto", func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	bus := events.New()
	rec := &eventRecorder{}
	subscribeEvents(t, bus, rec.handle, agentloop.EventToolCallStart, agentloop.EventToolCallEnd, agentloop.EventToolCallHeartbeat)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	before := countNumGoroutine(t)
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopHookVeto {
		t.Fatalf("Stop = %v, want StopHookVeto", res.Stop)
	}
	got := rec.names()
	want := []events.Name{agentloop.EventToolCallStart, agentloop.EventToolCallEnd}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}
	assertNoGoroutineLeak(t, before)
}

// TestRunOneToolCallHookErrorEventOrder proves a non-veto PointPreTool
// hook error still produces EventToolCallStart immediately followed
// by EventToolCallEnd, with no EventToolCallHeartbeat in between and
// no heartbeat ticker started, matching the veto path's bracket. See
// TestRunOneToolCallVetoEventOrder's comment for why the goroutine
// check, not the event assertion, is what actually proves the ticker
// never started.
func TestRunOneToolCallHookErrorEventOrder(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "boom", func(ctx context.Context, payload any) (bool, error) {
		return false, errBoom
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	bus := events.New()
	rec := &eventRecorder{}
	subscribeEvents(t, bus, rec.handle, agentloop.EventToolCallStart, agentloop.EventToolCallEnd, agentloop.EventToolCallHeartbeat)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	before := countNumGoroutine(t)
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); !errors.Is(err, errBoom) {
		t.Fatalf("Run() error = %v, want errBoom", err)
	}
	got := rec.names()
	want := []events.Name{agentloop.EventToolCallStart, agentloop.EventToolCallEnd}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}
	assertNoGoroutineLeak(t, before)
}

// TestRunOneToolCallHeartbeatPositivePath proves a non-vetoed tool
// call that blocks past two heartbeat intervals emits, in order,
// EventToolCallStart, at least two EventToolCallHeartbeat, then
// EventToolCallEnd.
func TestRunOneToolCallHeartbeatPositivePath(t *testing.T) {
	tool := &slowTool{name: "slow", delay: heartbeatTestBlock, result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	bus := events.New()
	ch, handler := eventChan()
	subscribeEvents(t, bus, handler, agentloop.EventToolCallStart, agentloop.EventToolCallHeartbeat, agentloop.EventToolCallEnd)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "slow", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
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

	timer := time.NewTimer(heartbeatTestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("timed out after %v waiting for Run to return", heartbeatTestTimeout)
	}

	got := drainAllBuffered(ch)
	assertToolCallHeartbeatSequence(t, got)
	wantData := "iteration 1: tool call call-1 (slow)"
	for _, e := range got {
		if e.Data != wantData {
			t.Fatalf("event %s Data = %q, want %q", e.Name, e.Data, wantData)
		}
	}
}

// TestRunHeartbeatGoroutineLeakAfterMultipleTicks proves the
// completion-heartbeat and tool-call-heartbeat ticker goroutines do
// not leak when stop() is called after the ticker has already fired
// more than once, complementing
// TestRunHeartbeatGoroutineLeakOnCtxCancel (stop before any tick) and
// TestRunHeartbeatGoroutineLeakNoTick (stop before the first tick).
func TestRunHeartbeatGoroutineLeakAfterMultipleTicks(t *testing.T) {
	tool := &slowTool{name: "slow", delay: heartbeatTestBlock, result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	bus := events.New()
	completer := &slowCompleter{delay: heartbeatTestBlock, resp: toolCallResponse(provider.ToolCall{ID: "call-1", Name: "slow", Arguments: []byte("{}")})}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 1, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	before := countNumGoroutine(t)
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	assertNoGoroutineLeak(t, before)
}

// panickingCompleter panics after delay instead of returning, so a
// caller must recover to keep the test process alive.
type panickingCompleter struct {
	delay time.Duration
}

func (p *panickingCompleter) Name() string { return "panicking" }

func (p *panickingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	<-time.After(p.delay)
	panic("panickingCompleter: boom")
}

func (p *panickingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("panickingCompleter: ChatStream not supported")
}

// runRecoveringPanic calls fn in a goroutine, recovers any panic, and
// blocks until fn returns or panics.
func runRecoveringPanic(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		fn()
	}()
	timer := time.NewTimer(heartbeatTestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("timed out after %v waiting for the panicking call to return", heartbeatTestTimeout)
	}
}

// TestRunHeartbeatGoroutineLeakOnCompleterPanic proves the completion-
// heartbeat ticker goroutine does not leak when the Completer call it
// brackets panics: stopHeartbeat must run via defer, not a plain
// statement that a panic would skip.
func TestRunHeartbeatGoroutineLeakOnCompleterPanic(t *testing.T) {
	bus := events.New()
	completer := &panickingCompleter{delay: heartbeatTestBlock}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 1, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	before := countNumGoroutine(t)
	runRecoveringPanic(t, func() {
		_, _ = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	})
	assertNoGoroutineLeak(t, before)
}

// panickingTool implements tools.SchemaTool. Run panics after delay
// instead of returning, so a caller must recover to keep the test
// process alive.
type panickingTool struct {
	name  string
	delay time.Duration
}

func (t *panickingTool) Name() string { return t.name }

func (t *panickingTool) ParameterSchema() []byte { return []byte(`{}`) }

func (t *panickingTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

func (t *panickingTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	<-time.After(t.delay)
	panic("panickingTool: boom")
}

// TestRunHeartbeatGoroutineLeakOnToolCallPanic proves the tool-call-
// heartbeat ticker goroutine does not leak when the tool call it
// brackets panics: stopHeartbeat must run via defer, not a plain
// statement that a panic would skip.
func TestRunHeartbeatGoroutineLeakOnToolCallPanic(t *testing.T) {
	tool := &panickingTool{name: "boom", delay: heartbeatTestBlock}
	reg := tools.New()
	mustAdd(t, reg, tool)
	bus := events.New()
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "boom", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	before := countNumGoroutine(t)
	runRecoveringPanic(t, func() {
		_, _ = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	})
	assertNoGoroutineLeak(t, before)
}

// TestRunCompletionHeartbeatStopsWhenChatReturns proves the
// completion-heartbeat ticker stops as soon as the Completer call it
// brackets returns, not when runIteration itself returns. A tool call
// dispatched from the same iteration blocks four times longer than
// the Completer call; if the ticker incorrectly kept running past
// Chat's return (its documented scope, heartbeat.go's
// EventCompletionHeartbeat comment: "while one Completer call is in
// flight"), events would keep arriving while the tool call is still
// in flight.
func TestRunCompletionHeartbeatStopsWhenChatReturns(t *testing.T) {
	tool := &slowTool{name: "slow", delay: 4 * heartbeatTestBlock, result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	bus := events.New()
	ch, handler := eventChan()
	subscribeEvents(t, bus, handler, agentloop.EventCompletionHeartbeat)
	completer := &slowCompleter{
		delay: heartbeatTestBlock,
		resp:  toolCallResponse(provider.ToolCall{ID: "call-1", Name: "slow", Arguments: []byte("{}")}),
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 1, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
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

	<-time.After(2 * heartbeatTestBlock)
	afterChat := len(drainAllBuffered(ch))
	if afterChat == 0 {
		t.Fatalf("EventCompletionHeartbeat count after Chat returns = 0, want > 0: the ticker never fired, so this test cannot prove it stopped")
	}

	<-time.After(heartbeatTestBlock)
	duringTool := len(drainAllBuffered(ch))
	if duringTool != 0 {
		t.Fatalf("EventCompletionHeartbeat count while the tool call is still in flight = %d, want 0: the ticker must stop when Chat returns, not when runIteration returns", duringTool)
	}

	timer := time.NewTimer(heartbeatTestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("timed out after %v waiting for Run to return", heartbeatTestTimeout)
	}
}

// TestRunToolCallHeartbeatStopsWhenToolReturns proves the tool-call-
// heartbeat ticker stops as soon as the tool's Run call it brackets
// returns, not when runOneToolCall itself returns. A PointPostTool
// hook wired on the same call blocks four times longer than the tool
// call; if the ticker incorrectly kept running past decodeAndRun's
// return (its documented scope, heartbeat.go's EventToolCallHeartbeat
// comment: "while one tool call is in flight"), events would keep
// arriving while the PostTool hook is still in flight.
func TestRunToolCallHeartbeatStopsWhenToolReturns(t *testing.T) {
	tool := &slowTool{name: "slow", delay: heartbeatTestBlock, result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPostTool, "slow-post", func(ctx context.Context, payload any) (bool, error) {
		<-time.After(4 * heartbeatTestBlock)
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	bus := events.New()
	ch, handler := eventChan()
	subscribeEvents(t, bus, handler, agentloop.EventToolCallHeartbeat)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "slow", Arguments: []byte("{}")}),
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 1, Hooks: hreg, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
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

	<-time.After(2 * heartbeatTestBlock)
	afterTool := len(drainAllBuffered(ch))
	if afterTool == 0 {
		t.Fatalf("EventToolCallHeartbeat count after the tool returns = 0, want > 0: the ticker never fired, so this test cannot prove it stopped")
	}

	<-time.After(heartbeatTestBlock)
	duringPostHook := len(drainAllBuffered(ch))
	if duringPostHook != 0 {
		t.Fatalf("EventToolCallHeartbeat count while PointPostTool is still in flight = %d, want 0: the ticker must stop when the tool returns, not when runOneToolCall returns", duringPostHook)
	}

	timer := time.NewTimer(heartbeatTestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("timed out after %v waiting for Run to return", heartbeatTestTimeout)
	}
}

// drainAllBuffered reads every event currently buffered on ch without
// blocking, returning them in arrival order.
func drainAllBuffered(ch <-chan events.Event) []events.Event {
	var got []events.Event
	for {
		select {
		case e := <-ch:
			got = append(got, e)
		default:
			return got
		}
	}
}

// assertToolCallHeartbeatSequence asserts got is Start, at least two
// Heartbeat, then End, in that order.
func assertToolCallHeartbeatSequence(t *testing.T, got []events.Event) {
	t.Helper()
	if len(got) < 3 {
		t.Fatalf("event count = %d, want at least 3 (start, >=2 heartbeat, end): %+v", len(got), got)
	}
	if got[0].Name != agentloop.EventToolCallStart {
		t.Fatalf("first event = %s, want EventToolCallStart", got[0].Name)
	}
	last := got[len(got)-1]
	if last.Name != agentloop.EventToolCallEnd {
		t.Fatalf("last event = %s, want EventToolCallEnd", last.Name)
	}
	middle := got[1 : len(got)-1]
	if len(middle) < 2 {
		t.Fatalf("heartbeat count = %d, want at least 2: %+v", len(middle), middle)
	}
	for _, e := range middle {
		if e.Name != agentloop.EventToolCallHeartbeat {
			t.Fatalf("middle event = %s, want EventToolCallHeartbeat", e.Name)
		}
	}
}
