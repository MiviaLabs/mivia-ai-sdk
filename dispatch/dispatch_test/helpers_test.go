// Package dispatch_test holds the external tests for the dispatch
// package. httptest.NewServer drives every case except the broken-body
// 400 case, which calls Endpoint.Handler().ServeHTTP directly, since a
// live client cannot manufacture a server-side body-read error.
package dispatch_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// echoHandler restates the payload it receives. errAfter, when
// non-nil, is returned by Handle instead of a restatement.
type echoHandler struct {
	prefix   string
	errAfter error
}

func (h echoHandler) Handle(ctx context.Context, m envelope.Message) (string, error) {
	if h.errAfter != nil {
		return "", h.errAfter
	}
	return h.prefix + m.Payload, nil
}

// blankHandler always returns an empty restatement and a nil error,
// which envelope.NewAck rejects.
type blankHandler struct{}

func (blankHandler) Handle(context.Context, envelope.Message) (string, error) {
	return "", nil
}

// newMember generates a fresh ed25519 key and returns its hex signer
// string plus the private key, for use as a room member.
func newMember(t testing.TB) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	m, err := envelope.Sign(priv, envelope.Message{
		Version:    envelope.Version,
		ID:         "probe",
		ThreadID:   "probe",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "probe",
	})
	if err != nil {
		t.Fatalf("sign probe: %v", err)
	}
	_ = pub
	return m.Signer, priv
}

// newRoom builds a room named roomID, founded by founder, with member
// admitted alongside it.
func newRoom(t testing.TB, roomID, founder, member string) *room.Room {
	t.Helper()
	r, err := room.New(roomID, founder)
	if err != nil {
		t.Fatalf("room.New() error: %v", err)
	}
	if member != "" && member != founder {
		if err := r.Admit(member, founder); err != nil {
			t.Fatalf("room.Admit() error: %v", err)
		}
	}
	return r
}

// signIn returns a signed, valid message in roomID from key, with id
// and payload set.
func signIn(t testing.TB, key ed25519.PrivateKey, roomID, id, payload string) envelope.Message {
	t.Helper()
	m, err := envelope.Sign(key, envelope.Message{
		Version:    envelope.Version,
		ID:         id,
		Room:       roomID,
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    payload,
	})
	if err != nil {
		t.Fatalf("sign message: %v", err)
	}
	return m
}

// resolveAlways returns a Resolve func that always answers h.
func resolveAlways(h dispatch.Handler) func(context.Context, envelope.Message) (dispatch.Handler, error) {
	return func(context.Context, envelope.Message) (dispatch.Handler, error) {
		return h, nil
	}
}

// readLines reads resp's whole body and splits it into non-empty
// NDJSON lines.
func readLines(t testing.TB, resp *http.Response) [][]byte {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out [][]byte
	for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}

// decodeAsError reports whether line is a dispatch error object
// {"error":"..."} and, if so, its message.
func decodeAsError(t testing.TB, line []byte) (string, bool) {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return "", false
	}
	if e.Error == "" {
		return "", false
	}
	return e.Error, true
}
