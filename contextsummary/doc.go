// Package contextsummary turns the messages a compaction drops into
// one validated, bounded summary document, through one bounded
// provider.Completer call. A summary failure is a caller-visible
// error; no structural fallback exists. Compaction policy lives in
// contextplan; the loop wiring lives in agentloop.
package contextsummary
