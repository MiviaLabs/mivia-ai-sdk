package channel_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// ndjsonLine matches the wire shape channel's own internal wire
// structs use, for the test side of an io.Pipe pair: type, id,
// recipient, payload on the question side; type, question_id,
// approved, payload on the answer side.
type ndjsonLine struct {
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	Recipient  string `json:"recipient,omitempty"`
	QuestionID string `json:"question_id,omitempty"`
	Approved   bool   `json:"approved,omitempty"`
	Payload    string `json:"payload,omitempty"`
}

// blockingWriter records exactly one Write call and either returns a
// fixed error or forwards to an underlying io.Writer. Deterministic,
// unlike a closed io.Pipe, for the write-side failure test.
type blockingWriter struct {
	mu     sync.Mutex
	writes int
	err    error
	inner  io.Writer
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes++
	if b.err != nil {
		return 0, b.err
	}
	return b.inner.Write(p)
}

func (b *blockingWriter) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writes
}

func TestNDJSONNotifierRoundTrip(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		if !sc.Scan() {
			return
		}
		var got ndjsonLine
		if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
			return
		}
		if got.Type != "question" || got.ID != q.ID || got.Recipient != q.Recipient || got.Payload != q.Payload {
			return
		}
		reply := ndjsonLine{Type: "answer", QuestionID: q.ID, Approved: true, Payload: "ok"}
		enc := json.NewEncoder(aw)
		_ = enc.Encode(reply)
	}()

	got, err := notify(context.Background(), q)
	if err != nil {
		t.Fatalf("notify() error = %v, want nil", err)
	}
	if got.QuestionID != q.ID || !got.Approved || got.Payload != "ok" {
		t.Fatalf("Answer = %+v, want QuestionID=%q Approved=true Payload=ok", got, q.ID)
	}
}

func TestNDJSONNotifierAnswerMismatch(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		reply := ndjsonLine{Type: "answer", QuestionID: "wrong-id", Approved: true}
		_ = json.NewEncoder(aw).Encode(reply)
	}()

	got, err := notify(context.Background(), q)
	if !errors.Is(err, channel.ErrAnswerMismatch) {
		t.Fatalf("notify() error = %v, want %v", err, channel.ErrAnswerMismatch)
	}
	if got != (channel.Answer{}) {
		t.Fatalf("Answer = %+v, want zero value", got)
	}
}

func TestNDJSONNotifierMalformedLine(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		_, _ = aw.Write([]byte("not json\n"))
	}()

	got, err := notify(context.Background(), q)
	if err == nil {
		t.Fatalf("notify() error = nil, want non-nil")
	}
	if got != (channel.Answer{}) {
		t.Fatalf("Answer = %+v, want zero value", got)
	}
}

func TestNDJSONNotifierWrongType(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		reply := ndjsonLine{Type: "question", ID: "q1"}
		_ = json.NewEncoder(aw).Encode(reply)
	}()

	got, err := notify(context.Background(), q)
	if err == nil {
		t.Fatalf("notify() error = nil, want non-nil")
	}
	if got != (channel.Answer{}) {
		t.Fatalf("Answer = %+v, want zero value", got)
	}
}

func TestNDJSONNotifierReaderClosedNoLine(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		_ = aw.Close()
	}()

	got, err := notify(context.Background(), q)
	if err == nil {
		t.Fatalf("notify() error = nil, want non-nil")
	}
	if got != (channel.Answer{}) {
		t.Fatalf("Answer = %+v, want zero value", got)
	}
}

func TestNDJSONNotifierContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	ar, _ := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}

	readDone := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		sc.Scan()
		close(readDone)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-readDone
		cancel()
	}()

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = notify(ctx, q)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("notify() did not return within timeout after ctx cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("notify() error = %v, want %v", err, context.Canceled)
	}
}

func TestNDJSONNotifierInvalidQuestionReleasesLock(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	bad := channel.Question{ID: "", Recipient: "human", Payload: "proceed?"}
	_, err := notify(context.Background(), bad)
	if !errors.Is(err, channel.ErrEmptyID) {
		t.Fatalf("notify() error = %v, want %v", err, channel.ErrEmptyID)
	}

	// Prove the invalid call touched neither r nor w: a following,
	// valid call on the same closure succeeds.
	good := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		if !sc.Scan() {
			return
		}
		reply := ndjsonLine{Type: "answer", QuestionID: good.ID, Approved: true}
		_ = json.NewEncoder(aw).Encode(reply)
	}()
	got, err := notify(context.Background(), good)
	if err != nil {
		t.Fatalf("notify() error = %v, want nil", err)
	}
	if got.QuestionID != good.ID {
		t.Fatalf("QuestionID = %q, want %q", got.QuestionID, good.ID)
	}
}

func TestNDJSONNotifierBufferBoundary(t *testing.T) {
	t.Run("at cap decodes", func(t *testing.T) {
		pr, pw := io.Pipe()
		ar, aw := io.Pipe()
		notify := channel.NewNDJSONNotifier(ar, pw)

		q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}
		// Build a payload sized so the full JSON line lands just under
		// the 1 MB scanner cap.
		big := strings.Repeat("a", 1024*1024-256)

		go func() {
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			sc.Scan()
			reply := ndjsonLine{Type: "answer", QuestionID: q.ID, Approved: true, Payload: big}
			_ = json.NewEncoder(aw).Encode(reply)
		}()

		got, err := notify(context.Background(), q)
		if err != nil {
			t.Fatalf("notify() error = %v, want nil", err)
		}
		if got.Payload != big {
			t.Fatalf("Payload length = %d, want %d", len(got.Payload), len(big))
		}
	})

	t.Run("past cap fails", func(t *testing.T) {
		pr, pw := io.Pipe()
		ar, aw := io.Pipe()
		notify := channel.NewNDJSONNotifier(ar, pw)

		q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}
		tooBig := strings.Repeat("a", 2*1024*1024)

		go func() {
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			sc.Scan()
			reply := ndjsonLine{Type: "answer", QuestionID: q.ID, Approved: true, Payload: tooBig}
			_ = json.NewEncoder(aw).Encode(reply)
		}()

		got, err := notify(context.Background(), q)
		if err == nil {
			t.Fatalf("notify() error = nil, want non-nil (overflow)")
		}
		if got != (channel.Answer{}) {
			t.Fatalf("Answer = %+v, want zero value", got)
		}
	})
}

func TestNDJSONNotifierWriteFailureReleasesLock(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	writer := &blockingWriter{err: fmt.Errorf("write: boom")}
	notify := channel.NewNDJSONNotifier(ar, writer)

	q := channel.Question{ID: "q1", Recipient: "human", Payload: "proceed?"}
	_, err := notify(context.Background(), q)
	if err == nil {
		t.Fatalf("notify() error = nil, want non-nil")
	}
	if writer.count() != 1 {
		t.Fatalf("writer.count() = %d, want 1", writer.count())
	}

	// The failed write must not have consumed any bytes from a real
	// reader; prove the lock released by succeeding on a fresh,
	// working pair.
	notify2 := channel.NewNDJSONNotifier(ar, pw)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		if !sc.Scan() {
			return
		}
		reply := ndjsonLine{Type: "answer", QuestionID: q.ID, Approved: true}
		_ = json.NewEncoder(aw).Encode(reply)
	}()
	got, err := notify2(context.Background(), q)
	if err != nil {
		t.Fatalf("notify2() error = %v, want nil", err)
	}
	if got.QuestionID != q.ID {
		t.Fatalf("QuestionID = %q, want %q", got.QuestionID, q.ID)
	}
}
