package a2aack_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aack"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
)

// TestWaitTimeout proves a fake that never reaches a terminal state
// yields ErrTimeout when the deadline fires, with the last seen state
// in the error text and no hang past the deadline.
func TestWaitTimeout(t *testing.T) {
	poll := 5 * time.Millisecond
	timeout := 25 * time.Millisecond

	fake := &fakeRemote{statusStates: []a2aclient.State{
		a2aclient.StateUnspecified,
		a2aclient.StateWorking,
	}}
	msg := signedMessage(t)

	opts := a2aack.Options{Poll: poll, Timeout: timeout}
	ackFn, err := a2aack.Wait(fake, opts)
	if err != nil {
		t.Fatalf("Wait returned validation error %v", err)
	}

	start := time.Now()
	_, ackErr := ackFn(context.Background(), msg)
	elapsed := time.Since(start)

	// The loop returns at the deadline, not before and not after a hang.
	if elapsed < timeout {
		t.Fatalf("wait returned after %v, want at least the %v deadline", elapsed, timeout)
	}
	if elapsed > timeout+time.Second {
		t.Fatalf("wait took %v, want return near the %v deadline", elapsed, timeout)
	}
	if !errors.Is(ackErr, a2aack.ErrTimeout) {
		t.Fatalf("error = %v, want errors.Is(ErrTimeout)", ackErr)
	}
	if !strings.Contains(ackErr.Error(), a2aclient.StateWorking.String()) {
		t.Fatalf("timeout error %q should contain the last seen state", ackErr)
	}
}
