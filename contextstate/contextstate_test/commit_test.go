package contextstate_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// fixtureBinding is a valid BindingRevision for fixtures.
var fixtureBinding = contextstate.BindingRevision{Provider: "provider-a", Model: "model-b", Generation: 1}

// fixtureEvent builds one valid event at seq for session.
func fixtureEvent(session string, seq uint64) contextstate.SourceEvent {
	return contextstate.SourceEvent{
		ID:              contextstate.SourceID{SessionID: session, Sequence: seq},
		Kind:            "message",
		Role:            "user",
		Provenance:      "fixture",
		RedactionStatus: "none",
		Size:            4,
	}
}

// fixtureEvents builds count valid events starting after head.
func fixtureEvents(session string, head uint64, count int) []contextstate.SourceEvent {
	events := make([]contextstate.SourceEvent, 0, count)
	for i := 0; i < count; i++ {
		events = append(events, fixtureEvent(session, head+uint64(i)+1))
	}
	return events
}

// fixturePayload builds one valid payload owned by session.
func fixturePayload(t *testing.T, session string, data []byte) contextstate.PayloadRecord {
	t.Helper()
	ref, err := contextstate.NewContentRef("fixture-ns", "workspace-a", session, "subject-a", data)
	if err != nil {
		t.Fatalf("NewContentRef: %v", err)
	}
	return contextstate.PayloadRecord{Ref: ref, Retention: contextstate.RetentionSession, Data: data}
}

// fixtureCheckpoint builds a checkpoint covering count events after
// expected, under key.
func commitCheckpoint(session, key string, expected contextstate.Revision, count int) contextstate.Checkpoint {
	start := expected.Source
	if count > 0 {
		start = expected.Source + 1
	}
	return contextstate.Checkpoint{
		ID: contextstate.CheckpointID{
			SessionID: session,
			SourceRange: contextstate.SourceRange{
				Start: contextstate.SourceID{SessionID: session, Sequence: start},
				End:   contextstate.SourceID{SessionID: session, Sequence: expected.Source + uint64(count)},
			},
			Algorithm:      "fixture-algorithm",
			SchemaVersion:  1,
			IdempotencyKey: key,
		},
		Revision: contextstate.Revision{
			Session: expected.Session + 1,
			Durable: expected.Durable + 1,
			Source:  expected.Source + uint64(count),
		},
		Binding:       fixtureBinding,
		ActiveContext: []byte("fixture-context"),
		TurnID:        7,
	}
}

// validRequest builds one valid CommitRequest for session under key.
func validRequest(t *testing.T, session, key string, expected contextstate.Revision, count int) contextstate.CommitRequest {
	t.Helper()
	events := fixtureEvents(session, expected.Source, count)
	payloads := []contextstate.PayloadRecord{fixturePayload(t, session, []byte("fixture-payload"))}
	req, err := contextstate.NewCommitRequest(session, expected, fixtureBinding, events, payloads, commitCheckpoint(session, key, expected, count), fixtureBinding, 7)
	if err != nil {
		t.Fatalf("NewCommitRequest: %v", err)
	}
	return req
}

func TestNewCommitRequestRoundTrip(t *testing.T) {
	expected := contextstate.Revision{Session: 2, Durable: 5, Source: 9}
	req := validRequest(t, "session-a", "op-1", expected, 2)
	if req.OperationID != "op-1" {
		t.Fatalf("OperationID = %q, want the checkpoint idempotency key", req.OperationID)
	}
	if req.NewSession != expected.Session+1 {
		t.Fatalf("NewSession = %d, want %d", req.NewSession, expected.Session+1)
	}
	if req.NewDurable != expected.Durable+1 {
		t.Fatalf("NewDurable = %d, want %d", req.NewDurable, expected.Durable+1)
	}
	if req.NewSourceSequence != expected.Source+2 {
		t.Fatalf("NewSourceSequence = %d, want %d", req.NewSourceSequence, expected.Source+2)
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("round-trip Validate: %v", err)
	}
	if len(req.Payloads) != 1 {
		t.Fatal("payloads must ride the request")
	}
	zero := validRequest(t, "session-a", "op-zero", contextstate.Revision{}, 1)
	if err := zero.Validate(); err != nil {
		t.Fatalf("first-commit Validate: %v", err)
	}
	empty := validRequest(t, "session-a", "op-empty", contextstate.Revision{}, 0)
	if err := empty.Validate(); err != nil {
		t.Fatalf("empty-commit Validate: %v", err)
	}
}

// commitRejections vary one Validate rule each.
var commitRejections = []struct {
	name   string
	mutate func(t *testing.T, r *contextstate.CommitRequest)
}{
	{"blank session id", func(_ *testing.T, r *contextstate.CommitRequest) { r.SessionID = "" }},
	{"blank operation id", func(_ *testing.T, r *contextstate.CommitRequest) { r.OperationID = "" }},
	{"invalid expected binding", func(_ *testing.T, r *contextstate.CommitRequest) { r.ExpectedBinding.Provider = "" }},
	{"invalid new binding", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewBinding.Model = "" }},
	{"session revision not next", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewSession++ }},
	{"durable revision not next", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewDurable++ }},
	{"session revision off with matching checkpoint", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.NewSession++
		r.Checkpoint.Revision.Session++
	}},
	{"durable revision off with matching checkpoint", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.NewDurable++
		r.Checkpoint.Revision.Durable++
	}},
	{"source sequence off event count", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewSourceSequence++ }},
	{"event from another session", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.NewSourceEvents[0].ID.SessionID = "session-b"
	}},
	{"event sequence non-contiguous", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.NewSourceEvents[1].ID.Sequence++
	}},
	{"event invalid", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewSourceEvents[0].Kind = "" }},
	{"payload from another session", func(t *testing.T, r *contextstate.CommitRequest) {
		r.Payloads[0] = fixturePayload(t, "session-b", []byte("fixture-payload"))
	}},
	{"payload invalid", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Payloads[0].Data[0] ^= 1
	}},
	{"checkpoint invalid", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Checkpoint.ActiveContext = nil
	}},
	{"checkpoint revision mismatch", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Checkpoint.Revision.Source--
	}},
	{"checkpoint binding mismatch", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Checkpoint.Binding.Model = "model-c"
	}},
	{"turn mismatch", func(_ *testing.T, r *contextstate.CommitRequest) { r.TurnID++ }},
	{"zero turn", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.TurnID = 0
		r.Checkpoint.TurnID = 0
	}},
	{"range misses new events", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Checkpoint.ID.SourceRange.End.Sequence++
	}},
}

func TestCommitRequestValidateRejections(t *testing.T) {
	for _, tc := range commitRejections {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest(t, "session-a", "op-1", contextstate.Revision{Session: 2, Durable: 5, Source: 9}, 2)
			tc.mutate(t, &req)
			err := req.Validate()
			if err == nil {
				t.Fatal("Validate accepted an invalid request")
			}
			if !errors.Is(err, contextstate.ErrInvalidRecord) {
				t.Fatalf("error %v does not wrap ErrInvalidRecord", err)
			}
		})
	}
}

// TestCommitRequestValidateMiddleEventSequence pins the contiguity
// rule on a middle event, where the checkpoint-range rule cannot
// catch the break: sequences 10, 13, 12 under range [10, 12].
func TestCommitRequestValidateMiddleEventSequence(t *testing.T) {
	expected := contextstate.Revision{Session: 2, Durable: 5, Source: 9}
	req := validRequest(t, "session-a", "op-1", expected, 3)
	if req.NewSourceEvents[1].ID.Sequence != 11 {
		t.Fatalf("fixture middle sequence = %d, want 11", req.NewSourceEvents[1].ID.Sequence)
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate rejected the contiguous base: %v", err)
	}
	req.NewSourceEvents[1].ID.Sequence = 13
	err := req.Validate()
	if err == nil {
		t.Fatal("Validate accepted a non-contiguous middle sequence")
	}
	if !errors.Is(err, contextstate.ErrInvalidRecord) {
		t.Fatalf("error %v does not wrap ErrInvalidRecord", err)
	}
}

func TestCommitRequestValidateEmptyCommitRange(t *testing.T) {
	req := validRequest(t, "session-a", "op-1", contextstate.Revision{Session: 2, Durable: 5, Source: 9}, 0)
	req.Checkpoint.ID.SourceRange.End.Sequence++
	if err := req.Validate(); err == nil {
		t.Fatal("Validate accepted an empty-commit range off the source head")
	}
	req.Checkpoint.ID.SourceRange.End.Sequence--
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate rejected a range at the source head: %v", err)
	}
}
