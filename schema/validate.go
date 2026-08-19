package schema

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validate validates payload as JSON against the compiled schema.
// Rejects payload over MaxPayloadBytes with ErrAdmission, before any
// unmarshal attempt; this mirrors Compile's byte cap on the schema
// document, since raw tool output and raw model completion text are
// this design's stated adversarial input. Returns ErrMalformedPayload
// when an admitted payload does not parse as JSON; the standard
// library's json decoder bounds its own recursion, so Validate adds
// no separate depth cap on payload. Returns ErrValidation, wrapped
// with the failing instance paths, when parsed JSON does not match
// the schema. Returns nil on a match. Safe for concurrent use: many
// goroutines may call Validate on one shared *Compiled value,
// matching this SDK's flow panel members, which run concurrently and
// may share one compiled schema across waves.
func (c *Compiled) Validate(payload []byte) error {
	if len(payload) > MaxPayloadBytes {
		return fmt.Errorf("%w: payload is %d bytes, over MaxPayloadBytes (%d)",
			ErrAdmission, len(payload), MaxPayloadBytes)
	}
	instance, err := parseJSON(payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedPayload, err)
	}
	if err := c.sch.Validate(instance); err != nil {
		verr, ok := err.(*jsonschema.ValidationError)
		if !ok {
			return fmt.Errorf("%w: %v", ErrValidation, err)
		}
		return &validationError{verr: verr}
	}
	return nil
}

// validationError wraps the underlying library's *jsonschema.
// ValidationError so Corrective can read its structured causes
// without ever formatting the library's own Error() text, which
// quotes the failing instance value. Unwrap reaches ErrValidation, so
// errors.Is(err, ErrValidation) matches.
type validationError struct {
	verr *jsonschema.ValidationError
}

// Error renders the failing instance paths and kinds, matching
// Corrective's non-leakage invariant: never the instance value.
func (e *validationError) Error() string {
	return fmt.Sprintf("%s: %s", ErrValidation, summarizeFailures(e.verr))
}

// Unwrap reaches ErrValidation for errors.Is.
func (e *validationError) Unwrap() error {
	return ErrValidation
}
