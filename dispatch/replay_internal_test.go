package dispatch

import (
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestReplayKeyDistinguishesThreadBoundary proves the length-prefixed
// encoding resolves a ThreadID/ID concatenation ambiguity a plain ':'
// join would not: {ThreadID: "a:b", ID: "c"} and
// {ThreadID: "a", ID: "b:c"} both join to "a:b:c" under a plain
// concatenation, but replayKey gives them distinct keys.
func TestReplayKeyDistinguishesThreadBoundary(t *testing.T) {
	a := replayKey(envelope.Message{ThreadID: "a:b", ID: "c"})
	b := replayKey(envelope.Message{ThreadID: "a", ID: "b:c"})
	if a == b {
		t.Fatalf("replayKey collided: %q == %q for distinct ThreadID/ID pairs", a, b)
	}
}

// TestIsReplayCoversTaskrunRunOutcomes pins every error taskrun.Run
// can return for a duplicate key, deterministically, rather than
// relying on goroutine scheduling to reproduce the race. A concurrent
// duplicate can lose to a winner that both claims and completes
// between the duplicate's own State check and its Claim call; Claim
// then reports that through its default terminal-status branch as
// ledger.ErrNotClaimed, not one of the three taskrun sentinels.
// Missing this case made isReplay's caller answer a raw, unmapped
// error line instead of "replay:" on that race, observed directly as
// an intermittent TestReplayConcurrentDuplicates failure.
func TestIsReplayCoversTaskrunRunOutcomes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"already completed", taskrun.ErrTaskDone, true},
		{"already failed", taskrun.ErrTaskFailed, true},
		{"blocked on a dependency", taskrun.ErrTaskBlocked, true},
		{"lease held by an in-flight duplicate", ledger.ErrLeaseActive, true},
		{"claim lost the terminal-status race", ledger.ErrNotClaimed, true},
		{"unrelated ledger error", ledger.ErrNoKey, false},
		{"unrelated taskrun error", taskrun.ErrNoLedger, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isReplay(tc.err); got != tc.want {
				t.Fatalf("isReplay(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if got := isReplay(fmt.Errorf("wrapped: %w", tc.err)); got != tc.want {
				t.Fatalf("isReplay(wrapped %v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
	if isReplay(errors.New("plain error")) {
		t.Fatalf("isReplay(plain error) = true, want false")
	}
}
