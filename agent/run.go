package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// AckWait resolves one step's ack. Run calls it once per step
// flow.Run gates behind Confirm, with the signed step message. It
// returns the receiver's real envelope.Ack, or an error. An
// implementation wraps ErrEscalated with %w to route the step to a
// human instead of resolving an ack.
type AckWait func(ctx context.Context, msg envelope.Message) (envelope.Ack, error)

// Sentinel errors for Run; test with errors.Is. ErrNoBus, already
// exported by the phase 20 translator, is reused for a nil bus.
var (
	ErrEscalated = errors.New("agent: step escalated")
	ErrNoWait    = errors.New("agent: wait is required")
	ErrNoThread  = errors.New("agent: thread id is required")
)

// Run drives a's bound plan (the *flow.Definition New bound) through
// flow.Run, in-process. threadID names the one envelope thread this
// run's step messages share. m is the status model the plan's steps
// target. in is the starting record. wait resolves each gated step's
// ack. bus receives MessageDeliveredEvent, MessageAckedEvent, and,
// once per successful run with one or more gated steps,
// ThreadVerifiedEvent.
//
// Run checks wait for nil, then bus for nil, then threadID for
// empty, in that order, before it touches m or a's plan; each check
// returns machine.Status(""), in unchanged, and its sentinel.
//
// For each step flow.Run gates behind Confirm, Run builds an
// envelope.Message from the step's ID, threadID, and Payload, with
// Version, Intent, and Epistemic set to values that pass Validate on
// their own, signs it with a's identity, and chains it to the
// previous step message with PrevHash. It calls EmitMessageDelivered,
// then wait. A wait error returns unchanged, without calling
// EmitMessageAcked. A nil wait error runs EmitMessageAcked and
// requires AckConfirmed before the step counts as done.
//
// On a successful run with one or more gated steps, Run calls
// EmitThreadVerified once, over every step message it built, in
// order.
func (a *Agent) Run(
	ctx context.Context, threadID string, m *machine.Definition,
	in machine.InOut, wait AckWait, bus *events.Bus,
) (machine.Status, machine.InOut, error) {
	if wait == nil {
		return machine.Status(""), in, ErrNoWait
	}
	if bus == nil {
		return machine.Status(""), in, ErrNoBus
	}
	if threadID == "" {
		return machine.Status(""), in, ErrNoThread
	}

	var built []envelope.Message
	confirm := a.confirmStep(threadID, wait, bus, &built)

	status, rec, err := flow.Run(ctx, a.plan, m, in, confirm, bus)
	if err != nil {
		return status, rec, err
	}
	if len(built) == 0 {
		return status, rec, nil
	}
	if err := EmitThreadVerified(ctx, bus, built); err != nil {
		return status, rec, err
	}
	return status, rec, nil
}

// confirmStep builds the flow.Confirm closure Run hands to flow.Run.
// built accumulates every message the closure signs, in step order,
// so Run can verify the full thread once the walk finishes. Run
// calls confirmStep sequentially per gated step; flow.Run never runs
// two Confirm calls concurrently, so built needs no lock.
func (a *Agent) confirmStep(threadID string, wait AckWait, bus *events.Bus, built *[]envelope.Message) flow.Confirm {
	return func(ctx context.Context, step flow.Step) error {
		msg := envelope.Message{
			Version:   envelope.Version,
			ID:        step.ID,
			ThreadID:  threadID,
			Intent:    envelope.IntentRequest,
			Epistemic: envelope.EpistemicAssumed,
			Payload:   step.Payload,
		}
		if n := len(*built); n > 0 {
			msg.PrevHash = (*built)[n-1].Hash()
		}
		signed, err := a.id.Sign(msg)
		if err != nil {
			return err
		}
		if err := EmitMessageDelivered(ctx, bus, signed); err != nil {
			return err
		}
		ack, err := wait(ctx, signed)
		if err != nil {
			return err
		}
		if err := EmitMessageAcked(ctx, bus, ack); err != nil {
			return err
		}
		if ack.Status != envelope.AckConfirmed {
			return fmt.Errorf("agent: step %q ack status %q, want %q", step.ID, ack.Status, envelope.AckConfirmed)
		}
		*built = append(*built, signed)
		return nil
	}
}
