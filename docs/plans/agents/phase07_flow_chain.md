# Phase 7: flow chaining and audit

Status: future. Builds on phase 6. This phase adds the chained step
and the audit thread. A chained step runs a nested workflow as one
step. The audit thread verifies after the run. See `docs/plans/agents/PHASES.md`.

## Goal

Compose a workflow from smaller workflows. A step nests a `Definition`
and returns its status as one output. The run leaves a thread that
`VerifyThread` validates.

## Scope

Inside: the chained step, the function composition, and the thread
records. Outside: retries, scheduling, and persistence. A future
version adds them only when a consumer asks.

## API

No new exported symbol. A `Step` gains optional chaining by nesting a
`Definition`. The runner detects the nested type and executes it. The
parent reads the child status as one output.

The run appends the envelope message of each step to one thread. The
thread uses `thread_id` and `prev_hash`. After the run, `VerifyThread`
checks the chain and the unique message ids.

## Design note: two attachment mechanisms, not three

A step attaches to real work through exactly two mechanisms. A third
must not appear. See `docs/packages/flow.md` for the full contract.

- A `machine.Transition`'s `OnEntry` and `OnExit` actions run
  arbitrary work: an agent call, a method call, a program, or a call
  into another package. `flow` never knows which.
- This phase adds the second mechanism: a step nests a `Definition`
  and runs it as a sub-workflow. This composes workflows; it does not
  run arbitrary code.

Do not add a third attachment field to `Step`, such as a `Handler` or
an `Executor` field, for a future use case. Route new work through an
action closure instead.

Options, recorded so the choice does not get lost before this phase
starts:

- Option A (recommended). Pin this two-mechanism rule before writing
  any code for this phase. Map every future use case to one of the
  two mechanisms, never to a new `Step` field.
- Option B. Defer the decision until this phase begins, and re-run an
  architecture assessment then. This risks losing the reasoning
  between now and this phase.

## Tests

Test files live in `flow/flow_test/`:

- `phase07_tdd_test.go` — the red-green cases for the chained step.
  Start with the assertions. Confirm they fail on the empty phase.
  Implement and watch them pass.
- `phase07_integration_test.go` — run a workflow that nests another
  workflow. Prove the child status returns to the parent. Verify the
  audit thread with `VerifyThread` after the run. Feed a tampered
  message and confirm the thread fails.
- `phase07_perf_test.go` — benchmark a three-level chain. Target under
  two milliseconds for the whole run.

## Verification

`make verify` passes. The coverage floor for `flow` holds. The flow
projection in `docs/protocol-design.md` updates if the wire changes.
The flow block is complete after this phase.
