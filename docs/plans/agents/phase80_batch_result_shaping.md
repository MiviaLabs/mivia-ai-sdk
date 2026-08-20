# Phase 80: per-batch tool-result size shaping

Status: plan, not scheduled.

## Why this plan exists

A gap analysis compared `agentloop` against `internal/agent.Loop`, a
production caller in a separate, external repository (`mivia-agent`).
It found a capability that repo's caller needs and `agentloop` lacks:
a cap on one turn's combined tool-result size, not only one call's own
result. This phase closes that gap. It has no code, no plan review,
and no `policy/layers.json` row yet. It needs a plan review before a
builder starts it.

## Goal

Cap one turn's tool results as a set, by summed byte size, instead of
capping only one call's result at a time. `tools.ResultBudgetOf`
already caps one call's own rendered content; nothing today caps the
combined size of several results returned in the same turn.

## Scope

Inside:

- One new `Options` field, `TurnResultBudget`.
- Shaping every call's already-rendered content, in `ToolCall.Index`
  order, against the running total for the turn, after each call's own
  `tools.ResultBudgetOf` bound already applied.
- One new exported constant marking a batch-truncated result,
  distinct from the existing `truncationMarker` a per-call truncation
  uses.

Outside:

- Any change to `tools.ResultBudgetOf`, `tools.ResultBudgetTool`, or
  any other `tools` symbol. This phase shapes already-rendered
  `string` content inside `agentloop`; it adds no new `tools` API.
- Skipping or refusing to run a call because the turn's budget is
  already exhausted. Every call in the turn still runs, and every
  call's hooks still fire, unchanged; only the rendered content that
  reaches history is shaped.
- Redistributing budget by call importance or size. The policy is
  first-come: earlier calls, in `Index` order, keep their content
  whole while budget remains; once the running total exhausts the
  budget, each later call's content is replaced with a fixed notice.

## API

```go
// Options gains:

// TurnResultBudget caps the summed byte size of one turn's rendered
// tool results, across every call in that turn, before they append to
// history. Zero means uncapped. Distinct from a Tool's own
// tools.ResultBudgetOf bound, which caps one call's content alone;
// TurnResultBudget shapes the batch as a set, after each call's own
// bound already applied, in ToolCall.Index order.
TurnResultBudget int
```

```go
// BatchTruncationNotice replaces a tool result's content when
// TurnResultBudget is exhausted before that call's turn in Index
// order. Distinct from ToolErrorPrefix: a batch-shaped result is not
// a tool-run error, and distinct from the per-call truncation marker,
// which trims content in place instead of replacing it outright.
const BatchTruncationNotice = "[batch-truncated] Turn tool-result budget exhausted; this result was omitted."
```

`Options.Validate` gains one rule: a negative `TurnResultBudget` fails
validation with a new sentinel, `ErrTurnResultBudget`.

## Tests

- `Options.Validate`: a negative `TurnResultBudget` fails with
  `errors.Is(err, ErrTurnResultBudget)`. Zero and positive values
  pass.
- A turn with two tool calls, a `TurnResultBudget` sized to fit the
  first call's content but not both, keeps the first call's content
  whole and replaces the second call's content with
  `BatchTruncationNotice`.
- The same setup with `TurnResultBudget` zero appends both calls'
  content whole, unchanged from the base plan.
- A turn where the first call's own `tools.ResultBudgetOf` bound
  already truncates it proves the two truncation layers apply in the
  documented order: the per-call bound first, the batch bound second.
- `Options.Audit`'s `AuditKindToolCall` record for a batch-truncated
  call carries the shaped `BatchTruncationNotice` content in
  `ToolResult`, and a nil `Err`, since a batch truncation is not a
  tool-run error.
- A `PointPreTool` veto partway through a turn stops later calls from
  running, unchanged from the base plan; the shaping pass only
  considers the calls that ran before the veto.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change.
