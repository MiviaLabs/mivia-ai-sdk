# Phase 22: flow admission and branch routing

Status: ready to build. Builds on phase 21 (shipped). This phase adds the
admission rule and the branch step. A branch step picks its successors
at run time. Unchosen successors become `OutcomeSkipped`. See
`docs/plans/agents/PHASES.md`.

## Goal

Let a step declare which prerequisite outcomes admit it. Let a branch
step route the run to a subset of its declared successors. Keep every
target statically declared in the graph.

## Scope

Inside: the `Admission` enum, the `Step.When` field, the `Step.Route`
field, the `Route` function type, skip production, and the new `New`
validations. This phase also updates the `OutcomeSkipped` doc comment
in `flow/outcome.go`: it drops the "no producer yet" note, since this
phase adds the producer. Outside: failure routing and the failure
context. Those land in phase 23. Any `Fire` failure still aborts the
run in this phase.

## API

- `type Admission int` — the rule that admits a step after its needs
  resolve.
- `AdmissionOnFinished` — the zero value, so existing steps keep their
  behavior. Admit when every need ended `OutcomeSucceeded` or
  `OutcomeSkipped`.
- `AdmissionOnSucceeded` — admit only when every need ended
  `OutcomeSucceeded`. A skipped need skips this step.
- `Step` gains `When Admission` and `Route Route`. Both are optional.
- `type Route func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error)`
  — the caller-supplied routing function of a branch step.

Routing lives in the runner, not in machine guards. A guard cannot
skip a step or select a successor. Scheduling is the runner's concern.
The machine package stays untouched.

`Route` is scheduling, not work attachment. It fires no transition and
runs no step work. The two-mechanism rule in `docs/packages/flow.md`
stands: work still attaches only through action closures and nested
definitions.

`Route`'s signature is final for its lifetime: `func(ctx
context.Context, cur machine.Status, rec machine.InOut) ([]string,
error)`. Phase 23's failure routing adds a separate mechanism and
type; it does not change `Route`'s signature or behavior.

### Admission

Admission is evaluated when every need of a step is terminal. A step
that fails its admission becomes `OutcomeSkipped`. A step with no
needs always admits. A skip never fires a transition and never calls
`Confirm`.

The default `AdmissionOnFinished` lets a skipped prerequisite pass
through. A skipped branch therefore never deadlocks a downstream join.
`AdmissionOnSucceeded` is the strict join: it propagates the skip.

### The branch step

A step with a non-nil `Route` is a branch step. It fires its own
transition and confirms its ack like any other step. `Run` then calls
`Route` with the post-step status and record. `Route` returns the IDs
of the direct dependents the run keeps. A direct dependent is a step
whose `Needs` names the branch step.

Unchosen direct dependents become `OutcomeSkipped` at once. This
exclusion is final: it overrides any other pending need the excluded
dependent has. A dependent with two parents, one of them the branch
step, skips as soon as the branch excludes it, even if its other
parent has not resolved yet. Chosen dependents follow normal admission
over their own needs. An empty return skips every direct dependent.
Duplicate IDs in the return collapse.

`Route` runs in the runner goroutine, after the wave logic, never
inside a panel goroutine. Two error shapes abort the run as config
errors. Both are pinned:

- `flow: step %q: route named %q, not a direct dependent` — the first
  `%q` binds the branch step's own ID; the second `%q` binds the
  invalid returned target ID that no direct dependent matches.
- `flow: step %q: route: %w` — wraps a `Route` error the same way
  `runSingleton` wraps a `Fire` error and an ack-not-confirmed error.
  `%q` binds the branch step's own ID; `%w` wraps the error `Route`
  returned. The branch step is marked `OutcomeFailed`. In this phase
  that failure aborts the run, exactly like a `Fire` failure.

A route exclusion of a panel member is a stall risk the plan closes
at build time, not at run time. `panelReady` treats a panel as one
atomic unit: it returns false the moment any member already has a
recorded outcome, so a route that skips one member mid-panel, while a
sibling still waits on an unrelated need, leaves that panel unable to
resolve. `New` closes this gap by barring a branch step's direct
dependents from panel membership, the same way it already bars the
branch step itself.

`New` gains four validations, with pinned messages:

- `flow: step %q has a route but no dependent` — a branch step that no
  step needs. Its `Route` could never select anything.
- `flow: panel %d names routed step %q` — a branch step inside a
  panel. A wave fires members concurrently, so per-member routing is
  undefined. Reject the shape at build time.
- `flow: step %q has both Sub and Route` — a step cannot be both a
  chained step and a branch step. Defining their combined behavior is
  out of scope; `New` rejects the shape instead.
- `flow: panel %d names step %q, a direct dependent of routed step %q`
  — a direct dependent of a branch step named in a panel. A route
  exclusion mid-panel would stall that panel forever; `New` rejects
  the shape instead.

A skipped step never fires `StepCompletedEvent`. This holds whether
admission, route exclusion, or a whole-panel skip produced the skip.
`StepCompletedEvent` means the step executed; a skip means it did not.

### Panels and admission

A panel resolves when every member's needs are terminal. Every member
admitted runs the wave as today. Any member not admitted skips the
whole panel: every member becomes `OutcomeSkipped`. The wave stays
one unit. The homogeneity and independence checks are unchanged.

A panel's members can reach "needs terminal" across separate loop
iterations. One member's needs may resolve in an earlier iteration
while a sibling's needs resolve later. The panel skip decision waits
until every member's needs are terminal, regardless of how many
iterations that takes; it never fires early on a partial view.

### The run loop

Each loop iteration runs one group, skips one group, or returns the
pinned stall error. Termination holds because every step resolves
exactly once. The scan prefers a runnable group in declaration order.
It marks skippable groups when no runnable group exists.

The status walk advances only through executed steps. A skipped step
fires nothing, so `cur` and the record stay as the last executed step
left them. This invariant lets a skipped branch leave the machine in
a well-defined status.

`flow/runner.go` already sits near the 500-line structure-gate cap.
The new admission and routing logic lands in a new file,
`flow/routing.go`, mirroring the package's per-concern split
(`validate.go`, `definition.go`, `panel.go`). `runner.go` changes only
where `nextReadyGroup` calls into the new file; the new file holds the
admission and routing helpers, so no function and no file nears its
cap:

- `admissionVerdict(s Step, outcomes map[string]Outcome) verdict` —
  returns wait, admit, or skip for one step. Lives in `routing.go`.
- `applyRoute(ctx, s Step, cur, rec, outcomes)` — runs `Route` and
  marks unchosen dependents skipped. Lives in `routing.go`.
- `nextReadyGroup`, in `runner.go`, gains the admission verdict in
  place of the boolean needs check, calling into `routing.go`.

## Tests

Test files live in `flow/flow_test/`:

- `routing_new_test.go` — the `New`-validation cases, matching the
  package's convention of a dedicated `*_new_test.go` file (see
  `chain_new_test.go`, `panel_new_test.go`). Red step: the file does
  not compile on the empty phase, because `Admission` and `Route` do
  not exist. Record the compiler error as the red. Cases:
  - `New` rejects a branch step with no dependent, pinned message.
  - `New` rejects a routed step named in a panel, pinned message.
  - `New` rejects a step with both `Sub` and `Route` non-nil, pinned
    message.
  - `New` rejects a panel that names a direct dependent of a branch
    step, pinned message.
  - `New` accepts a branch step with two dependents.
- `routing_test.go` — the behavioral red-green cases, kept separate
  from the `New`-validation cases so neither file nears the 500-line
  cap. Cases:
  - A branch route keeps one dependent and skips the other. Assert
    both outcomes.
  - An empty route return skips every direct dependent.
  - A duplicate ID in the route return collapses to one admission.
  - A route return naming a non-dependent aborts with the pinned
    message.
  - A `Route` error marks the branch step `OutcomeFailed` and aborts.
  - No `StepCompletedEvent` fires for a skipped step. Cover all three
    skip producers: admission, route exclusion, and whole-panel skip.
  - A route excludes a dependent that has a second, still-pending
    parent. Assert the excluded dependent skips at once and the
    exclusion is final, regardless of the second parent's later
    outcome.
  - Default admission admits a step whose need ended
    `OutcomeSkipped`.
  - `AdmissionOnSucceeded` skips a step whose need ended
    `OutcomeSkipped`. The skip cascades to that step's own dependent
    under default admission.
  - Two chained `AdmissionOnSucceeded` steps: the first need skips,
    the second step (needing the first) skips under
    `AdmissionOnSucceeded`, and a third step (needing the second,
    under default admission) also skips. Assert the cascade crosses
    two hops.
  - A panel with one unadmitted member skips every member.
  - A three-member panel where two members' needs resolve in one loop
    iteration and the third member's upstream step resolves only in a
    later iteration; the third member ends up unadmitted. Assert the
    panel skip decision waits for every member and then skips the
    whole panel.
  - `Route` receives the post-step status and record. Assert both
    inside the route function.
- `routing_integration_test.go` — run an if/else graph end to end:
  root, branch, two alternatives, one join with default admission.
  Assert one alternative `OutcomeSucceeded`, the other
  `OutcomeSkipped`, and the join `OutcomeSucceeded`. Repeat with a
  strict join on `AdmissionOnSucceeded` that needs both alternatives.
  Assert the strict join skips. Assert `Confirm` never runs for a
  skipped step. Assert the final status equals the chosen branch's
  target status.
- `routing_bench_test.go` — benchmark a five-step branch graph: root,
  branch, two alternatives, join. Measure the five-step linear
  baseline on the phase 21 code before this phase lands. Record both
  in the file's leading comment. Report the ratio. Set no fixed
  allocation budget. The route closure call adds non-deterministic
  overhead, so a fixed budget is not meaningful; `PHASES.md` permits
  reporting the allocs/op ratio instead.

## Verification

`make verify` passes. The coverage floor for `flow` holds.
`api/flow.txt` gains `Admission`, its two constants, the `Route` type,
and the two new `Step` fields via `make api-update`. Commit the
`api/` diff in the same change. `policy/layers.json` is unchanged.
`api/machine.txt` is unchanged.

`python3 scripts/check_structure.py` passes for the new
`flow/routing.go` file and for `flow/runner.go`. Neither nears the
500-line file cap or the 80-line function cap.

`flow/outcome.go`'s `OutcomeSkipped` doc comment drops the "no
producer yet" note in the same change, since this phase adds the
producer.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change. Describe admission, skip, and the branch
step. Amend the two-mechanism note in `docs/packages/flow.md`: state
that `Route` is scheduling, not a third attachment mechanism. The
amendment text ships with this plan round, so the note and the code
land together. `docs/plans/flow.md` already names this phase; no edit
needed there.
