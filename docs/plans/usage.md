# Plan: usage

Status: shipped. New top-level package. Depends on the shipped
`provider` package for its `Usage` type; `usage` defines no
token-count type of its own. `usage` imports `provider` only, plus
stdlib; no third-party import. This plan folded in from
`docs/plans/agents/phase58_usage.md` on shipping; no standalone
phase 58 plan file remains.

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

Inside: `WrapCompleter`, one composition seam. It wraps a
`provider.Completer` so every completed `Chat` turn records its
usage under a session. Any provider consumer — a subagent tool, a
providerregistry entry — then gains per-session totals without
calling `Record` itself. A streamed turn records nothing. A blank
sessionID, a nil `Accumulator`, or a nil `Completer` fails
construction.

Outside: cost-in-currency conversion. A caller multiplies the `Total`
fields by its own current price outside this package. Outside: budget
or limit enforcement; `contextbudget.Limits` already owns that gate.
Outside: persistence across process restarts; `Accumulator` holds its
totals in memory only. Outside: the `ledger` package's vocabulary;
`Accumulator` tracks a running sum per session, not a task's
lifecycle, ownership, or dependency graph.

## API

The surface below lands in `api/usage.txt` via `make api-update`.

- `WrapCompleter(sessionID string, a *Accumulator, c provider.Completer) (provider.Completer, error)` — the recording wrapper over one `Completer`.
- `type Accumulator struct { ... }` holds one running `provider.Usage`
  total per session identifier, guarded by a `sync.Mutex`. Unexported
  fields; built only through `New`.
- `func New() *Accumulator` returns an empty `Accumulator` ready to
  record. The only constructor; the zero value's nil map panics on
  write.
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
  call has ever named that `sessionID`.
- `func (a *Accumulator) Reset(sessionID string) error` clears the
  session's total back to zero, as if no `Record` call had ever named
  it. Returns `ErrBlankSessionID`, wrapped, when `sessionID` is empty
  after `strings.TrimSpace`. `Reset` on a `sessionID` with no prior
  `Record` call is a no-op that returns `nil`, not an error.
- `ErrBlankSessionID` is the sentinel `Record` and `Reset` return for a
  `sessionID` empty after `strings.TrimSpace`, checked with
  `errors.Is`. Matches the `ErrBlankName`/`ErrBlankID` sentinel shape
  already used by `tools`, `trigger`, `providerregistry`, and
  `scheduler`.

No `CostFunc` type and no `Sessions` or `List` method in this phase.

## Tests

Test files live in `usage/usage_test/`:

- `accumulator_test.go` — table-driven `Record`, `Total`, and `Reset`
  cases: first call sets the total, later calls add onto it, three or
  more calls sum in order, blank and whitespace-only `sessionID`
  values return `ErrBlankSessionID` and leave existing totals
  unchanged (`TestRecordBlankSessionID`, split out to hold each
  function at or under the 80-line function-length gate). `Total`
  cases cover an unknown `sessionID` and a known one. `Reset` cases
  cover a recorded session, an unknown session (no-op), a blank
  `sessionID`, and a `Record` call after `Reset` starting a fresh sum.
- `wrap_test.go` — per-turn recording, the construction rejections,
  and the streamed passthrough recording nothing.
- `accumulator_race_test.go` — run under `go test -race`. Concurrent
  `Record` calls against the same `sessionID` prove no lost update;
  concurrent `Record` calls across many distinct `sessionID` values
  prove no cross-session interference.
- `accumulator_integration_test.go` — a four-turn session simulation
  followed by a `Total` assertion against a hand-computed expected
  `provider.Usage`, with a second session in the same test proving no
  cross-session leakage.
- `accumulator_bench_test.go` — `BenchmarkRecordHundredCalls` reports
  throughput and `allocs/op` for visibility only, no exact assertion.
  `TestRecordAllocBudget`, mirroring
  `tools.TestRunAllocBudget`'s shape, asserts a hard allocation
  budget with `testing.AllocsPerRun`: an amortized-over-100-calls case
  against a persistent `Accumulator` (budget of at most 1 allocation
  per call, since only the first of the 100 calls allocates a new map
  entry), and an isolated-first-call case rebuilding a fresh
  `Accumulator` each measured run. The isolated case's measured
  baseline is 3 allocations, not the 1 of the original design:
  `New()` plus the first `Record` call together cost 3
  allocations on this implementation (the `Accumulator` escapes to
  the heap once its address is used through a mutex-guarded method,
  plus the map's initial bucket allocation on first insert). The test
  asserts a budget of at most 3 for that case; the amortized case
  above already carries the invariant that matters for a caller
  (steady-state `Record` cost stays at or under 1 allocation per
  call).

## Verification

- `go test ./usage/...`, `go vet ./usage/...`, and
  `go test -race ./usage/...` pass.
- The coverage floor of 85 holds for `usage`: measured 100% of
  statements.
- `policy/layers.json` carries the row `"usage": ["provider"]`.
- `api/usage.txt` lands via `make api-update`, holding `Accumulator`,
  `New`, `Record`, `Total`, `Reset`, `WrapCompleter`, and the
  sentinels `ErrBlankSessionID`, `ErrNilAccumulator`, and
  `ErrNilCompleter`.
- `docs/architecture.md` carries a `usage/` bullet describing the
  package and its one import.
- `AGENTS.md`'s Layout section carries a `usage/` line.
- `docs/packages/usage.md` documents the exported surface.
- This package defines no wire format, so it adds no conformance
  vector.
