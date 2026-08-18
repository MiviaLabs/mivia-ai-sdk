# Plan: flow

Status: the step graph, the sequential runner, the parallel panel
waves, chaining, per-step outcomes, the admission rule, branch
routing, the failure fallback path, the checkpoint pause/resume pair,
a bounded retry loop around a step's `Fire` call, and a loop-driving
repeat of a step's `Sub` all ship. This plan expands the earlier
step-list design into a step runner for v1. Rationale in
docs/plans/machine.md's "Why build, not buy" section. `Run` returns a `Report` holding every
step's terminal `Outcome`, replacing the boolean done map. Phase 22
shipped the admission rule, the skip semantics, and the branch step.
Phase 23 shipped the fallback path and the failure context. Phase 25
shipped the checkpoint, the pause rule, and `Resume`. Phase 30 shipped
a retry loop; see the Phase 30 subsection below. Phase 38 shipped a
loop-driving repeat of a step's `Sub`; see the Phase 38 subsection
below.

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

Shipped in phase 23: failure routing. A step declares a fallback
through `AdmissionOnFailed`, the third `Admission` value, an any-of
rule over its `Needs`: it admits once at least one need ends
`OutcomeFailed`, no matter what the other needs resolve to, unlike the
all-of rule the other `Admission` values keep. `Run` injects a
`Failure` into the fallback's transition `ctx`, readable through
`FailureFrom`; a step outside every failure's `Needs` set never sees
one. A failure without a declared `AdmissionOnFailed` dependent stays
fatal, keeping fail-fast the default. Only a `Fire` failure and a
`Route` error are catchable; a `Confirm` rejection and a
`pickTransition` failure always abort, since no fallback can repair a
missing transition row or an explicit reject. A panel failure catches
only when every failed member has its own fallback; one uncaught
member aborts the whole wave. `New` rejects an `AdmissionOnFailed`
step with no needs and one named in a panel, since a panel's shared
`ctx` has no clean home for a per-member `Failure`.

Shipped in phase 25: a `Checkpoint` of the current status, the
record, and the completed step IDs; a pause rule keyed on context
cancellation; and `Resume`, which restarts a walk from a stored
checkpoint. Persistence stays a caller concern: `flow` reports a
checkpoint through a hook and never writes storage itself.

Outside: compensation, scheduling, and history replay. A future
version adds these only when that consumer asks. Phase 30 shipped a
bounded, in-process retry of a single step's `Fire` call; see the
Phase 30 subsection below. A caller who needs a run-level retry still calls `Resume` again
on the same checkpoint after a step failure. A caller schedules a
resume from a cron job, a queue, or a webhook, since `Resume` is a
plain resumable function call. History
replay is rejected: a caller who persists every checkpoint already
holds a replayable log, and `flow` does not build an event log; event
sourcing is the wrong tool for a need that asks for current state, not
a log. Compensation has no named
caller yet; adding it now is speculative generality. Phase 38 shipped
a loop-driving change: repeated invocation of a step's `Sub` child
workflow, gated by a caller-supplied guard, in place of a graph cycle;
see the Phase 38 subsection below for the full contract, including why
a graph cycle is rejected.
Parallel panels run in goroutines; the runner is in-process, not a
distributed service. Each wave reads the incoming record. Each step in
a wave runs with a copy of that record. The wave collects results
and errors. errors.Join reports failures across the wave. No goroutine
mutates the shared record. The design is correct, not hardened. It
meets the need without overengineering.

## API

Proposed shape, subject to plan review. It follows the DAG scheduler
and step-as-data patterns. See docs/plans/machine.md's "Why build, not
buy" section for the pattern sources.

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
  dependents. `Step` gains `When Admission`. `AdmissionOnFailed`,
  shipped in phase 23, is the third value, for the fallback path: see
  the Scope section above.
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
  `func FailureFrom(ctx context.Context) (Failure, bool)` — shipped in
  phase 23, the failure context a fallback step reads. `Failure.Step`
  names the first failed need in the step's `Needs` declaration order;
  `Failure.Err` is that need's recorded error, wrapped
  `flow: step %q: %w`. `FailureFrom`'s boolean is false outside a
  fallback firing. A wave member whose `Fire` did not itself error but
  whose wave failed still becomes `OutcomeFailed`; a fallback that
  needs such a member receives the joined wave error through
  `Failure.Err`.
- `type Checkpoint struct { Status machine.Status; Record machine.InOut; Done []string; Skipped []string; Failed []string }`
  — shipped in phase 25, extended after ship to add `Skipped`, then
  extended again in phase 23 to add `Failed`, the full resumable state
  of a run. `Done` lists the lexicographically sorted step IDs of every
  `OutcomeSucceeded` entry at the moment the checkpoint is built;
  `Skipped` lists the lexicographically sorted step IDs of every
  `OutcomeSkipped` entry at the same moment; `Failed` lists the
  lexicographically sorted step IDs of every `OutcomeFailed` entry a
  fallback already handled. Neither list's order is a completion
  order. A route exclusion (`flow/routing.go`'s `applyRoute`) or an
  admission skip (`flow/runner.go`'s `nextReadyGroup`) is final
  regardless of the excluding step's later outcome; `Skipped`
  preserves that decision across a pause and a `Resume` the same way
  `Done` preserves a success. A pending, not-yet-resolved fallback's
  bookkeeping does not survive a pause and a `Resume`; only an already
  -caught failure's outcome does, through `Failed`.
  `(Checkpoint).Validate() error` rejects an empty `Status` and a step
  ID named in more than one of `Done`, `Skipped`, and `Failed`;
  `Encode` and `Decode` both call it. `Encode` marshals with
  `encoding/json`; a caller whose `Input` or `Output` must survive the
  round-trip is responsible for JSON-primitive-compatible types or its
  own re-hydration after `Decode`, since `encoding/json` decodes an
  `any` field back to `map[string]interface{}`, never the original
  concrete type. `flow` performs no type-fidelity handling and no
  registry lookup.
- `Run` gains a trailing `onCheckpoint func(Checkpoint)` parameter,
  shipped in phase 25: `Run(ctx, d, m, in, confirm, bus, onCheckpoint) (Report, error)`.
  `onCheckpoint` is nil-safe, matching the existing nil-tolerant `bus`
  parameter, and fires only after a step's or wave's outcome is marked
  `OutcomeSucceeded`, so a checkpoint never captures a step mid-flight.
  A nil `onCheckpoint` skips the call; the loop pays no cost building
  the checkpoint value when the hook is nil. A chained step's inner
  `Run` call passes a nil `onCheckpoint`; a chained step's child
  workflow is not independently resumable, only the parent step's
  completion is captured.
- `func Resume(ctx, d *Definition, m *machine.Definition, checkpoint Checkpoint, confirm Confirm, bus *events.Bus, onCheckpoint func(Checkpoint)) (Report, error)`
  — shipped in phase 25, extended after ship to seed `Skipped` too.
  Seeds `outcomes` from `checkpoint.Done` (every listed ID set to
  `OutcomeSucceeded`) and `checkpoint.Skipped` (every listed ID set to
  `OutcomeSkipped`), `cur` from `checkpoint.Status`, and `rec` from
  `checkpoint.Record`, then continues the same graph walk `Run` uses.
  `Resume` never re-runs a step already in `Done`, and never
  re-evaluates a step already in `Skipped`, since `nextReadyGroup`
  skips any step ID already present in the seeded `outcomes`. `Resume`
  runs five entry checks in order before seeding any state, the first
  failing check returning immediately with no step run: `d` nil, `m`
  nil, `confirm` nil (matching `Run`'s own nil-check order),
  `checkpoint.Validate()` failing, and `checkpoint.Done` or
  `checkpoint.Skipped` naming a step ID absent from `d`'s steps.
  `Resume` performs no topology check across `Done` or `Skipped`
  beyond that: a topologically-inconsistent checkpoint surfaces
  indirectly, when the seeded walk's `pickTransition` or
  `machine.Fire` call fails against a status the walk can no longer
  reach that step from. `Resume` on an all-done checkpoint returns the
  checkpoint's status and record without calling `confirm` or
  `onCheckpoint` again; there is no remaining work.

  A caller pauses a run by canceling `ctx`: at the top of each loop
  iteration, before the next step or wave starts, the loop checks
  `ctx.Err()` and returns the `Report` built so far alongside a
  wrapped pause error when it is non-nil. The check sits between
  steps, not inside one; a step already running keeps running to its
  own completion or failure. The last checkpoint `onCheckpoint`
  delivered is the resume point; `flow` adds no separate pause API.
  `Run` and `Resume` share one internal loop, differing only in how
  they seed `cur`, `rec`, and `outcomes`.

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
Outcomes, phase 22, phase 23, and phase 25 added no import edge.
`Checkpoint` uses only `encoding/json`, which is stdlib. The failure
context travels through `context.Context`, which is stdlib.

### Phase 30 (shipped): a bounded retry of a step's Fire call

This subsection states the shape the plan locks.

A step retries its own `Fire` call a bounded number of times, with
exponential backoff, before the run treats it as failed. The retry
loop wraps only `Fire`. It never wraps `Confirm` or `Route`. Phase
23's fallback stays the outer safety net for a step that exhausts its
retries; `FailureFrom` reads the last attempt's error, unchanged.

Retry state lives only in the call stack of the run in progress. No
ledger field, no checkpoint field, and no cross-run persistence. A
retried step rejects two shapes at `New` time, the same way phase 22
and phase 23 already reject unsafe shapes for `Route` and
`AdmissionOnFailed`: a step with both `Retry` and `Sub` non-nil, and a
retried step named in a panel. A panel wave shares one clock tick
across its members; a per-member retry loop would desynchronize that
shared transition. A chained step's `Fire` call sits behind a whole
child workflow; retrying it would silently re-run that child
workflow.

New exported surface, landing in `api/flow.txt` via `make
api-update`:

- `type RetryPolicy struct { MaxAttempts int; BaseDelay time.Duration; MaxDelay time.Duration; Retryable func(error) bool; Jitter func(time.Duration) time.Duration; Sleep func(context.Context, time.Duration) }`
  — a step's retry rule. `MaxAttempts` counts every attempt, including
  the first; a value of 1 disables retry. `BaseDelay` is the first
  retry's backoff; `MaxDelay` clamps every computed backoff.
  `Retryable`, when non-nil, gates each failure before the next
  attempt; a nil `Retryable` retries every error. `Jitter` and `Sleep`
  are determinism hooks: a nil `Jitter` leaves `NextDelay` fully
  deterministic; a nil `Sleep` uses a context-aware default sleep. A
  test supplies a recording or no-op `Sleep`, so a retry test never
  blocks on real wall-clock time.
- `func (p RetryPolicy) Validate() error` — enforces `MaxAttempts >= 1`
  and `MaxDelay > 0`. `New` calls this for every step whose `Retry` is
  non-nil.
- `func (p RetryPolicy) NextDelay(attempt int) time.Duration` — the
  backoff before the given retry attempt, one-indexed from the first
  retry. Pure: no field mutation, no sleep. Clamps before doubling, so
  `delay` never exceeds `MaxDelay` at the start of a doubling step and
  the multiply never overflows `time.Duration`'s range. `Jitter`, when
  non-nil, applies to the clamped result last.
- `Step` gains `Retry *RetryPolicy`. Nil means no retry, matching
  today's single-attempt behavior exactly.

`New` gains four validations, with pinned messages: a `RetryPolicy`
with `MaxAttempts < 1`; a `RetryPolicy` with `MaxDelay <= 0`; a step
with both `Retry` and `Sub` non-nil; and `Retry` set on a panel
member.

`runSingleton` calls a new unexported helper, `fireWithRetry`,
unconditionally in place of its former direct `fireStep` call. The
helper's signature is
`fireWithRetry(ctx, m, cur, rec, step, row) (machine.Status, machine.InOut, error)`,
matching `fireStep`'s argument and return shape. A nil `step.Retry`
calls `fireStep` once and returns its result unchanged, matching the
no-retry behavior exactly. The loop wraps `fireStep`, not `m.Fire`
directly: `fireStep` tags a `Fire` error `failureKindFire`, and
`resolveCatchable` requires that tag to route an exhausted retry into
a declared `AdmissionOnFailed` fallback. On a `fireStep` error the
loop checks the completed attempt count against `MaxAttempts` before
it checks `Retryable`, so a `MaxAttempts` of 1 never calls `Retryable`
or `Sleep`. A canceled `ctx`, during `Fire` or during a pending
`Sleep`, aborts the loop at once with the context error, the same way
a canceled `ctx` aborts `Fire` today. A retry that succeeds continues
exactly like today's single-attempt success; `Report` records no
attempt count. A step that exhausts its retries reports
`OutcomeFailed` with the last attempt's error, and phase 23's fallback
admission applies unchanged.

`policy/layers.json` gains no new row and no changed row for this
phase. `flow`'s existing row, `["events", "machine"]`, already covers
every edge this phase needs; `time` and `context` are standard-library
imports inside `flow` itself, not new internal package edges.

### Phase 38 (shipped): loop-driving a step's Sub

This subsection states the shape the plan locks, including the
reasoning against a graph cycle.

A step runs its `Sub` child workflow more than once, gated by a
caller-supplied guard, before the step's own transition and `Confirm`
fire. `Max`, the iteration cap, defaults to zero, meaning unbounded:
the loop runs until the guard clears or `ctx` is canceled or expires.
The loop reuses `Step.Sub`, the chaining mechanism phase 7 shipped, so
`flow.Definition` stays a DAG. `New`'s cycle rejection in
`flow/validate.go` is unchanged.

The loop-driving algorithm lives in a new file, `flow/loop.go`,
mirroring how phase 30 put `fireWithRetry` in `flow/retry.go`; neither
`flow/runner.go` nor `runSingleton` grows the algorithm inline.
`flow/runner.go` sits at the 500-line structure-gate cap before this
phase lands, with no slack for a net addition, so this phase relocates
`runChild` (today's lines 244-253 of `flow/runner.go`, roughly 11
lines including its doc comment and the following blank line) into
`flow/loop.go`, unchanged in body; `fireFromChild` stays in
`flow/runner.go`, since both the looped and the non-looped Sub path
still call it. `runSingleton`'s Sub branch then gains one `if
step.Loop != nil { ... } else { ... }` dispatch, roughly 6 to 8 lines,
around the existing `runChild`-then-`fireFromChild` call: the `else`
branch calls the relocated `runChild` then `fireFromChild`, unchanged;
the `if` branch calls `flow/loop.go`'s `runLoopedChild`. `runner.go`'s
net line count drops, landing under the 500-line cap with headroom.
After this phase, `runSingleton` proceeds through its existing,
unchanged `confirmStep` and `emitStep` calls in both branches.
`runLoopedChild` owns the iteration bookkeeping: the `ctx.Err()`
check, the `Max` cap, and the `Guard` evaluation. It delegates each
iteration's child-workflow run to a second, explicitly named function,
`runLoopChild`, the loop-aware variant of `runChild`: same
`(machine.Status, error)` return shape, but taking the iteration's
starting `machine.InOut` record as an extra argument, in place of
`runChild`'s hardcoded fresh `machine.InOut{}`. Naming `runLoopChild`
separately from `runLoopedChild` mirrors how `runChild` and
`fireFromChild` are already two separate functions today, and keeps
`runLoopedChild` a short driver rather than one long function inlining
the child run, the ctx check, the `fireFromChild` call, the
`LoopState` construction, and the `Max`/`Guard` evaluation together;
each of the three new functions in `flow/loop.go` stays at or below
the 80-line function cap on its own. Each iteration runs the child
workflow from the previous iteration's output record — except the
first iteration, which threads the parent step's own incoming record,
new behavior this phase introduces only for looped steps; a
non-looped Sub step is unaffected and keeps starting its child from a
fresh `machine.InOut{}`, exactly as `flow/loop.go`'s relocated
`runChild` does today, since `runSingleton`'s `else` branch still
calls `runChild`, not `runLoopChild`. Each iteration then fires the
parent's transition through the existing `fireFromChild` logic and
evaluates `LoopPolicy.Guard` with a `LoopState` injected into `ctx`.
The `ctx.Err()` check runs before every iteration, including the
first, so the loop is not an unconditional do-while: a `ctx` already
canceled at `Run`'s entry stops it before any child workflow runs. A
`ctx` error or a `Guard` error stops the loop and fails the step; a
`false` `Guard` result stops the loop as a normal, successful exit;
`Max` reached, when `Max` is non-zero, stops the loop before the next
`Guard` call. Once `runLoopedChild` returns success,
`runSingleton` calls `Confirm` once for the whole step, matching a
non-looped chained step's single `Confirm` call.

New exported surface, landing in `api/flow.txt` via `make
api-update`:

- `type LoopPolicy struct { Guard machine.Guard; Max int }` — a
  step's loop rule. `Guard` reuses `machine.Guard`'s exact type,
  `func(ctx context.Context) (bool, error)`; a nil `Guard` means
  "always continue," matching `machine`'s own nil convention. `Max`
  caps the iteration count; zero means unbounded, bounded only by the
  caller's own `ctx`. A negative `Max` is invalid.
- `func (p LoopPolicy) Validate() error` — rejects `Max < 0` with the
  pinned message `flow: loop: max must be at least 0`. `Validate` has
  no step ID to report, so its message names no step. A shared
  unexported helper, `loopValidateMessage`, returns the unprefixed
  text both `Validate` and `New`'s step-scoped check build their
  message from, the same way phase 30's `retryValidateMessage` backs
  both `RetryPolicy.Validate` and `validateRetry` in `flow/retry.go`.
- `type LoopState struct { Iteration int; Record machine.InOut }` —
  the loop context a `Guard` closure reads. `Iteration` counts
  completed iterations, starting at zero before the first `Guard`
  call. `Record` carries the most recent child workflow's output.
- `func LoopStateFrom(ctx context.Context) (LoopState, bool)` — reads
  the `LoopState` `runSingleton` injects before each `Guard` call. The
  boolean is false outside a loop step's `Guard` evaluation, matching
  `FailureFrom`'s shape.
- `Step` gains `Loop *LoopPolicy`. Nil means no loop, matching a plain
  chained step's single-run behavior exactly.

`New` gains three validations, with pinned messages: a `LoopPolicy`
with `Max < 0` (`flow: step %q loop: max must be at least 0`, built by
wrapping `loopValidateMessage`'s unprefixed text with the step's ID);
`Loop` set on a step whose `Sub` is nil (`flow: step %q has a loop
policy but no sub-workflow`); and `Loop` set on a panel member (`flow:
panel %d names looped step %q`). `Loop` requires a non-nil `Sub`, so
`Loop` and `Retry` are already mutually exclusive through phase 30's
existing Sub-versus-Retry rule; this phase adds no new check for that
combination.

`policy/layers.json` gains no new row and no changed row for this
phase. `flow`'s existing row, `["events", "machine"]`, already covers
every edge this phase needs.

### Phase 48 (shipped): run-time payloads, graph accessors, and derived transitions

Phase 48 widens `flow` in three ways. `Step.PayloadFrom` derives a
step's payload from the live record at run time. `Definition.Steps`
and `Definition.Panels` expose copied graph views. `TransitionsFor`
derives a machine transition table from a definition.

`PayloadFrom func(rec machine.InOut) string` resolves immediately
before each gated `Confirm` call, against the record that transition
produced. The resolved value rides the `Step` copy handed to
`Confirm`. `New` rejects a step that sets both `Payload` and
`PayloadFrom`, and a `PayloadFrom` on a member of a panel of two or
more members. A nil `PayloadFrom` keeps the prior behavior exactly.
The field never crosses the wire and never enters a checkpoint.

`Steps` returns a deep copy of the step graph, recursion into `Sub`
definitions included. `Panels` returns a copy of the panel list.
Both follow `Roots`: value receivers, fresh backing arrays, and no
way for a caller to mutate the stored graph.

`TransitionsFor(d *Definition, initial machine.Status)`
`([]machine.Transition, error)` derives the transition rows a walk of
`d` needs. Each plain step contributes one row per predecessor
status, with the step ID as trigger. A root's predecessor set is
`{initial}`. A need contributes its effective final statuses: its
`To` when it has no `Sub`, its child graph's final statuses
otherwise. A chained step, looped or not, targets its child's final
statuses; its own `To` stays unused. A panel of two or more members
contributes one shared row per predecessor, triggered by the first
member's ID. A fallback step also gains each failed need's
predecessor statuses, because a failed `Fire` leaves the pre-fire
status.

`TransitionsFor` rejects a nil definition, an empty initial status,
and a step that needs a target status but carries none. It rejects
two derived rows that share one `From` and `To`, because
`pickTransition` matches on `To` alone. It rejects a self loop. The
derived table models the declared happy path; route-excluded paths
stay the caller's own rows.

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

The fallback tests, shipped in phase 23, live in `flow/flow_test/`:

- `fallback_test.go` — red-green cases: `New` rejects an
  `AdmissionOnFailed` root and an `AdmissionOnFailed` panel member,
  each with a pinned message; a failed step with a fallback lets the
  run complete, and `FailureFrom` inside the fallback returns the
  failed step's ID and a wrapped error satisfying `errors.Is`; a
  fallback with two failed needs receives the first in `Needs`
  declaration order; a fallback needing an error-free wave member
  receives the joined wave error; `FailureFrom` inside a happy-path
  step returns false; a failed step with no fallback still aborts; an
  `AdmissionOnFailed` step whose needs all succeeded becomes
  `OutcomeSkipped`, and a happy-path dependent of a handled failure
  does too; a branch step that leaves a handled failure's sole handler
  unchosen aborts with the recorded error; a `Confirm` rejection
  aborts even when a fallback exists; a wave failure with a fallback
  for every failed member continues, but aborts when only one failed
  member has one; a panel's shared, pre-spawn `pickTransition` failure
  aborts uncatchably even when every member has a fallback declared; a
  chained step's own nested-`Run` failure aborts uncatchably at the
  parent level, even with a parent-level fallback declared for it; a
  `Route` error on a branch step with a fallback continues down the
  fallback path; a fallback with mixed needs (one failed, one
  succeeded) still admits, pinning the any-of rule; a fallback step's
  own `Fire` failure is itself catchable by a second, nested fallback;
  an `AdmissionOnFailed` step that is also a `Route` branch step works
  exactly as phase 22 describes; two independent failed steps each
  keep their own pending-handler set, so skipping the last handler of
  one never touches the other's still-pending handler.
- `fallback_integration_test.go` — a graph end to end with a rejecting
  guard, a fallback that records its `Failure`, and a final join;
  asserts the report outcomes, the final status, and that the fallback
  read the failed step's ID. Runs the confirm-rejection abort case and
  a panel failure case under the race detector.
- `fallback_bench_test.go` — benchmarks the failure-plus-fallback path
  against the all-success path on the same graph and reports the
  ratio, with no fixed allocation budget, since error wrapping and the
  context injection vary with the wave's goroutine count.

The checkpoint tests, shipped in phase 25, live in `flow/flow_test/`:

- `checkpoint_test.go` — red-green cases: `Checkpoint.Validate` rejects
  an empty `Status`, and rejects a step ID named in both `Done` and
  `Skipped`; `Encode` then `Decode` round-trips `Status`, `Record`,
  `Done`, and `Skipped`, and a decoded `Record.Input` comes back as
  `map[string]interface{}`, not the original struct type; `Decode`
  rejects malformed JSON and runs `Validate` on the parsed result; a
  zero-step `Definition` with a non-nil `onCheckpoint` never calls it;
  `onCheckpoint` fires once per singleton step and once per wave, with
  `Done` holding exactly the sorted IDs completed so far (a
  non-alphabetical fixture proves the sort, not completion order); a
  nil `onCheckpoint` behaves exactly as before the phase; `Run` returns
  the pinned pause error when `ctx` is already canceled, and again
  mid-graph after at least one checkpoint fired; `Resume` seeds
  `outcomes`, `cur`, and `rec` from a mid-graph checkpoint and reaches
  the same final `Report` an uninterrupted `Run` would; `Resume` on an
  all-done checkpoint, including the one-step short-circuit case,
  returns the checkpoint's status and record and calls neither
  `confirm` nor `onCheckpoint`; `Resume` rejects a nil `d`, `m`, or
  `confirm`, in that order, then an invalid checkpoint, then a `Done`
  or a `Skipped` entry naming a step absent from `d`, matching the
  five-check entry order; a `Done` entry naming a real step whose own
  `Needs` entry is absent from `Done` surfaces as an error through the
  seeded walk's own transition check, not a dedicated `Resume` check.
- `checkpoint_skip_resume_test.go` — closes the gap where a route
  exclusion or an admission skip, once dropped from a checkpoint, came
  back to life on `Resume`. A three-step graph pauses right after the
  branch step's checkpoint fires and resumes: the excluded step stays
  `OutcomeSkipped` and never runs, matching an uninterrupted `Run`. A
  five-step chain repeats the case for an admission-only skip that
  cascades from a route exclusion two hops away, through
  `nextReadyGroup` rather than `applyRoute`.
- `checkpoint_integration_test.go` — a multi-step graph end to end with
  a real `onCheckpoint` that appends `Encode`d bytes to an in-memory
  slice, standing in for caller-owned storage. Cancel `ctx` after the
  first checkpoint lands, decode the last stored checkpoint, and call
  `Resume`; assert the resumed run reaches the same final `Report` a
  plain, uninterrupted `Run` reaches, and that the step before the
  pause point runs exactly once. Repeats the pause-and-resume sequence
  across a wave boundary. A chained-step case captures a checkpoint
  right after the chained step's parent transition fires, cancels,
  resumes, and asserts the child's `confirm` closure is not invoked
  again and the chained step's ID appears once in `Done`.
- `checkpoint_bench_test.go` — benchmarks `Run` with a non-nil
  `onCheckpoint` against a nil one, on the same graph the chaining
  benchmark uses, and reports the allocs/op ratio rather than a fixed
  budget, since goroutine and closure overhead vary.

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

### Phase 30 (shipped) tests

Test files live in `flow/flow_test/`:

- `retry_test.go` — `RetryPolicy.Validate` rejects a `MaxAttempts`
  below 1 and a non-positive `MaxDelay`, each with its pinned message.
  `RetryPolicy.NextDelay` returns `BaseDelay` for attempt 1, doubles
  per later attempt, clamps at `MaxDelay`, and applies a non-nil
  `Jitter` last. A large-attempt case proves the clamp-before-double
  order never overflows `time.Duration`, where a naive
  multiply-then-clamp implementation would wrap to a negative value.
  `New` rejects a retried step with a sub-workflow and a retried panel
  member, each with its pinned message, and accepts a retried step
  with neither shape. A step whose guard fails twice then succeeds,
  under `MaxAttempts` of 3, ends `OutcomeSucceeded`; the guard,
  `OnExit`, and `OnEntry` each ran exactly three times, and the
  recording `Sleep` ran exactly twice. A step whose guard always
  fails, under `MaxAttempts` of 3, ends `OutcomeFailed` after exactly
  three guard calls. A `Retryable` predicate returning false stops the
  loop after the first failure. A step with `Retry` nil keeps today's
  single-attempt behavior. A `ctx` canceled during a pending `Sleep`
  call aborts the loop with the context error, short of `MaxAttempts`.
  The default `Sleep` returns before its full duration when its `ctx`
  cancels mid-wait. A step that exhausts its retries with a phase 23
  fallback declared continues down the fallback path, and
  `FailureFrom` returns the last attempt's error.
- `retry_integration_test.go` — a three-step linear graph where the
  middle step's guard fails twice then succeeds, under `MaxAttempts`
  of 3 and a recording `Sleep`; asserts every step's outcome, the
  final status and record, and the recorded sleep durations in order.
  A second case repeats the graph with a guard that never succeeds and
  a fallback step; asserts the fallback's `Failure.Err` wraps the last
  guard error.
- `retry_bench_test.go` — benchmarks a single-step run whose guard
  always succeeds on the first attempt, with a `RetryPolicy` present
  but never triggered, against the same step with `Retry` nil.
  Measures the `Retry`-nil baseline on the phase 23 code, before this
  phase lands, and records both in the file's leading comment.

### Phase 38 (shipped) tests

Test files land in `flow/flow_test/`:

- `loop_test.go` — red-green cases: `LoopPolicy.Validate` rejects a
  negative `Max`, pinned message, and accepts `Max` zero and one;
  `New` rejects a looped step with a nil `Sub` and a looped panel
  member, each with its pinned message, and accepts a looped step
  with a non-nil `Sub` and no panel; a loop step whose `Guard` returns
  false on the second call runs its child workflow exactly twice and
  ends `OutcomeSucceeded`, with `LoopStateFrom` inside the second
  `Guard` call reporting `Iteration` one and the first iteration's
  output record; a loop step with `Max` two and a `Guard` that always
  returns true stops after two iterations without a third `Guard`
  call; a loop step with `Max` zero and a `Guard` that returns false
  on the fifth call runs exactly five iterations; a loop step with
  `Max` zero, a `Guard` that always returns true, and a `ctx` canceled
  after the third iteration's child workflow completes stops at the
  next `ctx.Err()` check and ends `OutcomeFailed` wrapping
  `context.Canceled`; a loop step whose `Guard` errors on its first
  call stops after one iteration and ends `OutcomeFailed` wrapping
  that error; a loop step's second iteration receives the first
  iteration's output record as its input; `LoopStateFrom` outside any
  loop step's `Guard` call returns false and a zero `LoopState`; a
  loop step whose `ctx` is already canceled at `Run`'s entry ends
  `OutcomeFailed` after zero child workflow runs; a loop step that
  exhausts `ctx` with a phase 23 fallback declared continues down the
  fallback path, and `FailureFrom` inside the fallback returns the
  context error.
- `loop_integration_test.go` — a two-status graph where a looped
  step's child workflow moves a counter in `Record.Output` by one per
  iteration, and `Guard` reads `LoopStateFrom` to stop once the
  counter reaches three; asserts the final record, the final status,
  and `Iteration` at each `Guard` call. A second case sets `Max` zero
  with a short `ctx` deadline and asserts the run aborts once the
  deadline passes, with no `Max`-driven stop. A third case combines a
  loop step with a phase 23 fallback catching the deadline failure.
- `loop_bench_test.go` — benchmarks a ten-iteration loop step, with a
  `Guard` that always returns true until `Max`, against ten separate
  non-looped chained steps performing the same child workflow.
  Measures the ten-separate-steps baseline on the currently shipped
  code (through phase 30), before this phase lands, and records both
  in the file's leading comment; reports the ns/op and allocs/op
  ratio, since `LoopState` construction per iteration varies the
  allocation count.

### Phase 48 (shipped) tests

`flow/flow_test/payload_test.go` holds the red-green cases for
`PayloadFrom`. `New` rejects both fields set, admits the func with
an empty static payload, and rejects the func on a two-member panel
member. The resolved value reaches `Confirm`; a step without the
func behaves byte for byte as before. A step reads its own
transition's output. A chain carries one step's output into the
next step's `PayloadFrom`. A child resolves against a fresh record.
A looped child resolves per iteration. A fallback resolves against
the failure context's record. A resumed step re-resolves; a done
step never resolves again. A one-member panel member resolves like
any gated step.

`flow/flow_test/payload_integration_test.go` runs one three-step
chain end to end through a real machine.

`flow/flow_test/graph_accessors_test.go` proves the copies. Mutating
a returned step, need, or panel never changes the stored
definition. `Sub` graphs and their steps copy recursively.

`flow/flow_test/derive_test.go` drives `TransitionsFor` by table. A
chain derives one row per step. A diamond merge derives two rows
for the joining step. A panel wave derives one shared row. A
chained step targets its child's finals, not its own `To`. A
fallback gains the failed need's predecessors. The rejection cases
cover a nil definition, an empty initial, a missing `To`, a
`From`-`To` collision, and a self loop.

`flow/flow_test/derive_integration_test.go` builds a graph, derives
its rows, builds the machine from them, and walks it to completion.
No hand-written transition row survives in the test.

## Verification

`make verify`. Conformance vectors for the definition form. The
rationale lives in docs/plans/machine.md's "Why build, not buy"
section. `api/flow.txt` lands via make api-update. Phase 22 extended
`api/flow.txt` with
`Admission`, its two constants, the `Route` type, and the two new
`Step` fields. Phase 23 extended it with `AdmissionOnFailed`, the
`Failure` type, `FailureFrom`, and the `Failed` field on `Checkpoint`;
every helper `flow/failure.go` defines to implement the continue rule
stays unexported and does not appear in the lock. Phase 25 extended it
with `Checkpoint`, its `Validate`, `Encode`, and `Decode`, `Resume`,
and the changed seven-argument `Run` signature. Phase 22, phase 23,
and phase 25 each left `api/machine.txt` and
`policy/layers.json` unchanged.

No conformance-vector change from phase 22, phase 23, or phase 25:
`Checkpoint` and `Failure` carry no signed or threaded wire form, so
`envelope/testdata/vectors/` and `docs/architecture.md` stay
untouched.

### Phase 30 (shipped) verification

`make verify` passes, and the `flow` coverage floor holds. `api/flow.txt`
gains `RetryPolicy`, its `Validate` and `NextDelay` methods, and
`Step.Retry`, via `make api-update`; commit the `api/` diff in the
same change. `policy/layers.json` stays unchanged: `flow`'s allowed
imports, `["events", "machine"]`, already cover every edge this phase
needs. `api/machine.txt` stays unchanged; the `machine` package is
untouched. `docs/architecture.md` and `docs/packages/flow.md` update
their flow sections in the same change as the code, describing
`RetryPolicy`, `NextDelay`, and the retry loop's place between a
`Fire` failure and the phase 23 fallback admission. `AGENTS.md`
updates its `flow/` layout bullet in the same change, naming the retry
vocabulary next to outcome, admission, route, and fallback. No
conformance-vector change: `RetryPolicy` carries no signed or threaded
wire form.

### Phase 38 (shipped) verification

`make verify` passes, and the `flow` coverage floor holds. `flow/runner.go`
sits at the 500-line cap before this phase lands; relocating `runChild`
into `flow/loop.go` frees more lines than the new dispatch conditional
in `runSingleton` adds, so `flow/runner.go` lands under the cap
afterward. `flow/loop.go` holds `LoopPolicy`, `LoopState`,
`LoopStateFrom`, `loopValidateMessage`, the relocated `runChild`,
`runLoopChild`, and `runLoopedChild`, well under the 500-line file
cap. Every function in both files, including `runLoopChild` and
`runLoopedChild` as two separate, explicitly named functions, stays at
or below the 80-line function cap. `scripts/check_structure.py`
enforces both caps.
`api/flow.txt` gains `LoopPolicy`, its `Validate` method, `LoopState`,
`LoopStateFrom`, and `Step.Loop`, via `make api-update`; commit the
`api/` diff in the same change. `policy/layers.json` stays unchanged:
`flow`'s allowed imports, `["events", "machine"]`, already cover every
edge this phase needs. `api/machine.txt` stays unchanged; the
`machine` package is untouched. `docs/architecture.md` and
`docs/packages/flow.md` update their flow sections in the same change
as the code, describing `LoopPolicy`, `LoopState`, and the loop
driver's place between a chained step's single run and the phase 30
retry loop, and the unbounded-by-default contract. `AGENTS.md` updates
its `flow/` layout bullet in the same change, naming the loop
vocabulary next to outcome, admission, route, fallback, and retry. No
conformance-vector change: `LoopPolicy` carries no signed or threaded
wire form.

### Phase 48 (shipped) verification

`make verify` passes, and the `flow` coverage floor holds.
`api/flow.txt` gains `Step.PayloadFrom`, `Definition.Steps`,
`Definition.Panels`, and `TransitionsFor`, via `make api-update`;
the `api/` diff is committed in the same change.
`policy/layers.json` stays unchanged: `flow`'s allowed imports
already cover every edge. `docs/packages/flow.md` and
`docs/architecture.md` describe the run-time resolution, the
accessors, and the derivation helper in the same change as the code.
`AGENTS.md` updates its `flow/` layout bullet. No conformance-vector
change: none of the three additions carries a signed or threaded
wire form.
