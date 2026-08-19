package contextstate

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Shape bounds pin the FORM of a durable value, not its volume.
// Volume bounds are caller-owned; see limits.go.
const (
	// MaxIdentifierBytes bounds every identifier string.
	MaxIdentifierBytes = 128
	// MaxPayloadReferenceBytes bounds a payload reference string.
	MaxPayloadReferenceBytes = 256
	// MaxSourceRangeEvents bounds a source range's event span.
	MaxSourceRangeEvents = 100_000
)

// Sentinels. ErrInvalidRecord wraps every validation failure; each
// sentinel has a producer in this package.
var (
	// ErrInvalidRecord wraps every validation failure.
	ErrInvalidRecord = errors.New("invalid context record")
	// ErrSessionNotFound marks a read of an unknown session.
	ErrSessionNotFound = errors.New("session not found")
	// ErrStaleRevision marks a commit against a moved revision.
	ErrStaleRevision = errors.New("stale revision")
	// ErrStaleBinding marks a commit against a moved binding.
	ErrStaleBinding = errors.New("stale binding")
	// ErrCheckpointConflict marks a reused operation key that carries
	// a different request.
	ErrCheckpointConflict = errors.New("checkpoint conflict")
	// ErrPayloadNotFound marks a read of an unknown payload.
	ErrPayloadNotFound = errors.New("payload not found")
	// ErrPayloadRevoked marks a Get of a payload MemStore.Revoke marked
	// revoked. Get denies the content; MemStore.Status still answers.
	ErrPayloadRevoked = errors.New("payload revoked")
	// ErrOverLimit marks a commit that breaks a volume bound.
	ErrOverLimit = errors.New("commit over volume limit")
)

// ValidationError names the field that made a record invalid. Match
// with errors.Is(err, ErrInvalidRecord) through Unwrap.
type ValidationError struct {
	Field  string
	Reason string
}

// Error renders the sentinel, the field, and the reason.
func (e *ValidationError) Error() string {
	if e == nil {
		return ErrInvalidRecord.Error()
	}
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", ErrInvalidRecord, e.Reason)
	}
	return fmt.Sprintf("%s: %s: %s", ErrInvalidRecord, e.Field, e.Reason)
}

// Unwrap reports the sentinel under every validation failure.
func (e *ValidationError) Unwrap() error { return ErrInvalidRecord }

// invalid builds the wrapped validation failure for one field.
func invalid(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}

// validateIdentifier bounds a required identifier at
// MaxIdentifierBytes.
func validateIdentifier(field, value string) error {
	return validateBoundedText(field, value, MaxIdentifierBytes, false)
}

// validateBoundedText rejects empty text when required, then text
// over max, invalid UTF-8, or a control character.
func validateBoundedText(field, value string, max int, allowEmpty bool) error {
	if !allowEmpty && strings.TrimSpace(value) == "" {
		return invalid(field, "must not be empty")
	}
	if len(value) > max || !utf8.ValidString(value) {
		return invalid(field, "invalid or too long")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return invalid(field, "contains a control character")
		}
	}
	return nil
}

// ContentRef is the durable address of one shared context blob. Ref
// is the address a caller hands around; SHA256 is the bare digest the
// payload checks compare against; Size is the whole-payload byte
// count. The three owner strings replace the source's Principal.
type ContentRef struct {
	Ref         string `json:"ref"`
	Namespace   string `json:"namespace"`
	SHA256      string `json:"sha256"`
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
	SubjectID   string `json:"subject_id"`
	Size        int    `json:"size"`
}

// Validate enforces the canonical Ref form, its match with SHA256,
// the identifier bounds on Namespace and the owner strings, and a
// non-negative Size. Namespace is caller-owned; no SDK constant is
// compared against it.
func (r ContentRef) Validate() error {
	if !IsRef(r.Ref) {
		return invalid("content.ref", "must be a canonical content address")
	}
	// IsRef plus this equality imply SHA256 is 64 lowercase hex
	// characters; no third check can fire after both.
	if r.Ref != HashPrefix+r.SHA256 {
		return invalid("content.ref", "does not match the bare digest")
	}
	if err := validateIdentifier("content.namespace", r.Namespace); err != nil {
		return err
	}
	if err := validateIdentifier("content.workspace_id", r.WorkspaceID); err != nil {
		return err
	}
	if err := validateIdentifier("content.session_id", r.SessionID); err != nil {
		return err
	}
	if err := validateIdentifier("content.subject_id", r.SubjectID); err != nil {
		return err
	}
	if r.Size < 0 {
		return invalid("content.size", "must not be negative")
	}
	return nil
}

// NewContentRef mints a ContentRef over the concatenation of chunks.
// It fills the namespace and owner fields, validates the result, and
// wraps ErrInvalidRecord on failure.
func NewContentRef(namespace string, workspaceID string, sessionID string, subjectID string, chunks ...[]byte) (ContentRef, error) {
	size := 0
	for _, chunk := range chunks {
		size += len(chunk)
	}
	ref := ContentRef{
		Ref:         Mint(chunks...),
		Namespace:   namespace,
		SHA256:      Digest(chunks...),
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		SubjectID:   subjectID,
		Size:        size,
	}
	if err := ref.Validate(); err != nil {
		return ContentRef{}, err
	}
	return ref, nil
}

// RetentionClass labels how long a payload is kept. PayloadRecord
// accepts any non-empty class, so a caller may define its own.
type RetentionClass string

const (
	// RetentionSession keeps a payload for the session's lifetime.
	RetentionSession RetentionClass = "session"
	// RetentionCompliance keeps a payload past session deletion.
	RetentionCompliance RetentionClass = "compliance"
)

// PayloadRecord is one stored payload under its content address.
type PayloadRecord struct {
	Ref       ContentRef     `json:"ref"`
	Retention RetentionClass `json:"retention"`
	Revoked   bool           `json:"revoked"`
	Data      []byte         `json:"data,omitempty"`
}

// Validate enforces a valid Ref, a non-empty Retention, and, when
// Data is present, a length and a digest that match Ref.
func (p PayloadRecord) Validate() error {
	if err := p.Ref.Validate(); err != nil {
		return err
	}
	if p.Retention == "" {
		return invalid("payload.retention", "must not be empty")
	}
	if len(p.Data) > 0 && p.Ref.Size != len(p.Data) {
		return invalid("payload.data", "size does not match the content address")
	}
	if len(p.Data) > 0 && Digest(p.Data) != p.Ref.SHA256 {
		return invalid("payload.data", "does not match the content address digest")
	}
	return nil
}

// Reassemble concatenates the chunks in order under one ref and
// returns the record with Data set. It fails closed on a size or
// digest mismatch. The whole-payload digest is the contract; chunk
// boundaries are storage granularity.
func Reassemble(ref ContentRef, retention RetentionClass, chunks ...[]byte) (PayloadRecord, error) {
	if err := ref.Validate(); err != nil {
		return PayloadRecord{}, err
	}
	if retention == "" {
		return PayloadRecord{}, invalid("payload.retention", "must not be empty")
	}
	data := make([]byte, 0)
	for _, chunk := range chunks {
		data = append(data, chunk...)
	}
	if len(data) != ref.Size {
		return PayloadRecord{}, invalid("payload.data", "size does not match the content address")
	}
	if Digest(chunks...) != ref.SHA256 {
		return PayloadRecord{}, invalid("payload.data", "does not match the content address digest")
	}
	return PayloadRecord{Ref: ref, Retention: retention, Data: data}, nil
}
