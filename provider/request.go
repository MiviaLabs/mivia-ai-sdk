package provider

import "errors"

// ErrToolChoiceInvalid is Request.Validate's error when ToolChoice
// holds any value other than "", ToolChoiceAuto, or ToolChoiceNone.
var ErrToolChoiceInvalid = errors.New("provider: tool choice is not auto, none, or empty")

// ToolChoice controls whether and how a completion may call a tool.
// The empty value means unspecified: the completer's own default
// applies. ToolChoiceAuto and ToolChoiceNone are the two closed,
// provider-neutral overrides Request.Validate accepts.
type ToolChoice string

// The two closed ToolChoice overrides Request.Validate accepts,
// alongside the empty "unspecified" value.
const (
	ToolChoiceAuto ToolChoice = "auto"
	ToolChoiceNone ToolChoice = "none"
)

// Validate enforces the closed ToolChoice vocabulary: "", ToolChoiceAuto,
// or ToolChoiceNone, returning ErrToolChoiceInvalid for any other
// value. RunTurn calls it once, before it validates any Messages
// entry.
func (r Request) Validate() error {
	switch r.ToolChoice {
	case "", ToolChoiceAuto, ToolChoiceNone:
		return nil
	default:
		return ErrToolChoiceInvalid
	}
}

// ReasoningDialect names the wire dialect a Completer should use to
// carry ReasoningEffort to its provider. The empty value means "use
// the completer's own default dialect". provider defines no closed
// set of dialect names; a concrete client package owns its own
// vocabulary and compares against its own constants, never a
// provider literal.
type ReasoningDialect string

// CacheStyle names how a provider's wire format expresses prompt-cache
// reuse for one turn.
type CacheStyle string

// The three cache styles a provider's response may report.
const (
	CacheStyleNone     CacheStyle = "none"
	CacheStyleImplicit CacheStyle = "implicit"
	CacheStyleExplicit CacheStyle = "explicit"
)

// CacheUsage reports provider-side prompt-cache accounting for one
// turn. Reported false means the provider's response carried none of
// the recognized cache-usage fields; every other field is meaningless
// when Reported is false, the same "reported flag gates the rest"
// shape TokenEstimator's callers already expect from Usage-adjacent
// types.
type CacheUsage struct {
	Reported          bool
	Style             CacheStyle
	InputTokens       int
	CachedInputTokens int
	CacheWriteTokens  int
}

// WebSearchResult is one provider-supplied search result attached to
// a completion. Every field is a raw transport-level string; provider
// does not interpret or render it. No JSON tag: provider carries
// in-process values only and defines no wire format of its own.
type WebSearchResult struct {
	Title       string
	Content     string
	Link        string
	Media       string
	Icon        string
	Refer       string
	PublishDate string
}
