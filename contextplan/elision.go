package contextplan

import (
	"bytes"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// ElisionReason is the closed set of reasons Plan drops or trims a
// payload.
type ElisionReason string

// The three reasons Plan records against an Elision.
const (
	// ElisionReasonWindowOverflow marks a payload dropped because the
	// window filled before this payload's turn.
	ElisionReasonWindowOverflow ElisionReason = "window_overflow"
	// ElisionReasonRetentionExpired marks a payload whose full content
	// dropped for age, but whose RetentionClass earned it a stub
	// instead of a full removal.
	ElisionReasonRetentionExpired ElisionReason = "retention_expired"
	// ElisionReasonReasoningRedacted marks a payload excluded because
	// IsReasoningEvent marked its source event; the content never
	// entered Request.Messages at all.
	ElisionReasonReasoningRedacted ElisionReason = "reasoning_redacted"
)

// Elision is one drop or trim decision Plan made for one payload. Ref
// is always the resolved PayloadRecord's ContentRef. Kept is the byte
// length of StubContent's return for a stubbed payload; zero means
// Plan inserted no message at all for that payload.
type Elision struct {
	Ref    contextstate.ContentRef
	Reason ElisionReason
	Kept   int
}

// StubContentBytes bounds the stub Plan builds for a
// RetentionCompliance payload past its age-driven turn.
const StubContentBytes = 256

// truncationMarker is the fixed marker StubContent appends inside its
// byte cap when it truncates.
const truncationMarker = "...[elided]"

// StubContent truncates content to StubContentBytes, appending
// truncationMarker inside that cap when truncation occurs. It returns
// content unchanged when content already fits. StubContentBytes is a
// cap, not a promised length: the cut prefix passes through
// bytes.ToValidUTF8 with an empty replacement, so every invalid byte
// drops and the result may be shorter.
func StubContent(content []byte) []byte {
	if len(content) <= StubContentBytes {
		return content
	}
	keep := StubContentBytes - len(truncationMarker)
	if keep < 0 {
		keep = 0
	}
	stub := make([]byte, 0, StubContentBytes)
	stub = append(stub, bytes.ToValidUTF8(content[:keep], nil)...)
	stub = append(stub, truncationMarker...)
	return stub
}
