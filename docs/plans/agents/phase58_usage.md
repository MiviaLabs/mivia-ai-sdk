# Phase 58: usage tracking

Status: plan only. New top-level package `usage`. Depends on the
shipped `provider` package for its `Usage` type; `usage` defines no
token-count type of its own. Reusing `provider.Usage` keeps one
definition of `PromptTokens`, `CompletionTokens`, `TotalTokens`, and
`CachedTokens` across the SDK, matching the rule that a copied type
forks on the next change.

## Goal

Give a caller a per-session running total of `provider.Usage`. A
caller calls `Record` once per completed model call and reads the
accumulated total for that session at any time. `usage` adds no gate
and no policy; it only counts.

## Scope

Inside: an `Accumulator` that records one `provider.Usage` value per
call, keyed by a caller-supplied session identifier, and sums every
field across all calls recorded under that key. Inside: reading the
current total for a session, and resetting a session's total back to
zero. Inside: safe concurrent `Record` calls against the same session
from more than one goroutine.

Outside: cost-in-currency conversion. A token-to-dollar price changes
per provider and per date; a hardcoded table would go stale on
release. `usage` ships no `CostFunc` and no price table in this
phase. A caller who needs a dollar figure multiplies the `Total`
fields by its own current price outside this package. A future phase
may add a caller-supplied conversion function once a real consumer
names the exact shape it needs; no consumer exists yet, so adding one
now is speculative generality this plan rejects.

Outside: budget or limit enforcement. `contextbudget.Limits` already
owns the "does this still fit" gate, checked inside `agent.Run`
before a step runs. `usage` never compares a total against a cap and
never returns an error or a stop signal for a total that grows large.
`usage` is purely additive accounting a caller reads after the fact;
`contextbudget` is a gate a caller checks before an action. The two
compose in a caller: a caller may read `Accumulator.Total` and pass
its `TotalTokens` into a `contextbudget.Limits.Fits` call itself, but
`usage` imports neither `contextbudget` nor `agent`, and neither
imports `usage` in this phase.

Outside: persistence across process restarts. `Accumulator` holds its
totals in memory only, the same scope-out `memory.Store` already
states for its own blobs. A caller who needs a session's total to
survive a restart snapshots `Total` itself and restores it through
`Record` calls, or layers its own store on top; `usage` ships no
`Encode`, `Decode`, or file format.

Outside: the `ledger` package's vocabulary. `ledger` already owns
`Admit`, `Claim`, `Lease`, `Fence`, and `TaskState` for durable task
admission; `usage` names nothing that overlaps that set. `Accumulator`
is a new noun, not a `Ledger`-shaped one: `usage` tracks a running
sum per session, not a task's lifecycle, ownership, or dependency
graph.

`usage` imports `provider` for the `Usage` type only. No other
internal import. No third-party import. This is the first package
after `provider` itself to import `provider`; the `policy/layers.json`
row below is `"usage": ["provider"]`.

## API

The surface below is the lock target. It lands in `api/usage.txt` via
`make api-update`.

- `type Accumulator struct { ... }` holds one running `provider.Usage`
  total per session identifier, guarded for concurrent access. Its
  fields stay unexported; a caller reaches the state only through the
  methods below.
- `func New() *Accumulator` returns an empty `Accumulator` ready to
  record. No options; the zero-configuration constructor matches
  `contextbudget.Limits`'s no-constructor pattern in spirit, adjusted
  for `Accumulator`'s need to guard shared state with a mutex a
  struct literal cannot initialize safely for a caller.
- `func (a *Accumulator) Record(sessionID string, u provider.Usage) error`
  adds `u`'s four fields onto the running total keyed by `sessionID`.
  Returns `ErrBlankSessionID`, wrapped, when `sessionID` is empty
  after `strings.TrimSpace`. Creates the session's total on its first
  `Record` call; every later call for the same `sessionID` adds onto
  the existing total. Safe to call from more than one goroutine for
  the same or different `sessionID` values.
- `func (a *Accumulator) Total(sessionID string) (provider.Usage, bool)`
  returns the current summed `provider.Usage` for `sessionID` and
  `true`, or the zero `provider.Usage` and `false` when no `Record`
  call has ever named that `sessionID`. The bool is a plain
  map-lookup found signal (`v, ok := map[key]`), matching the
  found-bool half of `ledger.State`'s shape, minus the error return
  since `Total` has no I/O path.
- `func (a *Accumulator) Reset(sessionID string) error` clears the
  session's total back to zero, as if no `Record` call had ever
  named it. Returns `ErrBlankSessionID`, wrapped, when `sessionID` is
  empty after `strings.TrimSpace`. `Reset` on a `sessionID` with no
  prior `Record` call is a no-op that returns `nil`, not an error;
  the caller already gets the zero total from `Total`'s `false` case
  for that key.
- `ErrBlankSessionID` is the sentinel `Record` and `Reset` return when
  `sessionID` is empty after `strings.TrimSpace`, checked with
  `errors.Is`. The name and the TrimSpace-empty definition match the
  existing blank-identifier sentinels in this codebase:
  `tools.ErrBlankName`, `trigger.ErrBlankName`,
  `providerregistry.ErrBlankName`, and `scheduler.ErrBlankID`.

No `CostFunc` type and no `Sessions` or `List` method in this phase.
`Record`, `Total`, and `Reset` are the full method set; a caller that
needs every open session's identifiers layers its own tracking, since
no consumer of this package needs that list yet.

## Tests

Test files live in `usage/usage_test/`:

- `accumulator_test.go` — table-driven `Record` cases: a first
  `Record` call for a new session sets its total to that one call's
  `provider.Usage`; a second `Record` call for the same session adds
  onto the first, checked per field
  (`PromptTokens`, `CompletionTokens`, `TotalTokens`, `CachedTokens`);
  three or more `Record` calls for the same session sum correctly in
  order; `Record` with a `sessionID` that is empty or whitespace-only
  after `strings.TrimSpace` (both the `""` case and a `" "` case)
  returns `ErrBlankSessionID`, wrapped, and leaves every existing
  session's total unchanged. `Total` cases: an unknown `sessionID`
  returns the zero `provider.Usage` and `false`; a known `sessionID`
  returns the correct sum and `true`. `Reset` cases: `Reset` on a
  recorded session zeroes its total, confirmed through a following
  `Total` call; `Reset` on an unknown session returns `nil`; `Reset`
  with a `sessionID` that is empty or whitespace-only after
  `strings.TrimSpace` returns `ErrBlankSessionID`, wrapped; a `Record`
  call after `Reset` starts a fresh sum, not one carried over from
  before the reset.
- `accumulator_race_test.go` — run under `go test -race`. Many
  goroutines call `Record` concurrently against the same
  `sessionID`; the final `Total` equals the arithmetic sum of every
  recorded `provider.Usage`, proving no lost update. A second case
  runs concurrent `Record` calls across many distinct `sessionID`
  values and asserts every session's total independently, proving no
  cross-session interference.
- `accumulator_integration_test.go` — simulates one multi-turn
  session: four sequential `Record` calls with distinct
  `provider.Usage` values, standing in for four model turns in one
  conversation, followed by one `Total` call asserting the summed
  result against a hand-computed expected `provider.Usage`. A second
  session recorded in the same test, with different `sessionID` and
  `provider.Usage` values, proves the first session's total is
  unaffected by the second.
- `accumulator_bench_test.go` — follows `tools/tools_test/registry_bench_test.go`'s
  paired pattern: a `Benchmark`-style function for throughput
  visibility under `go test -bench`, plus a separate `Test`-style
  function, `TestRecordAllocBudget`, that asserts a hard allocation
  budget. `BenchmarkRecordHundredCalls` runs `Record` against a single
  persistent `Accumulator` and one reused `sessionID`, one hundred
  sequential calls per `b.N` iteration, single-goroutine (no
  concurrency, so the ledger benchmark's GOMAXPROCS-contention
  exception does not apply here). It reports `ns/op` and `allocs/op`
  for visibility only; it asserts no exact allocation count, since
  only the very first call across the whole benchmark run allocates a
  new map entry and every later call allocates zero, so `allocs/op`
  trends toward zero as `b.N` grows and carries no fixed value to
  assert. `TestRecordAllocBudget` uses
  `testing.AllocsPerRun(100, func() {...})` against one persistent
  `Accumulator` and one reused `sessionID`, matching
  `tools.TestRunAllocBudget`'s shape: only the first of the 100 calls
  allocates a new map entry, the other 99 allocate zero, so the
  amortized average is at most 1/100 allocations per call. The test
  states a budget of at most 1 allocation per call as a safe,
  checkable upper bound, and fails when the measured average exceeds
  1; a second case inside the same test isolates the first-call
  allocation by measuring one `Record` call against a fresh
  `Accumulator` (`testing.AllocsPerRun(1, func() {...})` rebuilding
  the `Accumulator` each run) and asserts that count is exactly 1.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- `go test -race ./usage/...` passes, covering
  `accumulator_race_test.go`.
- The coverage floor of 85 holds for `usage` and for the total.
- `policy/layers.json` gains the row `"usage": ["provider"]`, landed
  with this plan before the code.
- `api/usage.txt` lands via `make api-update` in the same change as
  the code, holding `Accumulator`, `New`, `Record`, `Total`, `Reset`,
  and `ErrBlankSessionID`.
- `docs/architecture.md` gains a `usage/` bullet describing the
  package and its one import, in the same change as the code.
- `AGENTS.md`'s Layout section gains a `usage/` line, in the same
  change as the code.
- `docs/packages/usage.md` documents the exported surface, matching
  the docs-maintenance convention already used for `provider` and
  `ledger`.
- This phase adds no conformance vector. `usage` defines no wire
  format; it carries in-process values only, the same as `provider`
  and `contextbudget`.
