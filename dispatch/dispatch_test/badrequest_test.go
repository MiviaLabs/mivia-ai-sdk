package dispatch_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
)

// brokenBody is an io.ReadCloser that always fails Read, to
// manufacture a server-side body-read error. A live client-and-server
// round trip cannot produce this: a broken client body only ever
// surfaces as a client-side transport error.
type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errBrokenBody }
func (brokenBody) Close() error             { return nil }

var errBrokenBody = errors.New("broken body reader")

// TestBadMethod answers 405 for a non-POST method over a live server.
func TestBadMethod(t *testing.T) {
	founder, _ := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolveAlways(echoHandler{})})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestBadRequestBrokenBody answers 400 when the request body fails to
// read, driven directly through ServeHTTP.
func TestBadRequestBrokenBody(t *testing.T) {
	founder, _ := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolveAlways(echoHandler{})})

	req := httptest.NewRequest(http.MethodPost, "/", brokenBody{})
	rec := httptest.NewRecorder()
	e.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read recorder body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("body is empty, want the ErrBadRequest message")
	}
}
