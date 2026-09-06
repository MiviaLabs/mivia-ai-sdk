package provider

// ReasoningEventKind is the contextstate.SourceEvent.Kind value that
// marks a reasoning trace. The one place the literal appears;
// contextsession.IsReasoningEvent compares against this constant, never
// the literal.
const ReasoningEventKind = "reasoning"

// ReasoningEffort is the provider-neutral reasoning effort vocabulary,
// closed by six constants below. A ReasoningPolicy implementation
// may report any of these from ReasoningEffort() string; the
// interface's return type stays string to keep the existing lock, but
// a caller compares against these constants instead of a literal.
type ReasoningEffort string

// The six reasoning effort levels.
const (
	ReasoningEffortNone   ReasoningEffort = "none"
	ReasoningEffortLow    ReasoningEffort = "low"
	ReasoningEffortMedium ReasoningEffort = "medium"
	ReasoningEffortHigh   ReasoningEffort = "high"
	ReasoningEffortXHigh  ReasoningEffort = "xhigh"
	ReasoningEffortMax    ReasoningEffort = "max"
)

// ReasoningBlock is one reasoning segment a model produced. Content
// is empty whenever Redacted is true. On a redacted block, Data holds
// the opaque encrypted payload the provider issued; Content stays
// empty. Signature is the opaque replay token the provider issued for
// a readable block; provider never validates or interprets it.
// Message.ReasoningBlocks carries these blocks in arrival order.
type ReasoningBlock struct {
	Content   string
	Signature string
	Redacted  bool
	Data      string
}

// RedactBlock returns b with Content cleared and Redacted set true.
// Signature and Data pass through unchanged. Idempotent: a second
// call on an already-redacted block returns it unchanged.
func RedactBlock(b ReasoningBlock) ReasoningBlock {
	b.Content = ""
	b.Redacted = true
	return b
}
