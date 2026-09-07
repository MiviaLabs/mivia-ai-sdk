package agentloop

import (
	"context"
	"io"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// Extensions groups the host-integration knobs one external loop
// mirror needs. Reached only through Options.Extensions; a nil
// pointer means every member at its zero value.
type Extensions struct {
	// OnToolCallError runs on the ErrorPolicyReport path after a
	// decodeAndRun or render failure. See ErrorFunc for the contract.
	OnToolCallError ErrorFunc
	// Surface, when non-nil, replaces the iteration's advertised
	// definitions, registry, and scope from iteration two onward.
	Surface func() *Surface
	// StreamingWriter, when non-nil, mirrors what the Completer
	// writes; on a Steered stop the buffered bytes become
	// Result.Final.Content. Must be safe for concurrent use.
	StreamingWriter io.Writer
	// Conclude groups the graceful-conclude terms; see the Conclude
	// type for Margin, Deadline, and Notice.
	Conclude Conclude
	// StartTime is the wall-clock anchor for Conclude.Deadline. Zero
	// falls back to the time of New.
	StartTime time.Time
	// DedupWithinTurn serves DuplicateCallNotice for a repeated
	// (tool, canonical-argument) call within one turn.
	DedupWithinTurn bool
	// WorkBudget, when non-nil, is the token-reservation surface the
	// loop invokes around each Completer call.
	WorkBudget *WorkBudget
	// ToolBudget, when non-nil, is the cumulative tool-call budget
	// invoked once per turn before dispatch.
	ToolBudget *ToolBudget
	// ContinueOnStop is consulted on every graceful stop; a non-empty
	// return continues the loop. See StopDecision.
	ContinueOnStop func(ctx context.Context, d StopDecision) []provider.Message
}
