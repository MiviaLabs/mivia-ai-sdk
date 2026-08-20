package contextplan_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// byteEstimator counts one token per content byte across every
// message; deterministic for budget arithmetic in tests. A non-nil
// err makes every EstimateTokens call fail.
type byteEstimator struct{ err error }

// EstimateTokens sums the byte length of every message's Content.
func (b byteEstimator) EstimateTokens(req provider.Request) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}

// newStore builds an empty MemStore, failing the test on error.
func newStore(t *testing.T) *contextstate.MemStore {
	t.Helper()
	store, err := contextstate.New(contextstate.Limits{})
	if err != nil {
		t.Fatalf("contextstate.New: %v", err)
	}
	return store
}

// newCache builds a decode cache with a generous byte budget.
func newCache(t *testing.T) *memory.Store {
	t.Helper()
	cache, err := memory.New(1 << 20)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	return cache
}

// putPayload mints a ContentRef over data, stores a PayloadRecord
// under retention, and returns the ref.
func putPayload(t *testing.T, store *contextstate.MemStore, sessionID string, retention contextstate.RetentionClass, data []byte) contextstate.ContentRef {
	t.Helper()
	return putPayloadForSubject(t, store, sessionID, "subject-a", retention, data)
}

// putPayloadForSubject mints a ContentRef over data under subjectID,
// stores a PayloadRecord under retention, and returns the ref.
func putPayloadForSubject(t *testing.T, store *contextstate.MemStore, sessionID, subjectID string, retention contextstate.RetentionClass, data []byte) contextstate.ContentRef {
	t.Helper()
	ref, err := contextstate.NewContentRef("contextplan_test", "workspace-a", sessionID, subjectID, data)
	if err != nil {
		t.Fatalf("NewContentRef: %v", err)
	}
	record := contextstate.PayloadRecord{Ref: ref, Retention: retention, Data: data}
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return ref
}

// unstoredRef mints a ContentRef over data without storing it, so a
// resolve against it fails with ErrPayloadNotFound.
func unstoredRef(t *testing.T, sessionID string, data []byte) contextstate.ContentRef {
	t.Helper()
	ref, err := contextstate.NewContentRef("contextplan_test", "workspace-a", sessionID, "subject-a", data)
	if err != nil {
		t.Fatalf("NewContentRef: %v", err)
	}
	return ref
}

// sourceEvent builds one SourceEvent for seq with the given ref, kind,
// and role.
func sourceEvent(sessionID string, seq uint64, kind, role string, ref contextstate.ContentRef, size int) contextstate.SourceEvent {
	return contextstate.SourceEvent{
		ID:              contextstate.SourceID{SessionID: sessionID, Sequence: seq},
		Kind:            kind,
		Role:            role,
		PayloadRef:      ref.Ref,
		Provenance:      "fixture",
		RedactionStatus: "none",
		Size:            size,
	}
}
