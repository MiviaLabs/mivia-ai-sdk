// Package schema_test holds schema's external test suite.
package schema_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/schema"
)

const simpleObjectSchema = `{
	"type": "object",
	"properties": {
		"name": {"type": "string"}
	},
	"required": ["name"]
}`

func TestCompileWellFormedSchema(t *testing.T) {
	c, err := schema.Compile([]byte(simpleObjectSchema))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c == nil {
		t.Fatal("Compile returned a nil *Compiled on success")
	}
}

// padSchemaToBytes builds a valid object schema padded with a
// description field so the encoded document is exactly target bytes.
func padSchemaToBytes(t *testing.T, target int) []byte {
	t.Helper()
	prefix := `{"type":"object","description":"`
	suffix := `"}`
	fillLen := target - len(prefix) - len(suffix)
	if fillLen < 0 {
		t.Fatalf("target %d too small for schema prefix/suffix", target)
	}
	return []byte(prefix + strings.Repeat("a", fillLen) + suffix)
}

func TestCompileAtMaxSchemaBytesAdmits(t *testing.T) {
	doc := padSchemaToBytes(t, schema.MaxSchemaBytes)
	if len(doc) != schema.MaxSchemaBytes {
		t.Fatalf("fixture is %d bytes, want exactly MaxSchemaBytes (%d)", len(doc), schema.MaxSchemaBytes)
	}
	if _, err := schema.Compile(doc); err != nil {
		t.Fatalf("Compile at MaxSchemaBytes: %v", err)
	}
}

// TestCompileOverMaxSchemaBytesRejectsBeforeParsing proves the byte-
// length cap short-circuits before any JSON parse attempt. The over-
// cap payload is deliberately malformed JSON (unbalanced braces, not
// merely oversized-but-valid). If Compile parsed it before checking
// length, the returned error would be a "not valid JSON" admission
// message; because the length check runs first, the error names only
// the byte-length reason, proving the parser's seam was never
// reached.
func TestCompileOverMaxSchemaBytesRejectsBeforeParsing(t *testing.T) {
	fillLen := schema.MaxSchemaBytes + 1
	doc := []byte("{not-json-and-unbalanced" + strings.Repeat("z", fillLen))
	_, err := schema.Compile(doc)
	if !errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Compile over MaxSchemaBytes: got %v, want ErrAdmission", err)
	}
	if strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("Compile over MaxSchemaBytes reached the parse path: %v", err)
	}
	if !strings.Contains(err.Error(), "MaxSchemaBytes") {
		t.Fatalf("Compile over MaxSchemaBytes error does not name the byte cap: %v", err)
	}
}

// nestedArraySchema builds a schema nested levels deep through a
// type:array/items chain. Each wrap adds exactly one nesting level, so
// nestedArraySchema(n) nests to depth n+1 under schema's own depth
// count (an empty leaf object counts as depth 1).
func nestedArraySchema(levels int) []byte {
	doc := []byte(`{"type":"string"}`)
	for i := 0; i < levels; i++ {
		doc = append(append([]byte(`{"type":"array","items":`), doc...), '}')
	}
	return doc
}

func TestCompileAtMaxSchemaDepthAdmits(t *testing.T) {
	doc := nestedArraySchema(schema.MaxSchemaDepth - 1)
	if _, err := schema.Compile(doc); err != nil {
		t.Fatalf("Compile at MaxSchemaDepth: %v", err)
	}
}

// TestCompileArrayWithVaryingElementDepthsAdmits exercises the depth
// scan's array branch across elements of different depths, proving it
// tracks the deepest element, not just the first.
func TestCompileArrayWithVaryingElementDepthsAdmits(t *testing.T) {
	doc := `{"type": "object", "examples": [1, {"a": {"b": 1}}]}`
	if _, err := schema.Compile([]byte(doc)); err != nil {
		t.Fatalf("Compile with varying-depth array elements: %v", err)
	}
}

func TestCompileOverMaxSchemaDepthRejects(t *testing.T) {
	doc := nestedArraySchema(schema.MaxSchemaDepth)
	_, err := schema.Compile(doc)
	if !errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Compile over MaxSchemaDepth: got %v, want ErrAdmission", err)
	}
}

func TestCompileOutOfDocumentRefRejects(t *testing.T) {
	cases := []string{
		`{"$ref": "http://example.com/other.json"}`,
		`{"$ref": "../other.json"}`,
		// Nested inside an object, so the admission scan must recurse
		// through a map value to find it.
		`{"type": "object", "properties": {"a": {"$ref": "http://example.com/other.json"}}}`,
		// Nested inside an array, so the admission scan must recurse
		// through an array element to find it.
		`{"anyOf": [{"type": "string"}, {"$ref": "http://example.com/other.json"}]}`,
	}
	for _, doc := range cases {
		_, err := schema.Compile([]byte(doc))
		if !errors.Is(err, schema.ErrAdmission) {
			t.Errorf("Compile(%q): got %v, want ErrAdmission", doc, err)
		}
	}
}

func TestCompileInDocumentRefAdmits(t *testing.T) {
	doc := `{
		"$defs": {"str": {"type": "string"}},
		"type": "object",
		"properties": {"a": {"$ref": "#/$defs/str"}}
	}`
	if _, err := schema.Compile([]byte(doc)); err != nil {
		t.Fatalf("Compile with in-document $ref: %v", err)
	}
}

// TestCompileIDRebaseWithUnresolvableFragmentRejects proves an inner
// $id that rebases resolution scope to a file:// URL does not smuggle
// a resolvable-looking $ref past compilation: a $ref that reads as an
// in-document pointer (#/$defs/...) but targets a fragment absent from
// the rebased scope still fails, with the library's own, unrelated
// "pointer not found" ErrCompile. This does not exercise
// Compile's UseLoader(nil) backstop: jsonschema/v6 v6.0.3 resolves a
// "#"-prefixed $ref by matching the current scope's URL against a
// resource id already collected from this same document, and the
// $id itself is that resource's id, so the match always succeeds
// in-document; the library never calls Loader.Load for this shape.
// See TestCompileSchemaKeywordExternalURIStaysClosed for the fixture
// that does exercise the backstop.
func TestCompileIDRebaseWithUnresolvableFragmentRejects(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "rebase-target.json")
	const marker = "mivia-schema-rebase-marker-do-not-leak"
	if err := os.WriteFile(markerPath, []byte(`{"`+marker+`": {"type": "string"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fileURL := "file://" + markerPath

	doc := `{
		"$defs": {
			"inner": {
				"$id": "` + fileURL + `",
				"type": "object",
				"properties": {"a": {"$ref": "#/$defs/x"}}
			}
		},
		"type": "object",
		"properties": {"b": {"$ref": "#/$defs/inner"}}
	}`

	_, err := schema.Compile([]byte(doc))
	if err == nil {
		t.Fatal("Compile with a file:// $id rebase and an unresolvable fragment compiled; want ErrCompile")
	}
	if !errors.Is(err, schema.ErrCompile) {
		t.Fatalf("Compile with a file:// $id rebase and an unresolvable fragment: got %v, want ErrCompile", err)
	}
	if errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Compile with a file:// $id rebase and an unresolvable fragment also matched ErrAdmission: %v", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("Compile error leaks the rebase target file's content: %v", err)
	}
}

// TestCompileIDRebaseWithResolvableFragmentAdmits proves the same
// rebased scope resolves entirely in-memory against the document tree
// when the referenced fragment does exist: no read of the file named
// by $id occurs either way, confirming the library never treats this
// shape as a remote reference.
func TestCompileIDRebaseWithResolvableFragmentAdmits(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "rebase-target.json")
	const marker = "mivia-schema-rebase-marker-do-not-leak"
	if err := os.WriteFile(markerPath, []byte(`{"`+marker+`": {"type": "string"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fileURL := "file://" + markerPath

	doc := `{
		"$defs": {
			"inner": {
				"$id": "` + fileURL + `",
				"type": "object",
				"$defs": {"x": {"type": "string"}},
				"properties": {"a": {"$ref": "#/$defs/x"}}
			}
		},
		"type": "object",
		"properties": {"b": {"$ref": "#/$defs/inner"}}
	}`

	if _, err := schema.Compile([]byte(doc)); err != nil {
		t.Fatalf("Compile with a file:// $id rebase and a resolvable in-document fragment: %v", err)
	}
}

// TestCompileSchemaKeywordExternalURIStaysClosed proves the disabled-
// loader backstop is effective on the vector it actually defends: a
// $schema keyword naming an external file:// URI. The admission scan
// inspects only $ref, so this reaches the compiler unblocked;
// jsonschema/v6's own meta-schema resolution then calls Loader.Load on
// it, and Compile's UseLoader(nil) makes that call fail closed with
// the library's own "no URLLoader set" reason instead of reading local
// disk. The marker file holds distinctive content that must never
// appear in the compiled schema or the returned error.
//
// Mutation-tested: commenting out Compile's c.UseLoader(nil) call and
// re-running this test makes Compile succeed and read the marker
// file's content into the compiled schema, proving this fixture is
// not vacuous.
func TestCompileSchemaKeywordExternalURIStaysClosed(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "schema-keyword-target.json")
	const marker = "mivia-schema-meta-marker-do-not-leak"
	if err := os.WriteFile(markerPath, []byte(`{"`+marker+`": true}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fileURL := "file://" + markerPath

	doc := `{"$schema": "` + fileURL + `", "type": "object"}`

	_, err := schema.Compile([]byte(doc))
	if err == nil {
		t.Fatal("Compile with a $schema naming a file:// URL compiled; want ErrCompile")
	}
	if !errors.Is(err, schema.ErrCompile) {
		t.Fatalf("Compile with a $schema naming a file:// URL: got %v, want ErrCompile", err)
	}
	if errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Compile with a $schema naming a file:// URL also matched ErrAdmission: %v", err)
	}
	if !strings.Contains(err.Error(), "no URLLoader set") {
		t.Fatalf("Compile with a $schema naming a file:// URL: got %v, want the disabled-loader's own reason", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("Compile error leaks the $schema target file's content: %v", err)
	}
}

func TestCompileMalformedJSONRejectsAsAdmission(t *testing.T) {
	_, err := schema.Compile([]byte(`{not valid json`))
	if !errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Compile with malformed JSON: got %v, want ErrAdmission", err)
	}
	if errors.Is(err, schema.ErrCompile) {
		t.Fatalf("Compile with malformed JSON also matched ErrCompile: %v", err)
	}
}

func TestCompileIllegalSchemaRejectsAsCompile(t *testing.T) {
	_, err := schema.Compile([]byte(`{"type": "not-a-real-type"}`))
	if !errors.Is(err, schema.ErrCompile) {
		t.Fatalf("Compile with an illegal schema: got %v, want ErrCompile", err)
	}
	if errors.Is(err, schema.ErrAdmission) {
		t.Fatalf("Compile with an illegal schema also matched ErrAdmission: %v", err)
	}
}

// TestCompileShallowRefChainAdmits proves the documented limit from
// docs/plans/schema.md: MaxSchemaDepth counts literal nesting only, so
// a schema built from many shallow, in-document $ref entries chained
// together compiles even though the chain amplifies resolution cost.
// This is by design, not by omission.
func TestCompileShallowRefChainAdmits(t *testing.T) {
	doc := `{
		"$defs": {
			"a": {"$ref": "#/$defs/b"},
			"b": {"$ref": "#/$defs/c"},
			"c": {"type": "string"}
		},
		"$ref": "#/$defs/a"
	}`
	if _, err := schema.Compile([]byte(doc)); err != nil {
		t.Fatalf("Compile with a shallow ref chain: %v", err)
	}
}

// nestedDefaultSchema builds a schema whose "default" value nests
// levels containers, each an object when object is true and a JSON
// array otherwise. The document's own object adds one more level, so
// the scanned depth is levels+1. Each fixture therefore drives one
// branch of the depth scan on its own.
func nestedDefaultSchema(levels int, object bool) []byte {
	inner := `1`
	for i := 0; i < levels; i++ {
		if object {
			inner = `{"a":` + inner + `}`
		} else {
			inner = `[` + inner + `]`
		}
	}
	return []byte(`{"type":"object","default":` + inner + `}`)
}

// TestCompileContainerDepthBoundary pins both branches of the depth
// scan at both sides of MaxSchemaDepth. A depth count that is off by
// one in either direction fails one of the four cases.
func TestCompileContainerDepthBoundary(t *testing.T) {
	cases := []struct {
		name       string
		levels     int
		object     bool
		wantReject bool
		wantDepth  bool
	}{
		{
			name:   "nested objects at MaxSchemaDepth admit",
			levels: schema.MaxSchemaDepth - 1,
			object: true,
		},
		{
			name:       "nested objects one level over MaxSchemaDepth reject",
			levels:     schema.MaxSchemaDepth,
			object:     true,
			wantReject: true,
			wantDepth:  true,
		},
		{
			name:   "nested arrays at MaxSchemaDepth admit",
			levels: schema.MaxSchemaDepth - 1,
		},
		{
			name:       "nested arrays one level over MaxSchemaDepth reject",
			levels:     schema.MaxSchemaDepth,
			wantReject: true,
			wantDepth:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := schema.Compile(nestedDefaultSchema(tc.levels, tc.object))
			if !tc.wantReject {
				if err != nil {
					t.Fatalf("Compile at MaxSchemaDepth: %v", err)
				}
				if c == nil {
					t.Fatal("Compile returned a nil *Compiled on success")
				}
				return
			}
			if !errors.Is(err, schema.ErrAdmission) {
				t.Fatalf("Compile over MaxSchemaDepth: got %v, want ErrAdmission", err)
			}
			wantText := fmt.Sprintf("nests %d levels deep", tc.levels+1)
			if tc.wantDepth && !strings.Contains(err.Error(), wantText) {
				t.Fatalf("Compile over MaxSchemaDepth: got %v, want the message to report %q",
					err, wantText)
			}
		})
	}
}

// TestCompileEmptyRefAdmits pins the admission scan's handling of an
// empty $ref value. An empty $ref names no target outside the
// document, so the scan passes it through to the compiler, and the
// scan must not index the empty string.
func TestCompileEmptyRefAdmits(t *testing.T) {
	c, err := schema.Compile([]byte(`{"type":"string","$defs":{"x":{"$ref":""}}}`))
	if err != nil {
		t.Fatalf("Compile with an empty $ref: %v", err)
	}
	if c == nil {
		t.Fatal("Compile returned a nil *Compiled on success")
	}
	if err := c.Validate([]byte(`"ok"`)); err != nil {
		t.Fatalf("Validate a matching payload: %v", err)
	}
	if err := c.Validate([]byte(`123`)); !errors.Is(err, schema.ErrValidation) {
		t.Fatalf("Validate a mismatching payload: got %v, want ErrValidation", err)
	}
}
