package schema_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/schema"
)

// reviewVerdictSchema shapes a realistic tool-output schema: a
// structured review verdict with a two-value enum and a string array.
const reviewVerdictSchema = `{
	"type": "object",
	"properties": {
		"verdict": {"type": "string", "enum": ["pass", "fail"]},
		"findings": {"type": "array", "items": {"type": "string"}}
	},
	"required": ["verdict", "findings"]
}`

// TestSchemaIntegrationReviewVerdict compiles a realistic tool-output
// schema once and validates more than one payload against it, proving
// one *Compiled value serves many Validate calls with no recompile.
func TestSchemaIntegrationReviewVerdict(t *testing.T) {
	c := compileFixture(t, reviewVerdictSchema)

	t.Run("matching payload", func(t *testing.T) {
		payload := `{"verdict": "pass", "findings": ["looks good"]}`
		if err := c.Validate([]byte(payload)); err != nil {
			t.Fatalf("Validate matching payload: %v", err)
		}
	})

	t.Run("bad enum value", func(t *testing.T) {
		payload := `{"verdict": "maybe", "findings": []}`
		err := c.Validate([]byte(payload))
		if !errors.Is(err, schema.ErrValidation) {
			t.Fatalf("Validate bad enum value: got %v, want ErrValidation", err)
		}
		corrective := schema.Corrective(err)
		if !strings.Contains(corrective, "verdict") {
			t.Errorf("Corrective(err) = %q, want it to name the failing path", corrective)
		}
	})

	t.Run("missing findings", func(t *testing.T) {
		payload := `{"verdict": "pass"}`
		err := c.Validate([]byte(payload))
		if !errors.Is(err, schema.ErrValidation) {
			t.Fatalf("Validate missing findings: got %v, want ErrValidation", err)
		}
		corrective := schema.Corrective(err)
		if !strings.Contains(corrective, "findings") {
			t.Errorf("Corrective(err) = %q, want it to name the missing field", corrective)
		}
	})
}
