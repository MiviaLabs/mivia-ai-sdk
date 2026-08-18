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

// This file covers NewNDJSONNotifier's one-caller-at-a-time contract:
// a second call while a first is in flight, the permanent-lockout
// limit after a ctx-canceled call, the two recourses (a late answer
// the leaked goroutine drops, or closing r), and the -race concurrent
// case. Split from ndjson_notifier_test.go to keep both files at or
// below the file-size cap.

func TestNDJSONNotifierBusyWhileBlocked(t *testing.T) {
	pr, pw := io.Pipe()
	ar, _ := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}

	// questionRead closes only once the background peer has scanned
	// the first call's written question line, which happens only
	// after writeQuestion completes; that guarantees the first call
	// already holds the internal lock and is blocked inside
	// readAnswer's own Scan before the second call below runs.
	questionRead := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		close(questionRead)
	}()

	firstDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(firstDone)
		_, _ = notify(ctx, q)
	}()

	<-questionRead

	_, err := notify(context.Background(), channel.Question{ID: "q2", Recipient: "human", Payload: "hi"})
	if !errors.Is(err, channel.ErrNotifierBusy) {
		t.Fatalf("second notify() error = %v, want %v", err, channel.ErrNotifierBusy)
	}

	cancel()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first notify() did not return after cancel")
	}
}

func TestNDJSONNotifierBusyThenLateAnswerRecovers(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q1 := channel.Question{ID: "q1", Recipient: "human", Payload: "first"}

	questionRead := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		close(questionRead)
	}()

	ctx1, cancel1 := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = notify(ctx1, q1)
	}()

	<-questionRead
	cancel1()

	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first notify() did not return after cancel")
	}

	// Second call arrives while the first call's background goroutine
	// is still pending: still busy.
	_, err := notify(context.Background(), channel.Question{ID: "q2", Recipient: "human", Payload: "second"})
	if !errors.Is(err, channel.ErrNotifierBusy) {
		t.Fatalf("second notify() error = %v, want %v", err, channel.ErrNotifierBusy)
	}

	// Deliver a late line: the leaked goroutine consumes and drops
	// it, releasing the lock.
	late := ndjsonLine{Type: "answer", QuestionID: q1.ID, Approved: true, Payload: "late"}
	if err := json.NewEncoder(aw).Encode(late); err != nil {
		t.Fatalf("Encode() error = %v, want nil", err)
	}

	// A third call, issued after that point, succeeds normally.
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

func TestNDJSONNotifierBusyThenCloseRecovers(t *testing.T) {
	pr, pw := io.Pipe()
	ar, _ := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q1 := channel.Question{ID: "q1", Recipient: "human", Payload: "first"}

	questionRead := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		close(questionRead)
	}()

	ctx1, cancel1 := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = notify(ctx1, q1)
	}()

	<-questionRead
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

	// Close r instead of delivering a line: the stale Scan errors,
	// releasing the lock.
	if err := ar.Close(); err != nil {
		t.Fatalf("ar.Close() error = %v, want nil", err)
	}

	// A peer must still read pr for a later writeQuestion on the
	// original closure to complete: keep a continuous reader running
	// so a poll never blocks on an unread pipe write.
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
		}
	}()

	// A subsequent call on the original closure no longer returns
	// ErrNotifierBusy once the stale read has resolved: r is closed
	// permanently, so the call itself still fails, but with a
	// different error, proving the lock released rather than staying
	// wedged forever.
	deadline := time.After(5 * time.Second)
	var err2 error
	for {
		_, err2 = notify(context.Background(), channel.Question{ID: "q4", Recipient: "human", Payload: "fourth"})
		if !errors.Is(err2, channel.ErrNotifierBusy) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("original notify closure stayed busy past timeout after closing r")
		default:
		}
	}
	if err2 == nil {
		t.Fatalf("notify() error = nil, want non-nil (r is closed)")
	}
}

// TestNDJSONNotifierSerialReuseNeverBusy proves readAnswer releases
// the lock before it sends the result on its internal channel, not
// after: several hundred serial calls on one closure, each answered
// immediately by a live responder, must never see ErrNotifierBusy. A
// deferred release after the send would leave a race window where the
// next serial call's TryLock can lose to the still-held lock even
// though notify already returned the previous answer to its caller.
func TestNDJSONNotifierSerialReuseNeverBusy(t *testing.T) {
	const iterations = 500

	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	responderDone := make(chan struct{})
	go func() {
		defer close(responderDone)
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

	for i := 0; i < iterations; i++ {
		q := channel.Question{ID: fmt.Sprintf("serial-%d", i), Recipient: "human", Payload: "reuse"}
		got, err := notify(context.Background(), q)
		if errors.Is(err, channel.ErrNotifierBusy) {
			t.Fatalf("iteration %d: notify() error = %v, want no ErrNotifierBusy on serial reuse", i, err)
		}
		if err != nil {
			t.Fatalf("iteration %d: notify() error = %v, want nil", i, err)
		}
		if got.QuestionID != q.ID {
			t.Fatalf("iteration %d: QuestionID = %q, want %q", i, got.QuestionID, q.ID)
		}
	}

	if err := pw.Close(); err != nil {
		t.Fatalf("pw.Close() error = %v, want nil", err)
	}
	select {
	case <-responderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("responder did not exit after pw.Close()")
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
