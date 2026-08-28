package schema

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// Corrective builds a bounded, plain-text corrective message from a
// Validate error, safe to resend to a model. Returns "" for a nil
// err. Truncates at MaxCorrectiveBytes, never splitting a UTF-8 rune.
// A non-ErrValidation error (a malformed-payload error, for example)
// still renders a bounded message naming the failure kind, not the
// raw error text, so a caller-supplied payload byte stream can never
// inject arbitrary text into the corrective message. For
// ErrValidation, renders only the failing schema path and kind, never
// the payload's failing instance value.
func Corrective(err error) string {
	if err == nil {
		return ""
	}
	var msg string
	var verr *validationError
	switch {
	case errors.As(err, &verr):
		msg = summarizeFailures(verr.verr)
	case errors.Is(err, ErrMalformedPayload):
		msg = "payload is not valid JSON"
	case errors.Is(err, ErrAdmission):
		msg = "input exceeded an admission cap"
	case errors.Is(err, ErrCompile):
		msg = "schema is not a legal JSON Schema"
	default:
		msg = "validation failed"
	}
	return truncateRunes(msg, MaxCorrectiveBytes)
}

// summarizeFailures renders every leaf validation failure in verr as
// "<kind> at <instance path>", joined by "; ". It reads only the
// schema-derived KeywordPath and the structural InstanceLocation,
// kind.Required's schema-declared Missing field names, and
// kind.AdditionalProperties's Properties field names; it never reads
// an ErrorKind's Got/instance-value fields.
func summarizeFailures(verr *jsonschema.ValidationError) string {
	return strings.Join(leafFailures(verr, nil), "; ")
}

// leafFailures walks verr's cause tree and returns one description per
// leaf node (a node with no further causes).
func leafFailures(e *jsonschema.ValidationError, out []string) []string {
	if len(e.Causes) == 0 {
		return append(out, describeFailure(e))
	}
	for _, c := range e.Causes {
		out = leafFailures(c, out)
	}
	return out
}

// describeFailure renders one leaf failure without the instance value.
func describeFailure(e *jsonschema.ValidationError) string {
	path := instancePath(e.InstanceLocation)
	if req, ok := e.ErrorKind.(*kind.Required); ok {
		return "missing required " + strings.Join(req.Missing, ", ") + " at " + path
	}
	if add, ok := e.ErrorKind.(*kind.AdditionalProperties); ok {
		return "additionalProperties " + strings.Join(add.Properties, ", ") + " not allowed at " + path
	}
	return keywordName(e.ErrorKind) + " mismatch at " + path
}

// instancePath renders an instance location as a "/"-joined path; an
// empty location is the document root.
func instancePath(loc []string) string {
	if len(loc) == 0 {
		return "/"
	}
	return "/" + strings.Join(loc, "/")
}

// keywordName reports the failing keyword's name, the last segment of
// its KeywordPath, or "schema" when the path is empty.
func keywordName(k jsonschema.ErrorKind) string {
	path := k.KeywordPath()
	if len(path) == 0 {
		return "schema"
	}
	return path[len(path)-1]
}

// truncateRunes truncates s to at most maxBytes bytes without splitting
// a UTF-8 rune.
func truncateRunes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 {
		r, size := utf8.DecodeLastRuneInString(b)
		if r == utf8.RuneError && size <= 1 {
			b = b[:len(b)-1]
			continue
		}
		break
	}
	return b
}
