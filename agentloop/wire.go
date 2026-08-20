package agentloop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// errTrailingArgsData is canonicalizeArgs's error when raw carries a
// valid JSON value followed by leftover bytes: trailing garbage, or a
// second concatenated JSON value. json.Decoder.Decode alone accepts
// the leading value and ignores the rest; dec.More() catches it.
var errTrailingArgsData = fmt.Errorf("agentloop: tool call arguments carry trailing data after one JSON value")

// truncationMarker is appended to a rendered tool result that exceeds
// t's published tools.ResultBudgetOf bound.
const truncationMarker = "...[truncated]"

// ToolErrorPrefix marks RoleTool message Content as an untrusted
// error report. runOneToolCall and decodeAndRun's validation-failure
// path both prefix error-report Content with it under
// ErrorPolicyReport, so the model-facing transcript distinguishes a
// reported failure from a normal tool result without a
// provider.Message schema change.
const ToolErrorPrefix = "[tool-error] "

// BatchTruncationNotice replaces a tool result's content when
// TurnResultBudget is exhausted before that call's turn in Index
// order. Distinct from ToolErrorPrefix: a batch-shaped result is not
// a tool-run error, and distinct from the per-call truncation marker,
// which trims content in place instead of replacing it outright.
const BatchTruncationNotice = "[batch-truncated] Turn tool-result budget exhausted; this result was omitted."

// render turns out into a RoleTool message's Content string, in a
// fixed order: a string value passes through unchanged; a []byte
// value that is valid UTF-8 becomes its string form; anything else
// falls back to json.Marshal. A marshal failure wraps
// ErrUnrenderableResult. When t implements tools.ResultBudgetTool
// with a positive bound smaller than the rendered content, render
// truncates the content and appends truncationMarker.
func (l *Loop) render(t tools.Tool, out tools.Out) (string, error) {
	content, err := renderValue(out.Value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnrenderableResult, err)
	}
	if budget, ok := tools.ResultBudgetOf(t); ok && budget > 0 && len(content) > budget {
		content = truncateContent(content, budget)
	}
	return content, nil
}

// renderValue applies the string/UTF-8-bytes/JSON-fallback order to
// one tool result value.
func renderValue(v any) (string, error) {
	switch val := v.(type) {
	case nil:
		return "", nil
	case string:
		return val, nil
	case []byte:
		if utf8.Valid(val) {
			return string(val), nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// truncateContent cuts content to fit within budget bytes, reserving
// room for truncationMarker when budget is large enough to hold it.
// render calls this only when 0 < budget < len(content), so both
// slice bounds below stay in range.
func truncateContent(content string, budget int) string {
	if budget < len(truncationMarker) {
		return validPrefix(content, budget)
	}
	return validPrefix(content, budget-len(truncationMarker)) + truncationMarker
}

// validPrefix cuts content to at most n bytes, then drops a rune left
// incomplete by that cut, so the result stays valid UTF-8.
func validPrefix(content string, n int) string {
	return strings.ToValidUTF8(content[:n], "")
}

// canonicalizeArgs decodes raw as exactly one JSON value and returns
// its canonical re-marshaled form, for dedup-key comparison. It
// decodes with UseNumber(), so every number token keeps its source
// digit string as a json.Number instead of collapsing into a float64:
// plain float64 decoding loses precision above 2^53 and would
// silently equate two distinct large integers. encoding/json sorts
// object keys on Marshal, so key order does not affect the result.
// raw must decode as exactly one JSON value with no bytes left over;
// canonicalizeArgs checks this with dec.More() right after
// dec.Decode, since json.Decoder.Decode alone accepts a valid JSON
// prefix and ignores trailing bytes. An error return, either a decode
// failure or errTrailingArgsData, means the caller must exclude this
// call from the dedup set: it always runs and is never treated as a
// duplicate.
func canonicalizeArgs(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	if dec.More() {
		return "", errTrailingArgsData
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
