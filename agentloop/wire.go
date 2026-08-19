package agentloop

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

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
