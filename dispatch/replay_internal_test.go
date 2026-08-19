package dispatch

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
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
