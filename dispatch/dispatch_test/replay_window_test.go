package dispatch_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// serverClock is a mutex-guarded clock a test hands to
// MemStoreOptions.Now. It starts at the wall clock and only advances.
// Endpoint stamps LeaseUntil from the wall clock, so a clock seeded
// earlier makes every lease read live forever.
type serverClock struct {
	mu sync.Mutex
	at time.Time
}

// now reads the current clock value.
func (c *serverClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// advance moves the clock forward by d.
func (c *serverClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// perKeyHandler counts Handle calls per message ID.
type perKeyHandler struct {
	mu     sync.Mutex
	counts map[string]int
}

func (h *perKeyHandler) Handle(_ context.Context, m envelope.Message) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.counts[m.ID]++
	return "got: " + m.Payload, nil
}

// count reads the Handle call count for id.
func (h *perKeyHandler) count(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts[id]
}

// blockingHandler reports that it started, then waits for the request
// context to end. A client that aborts mid-request leaves its ledger
// record StatusClaimed.
type blockingHandler struct {
	started chan struct{}
	once    sync.Once
}

func (h *blockingHandler) Handle(ctx context.Context, _ envelope.Message) (string, error) {
	h.once.Do(func() { close(h.started) })
	<-ctx.Done()
	return "", ctx.Err()
}

// cappedLedger builds a Ledger over a MemStore capped at maxEntries,
// using clock as the store's eviction clock.
func cappedLedger(t *testing.T, maxEntries int, clock *serverClock) *ledger.Ledger {
	t.Helper()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{
		MaxEntries: maxEntries,
		Now:        clock.now,
	})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	return l
}

// postConfirmed posts one signed message and asserts a confirmed ack.
func postConfirmed(t *testing.T, url string, key ed25519.PrivateKey, id string) {
	t.Helper()
	resp := postLine(t, url, encodeLine(t, signIn(t, key, "room-1", id, "hello")))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status for %s = %d, want 200", id, resp.StatusCode)
	}
	lines := readLines(t, resp)
	if len(lines) != 1 {
		t.Fatalf("reply lines for %s = %d, want 1", id, len(lines))
	}
	ack, err := envelope.DecodeAck(lines[0])
	if err != nil {
		t.Fatalf("DecodeAck for %s: %v, line %q", id, err, lines[0])
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("ack status for %s = %q, want confirmed", id, ack.Status)
	}
}

// recordCount reports how many records l holds.
func recordCount(t *testing.T, l *ledger.Ledger) int {
	t.Helper()
	snap, err := l.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return len(snap.Tasks)
}

// TestReplayCapacityBoundsRecordCount proves ReplayCapacity really
// bounds the record count an HTTP caller can grow.
func TestReplayCapacityBoundsRecordCount(t *testing.T) {
	founder, key := newMember(t)
	clock := &serverClock{at: time.Now()}
	led := cappedLedger(t, 2, clock)
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    newRoom(t, "room-1", founder, ""),
		Resolve: resolveAlways(&perKeyHandler{counts: map[string]int{}}),
		Ledger:  led,
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	for i := 0; i < 8; i++ {
		postConfirmed(t, srv.URL, key, fmt.Sprintf("m-%d", i))
	}
	if got := recordCount(t, led); got > 2 {
		t.Fatalf("ledger holds %d records, want at most the cap of 2", got)
	}
}

// TestEvictedKeyIsProcessedAgain proves the bounded window is
// deliberate: an evicted key runs its handler a second time.
func TestEvictedKeyIsProcessedAgain(t *testing.T) {
	founder, key := newMember(t)
	clock := &serverClock{at: time.Now()}
	handler := &perKeyHandler{counts: map[string]int{}}
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    newRoom(t, "room-1", founder, ""),
		Resolve: resolveAlways(handler),
		Ledger:  cappedLedger(t, 1, clock),
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	postConfirmed(t, srv.URL, key, "x")
	for i := 0; i < 4; i++ {
		postConfirmed(t, srv.URL, key, fmt.Sprintf("filler-%d", i))
	}
	postConfirmed(t, srv.URL, key, "x")

	if got := handler.count("x"); got != 2 {
		t.Fatalf("handler ran %d times for x, want 2 across the eviction", got)
	}
}

// TestAbortedRequestReleasesItsRecord proves an aborted request's
// claimed record stops pinning memory one lease after its claim.
func TestAbortedRequestReleasesItsRecord(t *testing.T) {
	founder, key := newMember(t)
	clock := &serverClock{at: time.Now()}
	led := cappedLedger(t, 2, clock)
	handler := &blockingHandler{started: make(chan struct{})}
	// Only the aborted message blocks. Every later message needs a
	// handler that returns, so the request completes.
	resolve := func(_ context.Context, m envelope.Message) (dispatch.Handler, error) {
		if m.ID == "aborted" {
			return handler, nil
		}
		return echoHandler{prefix: "got: "}, nil
	}
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    newRoom(t, "room-1", founder, ""),
		Resolve: resolve,
		Ledger:  led,
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	abortRequest(t, srv.URL, encodeLine(t, signIn(t, key, "room-1", "aborted", "hello")), handler.started)

	// The abandoned record is StatusClaimed with a live lease. It
	// becomes evictable one ReplayLease after its claim.
	clock.advance(2 * dispatch.DefaultReplayLease)
	for i := 0; i < 3; i++ {
		postConfirmed(t, srv.URL, key, fmt.Sprintf("after-%d", i))
	}
	if got := recordCount(t, led); got > 2 {
		t.Fatalf("ledger holds %d records, want at most the cap of 2", got)
	}
	// The count alone proves nothing here: three terminal records
	// can hold the count at the cap while the abandoned claim stays
	// pinned. Read the surviving statuses.
	snap, err := led.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, rec := range snap.Tasks {
		if rec.Status == ledger.StatusClaimed {
			t.Fatalf("record %+v still claimed, want the abandoned claim reclaimed", rec)
		}
	}
}

// abortRequest posts body and cancels the request once the handler
// reports that it started, leaving a claimed record behind.
func abortRequest(t *testing.T, url string, body []byte, started <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()
	<-done
}
