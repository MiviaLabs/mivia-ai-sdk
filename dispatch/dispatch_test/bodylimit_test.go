package dispatch_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
)

// TestValidateRejectsNegativeMaxBodyBytes covers the new Options
// field's invariant.
func TestValidateRejectsNegativeMaxBodyBytes(t *testing.T) {
	founder, _ := newMember(t)
	opts := dispatch.Options{
		ID:           "endpoint-1",
		Room:         newRoom(t, "room-1", founder, ""),
		Resolve:      resolveAlways(echoHandler{}),
		MaxBodyBytes: -1,
	}
	if err := opts.Validate(); !errors.Is(err, dispatch.ErrBadMaxBody) {
		t.Fatalf("Validate() error = %v, want ErrBadMaxBody", err)
	}
}

// TestOversizeBodyRejected answers 400 for a request body past the
// configured ceiling, instead of reading it whole into memory.
func TestOversizeBodyRejected(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{
		ID:           "endpoint-1",
		Room:         r,
		Resolve:      resolveAlways(echoHandler{prefix: "ack: "}),
		MaxBodyBytes: 512,
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	line := encodeLine(t, signIn(t, key, "room-1", "m-1", strings.Repeat("x", 1024)))
	if len(line) <= 512 {
		t.Fatalf("fixture line is %d bytes, want more than the 512 byte cap", len(line))
	}
	resp, err := http.Post(srv.URL, "application/x-ndjson", bytes.NewReader(line))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestBodyUnderCapProcessesEveryLine proves the cap leaves an ordinary
// multi-line request untouched.
func TestBodyUnderCapProcessesEveryLine(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{
		ID:           "endpoint-1",
		Room:         r,
		Resolve:      resolveAlways(echoHandler{prefix: "ack: "}),
		MaxBodyBytes: 1 << 16,
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	var body bytes.Buffer
	for _, id := range []string{"m-1", "m-2", "m-3"} {
		body.Write(encodeLine(t, signIn(t, key, "room-1", id, "payload "+id)))
		body.WriteByte('\n')
	}
	resp, err := http.Post(srv.URL, "application/x-ndjson", bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	lines := readLines(t, resp)
	if len(lines) != 3 {
		t.Fatalf("reply lines = %d, want 3", len(lines))
	}
	for i, line := range lines {
		if msg, isErr := decodeAsError(t, line); isErr {
			t.Errorf("line %d is an error object: %s", i, msg)
		}
	}
}

// TestDefaultMaxBodyBytesApplies proves a zero MaxBodyBytes resolves
// to the package default rather than to no cap at all.
func TestDefaultMaxBodyBytesApplies(t *testing.T) {
	if dispatch.DefaultMaxBodyBytes <= 0 {
		t.Fatalf("DefaultMaxBodyBytes = %d, want a positive ceiling", dispatch.DefaultMaxBodyBytes)
	}
	founder, _ := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolveAlways(echoHandler{})})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	oversize := bytes.Repeat([]byte("x"), int(dispatch.DefaultMaxBodyBytes)+1)
	resp, err := http.Post(srv.URL, "application/x-ndjson", bytes.NewReader(oversize))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
