package contextstate

import "fmt"

// CommitRequest is one atomic session advance: events, payloads, and
// the new active checkpoint under an idempotent operation key.
type CommitRequest struct {
	OperationID       string          `json:"operation_id"`
	SessionID         string          `json:"session_id"`
	Expected          Revision        `json:"expected"`
	ExpectedBinding   BindingRevision `json:"expected_binding"`
	NewSourceEvents   []SourceEvent   `json:"new_source_events"`
	Payloads          []PayloadRecord `json:"payloads,omitempty"`
	Checkpoint        Checkpoint      `json:"checkpoint"`
	NewSession        uint64          `json:"new_session"`
	NewDurable        uint64          `json:"new_durable"`
	NewSourceSequence uint64          `json:"new_source_sequence"`
	NewBinding        BindingRevision `json:"new_binding"`
	TurnID            uint64          `json:"turn_id"`
}

// NewCommitRequest builds one complete request. It sets OperationID
// from the checkpoint's idempotency key, derives the three new
// revision fields from expected and the event count, validates, and
// wraps ErrInvalidRecord on failure.
func NewCommitRequest(sessionID string, expected Revision, expectedBinding BindingRevision, events []SourceEvent, payloads []PayloadRecord, checkpoint Checkpoint, newBinding BindingRevision, turnID uint64) (CommitRequest, error) {
	r := CommitRequest{
		OperationID:       checkpoint.ID.IdempotencyKey,
		SessionID:         sessionID,
		Expected:          expected,
		ExpectedBinding:   expectedBinding,
		NewSourceEvents:   events,
		Payloads:          payloads,
		Checkpoint:        checkpoint,
		NewSession:        expected.Session + 1,
		NewDurable:        expected.Durable + 1,
		NewSourceSequence: expected.Source + uint64(len(events)),
		NewBinding:        newBinding,
		TurnID:            turnID,
	}
	if err := r.Validate(); err != nil {
		return CommitRequest{}, err
	}
	return r, nil
}

// Validate enforces shape only, in this order: identity, revision,
// events, payloads, checkpoint. Volume bounds live in the store
// (limits.go, store.go).
func (r CommitRequest) Validate() error {
	if err := validateCommitIdentity(r); err != nil {
		return err
	}
	if err := validateCommitRevision(r); err != nil {
		return err
	}
	if err := validateCommitEvents(r); err != nil {
		return err
	}
	if err := validateCommitPayloads(r); err != nil {
		return err
	}
	return validateCommitCheckpoint(r)
}

// validateCommitIdentity bounds SessionID and OperationID and
// validates both bindings.
func validateCommitIdentity(r CommitRequest) error {
	if err := validateIdentifier("session_id", r.SessionID); err != nil {
		return err
	}
	if err := validateIdentifier("operation_id", r.OperationID); err != nil {
		return err
	}
	if err := r.ExpectedBinding.Validate(); err != nil {
		return err
	}
	return r.NewBinding.Validate()
}

// validateCommitRevision requires every new revision field to be the
// next value.
func validateCommitRevision(r CommitRequest) error {
	if r.NewSession != r.Expected.Session+1 || r.NewDurable != r.Expected.Durable+1 {
		return invalid("revision", "new revisions are not the next revision")
	}
	if r.NewSourceSequence != r.Expected.Source+uint64(len(r.NewSourceEvents)) {
		return invalid("new_source_sequence", "does not match source events")
	}
	return nil
}

// validateCommitEvents validates every event, requires session
// membership, and requires contiguous sequences from the expected
// head.
func validateCommitEvents(r CommitRequest) error {
	for i, event := range r.NewSourceEvents {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("source event %d: %w", i, err)
		}
		if event.ID.SessionID != r.SessionID {
			return invalid("new_source_events", "event belongs to another session")
		}
		if event.ID.Sequence != r.Expected.Source+uint64(i)+1 {
			return invalid("new_source_events", "source sequence is not contiguous")
		}
	}
	return nil
}

// validateCommitPayloads validates every payload and requires the
// payload's ref to belong to SessionID.
func validateCommitPayloads(r CommitRequest) error {
	for i, payload := range r.Payloads {
		if err := payload.Validate(); err != nil {
			return fmt.Errorf("payload %d: %w", i, err)
		}
		if payload.Ref.SessionID != r.SessionID {
			return invalid("payloads", "payload belongs to another session")
		}
	}
	return nil
}

// validateCommitCheckpoint validates the checkpoint and requires the
// new revision, the new binding, the turn, and a range that covers
// the new events.
func validateCommitCheckpoint(r CommitRequest) error {
	if err := r.Checkpoint.Validate(); err != nil {
		return err
	}
	wantRevision := Revision{Session: r.NewSession, Durable: r.NewDurable, Source: r.NewSourceSequence}
	if r.Checkpoint.Revision != wantRevision {
		return invalid("checkpoint.revision", "does not match new revision")
	}
	if r.Checkpoint.Binding != r.NewBinding {
		return invalid("checkpoint.binding", "does not match new binding")
	}
	if r.Checkpoint.TurnID != r.TurnID || r.TurnID == 0 {
		return invalid("turn_id", "does not match checkpoint")
	}
	if len(r.NewSourceEvents) > 0 {
		return validateCommitRange(r)
	}
	if r.Checkpoint.ID.SourceRange.End.Sequence != r.Expected.Source {
		return invalid("checkpoint.source_range", "empty commit range does not end at source head")
	}
	return nil
}

// validateCommitRange requires the checkpoint range to cover the new
// events. The range may start before the first new event.
func validateCommitRange(r CommitRequest) error {
	first := r.NewSourceEvents[0].ID.Sequence
	last := r.NewSourceEvents[len(r.NewSourceEvents)-1].ID.Sequence
	if r.Checkpoint.ID.SourceRange.Start.Sequence > first ||
		r.Checkpoint.ID.SourceRange.End.Sequence != last {
		return invalid("checkpoint.source_range", "does not cover new source events")
	}
	return nil
}
