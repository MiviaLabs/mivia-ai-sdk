// Package plan manages token budget windows and compaction.
// Compact and Calibrated adapt provider messages to bounded context windows.
// Summarize turns the messages a compaction drops into one validated,
// bounded summary document, through one bounded provider.Completer
// call. A summary failure is a caller-visible error; no structural
// fallback exists. The loop wiring lives in agentloop.
//
// Layering: context holds three public packages. plan consumes ref
// and provider; agentloop wires plan above it. budget states byte
// caps beside it. The durable session contract and its store live in
// the x/ sub-module (github.com/MiviaLabs/mivia-ai-sdk/x), private to
// that module.
//
// See docs/plans/context/plan.md.
package plan
