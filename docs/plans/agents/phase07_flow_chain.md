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
