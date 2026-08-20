# Phase 82: prompt-injection-safe hook and steer framing

Status: plan, not scheduled. Part of this phase is blocked on a
`hooks` package change; see the flagged item below.

## Why this plan exists

A gap analysis compared `agentloop` against `internal/agent.Loop`, a
production caller in a separate, external repository (`mivia-agent`).
It found a capability that repo's caller needs and `agentloop` lacks:
a documented, forgery-resistant way to mark injected guidance text
apart from real tool output or user content. This phase closes that
gap. It has no code, no plan review, and no `policy/layers.json` row
yet. It needs a plan review before a builder starts it.

## Goal

Give `agentloop` one documented, delimited way to mark text it injects
into a model-visible message as injected guidance, not real tool
output or real user content. Neutralize the delimiter inside any
untrusted text before framing it, so a malicious tool result or hook
string cannot forge the closing delimiter and escape the frame.

## Scope

Inside:

- One new pair of exported constants, `InjectionOpenTag` and
  `InjectionCloseTag`.
- One new exported function, `WrapInjected`, the only sanctioned way
  to build framed, injected text.
- This phase ships the framing primitive alone. It applies
  `WrapInjected` to no existing text. No other phase in this set
  depends on this phase.

Outside, flagged:

- Injecting a `hooks.Handler` return value into the tool-result
  stream. `hooks.Handler`'s current signature is
  `func(ctx context.Context, payload any) (bool, error)`; it carries
  no text channel back to the caller. Giving a hook a way to inject
  text needs a `hooks.Handler` signature change, which needs its own
  plan review against `docs/plans/hooks.md`, since `hooks` is a shared
  package other callers depend on today. This phase does not propose
  that change; it only defines the framing primitive a future
  `hooks`-side change would use.
- Rewriting the already-shipped `ToolErrorPrefix` or `CompactionNotice`
  markers to use `WrapInjected`. Both ship today with their own
  documented, accepted-risk rationale in `docs/plans/agentloop.md`'s
  earlier addenda. Migrating them is a separate decision, not a side
  effect of adding a second marker scheme.
- Applying `WrapInjected` to phase 81's `DuplicateCallNotice`, once
  phase 81 ships. Migrating an already-shipped marker to a new framing
  scheme is a separate decision for phase 81's own follow-up, not a
  side effect of this phase.
- Framing phase 79's `ConcludeNotice` or phase 80's
  `BatchTruncationNotice`. Both are caller-authored, through `Options`,
  not derived from untrusted tool or model text, so neither carries a
  forgery risk and neither needs this phase's framing.

## API

```go
// InjectionOpenTag and InjectionCloseTag delimit text WrapInjected
// frames as caller- or agentloop-injected guidance, distinct from
// real tool output or real user content in the model-visible
// transcript.
const InjectionOpenTag = "<<mivia:injected>>"
const InjectionCloseTag = "<<//mivia:injected>>"

// WrapInjected frames text as injected guidance from the named
// source, for example "example-source". WrapInjected strips any
// literal occurrence of InjectionOpenTag or InjectionCloseTag already
// inside label or text before framing, so neither can forge the
// closing tag and escape the frame.
func WrapInjected(label, text string) string
```

`InjectionOpenTag`, `InjectionCloseTag`, and `WrapInjected` land in
`api/agentloop.txt` via `make api-update`, in the same change as the
code.

## Tests

- `WrapInjected("steer", "stop and answer now")` returns text that
  starts with `InjectionOpenTag`, ends with `InjectionCloseTag`, and
  contains the label and the body between them.
- `WrapInjected` on text that already contains a literal
  `InjectionCloseTag` strips it before framing; the returned value
  still has exactly one open tag and one close tag, at the start and
  the end.
- The same stripping case for `InjectionOpenTag` inside the label
  argument.
- An empty `text` argument still returns a well-formed, open-tag/
  close-tag-delimited value.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change for the `agentloop`-only part. The
`hooks`-side injection channel, if pursued later, needs its own
`docs/plans/hooks.md` review and its own `policy/layers.json` check;
it is not part of this phase.
