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
validations. Outside: failure routing and the failure context. Those
land in phase 23. Any `Fire` failure still aborts the run in this
phase.

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

Unchosen direct dependents become `OutcomeSkipped` at once. Chosen
dependents follow normal admission over their own needs. An empty
return skips every direct dependent. Duplicate IDs in the return
collapse.

`Route` runs in the runner goroutine, after the wave logic, never
inside a panel goroutine. Two error shapes abort the run as config
errors, with pinned messages:

- `flow: step %q: route named %q, not a direct dependent` — a
  returned ID that no direct dependent matches.
- A `Route` error marks the branch step `OutcomeFailed`. In this
  phase that failure aborts the run, exactly like a `Fire` failure.

`New` gains two validations, with pinned messages:

- `flow: step %q has a route but no dependent` — a branch step that no
  step needs. Its `Route` could never select anything.
- `flow: panel %d names routed step %q` — a branch step inside a
  panel. A wave fires members concurrently, so per-member routing is
  undefined. Reject the shape at build time.

### Panels and admission

A panel resolves when every member's needs are terminal. Every member
admitted runs the wave as today. Any member not admitted skips the
whole panel: every member becomes `OutcomeSkipped`. The wave stays
one unit. The homogeneity and independence checks are unchanged.

### The run loop

Each loop iteration runs one group, skips one group, or returns the
pinned stall error. Termination holds because every step resolves
exactly once. The scan prefers a runnable group in declaration order.
It marks skippable groups when no runnable group exists.

The status walk advances only through executed steps. A skipped step
fires nothing, so `cur` and the record stay as the last executed step
left them. This invariant lets a skipped branch leave the machine in
a well-defined status.

The builder splits the new logic into helpers, so no function nears
the 80-line cap:

- `admissionVerdict(s Step, outcomes map[string]Outcome) verdict` —
  returns wait, admit, or skip for one step.
- `applyRoute(ctx, s Step, cur, rec, outcomes)` — runs `Route` and
  marks unchosen dependents skipped.
- `nextReadyGroup` gains the admission verdict in place of the boolean
  needs check.

## Tests

Test files live in `flow/flow_test/`:

- `routing_test.go` — the red-green cases. Red step: the file does
  not compile on the empty phase, because `Admission` and `Route` do
  not exist. Record the compiler error as the red. Cases:
  - `New` rejects a branch step with no dependent, pinned message.
  - `New` rejects a routed step named in a panel, pinned message.
  - `New` accepts a branch step with two dependents.
  - A branch route keeps one dependent and skips the other. Assert
    both outcomes.
  - An empty route return skips every direct dependent.
  - A duplicate ID in the route return collapses to one admission.
  - A route return naming a non-dependent aborts with the pinned
    message.
  - A `Route` error marks the branch step `OutcomeFailed` and aborts.
  - Default admission admits a step whose need ended
    `OutcomeSkipped`.
  - `AdmissionOnSucceeded` skips a step whose need ended
    `OutcomeSkipped`. The skip cascades to that step's own dependent
    under default admission.
  - A panel with one unadmitted member skips every member.
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

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change. Describe admission, skip, and the branch
step. Amend the two-mechanism note in `docs/packages/flow.md`: state
that `Route` is scheduling, not a third attachment mechanism. The
amendment text ships with this plan round, so the note and the code
land together. `docs/plans/flow.md` already names this phase; no edit
needed there.
