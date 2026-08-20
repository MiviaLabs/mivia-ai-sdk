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
  error)`, builds the canonical form: decode `raw` with a
  `json.Decoder` configured with `UseNumber()`, into an `any`, then
  `json.Marshal` the result. `UseNumber()` decodes each JSON number
  token into a `json.Number`, a string type that keeps the source
  digits verbatim, instead of collapsing every number into a Go
  `float64`. Plain `float64` decoding loses precision above 2^53 and
  would silently canonicalize two distinct large integers (IDs,
  nanosecond timestamps) to the same value; `UseNumber()` avoids that
  collapse. `encoding/json` sorts object keys on marshal, so this
  normalizes key order without a repo-specific canonicalization
  utility; grep confirms this repo has none today.
- Trailing-data check after decode. `json.Decoder.Decode` consumes
  only one JSON value and does not check for bytes left over, unlike
  `json.Unmarshal`. `canonicalizeArgs` calls `dec.More()` right after
  `dec.Decode(&v)` succeeds. `dec.More()` true means bytes remain
  after the first value, for example trailing garbage
  (`{"a":1}garbage`) or a second concatenated JSON value
  (`{"a":1}{"b":2}`). `canonicalizeArgs` treats a true `dec.More()` as
  a canonicalization error and returns it, so the call falls under the
  fail-open contract below instead of canonicalizing from a partial,
  misleading prefix.
- Fail-open error contract for `canonicalizeArgs`. `call.Arguments` is
  raw wire bytes assembled from streaming deltas and can be malformed
  JSON before schema validation runs. When `canonicalizeArgs` returns
  an error, the call is excluded from the dedup set: it is never
  treated as a duplicate, and it always runs. A canonicalization
  error never blocks a call and never fails the turn.
- Hook-firing contract for a deduped call. The dedup check runs before
  `PointPreTool` and short-circuits: a call identified as a duplicate
  never reaches `PointPreTool` or `PointPostTool`. Those hook points
  fire once per turn for a given `(tool, canonical-argument)` pair, on
  the call that actually runs.
- Dedup-set seeding for an errored call. `runOneToolCall` can append a
  `RoleTool` error message via `ErrorPolicyReport` without the
  underlying tool ever running. Any `RoleTool` message reaching
  history for a call, success or error, seeds the dedup set for that
  call's `(tool, canonical-argument)` pair. A later byte-identical
  retry in the same turn is deduped either way, since the goal is to
  avoid re-triggering a call already resolved one way or another.
- One new exported constant, `DuplicateCallNotice`, served as the
  `RoleTool` content for a detected duplicate.

Outside:

- Any interaction with a `TurnResultBudget` type or similar
  turn-level result-size shaping. No such type exists in `agentloop`
  today. This plan's dedup check produces one `RoleTool` message per
  call, deduped or not, like any other tool result; it defines no
  contract with a result-shaping pass. A later phase that adds
  result-size shaping states its own ordering against dedup when it
  is scheduled and buildable.
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

`canonicalizeArgs` stays unexported: `canonicalizeArgs(raw
json.RawMessage) (string, error)`. It is not part of the locked
surface. Its contract, enforced by the caller in `agentloop`:

- On success, it returns a canonical string form of `raw`: numbers
  keep their source digits via `json.Number`, object keys sort by
  `encoding/json`'s marshal order. Success requires `raw` to decode as
  exactly one JSON value with no bytes left over; `canonicalizeArgs`
  checks this with `dec.More()` after `dec.Decode`, since
  `json.Decoder.Decode` alone accepts a valid JSON prefix and ignores
  trailing bytes.
- On error (malformed `Arguments`, or a valid JSON prefix followed by
  trailing bytes), the caller treats the call as
  never dedup-eligible: it runs unconditionally and is never recorded
  in, or matched against, the dedup set. This is a fail-open contract;
  a canonicalization error never blocks a call and never fails the
  turn.
- The dedup check, including a call to `canonicalizeArgs`, runs before
  `PointPreTool`. A call identified as a duplicate short-circuits
  there: it never reaches `PointPreTool` or `PointPostTool`, and the
  underlying tool never runs for it.

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
    other: `UseNumber()` keeps each source digit string as a distinct
    `json.Number` ("1" versus "1.0"), so `canonicalizeArgs` treats
    them as different and neither call is deduped against the other.
  - Two large distinct integers that collide under naive `float64`
    decoding, `9007199254740992` (2^53) and `9007199254740993`: both
    round to the same `float64` value, `9.007199254740992e+15`, under
    plain `json.Unmarshal` into `any`. With `UseNumber()`,
    `canonicalizeArgs` keeps their exact digit strings distinct, so
    the two calls compare unequal and both run; neither is deduped.
    This proves the fix in Scope closes the numeric-precision
    false-positive that the old `float64` decoding would have caused.
  - Raw JSON with a duplicate key, for example `{"a":1,"a":2}`:
    `encoding/json` keeps the last value on decode, so
    `canonicalizeArgs` sees only `{"a":2}`. A caller that sends
    ambiguous duplicate-key JSON gets that encoding/json behavior, not
    a dedup guarantee; this phase does not detect or reject the
    duplicate key itself.
  - Two calls whose string values differ only by Unicode-escape form,
    for example `"café"` versus `"caf\u00e9"`: `encoding/json`
    decodes both into the same Go string, so `canonicalizeArgs` treats
    them as equal and the second call is deduped.
  - Malformed `Arguments`, for example truncated or non-JSON bytes:
    `canonicalizeArgs` returns an error. The call still runs, is
    never treated as a duplicate, is never recorded in the dedup set,
    and the turn does not fail because of the canonicalization error.
    No panic.
  - `Arguments` holding a valid JSON value followed by trailing bytes,
    for example `{"a":1}garbage` or two concatenated JSON values
    `{"a":1}{"b":2}`: `dec.Decode` alone would accept the leading
    `{"a":1}` and silently ignore the rest. `canonicalizeArgs` detects
    the leftover bytes with `dec.More()` and returns an error instead.
    A second call carrying the same leading fragment but different
    trailing bytes is never falsely deduped against the first; both
    calls always run.
- The synthesized `RoleTool` message for a deduped call carries the
  duplicate call's own `ToolCallID`, not the first call's `ToolCallID`.
  This is the single most likely implementation bug for this phase: a
  naive dedup that copies the first call's message wholesale would
  send back the wrong `ToolCallID` and break the model's tool-call/
  tool-result pairing.
- `PointPreTool` and `PointPostTool` invocation counts for a turn with
  two identical calls: both hook points fire exactly once for the
  pair, on the first call only. The second, deduped call never
  triggers either hook point, since the dedup check short-circuits
  before them.
- A `PointPreTool` veto on the first of two identical calls stops the
  turn before the second call is ever reached; the second call is
  never evaluated for dedup, since the veto ends the turn first.
- The dedup set resets between iterations: a call repeated on a later
  iteration runs again, proving no cross-turn dedup happens.
- `Options.Audit`'s `AuditKindToolCall` record for a deduped call
  carries a nil `Err`, since a served duplicate is not a tool-run
  error.
- A first call that resolves as an `ErrorPolicyReport`-appended
  `RoleTool` error message, without the underlying tool running,
  still seeds the dedup set: a byte-identical retry of that same call
  later in the same turn is deduped and serves `DuplicateCallNotice`,
  proving an errored call counts as "served" for dedup purposes.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change.
