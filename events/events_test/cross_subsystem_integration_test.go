package events_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// e2eMember holds a postable identity for the cross-subsystem test.
// The public key is the envelope signer and the room member id.
type e2eMember struct {
	key ed25519.PrivateKey
	id  string
}

// newE2EMember generates a key and binds its hex public key.
func newE2EMember(t *testing.T) e2eMember {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return e2eMember{key: key, id: hex.EncodeToString(pub)}
}

// post signs, encodes, decodes, and verifies a message for a member.
func (m e2eMember) post(t *testing.T, msg envelope.Message) envelope.Message {
	t.Helper()
	signed, err := envelope.Sign(m.key, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	data, err := signed.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := envelope.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return got
}

// e2eMessage builds a valid request message for a room.
func e2eMessage(roomID string) envelope.Message {
	return envelope.Message{
		Version:    envelope.Version,
		Room:       roomID,
		ThreadID:   "thread-e2e",
		Intent:     envelope.IntentRequest,
		Epistemic:  envelope.EpistemicInferred,
		Confidence: 0.8,
		Provenance: envelope.Provenance{Source: "model:e2e"},
		Payload:    "Drive the move.",
	}
}

// e2eMachine builds a real machine definition for the run.
// idle starts. start moves idle to running. stop moves running to done
// through a guard that rejects once.
func e2eMachine() *machine.Definition {
	guardCalls := 0
	d, err := machine.New(
		machine.Status("idle"),
		machine.Transition{
			From:    machine.Status("idle"),
			To:      machine.Status("running"),
			Trigger: machine.Trigger("start"),
		},
		machine.Transition{
			From:    machine.Status("running"),
			To:      machine.Status("done"),
			Trigger: machine.Trigger("stop"),
			Guard: func(context.Context) (bool, error) {
				guardCalls++
				return guardCalls > 1, nil
			},
		},
	)
	if err != nil {
		panic("e2eMachine: " + err.Error())
	}
	return d
}

// moveData renders the data a caller emits after a successful Fire.
func moveData(from, to machine.Status) string {
	return string(from) + "->" + string(to)
}

// TestCrossSubsystemComposes proves envelope, room, machine, and events
// compose through their public APIs. A member posts a signed message
// that the room admits; a separate machine move fires onto a caller-owned
// bus under the typed event name; a guard failure emits nothing.
func TestCrossSubsystemComposes(t *testing.T) {
	founder := newE2EMember(t)
	member := newE2EMember(t)

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(member.id, founder.id); err != nil {
		t.Fatalf("admit member: %v", err)
	}

	// Envelope + room: a member's signed message passes admission.
	msg := e2eMessage(r.ID())
	msg.ID = "e2e-1"
	msg.To = []string{founder.id}
	msg = member.post(t, msg)
	if err := r.Accepts(msg); err != nil {
		t.Fatalf("room rejected a signed member message: %v", err)
	}
	if !r.IsMember(member.id) {
		t.Fatal("member is not on the roster after admission")
	}

	// Machine + events: a real move fires onto a caller-owned bus.
	d := e2eMachine()
	bus := events.New()
	var got []string
	if err := bus.Subscribe(machine.MoveEvent, func(_ context.Context, e events.Event) error {
		got = append(got, e.Data)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ctx := context.Background()
	to, _, err := d.Fire(ctx, "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if err := bus.Emit(ctx, events.Event{Name: machine.MoveEvent, Data: moveData("idle", to)}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	want := []string{moveData("idle", machine.Status("running"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bus events = %v, want %v", got, want)
	}
}

// TestCrossSubsystemGuardFailure returns an error on a rejected move while
// the envelope and room path is healthy. The caller never emits after a
// failed Fire, so no event can reach the bus. The Fire error is the
// contract; the bus is caller-owned.
func TestCrossSubsystemGuardFailure(t *testing.T) {
	founder := newE2EMember(t)
	member := newE2EMember(t)

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(member.id, founder.id); err != nil {
		t.Fatalf("admit member: %v", err)
	}
	msg := e2eMessage(r.ID())
	msg.ID = "e2e-2"
	msg = member.post(t, msg)
	if err := r.Accepts(msg); err != nil {
		t.Fatalf("room rejected a signed member message: %v", err)
	}
	if !r.IsMember(member.id) {
		t.Fatal("member is not on the roster after admission")
	}

	d := e2eMachine()
	ctx := context.Background()
	to, _, err := d.Fire(ctx, "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("Fire start: %v", err)
	}
	// The first stop fails the guard, so Fire returns an error.
	_, _, err = d.Fire(ctx, to, "stop", machine.InOut{})
	if err == nil {
		t.Fatal("Fire stop: expected a guard-failure error")
	}
}

// moveReportTool reports a machine move string back through Run. It
// proves a real tools.Tool implementation, not a fake, crosses the
// tools.Registry boundary.
type moveReportTool struct{}

func (moveReportTool) Name() string { return "move-report" }

func (moveReportTool) Run(_ context.Context, in tools.InOut) (tools.Out, error) {
	move, _ := in.Value.(string)
	return tools.Out{Value: "reported:" + move}, nil
}

// TestCrossSubsystemToolsCompose proves envelope, room, machine, tools,
// and events compose through their public APIs. A member posts a signed
// message the room admits; a real machine move fires; the move's data
// runs through a registered tools.Tool; the tool's real output, not a
// stand-in, reaches a caller-owned bus.
func TestCrossSubsystemToolsCompose(t *testing.T) {
	founder := newE2EMember(t)
	member := newE2EMember(t)

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(member.id, founder.id); err != nil {
		t.Fatalf("admit member: %v", err)
	}

	msg := e2eMessage(r.ID())
	msg.ID = "e2e-tools-1"
	msg = member.post(t, msg)
	if err := r.Accepts(msg); err != nil {
		t.Fatalf("room rejected a signed member message: %v", err)
	}

	d := e2eMachine()
	reg := tools.New()
	if err := reg.Add(moveReportTool{}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	bus := events.New()
	var got []string
	if err := bus.Subscribe(machine.MoveEvent, func(_ context.Context, e events.Event) error {
		got = append(got, e.Data)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ctx := context.Background()
	to, _, err := d.Fire(ctx, "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	out, err := reg.Run(ctx, "move-report", tools.InOut{Value: moveData("idle", to)})
	if err != nil {
		t.Fatalf("Registry.Run: %v", err)
	}
	if err := bus.Emit(ctx, events.Event{Name: machine.MoveEvent, Data: out.Value.(string)}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	want := []string{"reported:" + moveData("idle", machine.Status("running"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bus events = %v, want %v", got, want)
	}
}

// TestCrossSubsystemToolsUnknownName returns an error for a move whose
// report tool was never registered, while the envelope, room, and
// machine path is healthy. The caller never emits after a failed Run,
// so no event can reach the bus.
func TestCrossSubsystemToolsUnknownName(t *testing.T) {
	founder := newE2EMember(t)
	member := newE2EMember(t)

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(member.id, founder.id); err != nil {
		t.Fatalf("admit member: %v", err)
	}
	msg := e2eMessage(r.ID())
	msg.ID = "e2e-tools-2"
	msg = member.post(t, msg)
	if err := r.Accepts(msg); err != nil {
		t.Fatalf("room rejected a signed member message: %v", err)
	}

	d := e2eMachine()
	reg := tools.New()
	ctx := context.Background()
	to, _, err := d.Fire(ctx, "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	_, err = reg.Run(ctx, "move-report", tools.InOut{Value: moveData("idle", to)})
	if !errors.Is(err, tools.ErrUnknownName) {
		t.Fatalf("Registry.Run: got %v, want ErrUnknownName", err)
	}
}
