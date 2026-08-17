# Phase 7: flow chaining and audit

Status: ready to build. Builds on phase 6. This phase adds the chained
step and the audit thread contract. A chained step runs a nested
workflow as one step. A caller records the audit thread during the run.

## Goal

Compose a workflow from smaller workflows. A step nests a `Definition`
and returns the child status as one output. A caller records an audit
thread during the run. The caller runs `envelope.VerifyThread` after the
run.

## Scope

Inside: the chained step, the function composition, and the thread
contract for callers. Outside: retries, scheduling, and persistence. A
future version adds them only when a consumer asks. `flow` stays
independent of `envelope`; the caller owns the audit thread.

## API

`Step` gains one exported field: `Sub *Definition`. A step with a
non-nil `Sub` runs the child graph to completion before the parent
resumes. The child final status becomes the parent step's target status.
`make api-update` refreshes `api/flow.txt` in the same change.

### Expected API surface

The surface below must match `api/flow.txt` after `make api-update`.

```go
type Step struct {
    ID      string
    Needs   []string
    To      string
    Payload string
    Sub     *Definition
}
type Panel []string
type Definition struct{}
type Confirm func(ctx context.Context, step Step) error
func New(steps []Step, panels []Panel) (*Definition, error)
func (d Definition) Roots() ([]string)
func Run(ctx context.Context, d *Definition, m *machine.Definition, in machine.InOut, confirm Confirm) (machine.Status, machine.InOut, error)
```

`New` deep-copies each non-nil `Sub` recursively. A `Definition` is
immutable and was validated by its own `New`, so its internal graph is
already acyclic. `New` checks nesting depth. `New` rejects nesting deeper
than eight levels. Eight bounds the recursive runner's stack use while
still allowing deep workflow composition; it is a local safety guard,
not a wire-format limit.

### Data model

Add `Sub *Definition` as the last field of `flow.Step`. A nil `Sub`
preserves the phase 5 and phase 6 behavior. A non-nil `Sub` makes the
step a chained step.
`copySteps` deep-copies `Sub` recursively. It copies the child's steps,
panels, and roots. The parent Definition stays immutable after `New`.

For a chained step, `Step.To` is ignored by `Run` and may be left empty.
`New` still enforces panel homogeneity using `To`, so a chained step may
not appear in a multi-member panel.
Update the `Step` doc comment to mention `Sub` and that `To` is ignored for chained steps.

### Validation in `New`

`New` validates each non-nil `Sub` by checking nesting depth. A step
with a nil `Sub` has depth zero. A step with a non-nil `Sub` has depth
one plus the maximum depth among its child steps. `New` rejects any
step whose depth exceeds eight. Eight bounds the recursive runner's
stack use while still allowing deep workflow composition; it is a local
safety guard, not a wire-format limit.

`New` rejects a panel with two or more members if any member has
`Sub != nil`. A chained step may appear only as a singleton or as the
sole member of a one-member panel.

A child `Definition` is immutable. It was built by `New`, which already
enforced acyclicity and every other graph rule. The parent `New` does
not re-validate the child's internal graph.

### Runner behavior

When `Run` encounters a step with `Sub != nil`, it runs the child
`Definition` to completion. It passes the same `machine.Definition` and
a fresh `machine.InOut`. The child run starts from the machine's initial
status. It calls the same `confirm` closure for each child step.

After the child run completes, `Run` uses the child final status as the
target status for the parent step. It picks the parent transition row
from the current status to the child final status. It fires that row.
This advances the parent status and record as after a normal Fire.

Then `Run` calls `confirm(ctx, step)` for the chained parent step. This
matches the normal step order: Fire, then confirm. If `confirm` returns
an error, the run stops and returns the error.

If the child `Run` returns an error, `Run` returns it unchanged.
If `confirm` returns an error for the chained parent step, `Run` wraps it as `flow: step %q: ack not confirmed: %w`.
This matches the normal singleton step path.

If no parent transition matches the child final status, `Run` fails with
the no-transition error. If multiple rows match, it fails with the
ambiguity error.

### Confirm semantics

The same `confirm` closure serves both the parent and the child. The
parent `Run` calls it for each child step during the child run. The
parent `Run` calls it again for the chained parent step after the child
run completes. The closure distinguishes parent steps from child steps
by their IDs; `step.Sub` is non-nil for any chained step, including
nested children. The closure records envelope messages. `flow` does not
import `envelope`.

### Audit thread

`flow` does not import `envelope`. The caller records each step's
envelope message through an `OnEntry` action, an `OnExit` action, or the
`Confirm` closure. The caller runs `envelope.VerifyThread` after `Run`
returns. A tampered message fails verification.

### Design note: two attachment mechanisms, not three

A step attaches to real work through exactly two mechanisms. `Confirm`
is an ack gate, not an attachment mechanism. A third attachment mechanism
must not appear. See `docs/packages/flow.md` for the full contract.

- A `machine.Transition`'s `OnEntry` and `OnExit` actions run arbitrary
  work.
- This phase adds the second attachment mechanism: a step nests a
  `Definition` and runs it as a sub-workflow.

`Step.Sub` is the one new `Step` field allowed for this mechanism. Do not
add a third attachment field to `Step`.

## Tests

Test files live in `flow/flow_test/`:

- `phase07_tdd_test.go` — the red-green cases for the chained step.
  Start with the assertions. Confirm they fail on the empty phase.
  Implement and watch them pass. Cases:
  - `New` accepts a step with a nil `Sub`.
  - `New` accepts a step with a valid `Sub`.
  - `New` rejects a `Sub` chain deeper than eight levels.
  - `New` accepts a `Sub` chain of exactly eight levels.
  - `New` rejects a chained step that is a member of a multi-member panel.
  - `New` accepts a chained step that is the sole member of a one-member panel.
  - `New` deep-copies `Sub`, including its panels and roots. Mutating
    the original child steps, panels, or roots after building the parent
    does not affect the parent.
  - `Run` runs a chained step and uses the child final status as the
    parent step's target status.
  - `Run` calls `confirm` for each child step and for the chained parent
    step.
  - `Run` fails when the child final status has no matching parent
    transition.
  - `Run` fails when the child final status has an ambiguous parent
    transition.
  - `Run` returns the child final status as the parent status when the
    transition matches.
  - A chained step with a failing child step returns the child error
    unchanged. The child `Run` already wraps errors as `flow: step %q: ...`.
    The TDD assertion checks only that the error is non-nil and its
    message contains the failing child step ID.
  - `Run` wraps a confirm error for the chained parent step as
    `flow: step %q: ack not confirmed: %w`. It returns the final status
    and record at the point of failure.

  Keep each test file at or below 500 lines. Split the cases across
  `phase07_tdd_test.go` and `phase07_tdd_new_test.go` if needed.
- `phase07_integration_test.go` — run a workflow that nests another
  workflow. Prove the child status returns to the parent. Record an
  audit thread inside the `Confirm` closure. Run `envelope.VerifyThread`
  after `Run` returns. Feed a tampered message and confirm verification
  fails.
  - Build a parent flow with one chained step and one normal step that
    depends on it.
  - Build a child flow with two steps.
  - Run the parent flow.
  - Assert the parent status equals the child final status.
  - Assert `confirm` ran for both child steps and the parent chained
    step.
  - Record each step's envelope message in the `Confirm` closure.
  - Build the thread and run `envelope.VerifyThread`.
  - Tamper with one message's payload.
  - Assert `envelope.VerifyThread` returns an error.
- `phase07_perf_test.go` — before the phase code lands, benchmark an
  equivalent flat workflow on the current `Run` path. Record ns/op,
  B/op, and allocs/op in the file's leading comment. Then benchmark a
  three-level chain. The chain must stay under two milliseconds and must
  not allocate more than 1.5 times the flat baseline.
  - Flat baseline: a workflow with the same number of steps as the
    chained workflow but no nesting.
  - Chained workflow: three levels of nesting, with at least one step
    per level.
  - Assert chained ns/op is below two milliseconds.
  - Assert chained allocs/op is below 1.5 times the flat baseline.

## Verification

`make verify` passes. Run `make api-update` and commit the `api/flow.txt`
diff in the same change. The coverage floor for `flow` holds. The flow
projection in `docs/protocol-design.md` does not change; this phase adds
no wire change. Update `flow/doc.go` to state that chaining ships in
phase 7. Update `AGENTS.md` to state that chaining ships in phase 7.
