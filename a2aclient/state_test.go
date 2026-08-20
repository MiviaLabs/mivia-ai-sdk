package a2aclient

import (
	"context"
	"errors"
	"testing"
)

// TestResultAcceptsRejectedTask proves a rejected task is terminal, so
// Result fetches its output instead of returning ErrNotTerminal.
func TestResultAcceptsRejectedTask(t *testing.T) {
	msg := signedMessage(t)
	tr := &stubTransport{
		taskID: "task-1",
		states: []State{StateRejected},
		result: mappedResult(t, msg),
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := c.Result(context.Background(), h)
	if err != nil {
		t.Fatalf("Result on a rejected task = %v, want the mapped message", err)
	}
	if got.ID != msg.ID {
		t.Fatalf("Result message id = %q, want %q", got.ID, msg.ID)
	}
}

// TestResultRejectsBlockedTask proves the two states that wait for
// client action are not terminal, so Result refuses them.
func TestResultRejectsBlockedTask(t *testing.T) {
	for _, state := range []State{StateAuthRequired, StateInputRequired} {
		t.Run(state.String(), func(t *testing.T) {
			tr := &stubTransport{taskID: "task-1", states: []State{state}}
			c, err := newFromTransport(testBaseURL, tr)
			if err != nil {
				t.Fatalf("newFromTransport: %v", err)
			}
			h, err := c.Send(context.Background(), signedMessage(t))
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if _, err := c.Result(context.Background(), h); !errors.Is(err, ErrNotTerminal) {
				t.Fatalf("Result on %s = %v, want errors.Is ErrNotTerminal", state, err)
			}
		})
	}
}

// TestStateStringNamesNewStates pins each new constant's String
// against its literal upstream text.
func TestStateStringNamesNewStates(t *testing.T) {
	cases := []struct {
		state State
		want  string
	}{
		{StateRejected, "rejected"},
		{StateAuthRequired, "auth-required"},
		{StateInputRequired, "input-required"},
		{StateUnknown, "unknown"},
	}
	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Fatalf("State(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}
