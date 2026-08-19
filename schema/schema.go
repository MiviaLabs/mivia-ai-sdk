// Package schema compiles a JSON Schema document, validates JSON
// payloads against it, and builds a bounded, model-facing corrective
// message on a validation failure. See docs/plans/schema.md for the
// contract.
package schema

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// MaxSchemaBytes is the admission cap on a schema document's byte
// length, checked before Compile parses it.
const MaxSchemaBytes = 16 << 10

// MaxSchemaDepth is the admission cap on a schema document's
// object/array nesting depth, checked before Compile parses it.
const MaxSchemaDepth = 32

// MaxPayloadBytes is the admission cap on a Validate payload's byte
// length, checked before Validate unmarshals it. Symmetric to
// MaxSchemaBytes: the payload is this design's stated adversarial
// input (raw tool output, a raw model completion), so it gets the
// same fail-closed treatment as the schema document.
const MaxPayloadBytes = 64 << 10

// MaxCorrectiveBytes is the byte cap Corrective truncates its output
// to.
const MaxCorrectiveBytes = 1024

// ErrAdmission is Compile's error when a schema document fails an
// admission cap or carries an out-of-document $ref, before
// compilation runs. It is also Validate's error when payload exceeds
// MaxPayloadBytes, before any unmarshal attempt. Test with errors.Is.
var ErrAdmission = errors.New("schema: admission rejected")

// ErrCompile is Compile's error when an admitted document is not a
// legal JSON Schema. Test with errors.Is.
var ErrCompile = errors.New("schema: compile failed")

// ErrMalformedPayload is Validate's error when payload is not
// parseable JSON. Test with errors.Is.
var ErrMalformedPayload = errors.New("schema: payload is not valid JSON")

// ErrValidation is Validate's error when parsed JSON does not match
// the compiled schema. Test with errors.Is.
var ErrValidation = errors.New("schema: payload does not match schema")

// compiledResourceURL names the one virtual resource Compile registers
// per call; it names no real file and is never resolved through a
// loader.
const compiledResourceURL = "schema.json"

// Compiled is a schema ready for repeated Validate calls. Built only
// through Compile.
type Compiled struct {
	sch *jsonschema.Schema
}

// Compile admits and compiles a JSON Schema document. Rejects a
// document over MaxSchemaBytes, over MaxSchemaDepth nested objects or
// arrays, or carrying a $ref outside the document, all before
// compilation runs. Returns ErrAdmission, wrapped with the specific
// reason, for any of the three. Calls the underlying compiler's
// UseLoader(nil) before compiling, disabling the library's default
// FileLoader, so any resolution attempt the admission scan cannot see
// fails closed instead of reading from local disk. This backstop's
// proven vector is a $schema keyword naming an external URI: the
// admission scan only inspects $ref, so a $schema pointing at a
// file:// URL reaches the compiler unblocked, and jsonschema/v6
// v6.0.3's own meta-schema resolution calls Loader.Load on it. Verified
// against this library version: an in-document, "#"-prefixed $ref
// under an $id-rebased scope does not reach the loader at all, even
// when scope has shifted, because the library resolves such a $ref by
// matching the current scope's URL to a resource id already collected
// from this same document. Returns ErrCompile, wrapped with the
// compiler's own reason, when the document is not a legal JSON
// Schema.
func Compile(schemaBytes []byte) (*Compiled, error) {
	if len(schemaBytes) > MaxSchemaBytes {
		return nil, fmt.Errorf("%w: schema document is %d bytes, over MaxSchemaBytes (%d)",
			ErrAdmission, len(schemaBytes), MaxSchemaBytes)
	}
	doc, err := parseJSON(schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: schema document is not valid JSON: %v", ErrAdmission, err)
	}
	if depth := containerDepth(doc); depth > MaxSchemaDepth {
		return nil, fmt.Errorf("%w: schema nests %d levels deep, over MaxSchemaDepth (%d)",
			ErrAdmission, depth, MaxSchemaDepth)
	}
	if ref, ok := outOfDocumentRef(doc); ok {
		return nil, fmt.Errorf("%w: schema $ref %q resolves outside the document", ErrAdmission, ref)
	}

	c := jsonschema.NewCompiler()
	c.UseLoader(nil)
	if err := c.AddResource(compiledResourceURL, doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCompile, err)
	}
	sch, err := c.Compile(compiledResourceURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return &Compiled{sch: sch}, nil
}

// parseJSON parses data as one JSON value, preserving number
// precision the way the underlying schema compiler expects. Shared by
// Compile's schema-document parse and Validate's payload parse.
func parseJSON(data []byte) (any, error) {
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

// containerDepth reports the object/array nesting depth of v: a scalar
// is depth 0, an empty object or array is depth 1, and each further
// nesting level adds 1. Counts literal nesting only; a $ref hop is not
// a nesting level (see docs/plans/schema.md's accepted ref-chain
// limit).
func containerDepth(v any) int {
	switch t := v.(type) {
	case map[string]any:
		max := 0
		for _, val := range t {
			if d := containerDepth(val); d > max {
				max = d
			}
		}
		return max + 1
	case []any:
		max := 0
		for _, val := range t {
			if d := containerDepth(val); d > max {
				max = d
			}
		}
		return max + 1
	default:
		return 0
	}
}

// outOfDocumentRef reports the first $ref value found in v whose
// target is not an in-document JSON pointer (one starting with "#").
// This is a string-level scan over $ref only; it does not inspect
// $schema, so a $schema naming an external URI reaches the compiler
// unblocked. Compile's UseLoader(nil) is the fail-closed backstop for
// that case.
func outOfDocumentRef(v any) (string, bool) {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok && len(ref) > 0 && ref[0] != '#' {
			return ref, true
		}
		for _, val := range t {
			if ref, found := outOfDocumentRef(val); found {
				return ref, true
			}
		}
	case []any:
		for _, val := range t {
			if ref, found := outOfDocumentRef(val); found {
				return ref, true
			}
		}
	}
	return "", false
}
