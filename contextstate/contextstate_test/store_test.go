package contextstate_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// newStore builds a store under limits, failing the test on error.
func newStore(t *testing.T, limits contextstate.Limits) *contextstate.MemStore {
	t.Helper()
	store, err := contextstate.New(limits)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func TestNewRejectsNegativeLimits(t *testing.T) {
	for _, limits := range []contextstate.Limits{
		{CheckpointBytes: -1},
		{CommitEvents: -1},
		{CommitEventBytes: -1},
	} {
		store, err := contextstate.New(limits)
		if err == nil {
			t.Fatalf("New accepted a negative Limits: %+v", limits)
		}
		if store != nil {
			t.Fatal("New returned a store with an error")
		}
		if !errors.Is(err, contextstate.ErrInvalidRecord) {
			t.Fatalf("error %v does not wrap ErrInvalidRecord", err)
		}
	}
}

func TestStorePutGet(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	record := fixturePayload(t, "session-a", []byte("payload-bytes"))
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(record.Ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !payloadEqual(got, record) {
		t.Fatalf("Get returned %+v, want %+v", got, record)
	}
	// A repeat Put of the same address overwrites in place.
	updated := record
	updated.Revoked = true
	if err := store.Put(updated); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	got, err = store.Get(record.Ref)
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if !got.Revoked {
		t.Fatal("repeat Put did not overwrite in place")
	}
	unknown := record.Ref
	unknown.Ref = contextstate.Mint([]byte("unknown-payload"))
	if _, err := store.Get(unknown); !errors.Is(err, contextstate.ErrPayloadNotFound) {
		t.Fatalf("Get of unknown ref: %v, want ErrPayloadNotFound", err)
	}
	invalid := record
	invalid.Retention = ""
	if err := store.Put(invalid); !errors.Is(err, contextstate.ErrInvalidRecord) {
		t.Fatalf("Put of invalid record: %v, want ErrInvalidRecord", err)
	}
}

func payloadEqual(got, want contextstate.PayloadRecord) bool {
	return got.Ref == want.Ref && got.Retention == want.Retention &&
		got.Revoked == want.Revoked && bytes.Equal(got.Data, want.Data)
}

func TestStoreCheckpointFirstCommit(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	req := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 2)
	if err := store.Checkpoint(req); err != nil {
		t.Fatalf("first Checkpoint: %v", err)
	}
	session, err := store.Session("session-a")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if session.Revision != (contextstate.Revision{Session: 1, Durable: 1, Source: 2}) {
		t.Fatalf("session revision = %+v", session.Revision)
	}
	if session.Binding != req.NewBinding {
		t.Fatalf("session binding = %+v", session.Binding)
	}
	if session.Active.ID != req.Checkpoint.ID {
		t.Fatalf("active checkpoint id = %+v", session.Active.ID)
	}
	if !bytes.Equal(session.Active.ActiveContext, req.Checkpoint.ActiveContext) {
		t.Fatal("active context not stored")
	}
	if len(session.Source) != 2 {
		t.Fatalf("session holds %d events, want 2", len(session.Source))
	}
	if _, err := store.Session("session-b"); !errors.Is(err, contextstate.ErrSessionNotFound) {
		t.Fatalf("Session of unknown id: %v, want ErrSessionNotFound", err)
	}
	// An unknown session commits only against a zero Expected.
	stale := validRequest(t, "session-c", "op-2", contextstate.Revision{Session: 1}, 1)
	if err := store.Checkpoint(stale); !errors.Is(err, contextstate.ErrStaleRevision) {
		t.Fatalf("unknown session with nonzero Expected: %v, want ErrStaleRevision", err)
	}
	if _, err := store.Session("session-c"); !errors.Is(err, contextstate.ErrSessionNotFound) {
		t.Fatal("a rejected commit must not create the session")
	}
}

func TestStoreCheckpointInvalidRequest(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	req := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 1)
	req.SessionID = ""
	if err := store.Checkpoint(req); !errors.Is(err, contextstate.ErrInvalidRecord) {
		t.Fatalf("Checkpoint of invalid request: %v, want ErrInvalidRecord", err)
	}
}

func TestStoreCheckpointStaleGuards(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	head := contextstate.Revision{}
	if err := store.Checkpoint(validRequest(t, "session-a", "op-1", head, 1)); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	next := contextstate.Revision{Session: 1, Durable: 1, Source: 1}
	staleRevision := validRequest(t, "session-a", "op-2", contextstate.Revision{Session: 1, Durable: 1, Source: 2}, 1)
	if err := store.Checkpoint(staleRevision); !errors.Is(err, contextstate.ErrStaleRevision) {
		t.Fatalf("stale revision: %v, want ErrStaleRevision", err)
	}
	otherBinding := fixtureBinding
	otherBinding.Generation = 2
	staleBinding := validRequest(t, "session-a", "op-3", next, 1)
	staleBinding.ExpectedBinding = otherBinding
	if err := store.Checkpoint(staleBinding); !errors.Is(err, contextstate.ErrStaleBinding) {
		t.Fatalf("stale binding: %v, want ErrStaleBinding", err)
	}
	fresh := validRequest(t, "session-a", "op-4", next, 1)
	if err := store.Checkpoint(fresh); err != nil {
		t.Fatalf("second commit: %v", err)
	}
}

func TestStoreCheckpointRetryIsNoOp(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	req := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 1)
	if err := store.Checkpoint(req); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if err := store.Checkpoint(req); err != nil {
		t.Fatalf("retried equal request: %v", err)
	}
	session, err := store.Session("session-a")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if len(session.Source) != 1 {
		t.Fatalf("retry appended events: %d, want 1", len(session.Source))
	}
	if session.Revision != (contextstate.Revision{Session: 1, Durable: 1, Source: 1}) {
		t.Fatalf("retry moved the revision: %+v", session.Revision)
	}
}

// conflictRows vary exactly one field of a committed request, with the
// reused OperationID held fixed.
var conflictRows = []struct {
	name   string
	mutate func(t *testing.T, r *contextstate.CommitRequest)
}{
	{"session id", func(_ *testing.T, r *contextstate.CommitRequest) { r.SessionID = "session-b" }},
	{"new session", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewSession++ }},
	{"new durable", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewDurable++ }},
	{"new source sequence", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewSourceSequence++ }},
	{"expected", func(_ *testing.T, r *contextstate.CommitRequest) { r.Expected.Session-- }},
	{"expected binding", func(_ *testing.T, r *contextstate.CommitRequest) { r.ExpectedBinding.Generation++ }},
	{"new binding", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewBinding.Generation++ }},
	{"source event field", func(_ *testing.T, r *contextstate.CommitRequest) { r.NewSourceEvents[0].Kind = "edited" }},
	{"payload ref", func(t *testing.T, r *contextstate.CommitRequest) {
		r.Payloads[0] = fixturePayload(t, "session-a", []byte("other-payload"))
	}},
	{"payload retention", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Payloads[0].Retention = contextstate.RetentionCompliance
	}},
	{"payload revoked", func(_ *testing.T, r *contextstate.CommitRequest) { r.Payloads[0].Revoked = true }},
	{"payload data byte", func(_ *testing.T, r *contextstate.CommitRequest) { r.Payloads[0].Data[0] ^= 1 }},
	{"checkpoint id", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Checkpoint.ID.IdempotencyKey = "op-other"
	}},
	{"checkpoint revision", func(_ *testing.T, r *contextstate.CommitRequest) { r.Checkpoint.Revision.Source-- }},
	{"checkpoint binding", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Checkpoint.Binding.Generation++
	}},
	{"checkpoint turn", func(_ *testing.T, r *contextstate.CommitRequest) { r.Checkpoint.TurnID++ }},
	{"checkpoint active context", func(_ *testing.T, r *contextstate.CommitRequest) {
		r.Checkpoint.ActiveContext[0] ^= 1
	}},
	{"turn id", func(_ *testing.T, r *contextstate.CommitRequest) { r.TurnID++ }},
}

func TestStoreCheckpointConflictTable(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	committed := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 1)
	if err := store.Checkpoint(committed); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	for _, tc := range conflictRows {
		t.Run(tc.name, func(t *testing.T) {
			retry := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 1)
			tc.mutate(t, &retry)
			err := store.Checkpoint(retry)
			if !errors.Is(err, contextstate.ErrCheckpointConflict) {
				t.Fatalf("reused key with a varied request: %v, want ErrCheckpointConflict", err)
			}
		})
	}
}

func TestStoreCheckpointVolumeBounds(t *testing.T) {
	contextBytes := len("fixture-context")
	t.Run("event count over limit", func(t *testing.T) {
		store := newStore(t, contextstate.Limits{CommitEvents: 1})
		req := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 2)
		if err := store.Checkpoint(req); !errors.Is(err, contextstate.ErrOverLimit) {
			t.Fatalf("two events under one-event limit: %v, want ErrOverLimit", err)
		}
		if _, err := store.Session("session-a"); !errors.Is(err, contextstate.ErrSessionNotFound) {
			t.Fatal("an over-limit commit must store nothing")
		}
	})
	t.Run("event count at limit passes", func(t *testing.T) {
		store := newStore(t, contextstate.Limits{CommitEvents: 2})
		if err := store.Checkpoint(validRequest(t, "session-a", "op-1", contextstate.Revision{}, 2)); err != nil {
			t.Fatalf("two events under a two-event limit: %v", err)
		}
	})
	t.Run("event bytes over limit", func(t *testing.T) {
		store := newStore(t, contextstate.Limits{CommitEventBytes: 7})
		req := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 2)
		if err := store.Checkpoint(req); !errors.Is(err, contextstate.ErrOverLimit) {
			t.Fatalf("eight event bytes under a seven-byte limit: %v, want ErrOverLimit", err)
		}
	})
	t.Run("event bytes at limit passes", func(t *testing.T) {
		store := newStore(t, contextstate.Limits{CommitEventBytes: 8})
		if err := store.Checkpoint(validRequest(t, "session-a", "op-1", contextstate.Revision{}, 2)); err != nil {
			t.Fatalf("eight event bytes under an eight-byte limit: %v", err)
		}
	})
	t.Run("active context over limit", func(t *testing.T) {
		store := newStore(t, contextstate.Limits{CheckpointBytes: contextBytes - 1})
		req := validRequest(t, "session-a", "op-1", contextstate.Revision{}, 1)
		if err := store.Checkpoint(req); !errors.Is(err, contextstate.ErrOverLimit) {
			t.Fatalf("context over limit: %v, want ErrOverLimit", err)
		}
	})
	t.Run("active context at limit passes", func(t *testing.T) {
		store := newStore(t, contextstate.Limits{CheckpointBytes: contextBytes})
		if err := store.Checkpoint(validRequest(t, "session-a", "op-1", contextstate.Revision{}, 1)); err != nil {
			t.Fatalf("context at limit: %v", err)
		}
	})
}

func TestStoreZeroLimitsAdmitLargeCommit(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	events := make([]contextstate.SourceEvent, 0, 64)
	for i := 0; i < 64; i++ {
		event := fixtureEvent("session-a", uint64(i)+1)
		event.Size = 4096
		events = append(events, event)
	}
	checkpoint := commitCheckpoint("session-a", "op-big", contextstate.Revision{}, 64)
	checkpoint.ActiveContext = bytes.Repeat([]byte("c"), 1<<20)
	payloads := []contextstate.PayloadRecord{
		fixturePayload(t, "session-a", bytes.Repeat([]byte("p"), 1<<16)),
	}
	req, err := contextstate.NewCommitRequest("session-a", contextstate.Revision{}, fixtureBinding, events, payloads, checkpoint, fixtureBinding, 7)
	if err != nil {
		t.Fatalf("NewCommitRequest: %v", err)
	}
	if err := store.Checkpoint(req); err != nil {
		t.Fatalf("zero-value Limits rejected a large commit: %v", err)
	}
}

func TestStoreConcurrency(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	shared := fixturePayload(t, "shared-session", []byte("shared-payload"))
	if err := store.Put(shared); err != nil {
		t.Fatalf("seed Put: %v", err)
	}
	type worker struct {
		session string
		payload contextstate.PayloadRecord
		req     contextstate.CommitRequest
	}
	workers := make([]worker, 0, 8)
	for i := 0; i < 8; i++ {
		session := "race-session-" + string(rune('a'+i))
		workers = append(workers, worker{
			session: session,
			payload: fixturePayload(t, session, []byte("payload")),
			req:     validRequest(t, session, "op-"+string(rune('a'+i)), contextstate.Revision{}, 1),
		})
	}
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func(w worker) {
			defer wg.Done()
			if err := store.Put(w.payload); err != nil {
				t.Errorf("Put(%s): %v", w.session, err)
			}
			if _, err := store.Get(w.payload.Ref); err != nil {
				t.Errorf("Get(%s): %v", w.session, err)
			}
			if err := store.Checkpoint(w.req); err != nil {
				t.Errorf("Checkpoint(%s): %v", w.session, err)
			}
			snapshot, err := store.Session(w.session)
			if err != nil {
				t.Errorf("Session(%s): %v", w.session, err)
			} else if snapshot.Revision.Session != 1 {
				t.Errorf("Session(%s) revision = %+v", w.session, snapshot.Revision)
			}
			if _, err := store.Get(shared.Ref); err != nil {
				t.Errorf("shared Get: %v", err)
			}
		}(w)
	}
	wg.Wait()
	if t.Failed() {
		t.Fatal("concurrent store use failed; see logs")
	}
}
