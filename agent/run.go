package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
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
//
// hb is an optional step-liveness heartbeat. A nil hb skips every
// heartbeat call; Run's behavior is otherwise unchanged. A non-nil hb
// beats one id, a.id.Signer()+":"+threadID, right before each gated
// step's wait call, and forgets that id once, on every return path.
// A panel step reaches no beat call; see docs/plans/agents/
// phase26_agent_heartbeat.md's disclosed scope limit. Run never calls
// hb.Dead and never aborts a step on staleness; an external caller
// holding the same hb polls Dead on its own schedule.
//
// An empty room reproduces today's zero-value behavior: Message.Room
// stays "". A non-empty room makes confirmStep stamp it onto
// Message.Room before a.id.Sign runs, on every gated step's built
// message.
func (a *Agent) Run(
	ctx context.Context, threadID string, m *machine.Definition,
	in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor,
	room string,
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

	hbID := a.id.Signer() + ":" + threadID
	if hb != nil {
		defer hb.Forget(hbID)
	}

	var built []envelope.Message
	confirm := a.confirmStep(threadID, wait, bus, &built, hb, hbID, room)

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
// two Confirm calls concurrently, so built needs no lock. hb, when
// non-nil, beats hbID right before wait; a nil hb skips the beat. room,
// when non-empty, sets msg.Room before a.id.Sign; an empty room leaves
// Message.Room at the zero value.
func (a *Agent) confirmStep(threadID string, wait AckWait, bus *events.Bus, built *[]envelope.Message, hb *heartbeat.Monitor, hbID string, room string) flow.Confirm {
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
		if room != "" {
			msg.Room = room
		}
		signed, err := a.id.Sign(msg)
		if err != nil {
			return err
		}
		if err := EmitMessageDelivered(ctx, bus, signed); err != nil {
			return err
		}
		if hb != nil {
			_ = hb.Beat(hbID, time.Now())
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
