// Package agent_test also holds the cross-package proof that a
// non-empty room argument to Run produces a message a real room.Room
// admits: agent, envelope, identity, and room compose through their
// public API, no fake stands in for the trust boundary.
package agent_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// TestRunRoomStampedAdmitsIntoRealRoom proves a Run-built message,
// with Room set through the new trailing parameter, is admitted by a
// real room.Room: a real identity signs the agent's step message, a
// real room.Room admits that identity's signer, and Accepts returns
// nil once Run stamps the matching room ID before signing.
func TestRunRoomStampedAdmitsIntoRealRoom(t *testing.T) {
	founder, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	rm, err := room.New("room-1", founder.Signer())
	if err != nil {
		t.Fatalf("room.New() unexpected error: %v", err)
	}

	a, m := oneStepFixture(t)
	bus := newRunBus(t)

	var admitErr error
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		if err := rm.Admit(msg.Signer, founder.Signer()); err != nil {
			t.Fatalf("room.Admit() unexpected error: %v", err)
		}
		admitErr = rm.Accepts(msg)
		return confirmingWait(ctx, msg)
	}

	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, rm.ID(), nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}
	if admitErr != nil {
		t.Fatalf("Room.Accepts() unexpected error: %v", admitErr)
	}
}

// TestRunRoomEmptyRejectedByRealRoom reruns the same setup with room
// left empty and proves Accepts now returns a non-nil error, pinning
// the gap this phase closes as a regression check, not only a
// new-behavior check.
func TestRunRoomEmptyRejectedByRealRoom(t *testing.T) {
	founder, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	rm, err := room.New("room-1", founder.Signer())
	if err != nil {
		t.Fatalf("room.New() unexpected error: %v", err)
	}

	a, m := oneStepFixture(t)
	bus := newRunBus(t)

	var admitErr error
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		if err := rm.Admit(msg.Signer, founder.Signer()); err != nil {
			t.Fatalf("room.Admit() unexpected error: %v", err)
		}
		admitErr = rm.Accepts(msg)
		return confirmingWait(ctx, msg)
	}

	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "", nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}
	if admitErr == nil {
		t.Fatal("Room.Accepts() returned a nil error, want a non-nil error for an unstamped Room")
	}
}
