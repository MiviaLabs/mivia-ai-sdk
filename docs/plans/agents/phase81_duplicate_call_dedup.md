# Phase 81: duplicate-call dedup within a turn

Status: plan, not scheduled.

## Why this plan exists

A gap analysis compared `agentloop` against `internal/agent.Loop`, a
production caller in a separate, external repository (`mivia-agent`).
It found a capability that repo's caller needs and `agentloop` lacks:
detection of a duplicate tool call within one turn, before it runs
twice. This phase closes that gap. It has no code, no plan review, and
no `policy/layers.json` row yet. It needs a plan review before a
builder starts it.

## Goal

Detect an identical `(tool name, arguments)` call already served
earlier in the same turn, and serve a fixed notice instead of running
the tool a second time. Avoid a duplicate side effect from a model
that requests the same call twice in one turn's response.

## Scope

Inside:

- One new `Options` field, `DedupWithinTurn`.
- Comparing calls by tool name and a canonical form of `Arguments`,
  scoped to one turn: the set resets every iteration. One new
  unexported helper, `canonicalizeArgs(raw json.RawMessage) (string,
  error)`, builds the canonical form: `json.Unmarshal` `raw` into an
  `any`, then `json.Marshal` the result. `encoding/json` sorts object
  keys on marshal, so this normalizes key order without a
  repo-specific canonicalization utility; grep confirms this repo has
  none today.
- One new exported constant, `DuplicateCallNotice`, served as the
  `RoleTool` content for a detected duplicate.
- Pipeline order against phase 80's `TurnResultBudget`, when both are
  enabled: dedup runs first, per call. A deduped call's
  `DuplicateCallNotice` then counts toward `TurnResultBudget`'s
  running total for the turn, like any other rendered content.

Outside:

- Cross-turn dedup: a call repeated on a later iteration is not
  detected. `agentloop` holds no cross-iteration call history for this
  purpose, and adding one is a larger design question about how long a
  served call stays "recent" across a long-running loop.
- True in-flight, concurrent dedup. Today's tool calls run
  sequentially, one at a time; "already in flight" and "already
  served" are the same condition. A future concurrent-tool-call design
  needs its own dedup review.
- Reusing the first call's actual result for the duplicate. The
  duplicate always gets `DuplicateCallNotice`, a fixed notice, not the
  original result replayed. Replaying a stale result risks the model
  treating it as fresh, which is its own correctness problem outside
  this phase's scope.

## API

```go
// Options gains:

// DedupWithinTurn detects a duplicate (tool, canonical-argument) call
// already served earlier in the same turn, and serves
// DuplicateCallNotice instead of running the tool again. False, the
// zero value, runs every call, unchanged from the base plan.
DedupWithinTurn bool
```

```go
// DuplicateCallNotice replaces a tool result's content when
// DedupWithinTurn detects the same (tool, canonical-argument) call
// already served earlier in the same turn.
const DuplicateCallNotice = "[duplicate-call] This exact tool call was already served earlier in this turn; skipped to avoid a repeated side effect."
```

## Tests

- `DedupWithinTurn` false runs two identical calls in one turn twice,
  unchanged from the base plan.
- `DedupWithinTurn` true, two identical calls (same name, byte-equal
  `Arguments`) in one turn, runs the first and serves
  `DuplicateCallNotice` for the second, without a second `RunScoped`
  call reaching the underlying tool.
- Two calls with the same tool name but semantically identical
  `Arguments` in a different key order both compare equal under
  canonicalization, and the second is deduped.
- Two calls with the same tool name and genuinely different
  `Arguments` both run; neither is treated as a duplicate.
- `canonicalizeArgs` adversarial cases, each stating the expected
  behavior:
  - A numeric literal written as `1` in one call and `1.0` in the
    other: `json.Unmarshal` decodes both into the same `float64`
    value, so `canonicalizeArgs` treats them as equal and the second
    call is deduped.
  - Raw JSON with a duplicate key, for example `{"a":1,"a":2}`:
    `encoding/json` keeps the last value on `Unmarshal`, so
    `canonicalizeArgs` sees only `{"a":2}`. A caller that sends
    ambiguous duplicate-key JSON gets that encoding/json behavior, not
    a dedup guarantee; this phase does not detect or reject the
    duplicate key itself.
  - Two calls whose string values differ only by Unicode-escape form,
    for example `"café"` versus `"café"`: `json.Unmarshal`
    decodes both into the same Go string, so `canonicalizeArgs` treats
    them as equal and the second call is deduped.
- The synthesized `RoleTool` message for a deduped call carries the
  duplicate call's own `ToolCallID`, not the first call's `ToolCallID`.
  This is the single most likely implementation bug for this phase: a
  naive dedup that copies the first call's message wholesale would
  send back the wrong `ToolCallID` and break the model's tool-call/
  tool-result pairing.
- A `PointPreTool` veto on the first of two identical calls stops the
  turn before the second call is ever reached; the second call is
  never evaluated for dedup, since the veto ends the turn first.
- The dedup set resets between iterations: a call repeated on a later
  iteration runs again, proving no cross-turn dedup happens.
- `Options.Audit`'s `AuditKindToolCall` record for a deduped call
  carries a nil `Err`, since a served duplicate is not a tool-run
  error.
- `DedupWithinTurn` and phase 80's `TurnResultBudget` both set: a turn
  with a duplicate call proves the documented order. The dedup check
  runs first and serves `DuplicateCallNotice` for the duplicate; the
  budget shaping pass then counts `DuplicateCallNotice`'s bytes toward
  the turn's running total, and can still batch-truncate a later,
  non-duplicate call's content once the total is exhausted.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change.
