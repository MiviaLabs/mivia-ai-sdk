package provider

import "context"

// Completer is the required contract a caller uses to complete a chat
// turn against a language model. Name returns the provider's own
// label, for logs and error messages. Chat always waits for the
// complete response before it returns; a caller ignores Request.Stream
// when it calls Chat. ChatStream always returns a channel of Chunk
// values immediately; the channel closes after the final chunk.
type Completer interface {
	Name() string
	Chat(ctx context.Context, req Request) (Response, error)
	ChatStream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// ContextAccountant is an optional Completer capability exposing the
// bound model's maximum token count across one request. A caller
// type-asserts: if ca, ok := c.(provider.ContextAccountant); ok.
type ContextAccountant interface {
	ContextWindow() int
}

// ReasoningPolicy is an optional Completer capability exposing the
// configured reasoning-effort level for a model that supports
// extended reasoning. A caller type-asserts: if rp, ok :=
// c.(provider.ReasoningPolicy); ok.
type ReasoningPolicy interface {
	ReasoningEffort() string
}

// TokenEstimator is an optional Completer capability exposing a
// best-effort token count for a given Request, ahead of a Chat or
// ChatStream call. A caller type-asserts: if te, ok :=
// c.(provider.TokenEstimator); ok. EstimateTokens takes the same
// Request the caller intends to pass to Chat, so an implementation
// can account for every field, including Messages and Tools. The
// estimate is best-effort and provider-defined; provider states no
// accuracy guarantee and computes no estimate itself. EstimateTokens
// returns a non-nil error when it cannot produce an estimate for the
// given Request; it returns (0, nil) only for a Request the
// implementation judges to cost zero tokens, never as a failure
// signal.
type TokenEstimator interface {
	EstimateTokens(req Request) (int, error)
}
