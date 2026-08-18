# Plan: flow

Status: the step graph, the sequential runner, the parallel panel
waves, chaining, per-step outcomes, the admission rule, and branch
routing ship. Two more phases are planned: failure routing and the
checkpoint pause/resume pair. This plan expands the earlier step-list
design into a step runner for v1. Rationale in
docs/research-state-machine.md. `Run` returns a `Report` holding every
step's terminal `Outcome`, replacing the boolean done map. Phase 22
shipped the admission rule, the skip semantics, and the branch step.
Phase 23 owns the fallback path and the failure context; see
docs/plans/agents/phase23_flow_fallback.md. Phase 25 owns the
checkpoint, the pause rule, and `Resume`; see
docs/plans/agents/phase25_flow_checkpoint.md.

## Goal

Run a declarative workflow over steps. A workflow is a step graph.
Steps hold dependencies, gates, inputs, outputs, and a target status.
The runner schedules steps in topological order and supports parallel
panels.

## Scope

Inside: a step graph, panels, parallel execution, chaining of
workflows, and a runner. A step composes the machine package for its
status transitions. A panel is a group of independent steps that run
together. A chained step runs a nested workflow as one step. The
runner detects cycles with Kahn's algorithm before any step runs. The
consumer is real; another system needs these capabilities now.

Every step ends in one terminal state: succeeded, failed, or skipped.
`Report` exposes each step's `Outcome` and the run's final status and
record.

Shipped in phase 22: the admission rule and branch routing. A step
declares which prerequisite outcomes admit it, through `Step.When`. A
branch step picks its successors at run time from its declared
dependents, through `Step.Route`. The status walk advances only
through executed steps. A skipped step never fires a transition.

Planned for phase 23: failure routing. A fallback step admits on a
failed need and receives the failure context.

Inside, from phase 25: a `Checkpoint` of the current status, the
record, and the completed step IDs; a pause rule keyed on context
cancellation; and `Resume`, which restarts a walk from a stored
checkpoint. Persistence stays a caller concern: `flow` reports a
checkpoint through a hook and never writes storage itself. See
docs/plans/agents/phase25_flow_checkpoint.md.

Outside: retries, compensation, scheduling, and history replay. A
future version adds these only when that consumer asks. A caller
retries by calling `Resume` again on the same checkpoint after a step
failure. A caller schedules a resume from a cron job, a queue, or a
webhook, since `Resume` is a plain resumable function call. History
replay is rejected: a caller who persists every checkpoint already
holds a replayable log, and `flow` does not build an event log. See
docs/research-state-machine.md:236-238. Compensation has no named
caller yet; adding it now is speculative generality.
Parallel panels run in goroutines; the runner is in-process, not a
distributed service. Each wave reads the incoming record. Each step in
a wave runs with a copy of that record. The wave collects results
and errors. errors.Join reports failures across the wave. No goroutine
mutates the shared record. The design is correct, not hardened. It
meets the need without overengineering.

## API

Proposed shape, subject to plan review. It follows the DAG scheduler
and step-as-data patterns. See docs/research-state-machine.md for the
pattern sources.

- `type Step struct { ID string; Needs []string; To string; Payload string; Sub *Definition }`
  as a graph node. `Sub` is the chained child definition.
- `type Panel []string` as a group of step IDs that run in parallel.
- `type Definition struct` holding the step graph and the panels.
- `New(steps []Step, panels []Panel) (*Definition, error)` to build a
  definition and reject cycles with Kahn's algorithm.
- `type Confirm func(ctx context.Context, step Step) error` as the ack
  gate a caller supplies.
- `Run(ctx, d *Definition, m *machine.Definition, in machine.InOut, confirm Confirm, bus *events.Bus) (Report, error)`
  executes the graph and returns a `Report` in place of the earlier
  status and record pair; the six parameters, including `bus`, stay
  unchanged.
- A chained step nests another Definition as one step, through
  `Step.Sub`.
- `type Outcome int` with `OutcomeSucceeded`, `OutcomeFailed`, and
  `OutcomeSkipped` as the terminal states. Shipped in phase 22:
  admission, route exclusion, and a whole-panel skip each produce
  `OutcomeSkipped`.
- `type Report struct` with unexported fields, and `Status()`,
  `Record()`, `Outcome(id string) (Outcome, bool)`, and
  `Outcomes() map[string]Outcome` accessors. `Outcomes` returns a copy;
  caller mutation cannot change the report. `Run` returns it in place
  of the status and record pair.
- `type Admission int` with `AdmissionOnFinished` as the zero-value
  default and `AdmissionOnSucceeded`, shipped in phase 22. The default
  admits a need that ended `OutcomeSucceeded` or `OutcomeSkipped`.
  `AdmissionOnSucceeded` admits only a succeeded need; a skipped need
  skips the step too, and the skip can cascade to that step's own
  dependents. `Step` gains `When Admission`. `AdmissionOnFailed` lands
  in phase 23, for the fallback path.
- `type Route func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error)`
  as the branch step's routing function, shipped in phase 22. `Step`
  gains `Route Route`; a non-nil `Route` makes the step a branch step.
  `Route` runs in the runner goroutine, after the wave logic, never
  inside a panel goroutine. It receives the branch step's post-fire
  status and record and returns the IDs of the direct dependents the
  run keeps; every other direct dependent skips at once, even one with
  another, still-pending need. An empty return skips every direct
  dependent; a duplicate ID collapses to one admission. `Route` fires
  no transition and runs no step work — it is scheduling, not a third
  work-attachment mechanism, so the two-mechanism rule in
  docs/packages/flow.md stands. `Route`'s signature is final for its
  lifetime: phase 23's failure routing adds a separate mechanism and
  does not change it.

  `New` rejects four shapes, each with a pinned message: a branch step
  with no dependent (`flow: step %q has a route but no dependent`); a
  branch step named in a panel (`flow: panel %d names routed step
  %q`); a step with both `Sub` and `Route` non-nil (`flow: step %q has
  both Sub and Route`); and a panel that names a direct dependent of a
  branch step (`flow: panel %d names step %q, a direct dependent of
  routed step %q`). The last two close a stall risk: `panelReady`
  treats a panel as one atomic unit, so a route exclusion of a member,
  or of a member's sibling, would leave that panel unable to resolve.
  A `Route` error, or a return naming a step that is not a direct
  dependent, aborts the run: the branch step is marked
  `OutcomeFailed`, exactly like a `Fire` failure. Two pinned messages
  cover these: `flow: step %q: route named %q, not a direct dependent`
  and `flow: step %q: route: %w`.

  A panel resolves as one atomic unit: it runs its wave only once
  every member admits, and one unadmitted member skips every member,
  even a member whose own needs would otherwise admit it. The
  admission and routing logic lives in `flow/routing.go`, split out of
  `flow/runner.go` to stay under the 500-line structure-gate cap.
  `admissionVerdict` returns wait, admit, or skip for one step;
  `applyRoute` runs `Route` and marks unchosen dependents skipped;
  `nextReadyGroup`, in `runner.go`, calls into both.
- `type Failure struct { Step string; Err error }` and
  `FailureFrom(ctx)` as the failure context a fallback step reads.
  These land in phase 23.
- `type Checkpoint struct { Status machine.Status; Record machine.InOut; Done []string }`
  with `Validate`, `Encode`, and `Decode`, as the resumable run state.
  `Run` gains a trailing `onCheckpoint func(Checkpoint)` parameter.
  `Resume(ctx, d, m, checkpoint, confirm, bus, onCheckpoint)` restarts
  a walk from a stored checkpoint. These land in phase 25.

The machine instance passes by pointer. The input and output records
come from the machine package. Run may pass any in and out through the
graph. A panel of steps that run in parallel gather results and errors
without a third-party library.

Panels map to topological waves. A wave is a set of steps with no
remaining dependencies. The scheduler runs one wave at a time. Steps
inside a wave run in goroutines. It gathers results with a WaitGroup
and a buffered channel. It combines errors with errors.Join, which is
stdlib. It never uses errgroup.

Panel validation rejects a step ID named in two panels. The runner
schedules a step through the first panel that names it. A second panel
naming the same ID can never become ready, so the walk stalls or the
second panel is silently ignored. `validatePanels` in flow/validate.go
runs the check after the per-panel loop. Every panel passes the
unknown-step, duplicate, and To checks first. The scan walks panels in
declaration order and members in declaration order. It maps each step
ID to the first panel index that named it. The first member found
again returns the pinned error:

- `flow: step %q is named in panels %d and %d` — `%q` is the repeated
  step ID. The first `%d` is the first panel that names it. The second
  `%d` is the later panel that names it again.

Add the new rejection to `New`'s doc comment in flow/definition.go.
docs/packages/flow.md gains one Invariants bullet: "No step ID is
named in two panels. A repeat across panels fails." A cross-panel
scheduling deadlock stays a Run-time stall, not a `New` rejection.
Panels with no shared member may still need each other.
`TestRunCrossPanelDeadlockStalls` keeps passing unchanged.

Run's doc comment gains one sentence: a chained step's child workflow
runs with a nil bus, and its child steps emit no events.
docs/packages/flow.md needs no change for that sentence: its `Run`
entry already documents the nil-bus child behavior.

Chaining is function composition. A step takes an input and returns an
output. A chained step runs a nested Definition and returns its
status. The parent reads the child result as one output.

Routing stays in the runner, not in machine guards. A guard cannot
skip a step or select a successor. Scheduling is the runner's concern.
Failure routing uses admission over a failed need, not a separate
fallback field. A fallback field would duplicate the Needs edge and
can drift from it.

The policy/layers.json row for flow is `"flow": ["events", "machine"]`.
The `events` import carries the step outcome bus emit.
`flow` never imports `envelope`. The audit thread stays caller-owned.
The runner enforces the gate; the caller provides the transport.
Outcomes and phase 22 added no import edge; phase 23 adds none either.
The failure context travels through `context.Context`, which is
stdlib.

## Tests

Topological order on a diamond DAG. Cycle detection rejects a bad
graph. The sequential case covers: linear order, the
declaration-order tie-break, a gate failure, and an unconfirmed ack.
A panel of independent steps runs in parallel, covering a
successful wave, a rejected member, and a cross-panel scheduling
stall. Chaining runs a nested workflow and returns its status; this
lands in phase 7. The audit thread verifies with VerifyThread after the run,
once phase 7 lands.

The outcomes tests cover the report: outcomes per step, the failing
step marked failed, and the immutable outcomes copy.

Phase 22's routing tests live in `flow/flow_test/`:

- `routing_new_test.go` — the `New`-validation cases: a branch step
  with no dependent, a routed step named in a panel, a step with both
  `Sub` and `Route` non-nil, a panel that names a direct dependent of
  a branch step, and a branch step with two dependents that `New`
  accepts.
- `routing_test.go` — the behavioral cases: a branch route keeps one
  dependent and skips the other; an empty route return skips every
  direct dependent; a duplicate ID in the return collapses to one
  admission; a route return naming a non-dependent aborts with the
  pinned message; a `Route` error marks the branch step
  `OutcomeFailed` and aborts; no `StepCompletedEvent` fires for a
  skipped step, across all three skip producers; a route excludes a
  dependent that has a second, still-pending parent, and the exclusion
  is final regardless of that parent's later outcome; default
  admission admits a step whose need ended `OutcomeSkipped`;
  `AdmissionOnSucceeded` skips a step whose need ended
  `OutcomeSkipped`, and the skip cascades two hops through a chain of
  `AdmissionOnSucceeded` steps; a panel with one unadmitted member
  skips every member, including a three-member panel where the third
  member's needs resolve only in a later loop iteration; `Route`
  receives the post-step status and record.
- `routing_integration_test.go` — an if/else graph end to end: root,
  branch, two alternatives, one join. Default admission on the join
  lets one alternative succeed and the other skip while the join
  succeeds; `AdmissionOnSucceeded` on the join skips it instead.
  `Confirm` never runs for a skipped step, and the final status equals
  the chosen branch's target status.
- `routing_bench_test.go` — a five-step branch graph (root, branch, two
  alternatives, join) against the five-step linear baseline from
  before phase 22. The route closure call adds non-deterministic
  overhead, so the benchmark reports the allocs/op ratio instead of a
  fixed allocation budget.

Phase 23 covers the fallback: a handled failure lets the run
complete, the fallback reads the failure context, and an unhandled
failure still aborts. Phase 25 covers the checkpoint: the hook fires
once per completed step or wave, a paused run returns cleanly on
context cancellation, and `Resume` reaches the same final status a
plain `Run` reaches.

A logic review added three tests: the table-driven
TestNewPanelStepNamedInTwoPanels in flow/flow_test/panel_new_test.go,
TestRunNilMAndNilConfirmTogether, and TestEmitNoneOnConfirmFailure.
The panel table cases:

- `New` rejects the confirmed stall shapes: panels naming one shared
  step, two panels sharing a middle step, and one full duplicate
  panel. Each pins the exact message.
- `New` reports both panel indexes when the naming panels sit apart.
  Panels at index zero and index two pin
  `flow: step "a" is named in panels 0 and 2`.
- `New` reports the first repeated member in member order on a swap
  shape: panels naming "a" then "b", then "b" then "a", pin
  `flow: step "b" is named in panels 0 and 1`.
- `New` reports the first repeat when one step sits in three panels.
  Panels naming "a" three times pin
  `flow: step "a" is named in panels 0 and 1`.
- `New` reports the unknown-step message when a later panel holds both
  a repeat and an unknown step: steps "a" and "b", panels naming "a",
  then "a" and "nope", pin `flow: panel 1 names unknown step "nope"`.
  This proves the per-panel checks run before the overlap scan.
- `New` accepts panels whose members each sit in one panel only.

flow/flow_test/run_test.go gains TestRunNilMAndNilConfirmTogether next
to the other nil-argument cases. A valid definition, a nil machine,
and a nil confirm return exactly `flow: m must not be nil` and never
panic. flow/flow_test/emit_test.go gains TestEmitNoneOnConfirmFailure.
A subscribed bus and a confirm that fails on the first step emit zero
events. Run wraps the confirm error and names the failing step. The
chained-step bus sentence needs no new test. TestEmitOnChainedStep
already pins the nil-bus behavior.

## Verification

`make verify`. Conformance vectors for the definition form. The
rationale lives in docs/research-state-machine.md. `api/flow.txt`
lands via make api-update. Phase 22 extended `api/flow.txt` with
`Admission`, its two constants, the `Route` type, and the two new
`Step` fields, via make api-update, leaving `api/machine.txt` and
`policy/layers.json` unchanged. Phases 23 and 25 each extend
`api/flow.txt` the same way, in their own change.
