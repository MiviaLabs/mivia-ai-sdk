// Package contextplan manages token budget windows and compaction.
// Compact and Calibrated adapt provider messages to bounded context windows.
// Summarize turns the messages a compaction drops into one validated,
// bounded summary document, through one bounded provider.Completer
// call. A summary failure is a caller-visible error; no structural
// fallback exists. The loop wiring lives in agentloop.
// See docs/plans/contextplan.md.
package contextplan
