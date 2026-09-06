package dispatch_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
)

// TestReplyContentTypeIsNDJSON proves a successful reply stream names
// the NDJSON content type and still carries one reply line per input
// line.
func TestReplyContentTypeIsNDJSON(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(echoHandler{prefix: "ack: "}),
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	var body bytes.Buffer
	for _, id := range []string{"m-1", "m-2"} {
		body.Write(encodeLine(t, signIn(t, key, "room-1", id, "payload "+id)))
		body.WriteByte('\n')
	}
	resp, err := http.Post(srv.URL, "application/x-ndjson", bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}
	lines := readLines(t, resp)
	if len(lines) != 2 {
		t.Fatalf("reply lines = %d, want 2", len(lines))
	}
}
