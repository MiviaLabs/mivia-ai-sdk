package channel_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
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

// startSignalWriter wraps an io.Writer and closes started the moment
// its first Write call begins, before delegating to w. Used by
// TestNDJSONNotifierConcurrentCalls to prove the first call's write
// is already in flight (and therefore its lock is held) without
// racing a second call for the lock itself.
type startSignalWriter struct {
	w       io.Writer
	started chan struct{}
}

func (s *startSignalWriter) Write(p []byte) (int, error) {
	close(s.started)
	return s.w.Write(p)
}

// TestNDJSONNotifierConcurrentCalls proves the one-call-at-a-time
// invariant under real overlap. Launching two notify() calls with a
// bare go statement and a WaitGroup does not, by itself, guarantee
// they ever overlap: the scheduler is free to run the first call to
// full completion (lock, write, read, unlock) before the second is
// ever scheduled, which would make both succeed with no lock
// contention at all and is not the scenario this test exists to
// cover. Racing a second call against the first for the lock itself
// does not fix this either: if the second call wins TryLock instead,
// it blocks forever on the same undrained pw, deadlocking the test.
// Instead, force the overlap deterministically with a one-way signal:
// pw starts unread, so the first call's write blocks; startSignalWriter
// proves that write is already in flight — and therefore the lock is
// already held — before the second call ever runs, so it is
// guaranteed to observe ErrNotifierBusy without any risk of winning
// the lock itself. Only then does the echo reader start, letting the
// first call's write unblock and its round trip complete.
func TestNDJSONNotifierConcurrentCalls(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	sw := &startSignalWriter{w: pw, started: make(chan struct{})}
	notify := channel.NewNDJSONNotifier(ar, sw)

	q0 := channel.Question{ID: "q0", Recipient: "human", Payload: "hi"}
	firstDone := make(chan error, 1)
	go func() {
		_, err := notify(context.Background(), q0)
		firstDone <- err
	}()

	select {
	case <-sw.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first notify() never reached its write phase")
	}

	q1 := channel.Question{ID: "q1", Recipient: "human", Payload: "hi"}
	_, err := notify(context.Background(), q1)
	if !errors.Is(err, channel.ErrNotifierBusy) {
		t.Fatalf("second notify() error = %v, want %v", err, channel.ErrNotifierBusy)
	}

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

	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first notify() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first notify() did not complete after the echo reader started draining pw")
	}
}
