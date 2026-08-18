# Phase 21: flow step outcomes and run report

Status: ready to build. Builds on phases 5 through 7. Phase 21 extends
the same runner path; this phase must keep phase 7 behavior intact. This
phase replaces
the boolean done map with per-step outcomes. `Run` returns a `Report`.
The run stays fail-fast. See `docs/plans/agents/PHASES.md`.

## Goal

Give every step a terminal state. Expose the terminal states to the
caller. Keep every other run behavior unchanged.

## Scope

Inside: the `Outcome` type, the `Report` type, the `Run` signature
change, and the internal outcomes map. Outside: admission rules, skip
production, branch routing, and failure routing. Those land in phases
22 and 23. No producer of `OutcomeSkipped` exists yet. The constant
ships now because the enum is one type and phase 22 needs it.

## API

- `type Outcome int` — the terminal state of one step.
- `OutcomeSucceeded` — the step fired and its ack confirmed.
- `OutcomeFailed` — the step's `Fire` failed or its ack was rejected.
- `OutcomeSkipped` — admission or routing excluded the step. No
  producer before phase 22.
- `type Report struct` — the run result. The fields are unexported.
- `func (r Report) Status() machine.Status` — the final current
  status.
- `func (r Report) Record() machine.InOut` — the final record.
- `func (r Report) Outcome(id string) (Outcome, bool)` — one step's
  outcome. The boolean is false when the step never resolved.
- `func (r Report) Outcomes() map[string]Outcome` — a copy of every
  resolved outcome. Caller mutation cannot change the report.
- `Run(ctx, d, m, in, confirm, bus) (Report, error)` — the signature
  keeps all six parameters. Only the return type changes: the first
  three returns collapse into the `Report`.

The internal `done map[string]bool` becomes
`outcomes map[string]Outcome`. `needsMet` reads the new map: a need
is met when its outcome is `OutcomeSucceeded`. `markDone` becomes
`markOutcome(group, OutcomeSucceeded)`.

On every abort, `Run` returns the report built so far plus the error.
This replaces the current `(cur, rec, err)` triple on error paths.
The abort itself is unchanged: any step failure still stops the run.
A `Fire` failure marks the step `OutcomeFailed` before the return.
A `Confirm` rejection marks the step `OutcomeFailed` before the
return. Later steps stay absent from the outcomes map.

On any wave error, no member of that wave is marked in the outcomes
map. This mirrors `markDone`'s current all-or-nothing rule at
runner.go:118. A wave can join one member's `Fire` failure with
siblings that succeeded. Successful siblings inside a failed wave are
not marked `OutcomeSucceeded`. The failing member is not marked
`OutcomeFailed` either. The wave-level error is attributed at the
wave level, not per member.

The nil-argument contract keeps its pinned messages. A nil `d` or a
nil `m` returns a `Report` holding the zero `Status` and the caller's
original `in` as the `Record`. This is not a zero-value `InOut`; the
caller's `in` still comes back, matching today's `(machine.Status(""),
in, err)` return. A nil `confirm` returns a report holding
`m.Initial()` and the incoming record, matching today's returns.

## Tests

Test files live in `flow/flow_test/`.

The signature change touches every existing caller. Eleven test
files destructure the three-value return, 60 call sites in total:
`run_test.go` (11), `run_integration_test.go` (5),
`run_bench_test.go` (2), `panel_test.go` (9),
`panel_integration_test.go` (4), `panel_bench_test.go` (2),
`chain_test.go` (12), `chain_integration_test.go` (3),
`chain_bench_test.go` (5), `chain_new_test.go` (1), and
`emit_test.go` (6). Every file moves to the `Report` API in
the same change. Each file keeps its assertions; only the return
handling changes. The `new_test.go`, `new_integration_test.go`,
`new_bench_test.go`, and `panel_new_test.go` files test `New`,
whose signature stays unchanged. `chain_new_test.go` also holds
one `Run` call site; it moves to the `Report` API with the rest.
`emit_test.go` covers the event-bus behavior, including
`TestEmitNoneOnConfirmFailure`, the confirm-rejection abort case;
its six `Run` call sites move to the `Report` API too.

- `outcomes_test.go` — the red-green cases. Red step: the file
  does not compile on the empty phase, because `Report` and `Outcome`
  do not exist. Record the compiler error as the red. Cases:
  - A linear three-step run reports every step `OutcomeSucceeded`.
    `Status` and `Record` match the values the old signature
    returned.
  - A mid-graph `Fire` failure reports earlier steps
    `OutcomeSucceeded` and the failing step `OutcomeFailed`. Later
    steps report `false` from `Outcome`.
  - A `Confirm` rejection marks the rejected step `OutcomeFailed`.
  - A nil `d` returns the pinned error and a `Report` holding the
    zero `Status` and the caller's `in`. The test passes a non-zero
    `in` and asserts `Report.Record()` equals that `in`, not just
    the error. A nil `m` likewise. Both-nil keeps the `d` error and
    never panics.
  - A nil `confirm` returns the pinned error and a report holding the
    initial status and the incoming record.
  - Mutating the map `Outcomes` returns never changes a later
    `Outcome` call on the same report.
- `outcomes_integration_test.go` — rerun the phase 5 diamond graph
  through the new signature. Assert the outcome of every step. Assert
  the declaration-order tie-break still holds. The diamond graph is
  sequential, so it does not exercise a wave failure. Add a panel
  case: a panel with one failing member and one succeeding member.
  Assert the wave error aborts the run and assert `Outcomes()` holds
  no entry for either member.
- `outcomes_bench_test.go` — keep the existing benchmarks compiling
  under the new signature. Measure the three-step linear baseline
  again on the same machine before this phase's code lands. Record
  ns/op, B/op, and allocs/op in the file's leading comment. The
  outcomes map adds allocations, so the phase 5 budget of 9 allocs/op
  may rise. Set the new budget at the measured value plus 50 percent.

## Verification

`make verify` passes. The coverage floor for `flow` holds.
`api/flow.txt` gains `Outcome`, its constants, `Report`, its methods,
and the new `Run` signature via `make api-update`. Commit the `api/`
diff in the same change. `policy/layers.json` is unchanged.
`api/machine.txt` is unchanged; the machine package is untouched.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change. Name `Outcome` and `Report` next to
`Run` and `Confirm`.

`flow/doc.go` updates in the same change. Its package map already
names `runner.go`. Add the file that holds `Outcome` and `Report`.
