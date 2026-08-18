package a2aack_test

import (
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aack"
)

// TestOptionsFailBeforeSend proves Wait returns the reject errors for a
// nil client and invalid options before any Send reaches the transport,
// and leaves a valid input with a non-nil AckWait and nil error.
func TestOptionsFailBeforeSend(t *testing.T) {
	good := a2aack.Options{Poll: 5 * time.Millisecond, Timeout: time.Second}

	t.Run("nil client", func(t *testing.T) {
		fake := &fakeRemote{}
		ackFn, err := a2aack.Wait(nil, good)
		if ackFn != nil {
			t.Fatalf("Wait(nil) AckWait = %v, want nil", ackFn)
		}
		if !errors.Is(err, a2aack.ErrNoClient) {
			t.Fatalf("Wait(nil) error = %v, want ErrNoClient", err)
		}
		if got := fake.sendCalls.Load(); got != 0 {
			t.Fatalf("Send called %d times on the reject path, want 0", got)
		}
	})

	t.Run("zero poll", func(t *testing.T) {
		fake := &fakeRemote{}
		ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: 0, Timeout: time.Second})
		if ackFn != nil {
			t.Fatalf("Wait(Poll=0) AckWait = %v, want nil", ackFn)
		}
		if !errors.Is(err, a2aack.ErrNoPoll) {
			t.Fatalf("Wait(Poll=0) error = %v, want ErrNoPoll", err)
		}
		if got := fake.sendCalls.Load(); got != 0 {
			t.Fatalf("Send called %d times on the reject path, want 0", got)
		}
	})

	t.Run("short timeout", func(t *testing.T) {
		fake := &fakeRemote{}
		ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: time.Second, Timeout: time.Millisecond})
		if ackFn != nil {
			t.Fatalf("Wait(short timeout) AckWait = %v, want nil", ackFn)
		}
		if !errors.Is(err, a2aack.ErrShortTimeout) {
			t.Fatalf("Wait(short timeout) error = %v, want ErrShortTimeout", err)
		}
		if got := fake.sendCalls.Load(); got != 0 {
			t.Fatalf("Send called %d times on the reject path, want 0", got)
		}
	})

	t.Run("timeout equals poll is accepted", func(t *testing.T) {
		fake := &fakeRemote{}
		ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: 5 * time.Millisecond, Timeout: 5 * time.Millisecond})
		if ackFn == nil {
			t.Fatal("Wait(timeout==poll) AckWait = nil, want non-nil")
		}
		if err != nil {
			t.Fatalf("Wait(timeout==poll) error = %v, want nil", err)
		}
	})

	t.Run("accept path", func(t *testing.T) {
		fake := &fakeRemote{}
		ackFn, err := a2aack.Wait(fake, good)
		if ackFn == nil {
			t.Fatal("Wait(valid) AckWait = nil, want non-nil")
		}
		if err != nil {
			t.Fatalf("Wait(valid) error = %v, want nil", err)
		}
		if got := fake.sendCalls.Load(); got != 0 {
			t.Fatalf("Send called %d times at Wait time, want 0 (Send runs only when the AckWait is invoked)", got)
		}
	})
}
