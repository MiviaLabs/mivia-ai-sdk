# Plan: contextbudget

Status: shipped. A new leaf package with no internal imports. `agent`
imports it in the same change, as `Run`'s optional budget parameter.

## Goal

Give a caller a pure, storage-agnostic way to state and check a
budget for one model call's context: a byte cap, an event or message
count cap, and a check that says whether more content still fits.
`Limits` holds no data of its own beyond the limits; it does no I/O.

## Scope

Inside: the `Limits` type, its zero-means-uncapped semantics, a
`Validate` method that enforces the invariant, and a `Fits` method
that checks whether a given size and count still fit inside the
limits. Inside: one optional parameter on `agent.Run` that, when set,
calls `Validate` once and `Fits` before every gated step's `wait`
call.

Outside: summarization, trimming policy, and any decision about which
event or blob to drop. Outside: I/O, storage, and any coupling to a
specific memory store or model provider. The caller owns the trim or
summarize decision; `Limits` only tells the caller whether the
content still fits, and `agent.Run` only tells the caller the run
stopped because it did not.

Outside: any budget check for a panel member's message. The budget
check reaches only a gated, singleton step: the kind `confirmStep`
gates behind a `wait` call. `flow.Run`'s panel wave runs every panel
member concurrently, in a goroutine per member, with no `Confirm` or
`wait` call at all. `Run` checks `Fits` only inside `confirmStep`, so
a panel of two or more members' messages never add to `runningBytes`
and never trip `MaxEvents` or `MaxBytes`. This is the same gap phase
26 already discloses for the heartbeat beat, which also reaches only
`confirmStep`'s `wait` call and never a panel member's goroutine. This
is a known, disclosed scope limit, not a bug this phase fixes.

This is distinct from `memory.Store`. `memory.Store` holds
content-addressed blobs and evicts by insertion order once a byte
budget is spent; it does I/O and owns state. `Limits` holds no blobs
and does no eviction; it is an accounting check a caller runs before
it decides to call `memory.Store`, trim a message list, or summarize.
The two types may compose in a caller, but neither imports the other.

`Limits` lands in its own package, not inside `agent`. `AGENTS.md`'s
Building blocks rule requires leaf blocks first, composition last,
and states an agent imports blocks, a block never imports agent. A
future `provider` call site that needs `Limits.Fits` imports
`contextbudget` directly, the same way `agent` does; neither imports
the other to reach it.

## API

- `type Limits struct` holds `MaxBytes int` and `MaxEvents int`. Both
  fields are zero-value by default. A zero `MaxBytes` means no byte
  cap. A zero `MaxEvents` means no event-count cap. Both zero means
  the budget is uncapped.
- `(Limits) Validate() error` reports an error when `MaxBytes` is
  negative or when `MaxEvents` is negative. A zero or positive value
  in either field passes. `Validate` names the offending field in the
  error text: `"contextbudget: MaxBytes must not be negative"` when
  `MaxBytes` is negative, `"contextbudget: MaxEvents must not be
  negative"` when `MaxEvents` is negative and `MaxBytes` is not. When
  both are negative, `Validate` checks `MaxBytes` first and returns
  only the `MaxBytes` message; it does not join both.
- `(Limits) Fits(bytes, events int) bool` reports whether `bytes` and
  `events` both stay at or under their respective caps. A zero cap
  always reports fit for its own dimension. `Fits` takes the
  candidate totals the caller already has; it keeps no running total
  of its own, since `Limits` holds no state beyond the two caps.
  `Fits` does not call `Validate`; a caller that skips `Validate` and
  passes a negative cap gets whatever comparison `bytes <= cap`
  produces for that cap, which is caller error, not a `Fits`
  contract.

`Limits` needs no constructor. A caller builds it with a struct
literal; both fields default to the uncapped zero value.

`agent.Run` gains a trailing `budget *contextbudget.Limits`
parameter and a new sentinel, `ErrOverBudget`. See
`docs/plans/agent.md`'s "The budget parameter" section for the full
signature and behavior.

## Tests

`contextbudget/contextbudget_test.go` holds table-driven cases for
`Fits` (both caps zero including a large value of each; `MaxBytes`
set alone; `MaxEvents` set alone; both set; a negative `bytes` or
`events` argument) and for `Validate` (zero-value `Limits`; positive
`MaxBytes` and `MaxEvents`; negative `MaxBytes` alone; negative
`MaxEvents` alone; both fields negative, proving `MaxBytes` is
checked first).

No benchmark file. `Fits` and `Validate` are integer comparisons with
no allocation; a benchmark would measure noise, not signal.

`agent/agent_test/run_budget_test.go` and the
`TestBudgetPanelWaveReachesNoCheck` case in
`agent/agent_test/run_panel_integration_test.go` hold the `agent.Run`
integration cases; see `docs/plans/agent.md`'s "Context budget: tests"
section for the full case list.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `contextbudget` and for the
  total, with every new line counted in.
- `python3 scripts/check_deps.py` passes against the `"contextbudget":
  []` row and the `"contextbudget"` entry already present in `agent`'s
  row in `policy/layers.json`. This phase makes no edit to
  `policy/layers.json`.
- `api/contextbudget.txt` is created and `api/agent.txt` is updated
  via `make api-update`, in the same change as the code.
- `contextbudget/doc.go`'s file map gains `contextbudget.go`.
- `docs/architecture.md` gains a `contextbudget/` bullet next to the
  other leaf packages, and the `agent/` bullet gains the `budget`
  parameter and `ErrOverBudget`, in the same change as the code.
- This phase adds no conformance vector. `Limits` defines no wire
  schema; it is an in-process accounting check with no `Encode` or
  `Decode` counterpart, and `agent.Run`'s budget check changes no byte
  on the wire.
