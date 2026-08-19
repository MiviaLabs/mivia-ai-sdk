package dispatch_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// countingHandler counts every Handle call and restates the payload.
type countingHandler struct {
	count *int64
}

func (h countingHandler) Handle(_ context.Context, m envelope.Message) (string, error) {
	atomic.AddInt64(h.count, 1)
	return "got: " + m.Payload, nil
}

// tamperSignature flips the last hex character of m.Signature, kept a
// valid-length, valid-hex string that fails cryptographic verify.
func tamperSignature(t testing.TB, m envelope.Message) envelope.Message {
	t.Helper()
	sig := []byte(m.Signature)
	if len(sig) == 0 {
		t.Fatal("tamperSignature: message has no signature")
	}
	last := sig[len(sig)-1]
	if last == '0' {
		sig[len(sig)-1] = '1'
	} else {
		sig[len(sig)-1] = '0'
	}
	m.Signature = string(sig)
	return m
}

// TestReplayHandlerRunsOnce posts the same signed message twice: the
// first reply is a confirmed ack, the second is a "replay:" error
// line, and the handler's side effect runs exactly once.
func TestReplayHandlerRunsOnce(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	var count int64
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(countingHandler{count: &count}),
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	msg := signIn(t, key, "room-1", "m-1", "hello")
	data := encodeLine(t, msg)

	resp1 := postLine(t, srv.URL, data)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d, want 200", resp1.StatusCode)
	}
	lines1 := readLines(t, resp1)
	if len(lines1) != 1 {
		t.Fatalf("first reply lines = %d, want 1", len(lines1))
	}
	ack, err := envelope.DecodeAck(lines1[0])
	if err != nil {
		t.Fatalf("first reply DecodeAck() error: %v, line %q", err, lines1[0])
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("first reply ack status = %q, want confirmed", ack.Status)
	}

	resp2 := postLine(t, srv.URL, data)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d, want 200", resp2.StatusCode)
	}
	lines2 := readLines(t, resp2)
	if len(lines2) != 1 {
		t.Fatalf("second reply lines = %d, want 1", len(lines2))
	}
	msgTxt, ok := decodeAsError(t, lines2[0])
	if !ok {
		t.Fatalf("second reply = %q, want an error line", lines2[0])
	}
	if !strings.HasPrefix(msgTxt, "replay:") {
		t.Fatalf("second reply error = %q, want a replay: prefix", msgTxt)
	}

	if got := atomic.LoadInt64(&count); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

// TestReplayOrderPrecedesVerify proves VerifySignature runs before
// the replay check: a first valid submission completes normally, and
// a second submission at the same ThreadID/ID with a tampered
// signature fails at "verify:", not "replay:". If the replay check
// ran first, the second reply would be "replay:" instead, since the
// key is already terminal in the ledger.
func TestReplayOrderPrecedesVerify(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(echoHandler{}),
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	first := signIn(t, key, "room-1", "m-1", "hello")
	resp1 := postLine(t, srv.URL, encodeLine(t, first))
	lines1 := readLines(t, resp1)
	if len(lines1) != 1 {
		t.Fatalf("first reply lines = %d, want 1", len(lines1))
	}
	ack, err := envelope.DecodeAck(lines1[0])
	if err != nil {
		t.Fatalf("first reply DecodeAck() error: %v, line %q", err, lines1[0])
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("first reply ack status = %q, want confirmed", ack.Status)
	}

	tampered := tamperSignature(t, signIn(t, key, "room-1", "m-1", "hello"))
	resp2 := postLine(t, srv.URL, encodeLine(t, tampered))
	lines2 := readLines(t, resp2)
	if len(lines2) != 1 {
		t.Fatalf("second reply lines = %d, want 1", len(lines2))
	}
	msgTxt, ok := decodeAsError(t, lines2[0])
	if !ok {
		t.Fatalf("second reply = %q, want an error line", lines2[0])
	}
	if !strings.HasPrefix(msgTxt, "verify:") {
		t.Fatalf("second reply error = %q, want a verify: prefix, proving verify precedes replay", msgTxt)
	}
}

// TestReplayDifferentMessagesBothProcess proves two distinct messages
// at the same ThreadID both process normally, with no false-positive
// replay detection.
func TestReplayDifferentMessagesBothProcess(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	var count int64
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(countingHandler{count: &count}),
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	first := signIn(t, key, "room-1", "m-1", "hello")
	second := signIn(t, key, "room-1", "m-2", "world")
	body := append(append(encodeLine(t, first), '\n'), encodeLine(t, second)...)

	resp := postLine(t, srv.URL, body)
	lines := readLines(t, resp)
	if len(lines) != 2 {
		t.Fatalf("reply lines = %d, want 2: %q", len(lines), lines)
	}
	for i, line := range lines {
		ack, err := envelope.DecodeAck(line)
		if err != nil {
			t.Fatalf("line %d DecodeAck() error: %v, line %q", i, err, line)
		}
		if ack.Status != envelope.AckConfirmed {
			t.Fatalf("line %d ack status = %q, want confirmed", i, ack.Status)
		}
	}
	if got := atomic.LoadInt64(&count); got != 2 {
		t.Fatalf("handler ran %d times, want 2", got)
	}
}

// TestReplayConcurrentDuplicates posts the same signed message from N
// concurrent goroutines and asserts the handler ran exactly once,
// with every reply either a confirmed ack or a "replay:" ErrReplay
// line. A concurrent duplicate may observe ledger.ErrLeaseActive
// (still in flight) or taskrun.ErrTaskDone (already completed); both
// map to the same wire-visible ErrReplay line, so this test asserts
// the counter and the reply shape, not which sentinel a given
// duplicate saw. Run with go test -race.
func TestReplayConcurrentDuplicates(t *testing.T) {
	const n = 8
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	var count int64
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(countingHandler{count: &count}),
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	msg := signIn(t, key, "room-1", "m-1", "hello")
	data := encodeLine(t, msg)

	var wg sync.WaitGroup
	results := make([]bool, n) // true when the line is a confirmed ack
	oks := make([]bool, n)     // true when the line is well-formed
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := postLine(t, srv.URL, data)
			lines := readLines(t, resp)
			if len(lines) != 1 {
				return
			}
			if ack, err := envelope.DecodeAck(lines[0]); err == nil && ack.Status == envelope.AckConfirmed {
				results[i] = true
				oks[i] = true
				return
			}
			if msgTxt, ok := decodeAsError(t, lines[0]); ok && strings.HasPrefix(msgTxt, "replay:") {
				oks[i] = true
			}
		}(i)
	}
	wg.Wait()

	confirmed := 0
	for i := 0; i < n; i++ {
		if !oks[i] {
			t.Fatalf("goroutine %d produced neither a confirmed ack nor a replay: line", i)
		}
		if results[i] {
			confirmed++
		}
	}
	if confirmed != 1 {
		t.Fatalf("confirmed replies = %d, want 1", confirmed)
	}
	if got := atomic.LoadInt64(&count); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

// TestNewBuildsDefaultLedger proves New's zero-config path (no
// Options.Ledger set) still gets replay protection: the first post
// confirms, the second answers a "replay:" line, and the handler ran
// exactly once.
func TestNewBuildsDefaultLedger(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	var count int64
	e, err := dispatch.New(dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(countingHandler{count: &count}),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	msg := signIn(t, key, "room-1", "m-1", "hello")
	data := encodeLine(t, msg)

	resp1 := postLine(t, srv.URL, data)
	lines1 := readLines(t, resp1)
	if len(lines1) != 1 {
		t.Fatalf("first reply lines = %d, want 1", len(lines1))
	}
	ack, err := envelope.DecodeAck(lines1[0])
	if err != nil {
		t.Fatalf("first reply DecodeAck() error: %v, line %q", err, lines1[0])
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("first reply ack status = %q, want confirmed", ack.Status)
	}

	resp2 := postLine(t, srv.URL, data)
	lines2 := readLines(t, resp2)
	if len(lines2) != 1 {
		t.Fatalf("second reply lines = %d, want 1", len(lines2))
	}
	msgTxt, ok := decodeAsError(t, lines2[0])
	if !ok {
		t.Fatalf("second reply = %q, want an error line", lines2[0])
	}
	if !strings.HasPrefix(msgTxt, "replay:") {
		t.Fatalf("second reply error = %q, want a replay: prefix", msgTxt)
	}
	if got := atomic.LoadInt64(&count); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

// postLine posts body to url and returns the response, failing the
// test on a transport error.
func postLine(t testing.TB, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/x-ndjson", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
