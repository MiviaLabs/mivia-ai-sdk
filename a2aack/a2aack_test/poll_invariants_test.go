package a2aack_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aack"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// TestPollInvariants proves non-terminal states continue polling, a
// terminal state stops the loop, and ctx cancellation returns within
// one Poll interval without any sleep larger than the interval.
func TestPollInvariants(t *testing.T) {
	poll := 5 * time.Millisecond
	timeout := 50 * time.Millisecond

	t.Run("non-terminal continues, terminal stops", func(t *testing.T) {
		fake := &fakeRemote{statusStates: []a2aclient.State{
			a2aclient.StateUnspecified, // first Status call
			a2aclient.StateWorking,     // second Status call
			a2aclient.StateSubmitted,   // third Status call
			a2aclient.StateCompleted,   // fourth: terminal, stops the loop
		}}
		msg := signedMessage(t)
		opts := a2aack.Options{Poll: poll, Timeout: timeout}
		ackFn, err := a2aack.Wait(fake, opts)
		if err != nil {
			t.Fatalf("Wait returned validation error %v", err)
		}
		fake.result = signedResult(t)
		ack, err := ackFn(context.Background(), msg)
		if err != nil {
			t.Fatalf("ackFn() returned error: %v", err)
		}
		if ack.Status != envelope.AckConfirmed {
			t.Fatalf("ack status = %q, want confirmed", ack.Status)
		}
		if calls := fake.statusCalls.Load(); calls != 4 {
			t.Fatalf("Status called %d times, want 4", calls)
		}
	})

	t.Run("ctx cancel within one interval", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		fake := &fakeRemote{cancelOnStatus: cancel} // StateUnspecified always, never terminal
		msg := signedMessage(t)
		poll := 10 * time.Millisecond
		timeout := time.Second
		opts := a2aack.Options{Poll: poll, Timeout: timeout}
		ackFn, err := a2aack.Wait(fake, opts)
		if err != nil {
			t.Fatalf("Wait returned validation error %v", err)
		}

		start := time.Now()
		_, ackErr := ackFn(ctx, msg)
		elapsed := time.Since(start)
		// The fake cancels ctx on the first Status call, so ackFn must
		// return just after the first tick, far short of the timeout.
		if elapsed > 5*poll {
			t.Fatalf("ackFn took %v after cancel, want return near the first poll tick", elapsed)
		}
		if !errors.Is(ackErr, a2aack.ErrTimeout) {
			t.Fatalf("ackFn() error = %v, want errors.Is(ErrTimeout) after ctx cancellation", ackErr)
		}
	})
}
