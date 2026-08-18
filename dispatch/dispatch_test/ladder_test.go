package dispatch_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// TestLadderHappyPath sends one signed member message and expects one
// confirmed ack line whose From is the endpoint id and whose
// restatement is the handler's.
func TestLadderHappyPath(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e, err := dispatch.New(dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(echoHandler{prefix: "restated: "}),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	msg := signIn(t, key, "room-1", "m-1", "hello there")
	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	resp, err := http.Post(srv.URL, "application/x-ndjson", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	lines := readLines(t, resp)
	if len(lines) != 1 {
		t.Fatalf("reply lines = %d, want 1: %q", len(lines), lines)
	}
	ack, err := envelope.DecodeAck(lines[0])
	if err != nil {
		t.Fatalf("DecodeAck() error: %v, line: %s", err, lines[0])
	}
	if ack.From != "endpoint-1" {
		t.Fatalf("ack.From = %q, want %q", ack.From, "endpoint-1")
	}
	if ack.Restatement != "restated: hello there" {
		t.Fatalf("ack.Restatement = %q, want %q", ack.Restatement, "restated: hello there")
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("ack.Status = %q, want %q", ack.Status, envelope.AckConfirmed)
	}
	if ack.MessageID != msg.ID {
		t.Fatalf("ack.MessageID = %q, want %q", ack.MessageID, msg.ID)
	}
}
