// Package provider defines the Completer contract a caller uses to
// complete a chat turn against a language model, plus the request
// and response shapes the contract carries. The interface has no
// implementation in this SDK; a caller supplies a concrete type. See
// ../docs/plans/provider.md for the locked surface and
// ../docs/plans/agents/phase29_provider.md for the design rationale.
//
// Map: types.go = Role and its constants, Message, Message.Validate,
// ToolDefinition, ToolCall, Usage, Request, Response, Chunk,
// Chunk.Validate, and the sentinel errors ErrToolCallIDUnexpected,
// ErrToolCallIDRequired, ErrUnknownRole, ErrChunkErrDoneConflict;
// completer.go = Completer, ContextAccountant, ReasoningPolicy;
// runturn.go = RunTurn. Contribution rules: ../AGENTS.md.
package provider
