package contextstate

// SourceID names one event: a session and a sequence number.
type SourceID struct {
	SessionID string `json:"session_id"`
	Sequence  uint64 `json:"sequence"`
}

// Validate bounds the identifier.
func (id SourceID) Validate() error {
	return validateIdentifier("source_id.session_id", id.SessionID)
}

// SourceRange spans events of one session, inclusive of both ends.
type SourceRange struct {
	Start SourceID `json:"start"`
	End   SourceID `json:"end"`
}

// Validate enforces one session, an ordered span, and a span under
// MaxSourceRangeEvents.
func (r SourceRange) Validate() error {
	if err := r.Start.Validate(); err != nil {
		return err
	}
	if err := r.End.Validate(); err != nil {
		return err
	}
	if r.Start.SessionID != r.End.SessionID {
		return invalid("source_range", "start and end sessions differ")
	}
	if r.Start.Sequence > r.End.Sequence {
		return invalid("source_range", "start follows end")
	}
	if r.End.Sequence-r.Start.Sequence >= MaxSourceRangeEvents {
		return invalid("source_range", "range exceeds event limit")
	}
	return nil
}

// SourceEvent is one durable event in a session's source log.
// PayloadRef stays a bounded string, not a forced canonical form, so
// app-side key schemes stay legal.
type SourceEvent struct {
	ID              SourceID `json:"id"`
	Kind            string   `json:"kind"`
	Role            string   `json:"role"`
	ToolCallID      string   `json:"tool_call_id,omitempty"`
	PayloadRef      string   `json:"payload_ref,omitempty"`
	Provenance      string   `json:"provenance"`
	RedactionStatus string   `json:"redaction_status"`
	Size            int      `json:"size"`
}

// Validate bounds the four required text fields at 256 bytes, the
// two optional fields when set, and rejects a negative Size.
func (e SourceEvent) Validate() error {
	if err := e.ID.Validate(); err != nil {
		return err
	}
	required := []struct {
		field string
		value string
	}{
		{"source.kind", e.Kind},
		{"source.role", e.Role},
		{"source.provenance", e.Provenance},
		{"source.redaction_status", e.RedactionStatus},
	}
	for _, field := range required {
		if err := validateBoundedText(field.field, field.value, 256, false); err != nil {
			return err
		}
	}
	if e.ToolCallID != "" {
		if err := validateBoundedText("source.tool_call_id", e.ToolCallID, MaxIdentifierBytes, false); err != nil {
			return err
		}
	}
	if e.PayloadRef != "" {
		if err := validateBoundedText("source.payload_ref", e.PayloadRef, MaxPayloadReferenceBytes, false); err != nil {
			return err
		}
	}
	if e.Size < 0 {
		return invalid("source.size", "must not be negative")
	}
	return nil
}

// Revision is a session's three counters. It carries no Validate;
// the commit rules compare it as a whole.
type Revision struct {
	Session uint64 `json:"session"`
	Durable uint64 `json:"durable"`
	Source  uint64 `json:"source"`
}

// BindingRevision names the provider-model pair and its generation.
type BindingRevision struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Generation uint64 `json:"generation"`
}

// Validate bounds both identifiers and requires a positive
// generation.
func (b BindingRevision) Validate() error {
	if err := validateIdentifier("binding.provider", b.Provider); err != nil {
		return err
	}
	if err := validateIdentifier("binding.model", b.Model); err != nil {
		return err
	}
	if b.Generation == 0 {
		return invalid("binding.generation", "must be positive")
	}
	return nil
}

// CheckpointID identifies one checkpoint within a session.
type CheckpointID struct {
	SessionID      string      `json:"session_id"`
	SourceRange    SourceRange `json:"source_range"`
	Algorithm      string      `json:"algorithm"`
	SchemaVersion  uint32      `json:"schema_version"`
	IdempotencyKey string      `json:"idempotency_key"`
}

// Validate enforces the identifier bounds, a valid same-session
// SourceRange, an Algorithm bounded at 64 bytes, a positive
// SchemaVersion, and a bounded IdempotencyKey.
func (c CheckpointID) Validate() error {
	if err := validateIdentifier("checkpoint.session_id", c.SessionID); err != nil {
		return err
	}
	if err := c.SourceRange.Validate(); err != nil {
		return err
	}
	if c.SourceRange.Start.SessionID != c.SessionID {
		return invalid("checkpoint.source_range", "range belongs to another session")
	}
	if err := validateBoundedText("checkpoint.algorithm", c.Algorithm, 64, false); err != nil {
		return err
	}
	if c.SchemaVersion == 0 {
		return invalid("checkpoint.schema_version", "must be positive")
	}
	return validateBoundedText("checkpoint.idempotency_key", c.IdempotencyKey, MaxIdentifierBytes, false)
}

// Checkpoint is one committed state of a session.
type Checkpoint struct {
	ID            CheckpointID    `json:"id"`
	Revision      Revision        `json:"revision"`
	Binding       BindingRevision `json:"binding"`
	ActiveContext []byte          `json:"active_context"`
	TurnID        uint64          `json:"turn_id"`
}

// Validate enforces a valid ID, a valid Binding, a non-empty
// ActiveContext, and a positive TurnID.
func (c Checkpoint) Validate() error {
	if err := c.ID.Validate(); err != nil {
		return err
	}
	if err := c.Binding.Validate(); err != nil {
		return err
	}
	if len(c.ActiveContext) == 0 {
		return invalid("checkpoint.active_context", "must not be empty")
	}
	if c.TurnID == 0 {
		return invalid("checkpoint.turn_id", "must be positive")
	}
	return nil
}

// Session is the read model of one session. It carries no Validate,
// because every part carries its own.
type Session struct {
	Revision Revision        `json:"revision"`
	Binding  BindingRevision `json:"binding"`
	Active   Checkpoint      `json:"active"`
	Source   []SourceEvent   `json:"source"`
}
