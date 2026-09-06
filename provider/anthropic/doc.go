// Package anthropic implements a provider.Completer adapter for the
// Anthropic Messages API.
//
// Map: doc.go = package overview; options.go = Options and validation;
// client.go = Client type and Completer implementation; errors.go =
// sentinel errors; request.go = request translation; response.go =
// response translation; stream.go = SSE event streaming; wire.go =
// wire payload serialization.
package anthropic

//
// Replay: an assistant turn's thinking and redacted_thinking blocks
// ride on Message.ReasoningBlocks and replay on later requests. A
// block is valid only beside the Model that minted it; the adapter
// does not guard the pairing, so a caller that switches Request.Model
// between turns must set Request.DisableProviderReplay.
