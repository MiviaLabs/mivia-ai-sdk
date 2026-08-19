// Shared plumbing for the five workspace-and-diff file tools.

package subagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBadArguments reports a file tool call whose raw arguments failed
// to decode into the tool's typed argument struct, or whose
// tools.InOut.Value did not carry that struct. Separate from
// ErrBadCommand: the file tools take a typed, schema-validated
// argument struct instead of a JSON-command string.
var ErrBadArguments = errors.New("subagent: bad arguments")

// badArguments names the tool inside the shared sentinel.
func badArguments(name string) error {
	return fmt.Errorf("%s: %w", name, ErrBadArguments)
}

// decodeArgs unmarshals raw into a T value, mapping any parse fault
// to ErrBadArguments.
func decodeArgs[T any](name string, raw []byte) (T, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, badArguments(name)
	}
	return v, nil
}

// flatStringSchema builds a flat JSON Schema object, string-typed
// properties only, every named property required, and
// additionalProperties false. Well under schema.MaxSchemaBytes and
// schema.MaxSchemaDepth, so schema.Compile never fails on it. Built
// by hand, not through json.Marshal: this document is the wire form
// itself, offered to a model as a tool's parameter schema, so it
// follows the same "wire bytes only through a dedicated builder" rule
// envelope's Encode enforces for the message wire form.
func flatStringSchema(properties ...string) []byte {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	for i, p := range properties {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(p))
		b.WriteString(`:{"type":"string"}`)
	}
	b.WriteString(`},"required":[`)
	for i, p := range properties {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(p))
	}
	b.WriteString(`],"additionalProperties":false}`)
	return []byte(b.String())
}

// WorkspaceEntry is one directory entry WorkspaceListTool returns.
type WorkspaceEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}
