package dispatch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// TestSendReturnsAcksInOrder proves Send returns one SendResult per
// request message, in request order, and surfaces a server error line
// as that entry's Err.
func TestSendReturnsAcksInOrder(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(echoHandler{prefix: "ack: "}),
	})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	good1 := signIn(t, key, "room-1", "m-1", "first")
	bad := signIn(t, key, "other-room", "m-2", "wrong room")
	good2 := signIn(t, key, "room-1", "m-3", "third")

	results, err := dispatch.Send(context.Background(), srv.URL, []envelope.Message{good1, bad, good2})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("results[0].Err = %v, want nil", results[0].Err)
	}
	if results[0].Ack.MessageID != good1.ID || results[0].Ack.Restatement != "ack: first" {
		t.Fatalf("results[0] = %+v, want message %q restated", results[0], good1.ID)
	}
	if results[1].Err == nil {
		t.Fatal("results[1].Err is nil, want the admit-stage error")
	}
	if results[2].Err != nil {
		t.Fatalf("results[2].Err = %v, want nil", results[2].Err)
	}
	if results[2].Ack.MessageID != good2.ID || results[2].Ack.Restatement != "ack: third" {
		t.Fatalf("results[2] = %+v, want message %q restated", results[2], good2.ID)
	}
}

// TestSendEmpty proves Send tolerates a zero-message request and
// returns an empty result set.
func TestSendEmpty(t *testing.T) {
	founder, _ := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e := mustNew(t, dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolveAlways(echoHandler{})})
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	results, err := dispatch.Send(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results len = %d, want 0", len(results))
	}
}

// TestSendEncodeError proves Send rejects before any network call
// when a message fails to encode.
func TestSendEncodeError(t *testing.T) {
	invalid := envelope.Message{} // missing every required field
	_, err := dispatch.Send(context.Background(), "http://unused.invalid", []envelope.Message{invalid})
	if err == nil {
		t.Fatal("Send() error is nil, want an encode failure")
	}
}

// TestSendTransportError proves Send surfaces a transport failure
// when the request cannot reach a server.
func TestSendTransportError(t *testing.T) {
	_, key := newMember(t)
	msg := signIn(t, key, "room-1", "m-1", "hello")
	_, err := dispatch.Send(context.Background(), "http://127.0.0.1:1", []envelope.Message{msg})
	if err == nil {
		t.Fatal("Send() error is nil, want a transport failure")
	}
}

// TestSendMalformedReplyLine proves a reply line that is neither an
// error object nor a valid ack surfaces as that entry's Err.
func TestSendMalformedReplyLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json at all\n"))
	}))
	defer srv.Close()

	_, key := newMember(t)
	msg := signIn(t, key, "room-1", "m-1", "hello")
	results, err := dispatch.Send(context.Background(), srv.URL, []envelope.Message{msg})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("results[0].Err is nil, want a decode failure")
	}
}

// TestSendMatchesBadMethodSentinel proves Send matches a 405 response
// carrying ErrBadMethod's text, mirroring Endpoint.Handler's own
// bad-method write, with errors.Is.
func TestSendMatchesBadMethodSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, dispatch.ErrBadMethod.Error(), http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	_, key := newMember(t)
	msg := signIn(t, key, "room-1", "m-1", "hello")
	_, err := dispatch.Send(context.Background(), srv.URL, []envelope.Message{msg})
	if err == nil {
		t.Fatal("Send() error is nil, want a bad-method failure")
	}
	if !errors.Is(err, dispatch.ErrBadMethod) {
		t.Fatalf("Send() error = %v, want errors.Is ErrBadMethod", err)
	}
}

// TestSendMatchesBadRequestSentinel proves Send matches a 400
// response carrying ErrBadRequest's text, mirroring Endpoint.Handler's
// own bad-request write, with errors.Is.
func TestSendMatchesBadRequestSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, dispatch.ErrBadRequest.Error(), http.StatusBadRequest)
	}))
	defer srv.Close()

	_, key := newMember(t)
	msg := signIn(t, key, "room-1", "m-1", "hello")
	_, err := dispatch.Send(context.Background(), srv.URL, []envelope.Message{msg})
	if err == nil {
		t.Fatal("Send() error is nil, want a bad-request failure")
	}
	if !errors.Is(err, dispatch.ErrBadRequest) {
		t.Fatalf("Send() error = %v, want errors.Is ErrBadRequest", err)
	}
}
