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
  `tools.ResultBudgetOf` bound already applied. Shaping applies to
  every RoleTool message `runOneToolCall` produces, whether it carries
  a successful result or a reported tool-run error (the
  `ToolErrorPrefix`-marked content `ErrorPolicyReport` builds). The
  running total is a byte count over `provider.Message.Content`; it
  does not distinguish a successful result from a reported error.
- The running total is a local accumulator inside `runToolCalls`'s
  existing sequential per-call loop. `runToolCalls` applies it to
  `msg.Content` after `runOneToolCall` returns and before both the
  history append and the `l.audit` call. It resets to zero once per
  `runToolCalls` invocation, at the start of that turn's batch, not
  once per `Run` call.
- The exact threshold rule (hard cap, never overshoots), and the
  zero-budget exception: the comparison, and any shaping, applies only
  when `TurnResultBudget` is positive; zero skips the check entirely
  and every call's content passes through whole. When
  `TurnResultBudget` is positive, before appending call `i`'s content,
  compare `runningTotal + len(content)` against `TurnResultBudget`. If
  the sum is less than or equal to `TurnResultBudget`, keep the
  content whole and add `len(content)` to `runningTotal`. If the sum
  would exceed `TurnResultBudget`, replace the content with
  `BatchTruncationNotice` instead, and leave `runningTotal`
  unchanged. A later call is judged against the same, unchanged
  `runningTotal`, so once one call is replaced, every following call
  in the turn is also replaced, unless `runningTotal` still has room
  for a later, smaller call's content once compared for it — the
  comparison runs fresh, per call, against the current
  `runningTotal`.
- Switching `runToolCalls`'s sort from `sort.Slice` to
  `sort.SliceStable`. `TurnResultBudget` makes tie-order among calls
  that share one `Index` value decide which call's content survives
  whole; a stable sort keeps that tie-order reproducible across runs
  of the same input slice, at negligible cost over the existing sort
  at typical per-turn call counts.
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
  whole while the running total plus that call's content still fits
  the budget; once a call's content would push the running total over
  budget, that call's content is replaced with a fixed notice, and
  the running total does not grow for it.
- Changing `AuditRecord.Err`. A batch-truncated call's `Err` still
  reflects the true per-call outcome `runOneToolCall` reported,
  independent of whether `TurnResultBudget` shaped its `Content`. Only
  `ToolResult.Content` changes when a call is batch-truncated.

## API

```go
// Options gains:

// TurnResultBudget caps the summed byte size of one turn's rendered
// tool results, across every call in that turn, before they append to
// history. Zero means uncapped: the budget comparison and any shaping
// are skipped entirely, and every call's content passes through
// whole. Distinct from a Tool's own tools.ResultBudgetOf bound, which
// caps one call's content alone; TurnResultBudget shapes the batch as
// a set, after each call's own bound already applied, in
// ToolCall.Index order. Hard cap when positive: a call's content
// stays whole only when the running total plus that content's byte
// length does not exceed TurnResultBudget; otherwise the content is
// replaced with BatchTruncationNotice and the running total does not
// grow for it. The running total never exceeds TurnResultBudget.
// Applies to every appended RoleTool content, including a reported
// tool-run error under ErrorPolicyReport.
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
- A three-call turn pins the hard-cap threshold rule against the
  once-under-budget-before-the-call reading it rejects. `TurnResultBudget`
  is 10. Call 1's rendered content is 6 bytes: the running total
  starts at 0, 0+6 <= 10, so call 1 keeps its content whole and the
  running total becomes 6. Call 2's rendered content is 5 bytes: the
  running total before call 2 is 6, which is still under 10, but 6+5
  is 11, over 10, so call 2's content is replaced with
  `BatchTruncationNotice` and the running total stays 6. This is the
  straddle case: a before-the-call check would have kept call 2 whole,
  the chosen after-the-call check does not. Call 3's rendered content
  is 5 bytes: 6+5 is again 11, over 10, so call 3's content is also
  replaced, regardless of which rule call 2 had followed, pinning that
  the running total, once a call is rejected, does not grow and later
  calls keep failing the same comparison. A fourth call in the same
  turn has 4 bytes of rendered content: the running total is still 6,
  so 6+4 equals `TurnResultBudget` exactly. The rule keeps content
  whole at `<=`, so call 4's content stays whole and unreplaced, and
  the running total becomes 10, pinning the exact-equality boundary
  against an off-by-one `<` that would wrongly replace it.
- A turn with `OnToolError: ErrorPolicyReport` where one call's
  decoded arguments fail schema validation: the reported error's
  `ToolErrorPrefix`-marked content counts toward the running total
  exactly like a successful result's content, and a `TurnResultBudget`
  too small for it plus an earlier call's content replaces the error
  report's content with `BatchTruncationNotice`.
- `Options.Audit`'s `AuditKindToolCall` record for a batch-truncated,
  otherwise-successful call carries the shaped `BatchTruncationNotice`
  content in `ToolResult`, and a nil `Err`, matching a normal
  successful call.
- A distinct `Options.Audit` case: a call that is both a reported
  tool-run error and batch-truncated. `ToolResult.Content` carries
  `BatchTruncationNotice`, but `Err` stays the non-nil error
  `runOneToolCall` reported, unchanged by the batch shaping.
- A `PointPreTool` veto partway through a turn stops later calls from
  running, unchanged from the base plan; the shaping pass only
  considers the calls that ran before the veto.
- Two `ToolCall` values that share one `Index` prove `runToolCalls`'s
  `sort.SliceStable` switch keeps their relative order from the input
  `calls` slice, so the first one in slice order is the one judged
  against the smaller running total.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change.
