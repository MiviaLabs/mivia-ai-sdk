# Phase 62: provider tool-call history

Status: plan, ready to build. Extends the shipped `provider` package
(see docs/plans/provider.md). Depends on no unshipped phase and adds
no new package, so `policy/layers.json` needs no new row; `provider`
keeps its existing empty import list.

## Goal

Let a caller represent a completed assistant turn, tool calls
included, as a `Message` it can append to `Request.Messages` for the
next turn. Today `Response` carries `ToolCalls`, but `Message` does
not, so a caller cannot replay a turn's tool calls back into history.

## Scope

Inside: one new field on `Message`, `ToolCalls []ToolCall`, the
matching rule in `Message.Validate`, and one field literal in
`runturn.go`'s unexported `buildResponse` that copies the merged
tool calls onto `Response.Message.ToolCalls`. Outside: any change to
`Request`, `Response`, `Chunk`, `RunTurn`'s aggregation algorithm
(the merge-by-index, ordering, and dedupe logic stays as shipped), or
any other exported symbol. This phase closes the one gap that blocks
history representation outright; it adds no request-tuning field.

**Why only `Message.ToolCalls`, not `Request` fields too.** A caller
that wants `Temperature`, `MaxTokens`, or `ToolChoice` on a `Request`
already has a workaround: a concrete `Completer` reads its own
provider-specific config out of band, or a caller layers those knobs
on `Request` in a later phase once a real caller states the need.
`Message.ToolCalls` has no such workaround. A caller with an
assistant turn that made tool calls has no legal way to place those
calls into the next `Request.Messages` entry today; the type does not
exist. That is a structural gap, not a convenience gap, so this phase
fixes it alone. AGENTS.md rejects speculative generality; the other
fields stay out until a concrete caller blocks on them.

**The new field's type matches `Response.ToolCalls`, and `RunTurn`
populates both.** `Message.ToolCalls` is `[]ToolCall`, the same type
`Response.ToolCalls` already uses. `buildResponse` in `runturn.go`
copies the same merged slice onto both fields, so `Response.Message`
is a complete, directly-appendable history entry on its own:

```go
history = append(history, resp.Message)
```

This is the idiomatic move for a streamed turn, the same pattern
`Response.Message.Content` already invites. A caller who instead
builds a `Message` field by field, copying `Content` and `ToolCalls`
across explicitly, still works, since both fields hold the same
data:

```go
history = append(history, provider.Message{
    Role:      provider.RoleAssistant,
    Content:   resp.Message.Content,
    ToolCalls: resp.ToolCalls,
})
```

`Response.ToolCalls` stays exactly as shipped, for a caller that does
not use `Message.ToolCalls`.

**The new validation rule.** `ToolCalls` may be non-empty only on a
`RoleAssistant` message. A non-empty `ToolCalls` on any other known
role returns a new sentinel, `ErrToolCallsUnexpected`. `Validate`
checks `Role` legality first, as it does today; `ErrUnknownRole` still
wins over any field-pairing check.

**Why this does not collide with the existing `ToolCallID` rule.**
`ToolCallID` and `ToolCalls` name two different relationships and
never apply to the same role. `ToolCallID` names the one call a
`RoleTool` reply answers; `Validate` already requires it there and
forbids it everywhere else, `RoleAssistant` included. `ToolCalls`
names the calls a `RoleAssistant` turn made; this phase requires it
stay empty everywhere except `RoleAssistant`, `RoleTool` included. A
`RoleAssistant` message therefore carries `ToolCalls` and never
`ToolCallID`; a `RoleTool` message carries `ToolCallID` and never
`ToolCalls`. The two fields partition cleanly across roles, so no
message can trigger both rules at once, and the existing
`RoleAssistant`/`ToolCallID` check keeps its current meaning
unchanged.

**Additive, not breaking.** Adding an unexported-by-default zero-value
field to an existing struct never breaks an existing caller in Go: a
caller who does not set `ToolCalls` gets a nil slice, and a nil slice
is empty, so the new `Validate` check never rejects a message an old
caller already built. `RunTurn`'s call to `Message.Validate` on every
`Request.Messages` entry needs no change: the same call now also
checks the new field, and every existing valid message still passes,
since none of them set `ToolCalls`.

**`RunTurn` gains one field literal, not a code change to its
aggregation logic.** `runturn.go`'s `buildResponse` sets
`Response.Message` to `Message{Role: RoleAssistant, Content:
content.String()}` and separately sets `Response.ToolCalls` from the
merged tool-call map. Without a matching change, the idiomatic
`history = append(history, resp.Message)` move compiles, passes
`Validate` (a nil `ToolCalls` is legal on `RoleAssistant`), and
silently drops the turn's tool calls from history on every streamed
turn. This phase closes that gap: `buildResponse` also assigns the
same merged `toolCalls` slice to `Response.Message.ToolCalls`, one
field literal addition alongside the existing `Response.ToolCalls:
toolCalls` line. The merge, ordering, and dedupe algorithm that
produces `toolCalls` does not change; only its assignment target
gains a second field. `Response.ToolCalls` keeps its current value
and its current callers unaffected, since both fields end up holding
the same data.

## API

The surface below extends `api/provider.txt`; land it with
`make api-update` in the same change as the code. Every other
existing symbol keeps its current shape.

- `type Message struct { Role Role; Content string; ToolCallID string; ToolCalls []ToolCall }`
  adds `ToolCalls`. `ToolCalls` is non-empty only on a `RoleAssistant`
  message; it holds the calls that assistant turn made, in the same
  `[]ToolCall` shape `Response.ToolCalls` already returns.
- `func (m Message) Validate() error` gains one more check: on a
  known `Role` other than `RoleAssistant`, a non-empty `ToolCalls`
  returns `ErrToolCallsUnexpected`. The `Role`-legality check and the
  existing `ToolCallID` check both run first and keep their current
  behavior and error precedence.
- `ErrToolCallsUnexpected` is the new sentinel, checked with
  `errors.Is`, alongside `ErrToolCallIDUnexpected`,
  `ErrToolCallIDRequired`, and `ErrUnknownRole`.

No other symbol in `api/provider.txt` changes shape. `RunTurn`,
`Response`, `Chunk`, `Completer`, `Request`, and every other type and
constant stay exactly as shipped: `RunTurn`'s signature, and
`Response`'s and `Chunk`'s field lists, are untouched. `RunTurn`'s
behavior gains the invariant stated above: `Response.Message.ToolCalls`
carries the same merged calls as `Response.ToolCalls` after every
call, streamed or not. `api/provider.txt` locks type shapes, not this
behavior, so this invariant is enforced by the `completer_test.go` and
`completer_integration_test.go` cases below, not by the API lock.

## Tests

New and widened cases live in `provider/provider_test/`, following
the existing file layout.

- `types_test.go` gains `Message.Validate` cases: a `RoleAssistant`
  message with a non-empty `ToolCalls` and an empty `ToolCallID`
  validates; a `RoleAssistant` message with both a non-empty
  `ToolCalls` and a non-empty `ToolCallID` is rejected with
  `ErrToolCallIDUnexpected`, since the existing `ToolCallID` check
  runs first and independently of `ToolCalls`; a `RoleTool` message
  with a non-empty `ToolCalls` (and a correctly paired `ToolCallID`)
  is rejected with `ErrToolCallsUnexpected`; a `RoleSystem` or
  `RoleUser` message with a non-empty `ToolCalls` is rejected with
  `ErrToolCallsUnexpected`; an unknown `Role` with a non-empty
  `ToolCalls` still returns `ErrUnknownRole`, proving role legality
  wins first. No separate case covers a `RoleSystem` or `RoleUser`
  message with both a non-empty `ToolCallID` and a non-empty
  `ToolCalls` set together: `Validate`'s `switch` groups
  `RoleSystem`, `RoleUser`, and `RoleAssistant` under one case, and
  the `ToolCallID != ""` check inside that case returns
  `ErrToolCallIDUnexpected` before the new `ToolCalls` check runs,
  for all three roles alike. The `RoleAssistant`-with-both-fields case
  above already exercises that exact branch and that exact
  precedence; a `RoleSystem` or `RoleUser` variant would run the same
  code path to the same outcome and add no new branch coverage, so
  this phase does not add it.
- `completer_test.go` gains one case: build a `Response` with a
  non-empty `ToolCalls` from a fake `Completer`, assert
  `resp.Message.ToolCalls` equals `resp.ToolCalls` (proving
  `buildResponse`'s new field literal keeps both in sync), and assert
  `Message.Validate` accepts `resp.Message` unchanged, proving
  `history = append(history, resp.Message)` is a legal, complete
  history entry with no field-by-field copy.
- `completer_integration_test.go` gains a multi-turn case: a
  `RoleUser` message, `resp.Message` appended directly from a first
  `RunTurn` call's `Response` (carrying `ToolCalls` through the
  invariant above), one `RoleTool` reply `Message` per call with the
  matching `ToolCallID`, and a second `RunTurn` call over the full
  `Request.Messages` slice that returns a final text `Response`. The
  test asserts `RunTurn` accepts the full history with no error,
  proving the type change composes through the real dispatch path
  using the idiomatic append.
- `validate_fuzz_test.go`'s `FuzzMessageValidate` widens to fuzz the
  new field: the fuzz function gains a `toolCallsLen int` parameter,
  builds `Message{Role: Role(role), ToolCallID: toolCallID, ToolCalls:
  make([]ToolCall, toolCallsLen)}` (a zero or negative `toolCallsLen`
  yields a nil or empty slice; `make` panics on a negative length, so
  the fuzz body clamps `toolCallsLen` to zero when negative before
  calling `make`), and extends the assertions: for a known role other
  than `RoleAssistant` with `toolCallsLen > 0` and no `ToolCallID`
  conflict, `Validate` must return `errors.Is(err,
  ErrToolCallsUnexpected)`; for `RoleAssistant` with `toolCallsLen > 0`
  and an empty `ToolCallID`, `Validate` must return `nil`. Existing
  seed corpus entries gain a `0` `toolCallsLen` to keep today's
  coverage; new seeds add a non-zero `toolCallsLen` per role.
- No new benchmark. `completer_bench_test.go`'s existing benchmark
  already exercises `RunTurn` and `Message.Validate`, and one more
  struct field, one more `switch` case, and one more field literal in
  `buildResponse` add no measurable cost; a new benchmark would not
  change the recorded baseline.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `provider` and for the total.
- `api/provider.txt` gains `Message.ToolCalls` and
  `ErrToolCallsUnexpected` via `make api-update`, in the same change
  as the code. Every other locked symbol keeps its current text.
- `policy/layers.json` needs no new row and no edge change: `provider`
  stays a leaf package with an empty import list.
- `docs/plans/provider.md`'s API section gains the widened `Message`
  shape, the new `Validate` rule, and `ErrToolCallsUnexpected`, in the
  same change as the code, matching the convention phase 44 already
  set for `TokenEstimator`.
- `docs/packages/provider.md` gains the widened `Message` field list
  and the new sentinel, matching the docs-maintenance convention.
- `docs/architecture.md`'s `provider/` bullet gains `Message.ToolCalls`
  in its symbol list and `ErrToolCallsUnexpected` alongside the other
  named sentinels, in the same change as the code, matching the
  convention phase 44 set when it added `TokenEstimator` to the same
  bullet.
- This phase adds no conformance vector. `provider` carries
  in-process values only; it defines no wire format.
