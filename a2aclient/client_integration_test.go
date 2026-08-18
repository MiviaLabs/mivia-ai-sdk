package a2aclient

import (
	"context"
	"testing"
)

// TestSendStatusResultRoundTrip drives the full task lifecycle against
// a recorded transcript: sign a message, send it, poll status to a
// terminal state, fetch the result, verify its signature, then close
// the client. No step opens a live network connection; stubTransport
// stands in for the remote agent.
func TestSendStatusResultRoundTrip(t *testing.T) {
	msg := signedMessage(t)
	tr := &stubTransport{
		taskID: "task-round-trip",
		states: []State{
			StateSubmitted,
			StateWorking,
			StateCompleted,
		},
		result: mappedResult(t, msg),
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	ctx := context.Background()
	h, err := c.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	var state State
	for i := 0; i < len(tr.states); i++ {
		state, err = c.Status(ctx, h)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if state == StateCompleted {
			break
		}
	}
	if state != StateCompleted {
		t.Fatalf("final state = %s, want %s", state, StateCompleted)
	}

	result, err := c.Result(ctx, h)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if err := result.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	if result.Payload != msg.Payload {
		t.Fatalf("result payload = %q, want %q", result.Payload, msg.Payload)
	}
}
