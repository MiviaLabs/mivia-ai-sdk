package a2aack_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aack"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// TestWaitFailsCorrectly proves a fake that returns StateFailed and one
// that returns StateCanceled each yield ErrRemoteFailed wrapping the
// state's String, not a nil or confirmed ack.
func TestWaitFailsCorrectly(t *testing.T) {
	good := a2aack.Options{Poll: time.Millisecond, Timeout: time.Second}

	t.Run("StateFailed", func(t *testing.T) {
		fake := &fakeRemote{statusStates: []a2aclient.State{a2aclient.StateFailed}}
		msg := signedMessage(t)
		ackFn, err := a2aack.Wait(fake, good)
		if err != nil {
			t.Fatalf("Wait returned validation error %v before any task ran", err)
		}
		resultMsg := envelope.Message{ID: "res-fail", Payload: "done"}
		fake.result = resultMsg
		_, err = ackFn(context.Background(), msg)
		if err == nil {
			t.Fatal("ackFn() expected an error for failed state")
		}
		if !errors.Is(err, a2aack.ErrRemoteFailed) {
			t.Fatalf("error = %v, want errors.Is(ErrRemoteFailed)", err)
		}
		if !strings.Contains(err.Error(), a2aclient.StateFailed.String()) {
			t.Fatalf("error = %q does not contain the state string", err)
		}
	})

	t.Run("StateCanceled", func(t *testing.T) {
		fake := &fakeRemote{statusStates: []a2aclient.State{a2aclient.StateCanceled}}
		msg := signedMessage(t)
		ackFn, _ := a2aack.Wait(fake, good)
		resultMsg := envelope.Message{ID: "res-cancel", Payload: "canceled"}
		fake.result = resultMsg
		_, err := ackFn(context.Background(), msg)
		if err == nil {
			t.Fatal("ackFn() expected an error for canceled state")
		}
		if !errors.Is(err, a2aack.ErrRemoteFailed) {
			t.Fatalf("error = %v, want errors.Is(ErrRemoteFailed)", err)
		}
		if !strings.Contains(err.Error(), a2aclient.StateCanceled.String()) {
			t.Fatalf("error = %q does not contain the state string", err)
		}
	})
}
