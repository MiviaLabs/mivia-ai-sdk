package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// ErrNoBus is the sentinel every EmitX function returns when its bus
// argument is nil. It replaces a nil-pointer panic inside
// events.Bus.Emit; panics inside a package violate AGENTS.md.
var ErrNoBus = errors.New("agent: bus is required")

// EmitMessageDelivered verifies m's signature, then emits one Event
// named MessageDeliveredEvent onto bus. It returns ErrNoBus when bus
// is nil. It returns the VerifySignature error, unwrapped, when m
// fails to verify; neither failure emits an event. On success it
// returns the raw error from bus.Emit.
func EmitMessageDelivered(ctx context.Context, bus *events.Bus, m envelope.Message) error {
	if bus == nil {
		return ErrNoBus
	}
	if err := m.VerifySignature(); err != nil {
		return err
	}
	return bus.Emit(ctx, events.Event{
		Name: MessageDeliveredEvent,
		Data: fmt.Sprintf("message %s delivered", m.ID),
	})
}

// EmitMessageAcked validates a, then emits one Event named
// MessageAckedEvent onto bus. It returns ErrNoBus when bus is nil. It
// returns the Validate error, unwrapped, when a fails to validate;
// neither failure emits an event. On success it returns the raw
// error from bus.Emit.
func EmitMessageAcked(ctx context.Context, bus *events.Bus, a envelope.Ack) error {
	if bus == nil {
		return ErrNoBus
	}
	if err := a.Validate(); err != nil {
		return err
	}
	return bus.Emit(ctx, events.Event{
		Name: MessageAckedEvent,
		Data: fmt.Sprintf("ack for message %s status %s", a.MessageID, a.Status),
	})
}

// EmitThreadVerified verifies msgs as one hash-linked thread, then
// emits one Event named ThreadVerifiedEvent onto bus. It returns
// ErrNoBus when bus is nil. It returns the VerifyThread error,
// unwrapped, when the thread fails to verify; neither failure emits
// an event. On success it returns the raw error from bus.Emit.
func EmitThreadVerified(ctx context.Context, bus *events.Bus, msgs []envelope.Message) error {
	if bus == nil {
		return ErrNoBus
	}
	if err := envelope.VerifyThread(msgs); err != nil {
		return err
	}
	return bus.Emit(ctx, events.Event{
		Name: ThreadVerifiedEvent,
		Data: fmt.Sprintf("thread of %d messages verified", len(msgs)),
	})
}
