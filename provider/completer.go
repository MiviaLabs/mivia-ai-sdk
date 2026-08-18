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
