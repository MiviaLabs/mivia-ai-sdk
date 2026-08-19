package contextstate

import (
	"bytes"
	"fmt"
	"sync"
)

// MemStore is the shipped in-memory store: payloads by content
// address, sessions by id, committed operations by key.
// Mutex-guarded and safe for concurrent use; built through New. The
// zero value is not usable.
type MemStore struct {
	mu         sync.Mutex
	limits     Limits
	payloads   map[string]PayloadRecord
	sessions   map[string]*sessionState
	operations map[string]CommitRequest
}

// sessionState is one session's committed state under the store lock.
type sessionState struct {
	revision Revision
	binding  BindingRevision
	active   Checkpoint
	events   []SourceEvent
}

// New builds an empty MemStore under limits. A negative field wraps
// ErrInvalidRecord.
func New(limits Limits) (*MemStore, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &MemStore{
		limits:     limits,
		payloads:   make(map[string]PayloadRecord),
		sessions:   make(map[string]*sessionState),
		operations: make(map[string]CommitRequest),
	}, nil
}

// Put validates record and stores a copy under record.Ref.Ref.
// Content-addressed, so a repeat Put of equal bytes overwrites in
// place. A Put under a ref already revoked is a no-op: it returns nil
// and leaves the stored record, including Revoked == true, untouched.
func (m *MemStore) Put(record PayloadRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.payloads[record.Ref.Ref]; ok && existing.Revoked {
		return nil
	}
	m.payloads[record.Ref.Ref] = clonePayload(record)
	return nil
}

// Get returns a copy of the record stored under ref.Ref. Every error
// case returns the zero PayloadRecord: an unknown ref wraps
// ErrPayloadNotFound; a revoked record wraps ErrPayloadRevoked and
// denies Data. Status is the path for a revoked ref's metadata.
func (m *MemStore) Get(ref ContentRef) (PayloadRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.payloads[ref.Ref]
	if !ok {
		return PayloadRecord{}, fmt.Errorf("%w: %s", ErrPayloadNotFound, ref.Ref)
	}
	if stored.Revoked {
		return PayloadRecord{}, fmt.Errorf("%w: %s", ErrPayloadRevoked, ref.Ref)
	}
	return clonePayload(stored), nil
}

// Revoke sets Revoked on the stored record under ref.Ref, the only way
// a caller revokes a record after Put or Checkpoint. An unknown ref
// wraps ErrPayloadNotFound. A second Revoke on an already-revoked
// record is a no-op success.
func (m *MemStore) Revoke(ref ContentRef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.payloads[ref.Ref]
	if !ok {
		return fmt.Errorf("%w: %s", ErrPayloadNotFound, ref.Ref)
	}
	stored.Revoked = true
	m.payloads[ref.Ref] = stored
	return nil
}

// Status returns a copy of the record stored under ref.Ref with Data
// always cleared, whether or not it is revoked. It never wraps
// ErrPayloadRevoked: revocation is reported through the returned
// record's Revoked field. An unknown ref wraps ErrPayloadNotFound.
func (m *MemStore) Status(ref ContentRef) (PayloadRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.payloads[ref.Ref]
	if !ok {
		return PayloadRecord{}, fmt.Errorf("%w: %s", ErrPayloadNotFound, ref.Ref)
	}
	status := clonePayload(stored)
	status.Data = nil
	return status, nil
}

// Checkpoint applies one commit atomically. A reused OperationID
// with an equal request is a no-op success; a different request
// wraps ErrCheckpointConflict, before any other check. A new key
// runs req.Validate, the volume bounds, and the stale guards. An
// unknown session commits only against a zero Expected.
func (m *MemStore) Checkpoint(req CommitRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prior, ok := m.operations[req.OperationID]; ok {
		if equalCommitRequest(prior, req) {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrCheckpointConflict, req.OperationID)
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if err := m.enforceLimits(req); err != nil {
		return err
	}
	state, err := m.sessionFor(req)
	if err != nil {
		return err
	}
	for _, payload := range req.Payloads {
		m.payloads[payload.Ref.Ref] = clonePayload(payload)
	}
	state.events = append(state.events, req.NewSourceEvents...)
	state.active = cloneCheckpoint(req.Checkpoint)
	state.revision = Revision{Session: req.NewSession, Durable: req.NewDurable, Source: req.NewSourceSequence}
	state.binding = req.NewBinding
	m.operations[req.OperationID] = cloneCommitRequest(req)
	return nil
}

// Session returns the session's revision, binding, active
// checkpoint, and a copy of its events. An unknown id wraps
// ErrSessionNotFound.
func (m *MemStore) Session(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.sessions[id]
	if !ok {
		return Session{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return Session{
		Revision: state.revision,
		Binding:  state.binding,
		Active:   cloneCheckpoint(state.active),
		Source:   append([]SourceEvent(nil), state.events...),
	}, nil
}

// sessionFor returns the session's state after the stale guards,
// creating it for a first commit against a zero Expected.
func (m *MemStore) sessionFor(req CommitRequest) (*sessionState, error) {
	state, ok := m.sessions[req.SessionID]
	if !ok {
		if req.Expected != (Revision{}) {
			return nil, fmt.Errorf("%w: session %s does not start at %+v", ErrStaleRevision, req.SessionID, req.Expected)
		}
		state = &sessionState{}
		m.sessions[req.SessionID] = state
		return state, nil
	}
	if state.revision != req.Expected {
		return nil, fmt.Errorf("%w: session %s holds revision %+v", ErrStaleRevision, req.SessionID, state.revision)
	}
	if state.binding != req.ExpectedBinding {
		return nil, fmt.Errorf("%w: session %s holds binding %+v", ErrStaleBinding, req.SessionID, state.binding)
	}
	return state, nil
}

// enforceLimits rejects a commit whose volume breaks an enabled
// bound. A rejected commit stores nothing.
func (m *MemStore) enforceLimits(req CommitRequest) error {
	if exceeds(len(req.NewSourceEvents), m.limits.CommitEvents) {
		return fmt.Errorf("%w: %d source events", ErrOverLimit, len(req.NewSourceEvents))
	}
	total := 0
	for _, event := range req.NewSourceEvents {
		total += event.Size
	}
	if exceeds(total, m.limits.CommitEventBytes) {
		return fmt.Errorf("%w: %d source event bytes", ErrOverLimit, total)
	}
	if exceeds(len(req.Checkpoint.ActiveContext), m.limits.CheckpointBytes) {
		return fmt.Errorf("%w: active context of %d bytes", ErrOverLimit, len(req.Checkpoint.ActiveContext))
	}
	return nil
}

// equalCommitRequest compares two requests field by field. Slices
// compare element-wise; byte slices compare through bytes.Equal.
func equalCommitRequest(a, b CommitRequest) bool {
	if a.OperationID != b.OperationID || a.SessionID != b.SessionID ||
		a.Expected != b.Expected || a.ExpectedBinding != b.ExpectedBinding ||
		a.NewSession != b.NewSession || a.NewDurable != b.NewDurable ||
		a.NewSourceSequence != b.NewSourceSequence || a.NewBinding != b.NewBinding ||
		a.TurnID != b.TurnID {
		return false
	}
	if !equalCheckpoint(a.Checkpoint, b.Checkpoint) {
		return false
	}
	if !equalSourceEvents(a.NewSourceEvents, b.NewSourceEvents) {
		return false
	}
	return equalPayloads(a.Payloads, b.Payloads)
}

// equalCheckpoint compares two checkpoints; the active context
// compares through bytes.Equal.
func equalCheckpoint(a, b Checkpoint) bool {
	if a.ID != b.ID || a.Revision != b.Revision || a.Binding != b.Binding || a.TurnID != b.TurnID {
		return false
	}
	return bytes.Equal(a.ActiveContext, b.ActiveContext)
}

// equalSourceEvents compares two event slices element-wise.
func equalSourceEvents(a, b []SourceEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalPayloads compares two payload slices; data compares through
// bytes.Equal.
func equalPayloads(a, b []PayloadRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Ref != b[i].Ref || a[i].Retention != b[i].Retention || a[i].Revoked != b[i].Revoked {
			return false
		}
		if !bytes.Equal(a[i].Data, b[i].Data) {
			return false
		}
	}
	return true
}

// clonePayload deep-copies a record's data.
func clonePayload(p PayloadRecord) PayloadRecord {
	p.Data = bytes.Clone(p.Data)
	return p
}

// cloneCheckpoint deep-copies a checkpoint's active context.
func cloneCheckpoint(c Checkpoint) Checkpoint {
	c.ActiveContext = bytes.Clone(c.ActiveContext)
	return c
}

// cloneCommitRequest deep-copies a request's slices, so a later
// retry comparison cannot see caller mutations.
func cloneCommitRequest(r CommitRequest) CommitRequest {
	r.NewSourceEvents = append([]SourceEvent(nil), r.NewSourceEvents...)
	payloads := make([]PayloadRecord, len(r.Payloads))
	for i, payload := range r.Payloads {
		payloads[i] = clonePayload(payload)
	}
	r.Payloads = payloads
	r.Checkpoint = cloneCheckpoint(r.Checkpoint)
	return r
}
