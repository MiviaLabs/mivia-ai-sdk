# Phase 7: flow chaining and audit

Status: future. Builds on phase 6. This phase adds the chained step
and the audit thread contract. A chained step runs a nested workflow
as one step. A caller records the audit thread during the run. See
`docs/plans/agents/PHASES.md`.

## Goal

Compose a workflow from smaller workflows. A step nests a `Definition`
and returns the child status as one output. A caller records an audit
thread during the run. The caller runs `envelope.VerifyThread` after
the run.

## Scope

Inside: the chained step, the function composition, and the thread
contract for callers. Outside: retries, scheduling, and persistence.
A future version adds them only when a consumer asks. `flow` stays
independent of `envelope`; the caller owns the audit thread.

## API

`Step` gains one exported field: `Sub *Definition`. A step with a
non-nil `Sub` runs the child graph to completion before the parent
resumes. The child status becomes the step's output. `make api-update`
refreshes `api/flow.txt` in the same change.

`New` validates each non-nil `Sub` with the same rules as a top-level
graph. A child graph must be acyclic. Every child dependency must
resolve inside the child graph.

`flow` does not import `envelope`. The audit thread is caller-owned.
A caller records each step's envelope message through an `OnEntry`
action, an `OnExit` action, or the `Confirm` closure. The caller runs
`envelope.VerifyThread` after `Run` returns.

## Design note: two attachment mechanisms, not three

A step attaches to real work through exactly two mechanisms. `Confirm`
is an ack gate, not an attachment mechanism. A third attachment
mechanism must not appear. See `docs/packages/flow.md` for the full
contract.

- A `machine.Transition`'s `OnEntry` and `OnExit` actions run
  arbitrary work: an agent call, a method call, a program, or a call
  into another package. `flow` never knows which.
- This phase adds the second attachment mechanism: a step nests a
  `Definition` and runs it as a sub-workflow. This composes workflows;
  it does not run arbitrary code.

`Step.Sub` is the one new `Step` field allowed for this mechanism.
Do not add a third attachment field to `Step`, such as a `Handler` or
an `Executor` field, for a future use case. Route new work through an
action closure instead.

Options, recorded so the choice does not get lost before this phase
starts:

- Option A (recommended). Pin this two-mechanism rule before writing
  any code for this phase. Map every future use case to one of the
  two attachment mechanisms, never to a new `Step` field beyond `Sub`.
- Option B. Defer the decision until this phase begins, and re-run an
  architecture assessment then. This risks losing the reasoning
  between now and this phase.

## Tests

Test files live in `flow/flow_test/`:

- `phase07_tdd_test.go` — the red-green cases for the chained step.
  Start with the assertions. Confirm they fail on the empty phase.
  Implement and watch them pass.
- `phase07_integration_test.go` — run a workflow that nests another
  workflow. Prove the child status returns to the parent. Record an
  audit thread inside the `Confirm` closure. Run `envelope.VerifyThread`
  after `Run` returns. Feed a tampered message and confirm verification
  fails.
- `phase07_perf_test.go` — before the phase code lands, benchmark an
  equivalent flat workflow on the current `Run` path. Record ns/op,
  B/op, and allocs/op in the file's leading comment. Then benchmark a
  three-level chain. The chain must stay under two milliseconds and
  must not allocate more than 1.5 times the flat baseline.

## Verification

`make verify` passes. Run `make api-update` and commit the `api/flow.txt`
diff in the same change. The coverage floor for `flow` holds. The flow
projection in `docs/protocol-design.md` updates if the wire changes.
The flow block is complete after this phase.
