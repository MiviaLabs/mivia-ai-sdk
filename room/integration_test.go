package room_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

type agent struct {
	name string
	key  ed25519.PrivateKey
	id   string // hex public key = room member id = envelope signer
}

func newAgent(t *testing.T, name string) agent {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return agent{name: name, key: key, id: hex.EncodeToString(pub)}
}

func (a agent) post(t *testing.T, m envelope.Message) envelope.Message {
	t.Helper()
	signed, err := envelope.Sign(a.key, m)
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

func baseMessage(roomID string) envelope.Message {
	return envelope.Message{
		Version:    envelope.Version,
		Room:       roomID,
		ThreadID:   "thread-1",
		Intent:     envelope.IntentRequest,
		Epistemic:  envelope.EpistemicInferred,
		Confidence: 0.7,
		Provenance: envelope.Provenance{Source: "model:self"},
		Payload:    "Review the config loader.",
	}
}

// TestGroupLifecycle runs the full path: room setup, moderated admit,
// signed posting with admission gating, ack flow with attribution,
// thread chain verification, and post-removal rejection.
func TestGroupLifecycle(t *testing.T) {
	founder := newAgent(t, "founder")
	alice := newAgent(t, "alice")
	bob := newAgent(t, "bob")

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	for _, a := range []agent{alice, bob} {
		if err := r.Admit(a.id, founder.id); err != nil {
			t.Fatalf("admit %s: %v", a.name, err)
		}
	}

	// 1-to-many request from alice to bob.
	m1 := baseMessage(r.ID())
	m1.ID = "m1"
	m1.To = []string{bob.id}
	m1 = alice.post(t, m1)
	if err := r.Accepts(m1); err != nil {
		t.Fatalf("member message rejected: %v", err)
	}

	// Bob acks with attribution; alice confirms.
	ack, err := envelope.NewAck(m1, bob.id, "You want a review of the config loader.")
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if ack = ack.Confirm(); ack.From != bob.id {
		t.Fatalf("ack from = %q, want %q", ack.From, bob.id)
	}

	// Bob answers; chain grows.
	m2 := baseMessage(r.ID())
	m2.ID = "m2"
	m2.Intent = envelope.IntentAssert
	m2.To = []string{alice.id}
	m2.PrevHash = m1.Hash()
	m2.Payload = "The config loader reads mivia.toml first."
	m2 = bob.post(t, m2)
	if err := r.Accepts(m2); err != nil {
		t.Fatalf("reply rejected: %v", err)
	}
	if err := envelope.VerifyThread([]envelope.Message{m1, m2}); err != nil {
		t.Fatalf("thread chain rejected: %v", err)
	}

	// Tampered message fails verification even from a member.
	forged := m2
	forged.Payload = "Forged."
	if err := forged.VerifySignature(); err == nil {
		t.Fatal("forged message passed verification")
	}

	// Removed member can no longer post.
	if err := r.Remove(bob.id, founder.id); err != nil {
		t.Fatalf("remove bob: %v", err)
	}
	m3 := baseMessage(r.ID())
	m3.ID = "m3"
	m3 = bob.post(t, m3)
	if err := r.Accepts(m3); !errors.Is(err, room.ErrNotMember) {
		t.Fatalf("removed member: err = %v, want %v", err, room.ErrNotMember)
	}
}

// TestAcceptsBroadcastWithNoRecipients proves a member message with a
// nil To list is admitted: nil To means broadcast to the room, and
// Accepts' recipient loop must not reject an empty list.
func TestAcceptsBroadcastWithNoRecipients(t *testing.T) {
	founder := newAgent(t, "founder")
	alice := newAgent(t, "alice")

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(alice.id, founder.id); err != nil {
		t.Fatalf("admit alice: %v", err)
	}

	m := baseMessage(r.ID())
	m.ID = "broadcast-1"
	if m.To != nil {
		t.Fatalf("baseMessage() To = %v, want nil for this test's premise", m.To)
	}
	m = alice.post(t, m)
	if err := r.Accepts(m); err != nil {
		t.Fatalf("broadcast message rejected: %v", err)
	}
}

// errInvalidMessage marks a table case whose Accepts error comes from
// envelope.Message.Validate, which has no sentinel error to compare against.
var errInvalidMessage = errors.New("invalid message")

// TestAdmissionFailures gates messages that must not enter a room.
func TestAdmissionFailures(t *testing.T) {
	founder := newAgent(t, "founder")
	alice := newAgent(t, "alice")
	eve := newAgent(t, "eve") // never admitted

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(alice.id, founder.id); err != nil {
		t.Fatalf("admit alice: %v", err)
	}

	cases := map[string]struct {
		err error
		m   envelope.Message
	}{
		"outsider cannot post": {room.ErrNotMember, func() envelope.Message { m := baseMessage(r.ID()); m.ID = "x1"; return eve.post(t, m) }()},
		"unsigned cannot post": {room.ErrUnsigned, func() envelope.Message { m := baseMessage(r.ID()); m.ID = "x2"; return m }()},
		"wrong room":           {room.ErrWrongRoom, func() envelope.Message { m := baseMessage("other-room"); m.ID = "x3"; return alice.post(t, m) }()},
		"forged signature": {room.ErrUnsigned, func() envelope.Message {
			m := baseMessage(r.ID())
			m.ID = "x5"
			m = alice.post(t, m)
			m.Payload = "Forged claim." // breaks the signature
			return m
		}()},
		"ghost recipient": {room.ErrNotMember, func() envelope.Message {
			m := baseMessage(r.ID())
			m.ID = "x4"
			m.To = []string{eve.id}
			return alice.post(t, m)
		}()},
		"invalid payload, validly signed": {errInvalidMessage, func() envelope.Message {
			m := baseMessage(r.ID())
			m.ID = "x6"
			m.Payload = ""
			signed, err := envelope.Sign(alice.key, m)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		}()},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := r.Accepts(tc.m)
			if tc.err == errInvalidMessage {
				if err == nil {
					t.Fatalf("err = nil, want a validation error")
				}
				return
			}
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

// TestConcurrentRoster hammers the mutex-guarded roster: goroutines mix
// Admit, Promote, Leave, Remove, and Accepts against one Room. Errors
// are expected (ops race each other); the race detector is the oracle.
// Synchronization is sync.WaitGroup only; never time.Sleep.
func TestConcurrentRoster(t *testing.T) {
	founder := newAgent(t, "founder")
	poster := newAgent(t, "poster")

	r, err := room.New("platform-team", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(poster.id, founder.id); err != nil {
		t.Fatalf("admit poster: %v", err)
	}
	msg := baseMessage(r.ID())
	msg.ID = "stress"
	msg = poster.post(t, msg)

	const workers = 8
	const opsPerWorker = 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				id := fmt.Sprintf("agent-%d-%d", w, i)
				switch i % 5 {
				case 0:
					r.Admit(id, founder.id)
				case 1:
					r.Promote(id, founder.id)
				case 2:
					r.Leave(id)
				case 3:
					r.Remove(id, founder.id)
				case 4:
					r.Accepts(msg)
				}
			}
		}(w)
	}
	wg.Wait()

	// Founder and poster are never targeted above; both must survive.
	if !r.IsMember(founder.id) {
		t.Fatal("founder must stay a member")
	}
	if !r.IsMember(poster.id) {
		t.Fatal("poster must stay a member")
	}
}
