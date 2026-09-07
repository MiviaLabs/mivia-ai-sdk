package a2a_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// signerResult returns one fresh signer id plus a result message it
// signed. The caller pins the id or scripts the result.
func signerResult(t testing.TB, id string) (string, envelope.Message) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signed, err := envelope.Sign(key, envelope.Message{
		Version:    envelope.Version,
		ID:         id,
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "remote finished",
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed.Signer, signed
}

// TestWaitPinsExpectedSigner proves Options.ExpectSigner gates the
// confirmed ack on the result signer's identity. A result that merely
// verifies against its own embedded key must not confirm a step when
// the caller pinned a different remote.
func TestWaitPinsExpectedSigner(t *testing.T) {
	pinned, pinnedResult := signerResult(t, "res-1")
	_, forgedResult := signerResult(t, "res-1")

	cases := []struct {
		name    string
		pin     string
		result  envelope.Message
		wantErr error
	}{
		{
			name:   "matching signer confirms",
			pin:    pinned,
			result: pinnedResult,
		},
		{
			name:    "mismatched signer fails",
			pin:     pinned,
			result:  forgedResult,
			wantErr: a2a.ErrSignerMismatch,
		},
		{
			name:   "empty pin keeps the unpinned contract",
			pin:    "",
			result: forgedResult,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeRemote{
				statusStates: []a2a.State{a2a.StateCompleted},
				result:       c.result,
			}
			ackFn, err := a2a.Wait(fake, a2a.Options{
				Poll:         time.Millisecond,
				Timeout:      time.Second,
				ExpectSigner: c.pin,
			})
			if err != nil {
				t.Fatalf("Wait: %v", err)
			}
			ack, err := ackFn(context.Background(), signedMessage(t))
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("ackFn: %v", err)
				}
				if ack.Status != envelope.AckConfirmed {
					t.Fatalf("ack.Status = %q, want confirmed", ack.Status)
				}
				return
			}
			if !errors.Is(err, a2a.ErrSignerMismatch) {
				t.Fatalf("ackFn error = %v, want ErrSignerMismatch", err)
			}
		})
	}
}
