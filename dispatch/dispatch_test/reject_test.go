package dispatch_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// errBoom is the failing handler's sentinel.
var errBoom = errors.New("handler boom")

// TestRejectMalformedLine proves a line that fails envelope.Decode
// answers one error and a following good line still confirms.
func TestRejectMalformedLine(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolveAlways(echoHandler{})})
	goodMsg := signIn(t, key, "room-1", "good-0", "still works")
	goodData, err := goodMsg.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	assertBadThenGood(t, e, []byte(`{not valid json`), goodData, goodMsg.ID)
}

// rejectCase names one ladder stage a bad line fails at.
type rejectCase struct {
	name    string
	room    string
	id      string
	signed  bool
	resolve func(context.Context, envelope.Message) (dispatch.Handler, error)
}

// TestRejectStages drives, per case, a bad line failing one ladder
// stage followed by a good line in the same request. The bad line
// answers one error object; the good line still confirms, proving
// the stream stays open across a failure.
func TestRejectStages(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")

	cases := []rejectCase{
		{name: "resolve fails", room: "room-1", id: "bad-5", signed: true, resolve: resolveFailsFor("bad-5")},
		{name: "unsigned message", room: "room-1", id: "bad-1", signed: false, resolve: resolveAlways(echoHandler{})},
		{name: "wrong room", room: "other-room", id: "bad-2", signed: true, resolve: resolveAlways(echoHandler{})},
		{name: "failing handler", room: "room-1", id: "bad-3", signed: true, resolve: handleFailsFor("bad-3")},
		{name: "blank restatement", room: "room-1", id: "bad-4", signed: true, resolve: blankFor("bad-4")},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := mustNew(t, dispatch.Options{ID: "endpoint-1", Room: r, Resolve: tc.resolve})
			badData := caseMessageData(t, key, tc.room, tc.id, tc.signed)
			goodMsg := signIn(t, key, "room-1", fmt.Sprintf("good-%d", i), "still works")
			goodData, err := goodMsg.Encode()
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			assertBadThenGood(t, e, badData, goodData, goodMsg.ID)
		})
	}
}

// caseMessageData builds and encodes a message in room with id,
// signed with key when signed is true, unsigned otherwise.
func caseMessageData(t testing.TB, key ed25519.PrivateKey, room, id string, signed bool) []byte {
	t.Helper()
	if signed {
		m := signIn(t, key, room, id, "case payload")
		data, err := m.Encode()
		if err != nil {
			t.Fatalf("Encode() error: %v", err)
		}
		return data
	}
	m := envelope.Message{
		Version:    envelope.Version,
		ID:         id,
		Room:       room,
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "case payload",
	}
	data, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	return data
}

// resolveFailsFor returns a Resolve func that fails only for id.
func resolveFailsFor(id string) func(context.Context, envelope.Message) (dispatch.Handler, error) {
	return func(_ context.Context, m envelope.Message) (dispatch.Handler, error) {
		if m.ID == id {
			return nil, errBoom
		}
		return echoHandler{}, nil
	}
}

// handleFailsFor returns a Resolve func whose Handler fails only for
// id.
func handleFailsFor(id string) func(context.Context, envelope.Message) (dispatch.Handler, error) {
	return func(_ context.Context, m envelope.Message) (dispatch.Handler, error) {
		if m.ID == id {
			return echoHandler{errAfter: errBoom}, nil
		}
		return echoHandler{}, nil
	}
}

// blankFor returns a Resolve func whose Handler returns a blank
// restatement only for id.
func blankFor(id string) func(context.Context, envelope.Message) (dispatch.Handler, error) {
	return func(_ context.Context, m envelope.Message) (dispatch.Handler, error) {
		if m.ID == id {
			return blankHandler{}, nil
		}
		return echoHandler{}, nil
	}
}

// mustNew builds an Endpoint or fails the test.
func mustNew(t testing.TB, opts dispatch.Options) *dispatch.Endpoint {
	t.Helper()
	e, err := dispatch.New(opts)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return e
}

// assertBadThenGood posts bad followed by a good line, over e, and
// checks the bad line answers an error and the good line still
// confirms.
func assertBadThenGood(t *testing.T, e *dispatch.Endpoint, bad, good []byte, goodID string) {
	t.Helper()
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()
	body := append(append([]byte{}, bad...), '\n')
	body = append(body, good...)
	resp, err := http.Post(srv.URL, "application/x-ndjson", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	defer resp.Body.Close()
	lines := readLines(t, resp)
	if len(lines) != 2 {
		t.Fatalf("reply lines = %d, want 2: %q", len(lines), lines)
	}
	if _, ok := decodeAsError(t, lines[0]); !ok {
		t.Fatalf("line 0 = %q, want an error line", lines[0])
	}
	ack, err := envelope.DecodeAck(lines[1])
	if err != nil {
		t.Fatalf("line 1 DecodeAck() error: %v, line: %s", err, lines[1])
	}
	if ack.MessageID != goodID {
		t.Fatalf("ack.MessageID = %q, want %q", ack.MessageID, goodID)
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("ack.Status = %q, want %q", ack.Status, envelope.AckConfirmed)
	}
}
