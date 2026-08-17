# Phase 21: flow step outcomes and run report

Status: ready to build. Builds on phase 6. Independent of phases 7
through 20; those tracks do not touch the runner. This phase replaces
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
- `Run(ctx, d, m, in, confirm) (Report, error)` — the signature
  changes. The first three returns collapse into the `Report`.

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

The nil-argument contract keeps its pinned messages. A nil `d` or a
nil `m` returns the zero `Report`. A nil `confirm` returns a report
holding `m.Initial()` and the incoming record, matching today's
returns.

## Tests

Test files live in `flow/flow_test/`.

The signature change touches every existing caller. Six test files
destructure the three-value return today, 32 call sites in total:
`run_tdd_test.go`, `run_integration_test.go`, `run_perf_test.go`,
`phase06_tdd_test.go`, `phase06_integration_test.go`, and
`phase06_perf_test.go`. Every file moves to the `Report` API in the
same change. Each file keeps its assertions; only the return handling
changes. The `new_*` files and `phase06_tdd_new_test.go` test `New`,
whose signature stays unchanged.

- `outcomes_tdd_test.go` — the red-green cases. Red step: the file
  does not compile on the empty phase, because `Report` and `Outcome`
  do not exist. Record the compiler error as the red. Cases:
  - A linear three-step run reports every step `OutcomeSucceeded`.
    `Status` and `Record` match the values the old signature
    returned.
  - A mid-graph `Fire` failure reports earlier steps
    `OutcomeSucceeded` and the failing step `OutcomeFailed`. Later
    steps report `false` from `Outcome`.
  - A `Confirm` rejection marks the rejected step `OutcomeFailed`.
  - A nil `d` returns the zero `Report` and the pinned error. A nil
    `m` likewise. Both-nil keeps the `d` error and never panics.
  - A nil `confirm` returns the pinned error and a report holding the
    initial status and the incoming record.
  - Mutating the map `Outcomes` returns never changes a later
    `Outcome` call on the same report.
- `outcomes_integration_test.go` — rerun the phase 5 diamond graph
  through the new signature. Assert the outcome of every step. Assert
  the declaration-order tie-break still holds.
- `outcomes_perf_test.go` — keep the existing benchmarks compiling
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

`flow/doc.go` updates in the same change. Its package map is already
stale: it says the runner, the parallel waves, and the chaining land
later. Rewrite the map to name `runner.go` and the file that holds
`Outcome` and `Report`. Drop the "land later" wording for the runner
and the waves. Keep the chaining marked future.
