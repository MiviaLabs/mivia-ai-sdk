package channel_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// This file continues ndjson_notifier_lockout_test.go's coverage: the
// write-phase lockout's own close-to-recover path, the exact abandoned-
// write-succeeded race Finding 1 in the phase-43 review described, and
// the concurrent-call race. Split out to keep both files at or below the
// file-size cap.

// TestNDJSONNotifierWriteBusyThenCloseWRecovers mirrors
// TestNDJSONNotifierBusyThenCloseRecovers on the write side: cancel
// ctx while the write to w is permanently blocked (no reader ever
// drains pw), confirm a concurrent second call is busy, then close pw
// so the abandoned write errors instead of hanging forever. The lock
// must release once that error resolves, even though the release path
// runs inside continueAfterAbandonedWrite's failure branch rather than
// readAnswer.
func TestNDJSONNotifierWriteBusyThenCloseWRecovers(t *testing.T) {
	_, pw := io.Pipe() // no reader ever drains pw; every Write blocks.
	ar, _ := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q1 := channel.Question{ID: "q1", Recipient: "human", Payload: "first"}
	ctx1, cancel1 := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = notify(ctx1, q1)
	}()
	cancel1()

	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first notify() did not return after cancel")
	}

	_, err := notify(context.Background(), channel.Question{ID: "q2", Recipient: "human", Payload: "second"})
	if !errors.Is(err, channel.ErrNotifierBusy) {
		t.Fatalf("second notify() error = %v, want %v", err, channel.ErrNotifierBusy)
	}

	// Close pw instead of delivering a reader: the stale Write errors,
	// releasing the lock through continueAfterAbandonedWrite's failure
	// branch.
	if err := pw.Close(); err != nil {
		t.Fatalf("pw.Close() error = %v, want nil", err)
	}

	deadline := time.After(5 * time.Second)
	var err3 error
	for {
		_, err3 = notify(context.Background(), channel.Question{ID: "q3", Recipient: "human", Payload: "third"})
		if !errors.Is(err3, channel.ErrNotifierBusy) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("original notify closure stayed busy past timeout after closing pw")
		default:
		}
	}
	if err3 == nil {
		t.Fatalf("notify() error = nil, want non-nil (pw is closed)")
	}
}

// TestNDJSONNotifierAbandonedWriteSuccessStaysBusyUntilReadResolves
// proves the exact race Finding 1 in the phase-43 review described: a
// ctx-canceled call whose write nonetheless reaches the peer must not
// release the lock until the abandoned read phase also resolves. This
// deterministically forces the ctx.Done() branch of writeQuestion by
// canceling ctx before the peer drains pw, so the write goroutine
// cannot have resolved yet when select runs; the peer then drains pw
// afterward, letting the write succeed only once writeQuestion has
// already returned ctx.Err() to its caller. A wrong implementation
// that releases the lock as soon as the (now-successful) write
// resolves, instead of also waiting on the abandoned read, would let
// the second call below observe something other than ErrNotifierBusy.
func TestNDJSONNotifierAbandonedWriteSuccessStaysBusyUntilReadResolves(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q1 := abandonWriteThatSucceeds(t, notify, pr, pw)

	// A concurrent second call, issued while the abandoned write's
	// success has already been observed but the abandoned read is
	// still pending, must see ErrNotifierBusy, never a chance to write
	// its own question line onto the same w.
	deadlineBusy := time.After(2 * time.Second)
	busyConfirmed := false
	for !busyConfirmed {
		_, err := notify(context.Background(), channel.Question{ID: "q2", Recipient: "human", Payload: "second"})
		if err == nil {
			t.Fatal("second notify() succeeded while the abandoned read phase was still pending")
		}
		if errors.Is(err, channel.ErrNotifierBusy) {
			busyConfirmed = true
			break
		}
		select {
		case <-deadlineBusy:
			t.Fatalf("second notify() never reported busy before the abandoned read resolved: last error = %v", err)
		default:
		}
	}

	// Deliver the late answer: the abandoned read consumes and drops
	// it, releasing the lock.
	late := ndjsonLine{Type: "answer", QuestionID: q1.ID, Approved: true, Payload: "late"}
	if err := json.NewEncoder(aw).Encode(late); err != nil {
		t.Fatalf("Encode() error = %v, want nil", err)
	}

	assertNotifyRecoversAfterLateAnswer(t, notify, pr, aw)
}

// abandonWriteThatSucceeds cancels ctx before notify starts and before
// any reader drains pw, so writeQuestion's select is guaranteed to
// pick ctx.Done(): the write goroutine cannot yet have sent to its
// done channel. It then drains pw so the abandoned write succeeds,
// returning the Question the abandoned call sent.
func abandonWriteThatSucceeds(t *testing.T, notify channel.Notifier, pr *io.PipeReader, pw *io.PipeWriter) channel.Question {
	t.Helper()
	q1 := channel.Question{ID: "q1", Recipient: "human", Payload: "first"}
	ctx1, cancel1 := context.WithCancel(context.Background())
	cancel1()

	firstDone := make(chan struct{})
	var firstErr error
	go func() {
		defer close(firstDone)
		_, firstErr = notify(ctx1, q1)
	}()

	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first notify() did not return promptly on already-canceled ctx")
	}
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("first notify() error = %v, want %v", firstErr, context.Canceled)
	}

	questionRead := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		close(questionRead)
	}()
	select {
	case <-questionRead:
	case <-time.After(5 * time.Second):
		t.Fatal("abandoned write did not deliver its line")
	}
	return q1
}

// assertNotifyRecoversAfterLateAnswer proves a third call on notify
// succeeds once the abandoned read phase has resolved, polling through
// ErrNotifierBusy until the lock releases.
func assertNotifyRecoversAfterLateAnswer(t *testing.T, notify channel.Notifier, pr *io.PipeReader, aw *io.PipeWriter) {
	t.Helper()
	q3 := channel.Question{ID: "q3", Recipient: "human", Payload: "third"}
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		if !sc.Scan() {
			return
		}
		reply := ndjsonLine{Type: "answer", QuestionID: q3.ID, Approved: true}
		_ = json.NewEncoder(aw).Encode(reply)
	}()

	deadline := time.After(5 * time.Second)
	var got channel.Answer
	var err error
	for {
		got, err = notify(context.Background(), q3)
		if err == nil {
			break
		}
		if !errors.Is(err, channel.ErrNotifierBusy) {
			t.Fatalf("third notify() error = %v, want nil or %v", err, channel.ErrNotifierBusy)
		}
		select {
		case <-deadline:
			t.Fatal("third notify() stayed busy past timeout")
		default:
		}
	}
	if got.QuestionID != q3.ID {
		t.Fatalf("QuestionID = %q, want %q", got.QuestionID, q3.ID)
	}
}

func TestNDJSONNotifierConcurrentCalls(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var got ndjsonLine
			if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
				return
			}
			reply := ndjsonLine{Type: "answer", QuestionID: got.ID, Approved: true}
			if err := json.NewEncoder(aw).Encode(reply); err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := channel.Question{ID: fmt.Sprintf("q%d", i), Recipient: "human", Payload: "hi"}
			_, err := notify(context.Background(), q)
			results[i] = err
		}(i)
	}
	wg.Wait()

	successes, busy := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, channel.ErrNotifierBusy):
			busy++
		default:
			t.Fatalf("unexpected error = %v", err)
		}
	}
	if successes != 1 || busy != 1 {
		t.Fatalf("successes = %d, busy = %d, want 1 and 1", successes, busy)
	}
}
