package agentloop

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// StopReason names why Run stopped gracefully. No StopToolError
// constant exists: a tool error under ErrorPolicyFail is a hard
// failure, not a graceful stop.
type StopReason string

// The declared StopReason values.
const (
	// StopNoToolCalls is Run's stop reason when the model's response
	// carries no tool call and carries text content.
	StopNoToolCalls StopReason = "no_tool_calls"
	// StopEmptyResponse is Run's stop reason when the model returns no
	// tool call and no non-blank assistant text content.
	StopEmptyResponse StopReason = "empty_response"
	// StopMaxIterations is Run's stop reason when the iteration count
	// reaches Options.MaxIterations. Not an error.
	StopMaxIterations StopReason = "max_iterations"
	// StopHookVeto is Run's stop reason when a PointPreTool handler
	// vetoes a tool call. The tool does not run.
	StopHookVeto StopReason = "hook_veto"
	// StopConcluded is Run's stop reason when the model returns no tool
	// call on an iteration ConcludeMargin nudged. Graceful, same
	// Result-shape rule as StopNoToolCalls.
	StopConcluded StopReason = "concluded"
	// StopSteered is Run's stop reason when a Steer.Trigger call requests
	// a soft-cancel of the in-flight Completer.Chat call. Graceful: nil
	// error, the same Result-shape rule as every other graceful stop that
	// happens before a new response arrives.
	StopSteered StopReason = "steered"
	// StopRepeatedToolFailures is Run's stop reason when consecutive
	// turns each fail all dispatched tool calls with unknown tool
	// errors, reaching MaxConsecutiveToolFailures. Graceful, same
	// Result-shape rule as StopMaxIterations.
	StopRepeatedToolFailures StopReason = "repeated_tool_failures"
)

// StopDecision is the evidence the loop had when it decided to stop.
// Consulted only on a graceful stop; see Options.ContinueOnStop.
type StopDecision struct {
	// Stop is the graceful reason the loop picked.
	Stop StopReason
	// Message is the assistant turn that ended the run.
	Message provider.Message
	// ToolCalls is the response's tool-call list, empty at every
	// call site the loop consults the hook from.
	ToolCalls []provider.ToolCall
	// Iterations counts the Completer calls that completed.
	Iterations int
	// History carries every message appended so far.
	History []provider.Message
}

// gracefulStop consults l.continueOnStop and returns runToolStage's
// three values. A non-empty hook return continues the loop with the
// grown history; a nil hook, an empty return, or a panic keeps the
// stop or fails the run closed.
func (l *Loop) gracefulStop(ctx context.Context, history []provider.Message, resp provider.Response, iterations int, totalUsage provider.Usage, stop StopReason) (Result, bool, error) {
	if l.continueOnStop != nil {
		msgs, err := safeContinue(ctx, l.continueOnStop, StopDecision{
			Stop:       stop,
			Message:    resp.Message,
			ToolCalls:  resp.ToolCalls,
			Iterations: iterations,
			History:    history,
		})
		if err != nil {
			return l.hardFail(history, iterations, totalUsage), true, err
		}
		if len(msgs) > 0 {
			return Result{History: append(history, msgs...)}, false, nil
		}
	}
	return Result{Final: resp.Message, History: history, Iterations: iterations, Usage: totalUsage, Stop: stop}, true, nil
}

// safeContinue invokes fn, converting a panic into a plain error so a
// hostile host hook fails the run closed instead of half-continuing.
func safeContinue(ctx context.Context, fn func(context.Context, StopDecision) []provider.Message, d StopDecision) (msgs []provider.Message, err error) {
	defer func() {
		if r := recover(); r != nil {
			msgs = nil
			err = fmt.Errorf("agentloop: ContinueOnStop hook panicked: %v", r)
		}
	}()
	return fn(ctx, d), nil
}
