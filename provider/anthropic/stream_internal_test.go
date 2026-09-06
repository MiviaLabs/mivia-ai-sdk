package anthropic

import (
	"fmt"
	"io"
	"testing"
)

// TestReadSSEEventsAbandonsBlockedSend pins the producer-exit contract:
// a pending channel send must fall through to done and the producer must
// close the channel instead of parking forever.
func TestReadSSEEventsAbandonsBlockedSend(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	done := make(chan struct{})

	ch := readSSEEvents(pr, done)

	// First event: written, then consumed synchronously.
	fmt.Fprintf(pw, "event: a\ndata: 1\n\n")
	ev, ok := <-ch
	if !ok || ev.Event != "a" {
		t.Fatalf("first event = %+v ok=%v, want event a", ev, ok)
	}

	// Second event: nobody receives, so the producer parks on the send.
	fmt.Fprintf(pw, "event: b\ndata: 2\n\n")

	// Abandon the stream. The producer must leave via done, whether it
	// is parked on the send or still scanning; closing the pipe unblocks
	// the scan path. Either way the channel must close with no delivery.
	close(done)
	pw.Close()
	got, ok := <-ch
	if ok {
		t.Fatalf("received %+v after done, want closed channel", got)
	}
}
