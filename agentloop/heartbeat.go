package agentloop

import (
	"context"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// The Event Names agentloop emits on Options.Bus. The four
// lifecycle names fire once at their boundary whenever Bus is
// non-nil; the two heartbeat names tick at HeartbeatInterval only.
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
	// EventAssistant fires once per completed Completer turn whose
	// Message role is assistant, with the message content as Data.
	EventAssistant events.Name = "agentloop.assistant"
	// EventThinkingStart fires at the start of the thinking bracket
	// for one assistant turn that produced ReasoningContent.
	EventThinkingStart events.Name = "agentloop.thinking.start"
	// EventThinkingDelta carries one assistant turn's ReasoningContent
	// as Data, between EventThinkingStart and EventThinkingEnd.
	EventThinkingDelta events.Name = "agentloop.thinking.delta"
	// EventThinkingEnd closes the thinking bracket for one assistant
	// turn that produced ReasoningContent.
	EventThinkingEnd events.Name = "agentloop.thinking.end"
	// EventCacheUsage fires after a Completer turn whose response
	// reported prompt-cache accounting; Data is the JSON-encoded
	// provider.CacheUsage.
	EventCacheUsage events.Name = "agentloop.cache_usage"
	// EventCalibrationDelta fires after every Calibrated.Observe call;
	// Data is the JSON-encoded calibrationPayload.
	EventCalibrationDelta events.Name = "agentloop.calibration_delta"
	// EventToolParallel fires once per turn dispatched with more than
	// one tool call, before the calls run; Data names the call count.
	EventToolParallel events.Name = "agentloop.tool_parallel"
)

// emitEvent emits name on l.bus with data as the payload when
// l.bus is non-nil. Lifecycle events therefore fire for any caller
// that wires a Bus, without also arming a heartbeat cadence;
// HeartbeatInterval gates only the ticking names, through
// startHeartbeat. Bus.Emit errors, including "no subscriber for
// name", are swallowed, matching the fireStop/hooks.Registry.
// Fire-swallow precedent in run.go.
func (l *Loop) emitEvent(ctx context.Context, name events.Name, data string) {
	if l.bus == nil {
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

// parallelLabel builds the Data string for EventToolParallel: the
// turn's tool-call count, the number a renderer highlights as the
// parallel-dispatch width.
func parallelLabel(n int) string {
	return fmt.Sprintf("parallel tool calls: %d", n)
}

// emitThinkingEvents fires the Start/Delta/End bracket for one
// assistant turn whose ReasoningContent is non-empty. A turn with
// an empty ReasoningContent fires nothing, so the bracket remains a
// faithful signal of "this turn produced chain-of-thought" without
// also tagging turns whose chain-of-thought was off.
func (l *Loop) emitThinkingEvents(ctx context.Context, reasoning string) {
	if reasoning == "" {
		return
	}
	l.emitEvent(ctx, EventThinkingStart, reasoning)
	l.emitEvent(ctx, EventThinkingDelta, reasoning)
	l.emitEvent(ctx, EventThinkingEnd, reasoning)
}

// calibrationPayload is the JSON shape EventCalibrationDelta's Data
// carries. Estimated is the value l.calibrated.EstimateTokens
// returned for this iteration's request; Actual is the response's
// provider.Usage.TotalTokens.
type calibrationPayload struct {
	Estimated int `json:"estimated"`
	Actual    int `json:"actual"`
}
