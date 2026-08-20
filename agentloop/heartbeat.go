package agentloop

import (
	"context"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// The Event Names agentloop emits on Options.Bus when
// Options.HeartbeatInterval is positive.
const (
	// EventIterationStart fires once at the start of every iteration.
	EventIterationStart events.Name = "agentloop.iteration.start"
	// EventCompletionHeartbeat fires every HeartbeatInterval while one
	// Completer call is in flight.
	EventCompletionHeartbeat events.Name = "agentloop.completion.heartbeat"
	// EventToolCallStart fires once at the start of every tool call,
	// before the PointPreTool hook fires.
	EventToolCallStart events.Name = "agentloop.tool_call.start"
	// EventToolCallHeartbeat fires every HeartbeatInterval while one
	// tool call is in flight. Never fires for a PointPreTool-vetoed
	// call, since a vetoed call never reaches the blocking work a
	// heartbeat reports progress on.
	EventToolCallHeartbeat events.Name = "agentloop.tool_call.heartbeat"
	// EventToolCallEnd fires once at the end of every tool call,
	// including a PointPreTool veto or hook-error return.
	EventToolCallEnd events.Name = "agentloop.tool_call.end"
	// EventIterationEnd fires once at the end of every iteration,
	// covering every exit path: a graceful stop or a hard-fail error.
	EventIterationEnd events.Name = "agentloop.iteration.end"
)

// emitEvent emits name on l.bus with data as the payload when
// l.heartbeat is positive. Silent otherwise: HeartbeatInterval gates
// every event this file defines, not only the ticking ones. Bus.Emit
// errors, including "no subscriber for name", are swallowed, matching
// the fireStop/hooks.Registry.Fire-swallow precedent in run.go.
func (l *Loop) emitEvent(ctx context.Context, name events.Name, data string) {
	if l.heartbeat <= 0 || l.bus == nil {
		return
	}
	_ = l.bus.Emit(ctx, events.Event{Name: name, Data: data})
}

// startHeartbeat starts a time.Ticker at l.heartbeat that emits name
// with data on l.bus every tick, until the returned stop is called.
// stop stops the ticker and joins the goroutine before returning, so
// no goroutine or ticker outlives the call this brackets. A
// non-positive l.heartbeat starts nothing; stop is then a no-op.
func (l *Loop) startHeartbeat(ctx context.Context, name events.Name, data string) (stop func()) {
	if l.heartbeat <= 0 {
		return func() {}
	}
	ticker := time.NewTicker(l.heartbeat)
	done := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for {
			select {
			case <-ticker.C:
				l.emitEvent(ctx, name, data)
			case <-done:
				return
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
		<-joined
	}
}

// iterationLabel builds the Data string for an iteration-scoped event.
// n is 1-based, matching AuditRecord.Iteration's convention.
func iterationLabel(n int) string {
	return fmt.Sprintf("iteration %d", n)
}

// toolCallLabel builds the Data string for a tool-call-scoped event.
func toolCallLabel(iteration int, call provider.ToolCall) string {
	return fmt.Sprintf("iteration %d: tool call %s (%s)", iteration, call.ID, call.Name)
}
