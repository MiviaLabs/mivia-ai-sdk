package provider

// ReasoningEventKind is the contextstate.SourceEvent.Kind value that
// marks a reasoning trace. The one place the literal appears;
// contextplan.IsReasoningEvent compares against this constant, never
// the literal.
const ReasoningEventKind = "reasoning"

// ReasoningEffort is the provider-neutral reasoning effort vocabulary,
// closed by four constants below. A ReasoningPolicy implementation
// may report any of these from ReasoningEffort() string; the
// interface's return type stays string to keep the existing lock, but
// a caller compares against these constants instead of a literal.
type ReasoningEffort string

// The four reasoning effort levels.
const (
	ReasoningEffortNone   ReasoningEffort = "none"
	ReasoningEffortLow    ReasoningEffort = "low"
	ReasoningEffortMedium ReasoningEffort = "medium"
	ReasoningEffortHigh   ReasoningEffort = "high"
)

// ReasoningBlock is one reasoning segment a model produced. Content is
// empty whenever Redacted is true. ReasoningBlock never appears on
// Message or Response; it is a value a caller carries alongside its
// own session state.
type ReasoningBlock struct {
	Content  string
	Redacted bool
}

// RedactBlock returns b with Content cleared and Redacted set true.
// Idempotent: a second call on an already-redacted block returns it
// unchanged.
func RedactBlock(b ReasoningBlock) ReasoningBlock {
	b.Content = ""
	b.Redacted = true
	return b
}
