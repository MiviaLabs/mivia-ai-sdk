package schema_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/schema"
)

func compileFixture(t testing.TB, doc string) *schema.Compiled {
	t.Helper()
	c, err := schema.Compile([]byte(doc))
	if err != nil {
		t.Fatalf("Compile fixture: %v", err)
	}
	return c
}

func TestValidateMatchingPayload(t *testing.T) {
	c := compileFixture(t, simpleObjectSchema)
	if err := c.Validate([]byte(`{"name": "alice"}`)); err != nil {
		t.Fatalf("Validate matching payload: %v", err)
	}
}

// TestValidateErrorTextNamesPathsNotInstanceValue exercises the
// validation error's own Error() text directly (not through
// Corrective), pinning that it too renders only the failing path and
// keyword, never the offending instance value.
func TestValidateErrorTextNamesPathsNotInstanceValue(t *testing.T) {
	c := compileFixture(t, simpleObjectSchema)
	err := c.Validate([]byte(`{}`))
	if err == nil {
		t.Fatal("Validate missing required field: want a non-nil error")
	}
	text := err.Error()
	if !strings.Contains(text, "name") {
		t.Errorf("err.Error() = %q, want it to name the missing field", text)
	}
	if !strings.Contains(text, schema.ErrValidation.Error()) {
		t.Errorf("err.Error() = %q, want it to mention %v", text, schema.ErrValidation)
	}
}

func TestValidateMissingRequiredFieldReturnsValidationError(t *testing.T) {
	c := compileFixture(t, simpleObjectSchema)
	err := c.Validate([]byte(`{}`))
	if !errors.Is(err, schema.ErrValidation) {
		t.Fatalf("Validate missing required field: got %v, want ErrValidation", err)
	}
	corrective := schema.Corrective(err)
	if !strings.Contains(corrective, "name") {
		t.Errorf("Corrective(err) = %q, want it to name the missing field", corrective)
	}
	if len(corrective) > schema.MaxCorrectiveBytes {
		t.Errorf("Corrective(err) is %d bytes, over MaxCorrectiveBytes (%d)", len(corrective), schema.MaxCorrectiveBytes)
	}
}

func TestValidateMalformedPayloadReturnsMalformedPayload(t *testing.T) {
	c := compileFixture(t, simpleObjectSchema)
	err := c.Validate([]byte(`{not valid json`))
	if !errors.Is(err, schema.ErrMalformedPayload) {
		t.Fatalf("Validate malformed payload: got %v, want ErrMalformedPayload", err)
	}
}

func TestValidateAdditionalPropertiesViolationReturnsValidationError(t *testing.T) {
	doc := `{
		"type": "object",
		"properties": {"name": {"type": "string"}},
		"required": ["name"],
		"additionalProperties": false
	}`
	c := compileFixture(t, doc)
	err := c.Validate([]byte(`{"name": "alice", "extra": true}`))
	if !errors.Is(err, schema.ErrValidation) {
		t.Fatalf("Validate additionalProperties violation: got %v, want ErrValidation", err)
	}
}

func TestValidateAtMaxPayloadBytesAdmits(t *testing.T) {
	doc := `{"type": "object", "properties": {"note": {"type": "string"}}}`
	c := compileFixture(t, doc)
	prefix := `{"note":"`
	suffix := `"}`
	fillLen := schema.MaxPayloadBytes - len(prefix) - len(suffix)
	if fillLen < 0 {
		t.Fatalf("target %d too small for payload prefix/suffix", schema.MaxPayloadBytes)
	}
	payload := []byte(prefix + strings.Repeat("a", fillLen) + suffix)
	if len(payload) != schema.MaxPayloadBytes {
		t.Fatalf("fixture is %d bytes, want exactly MaxPayloadBytes (%d)", len(payload), schema.MaxPayloadBytes)
	}
	if err := c.Validate(payload); err != nil {
		t.Fatalf("Validate at MaxPayloadBytes: %v", err)
	}
}

// TestValidateOverMaxPayloadBytesRejectsBeforeParsing proves the byte-
// length cap short-circuits before any JSON unmarshal attempt. The
// over-cap payload is deliberately malformed JSON; if Validate
// unmarshaled it first, the returned error would be
// ErrMalformedPayload, not ErrAdmission.
func TestValidateOverMaxPayloadBytesRejectsBeforeParsing(t *testing.T) {
	doc := `{"type": "object"}`
	c := compileFixture(t, doc)
	fillLen := schema.MaxPayloadBytes + 1
	payload := []byte("{not-json-and-unbalanced" + strings.Repeat("z", fillLen))
	err := c.Validate(payload)
	if !errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Validate over MaxPayloadBytes: got %v, want ErrAdmission", err)
	}
	if errors.Is(err, schema.ErrMalformedPayload) {
		t.Fatalf("Validate over MaxPayloadBytes reached the unmarshal path: %v", err)
	}
	if !strings.Contains(err.Error(), "MaxPayloadBytes") {
		t.Fatalf("Validate over MaxPayloadBytes error does not name the byte cap: %v", err)
	}
}
