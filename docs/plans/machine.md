# Plan: machine

Status: shipped through phases 1 through 3. The build phases live in
docs/plans/agents/. See phases 1 through 3.

## Goal

Own status mechanics for one record. A definition lists statuses,
transitions, gates, and input and output bindings. A transition fires
only when its gate passes. Entry and exit actions run on the move.

## Scope

Inside: typed statuses, transitions, gates, inputs, outputs, entry
actions, and exit actions. A definition has a Validate method that
checks every listed transition. A transition moves one record from a
status to a status and passes an input and an output. The machine is
data-driven and serializable.

Outside: the step graph, panels, parallel execution, scheduling, and
chaining. The flow package owns those concerns. The machine never
schedules. It never knows a graph. It stays reusable on its own.

## Why build, not buy

No Go library covers a typed state machine plus a step runner as one
small, generic, stdlib-only construct. The ecosystem splits into two
camps: simple but narrow flat finite-state machines (`looplab/fsm`,
`cocoonspace/fsm`), and capable but heavy distributed workflow
engines (Temporal, Cadence, `cschleiden/go-workflows`,
`luno/workflow`) that need a server, a database, or persistence and
eventing adapters. This repo's one hard rule, standard library only,
rules out the heavy camp outright.

Two libraries came closest. `qmuntal/stateless` covers states,
triggers, guards, and entry and exit actions, the richest coverage of
any candidate, and is stdlib-only in its own imports. It requires a
newer Go than this repo targets, uses reflection for parameterized
triggers (a type mismatch panics), and has no step model: it is a
state machine, not a workflow step engine. `luno/workflow` gives
steps with a from-status, a worker, and a to-status, but needs store
and stream backends for its production path, violating the
stdlib-only rule.

Adopting a foreign library still means wrapping it: the wrapper is
code this SDK writes and maintains anyway, plus the foreign surface's
own learning cost and version churn. A typed state machine with
guards and step metadata is small; `envelope` already models states
as typed enums with `Validate` methods, so the skill was already in
house. The requirement is a specification, not a download: a state
machine with steps, gates, inputs, outputs, and statuses is a
definition language, and building it in house keeps the shape and the
semantics explicit.

The decision splits the concern into two packages, following the rule
that one package owns one concern: `machine` owns status mechanics
(typed statuses, gates, inputs, outputs, entry and exit actions, no
graph, no scheduler), and `flow` owns step scheduling (a step graph,
panels, parallel execution, chaining), composing `machine` for each
step's status transitions. See docs/plans/flow.md.

Every shipped requirement maps to a published, simple pattern, none
of which drags in a framework. The action model (a transition table
row: from, trigger, guard, to, on-exit, on-entry) matches XState's and
PyTransitions' shape. Kahn's algorithm sorts the step graph in
topological order in about twenty lines and gives parallel panels for
free: a wave is the set of nodes with no remaining dependencies, and
the same pass detects cycles. Stdlib parallel execution puts tasks in
goroutines, gathers results with a `WaitGroup` and a buffered channel,
and combines errors with `errors.Join` (stdlib since Go 1.20); this
keeps `flow` dependency-free with no `errgroup` import. Chaining is
function composition: a step is a callable that takes an input and
returns an output, and a sub-workflow is a step that calls a nested
workflow and returns its result.

The strongest counter-argument: a naive team builds a worse engine
than a mature library, and DAG scheduling plus parallel panels are
real complexity. The mitigation is a hard layer boundary: `machine`
never schedules, `flow` never knows a status count, and each stays
replaceable and testable on its own.

## API

Proposed shape, subject to plan review. It follows the action model
pattern above.

- `type Status string` as the typed status enum base.
- `type Trigger string` as the label that selects a transition.
- `type InOut struct { Input any; Output any }` holding the input
  record and the output record. A bound function reads Input and
  writes Output. The caller type-asserts concrete payloads.
- `type Action func(ctx context.Context, rec *InOut) error` as an entry
  or exit action. The action writes the output record through `rec`.
- `type Guard func(ctx Context) (bool, error)` as a transition guard.
- `type Transition struct { From, To Status; Trigger Trigger; Guard Guard; OnEntry Action; OnExit Action }`
  as a table row. Phase 1 omits `OnEntry` and `OnExit`; the struct
  grows in Phase 2.
- `type Definition struct` with the unexported fields `initial Status`
  and `transitions []Transition`. Callers read them through `Initial`
  and `Transitions`.
- `(d Definition).Initial() Status` reads the initial status.
- `(d Definition).Transitions() []Transition` returns a copy of the
  transition table. The value receiver also serves non-addressable
  values, such as the `Decode` result in Phase 3.
- `(d Definition).AllowedTransitions(from Status) []Transition` returns
  all transitions whose From matches the argument. Returns an empty
  slice when no transitions match. The returned slice is a fresh copy.
- `(d Definition).AllowedTriggers(from Status) []Trigger` returns the
  distinct triggers available from the given status. Returns an empty
  slice when no transitions match.

A `Definition` is immutable after `New`. The unexported fields make
the invariant enforced, not caller-honored. `Transitions` returns a
copy, so a caller cannot mutate the internal table. The `Definition`
doc comment states the enforced invariant.

- `New(initial Status, ts ...Transition) (*Definition, error)` to
  build a definition and reject a bad shape.
- `(*Definition).Fire(ctx, from Status, trig Trigger, in InOut) (Status, InOut, error)`
  to move a record when the guard passes. The output record fills the
  returned InOut. Fire wraps `ErrNoTransition` when no row matches and
  `ErrGuardRejected` when the row's guard returns false. Test both
  with `errors.Is`.
- `(*Definition).Validate() error` on the transitions. It stays
  exported. Phase 3 wire decode calls it. It still rejects an empty
  zero-value `Definition`.

Fire resolves the row by from and trigger. It runs the guard, then the
exit action, then the entry action. Dispatch is a scan over the
transition list, not reflection. A trigger that does not match returns
an error. OnExit does not run when the guard fails. A nil Guard or a
nil Action is checked, never invoked.

Both row accessors allocate exactly one slice per call. The first
pass counts matching rows; the second pass fills one slice of that
exact size.

Phase 18 leaves `machine`'s dispatch logic untouched. The move emit
happens at the call site, not inside `Fire`. A caller reads the move
from the `Fire` return value and emits onto a caller-owned bus. The
package defines the typed event name `MoveEvent`, so it imports the
events package for the `events.Name` type. The `machine` row in
policy/layers.json lists `events`. Phase 18 proves this wiring with an
external test package. The flow package imports machine.

## Tests

Table-driven transition tests for each gate path. A gate that fails
blocks the move. Entry and exit actions run in order. Round-trip of a
definition through the wire form. A bad transition shape fails `New`.
Semgrep proves the machine uses no reflection.

Definition tests construct via `machine.New`. External code cannot
build a `Definition` directly. `TestNewRejects` covers the reject
cases. `TestNewAccepts` covers the accept cases. The two names replace
`TestValidateRejects` and `TestValidateAccepts`. `TestNew` folds into
the two tables. Its empty-list case lands in `TestNewRejects`. Its
valid-list case lands in `TestNewAccepts`. The reject cases cover the
empty list, a self loop, and an unreachable `From`. They cover a
duplicate `From` and `Trigger` pair. `TestNewAccepts` covers a nil
`Guard` and a valid table.

`TestValidateRejectsZeroValue` calls `Validate` on a zero-value
`Definition`. It asserts the "must not be empty" error. This path is
reachable only through a zero-value `Definition`. `New` returns early
on an empty list, so it never reaches `Validate`'s empty-list branch.
`TestDefinitionFields` reads the state through `Initial` and
`Transitions`. `TestNewCopiesInputSlice` writes one element of the
caller's slice after `New`. It sets `ts[0].To = "evil"`. Then `Fire`
still lands on the original target. Appending to the caller's slice
cannot change the internal table. `TestTransitionsReturnsCopy` writes
one element of the returned slice. It sets `copy[0].To = "evil"`. A
second `Transitions` read returns the original `To`. No test covered
`New`'s validation-error path before.

## Verification

`make verify`. Conformance vectors for the definition form. The
rationale lives in the "Why build, not buy" section above.
`api/machine.txt` lands via make api-update. The lock update drops
the exported fields and adds the two methods.

## Addendum: wire surface removal

Status: shipped. This addendum records the removal decision.

### Goal

Remove the exported JSON wire surface from `machine`. The repo rule
is no abstraction without a caller. The wire surface has zero
production callers. The JSON schema already lives in two places:
`machine/wire.go` and runconfig's private `buildMachine` decode. This
change leaves runconfig's decode as the one wire-schema consumer.

### Evidence

Grep for `machine.Encode`, `machine.Decode`, `machine.NewRegistry`,
and `machine.Registry` across the tree. Production callers: none.
Consumers live only in `machine/machine_test/` and in comments
(`flow/wire.go`, `flow/flow_test/checkpoint_fuzz_test.go`,
`docs/plans/agentrun.md`).

`Encode` is unusable on definitions built through `machine.New`.
`wireName` rejects a bound function that carries no recorded wire
name. `machine.New` never records names. The machine wire tests
decode before they encode, so they never hit this path.

`Decode` returns `Definition` by value while `Fire` and `Validate`
use pointer receivers. `machine.Decode(...).Fire(...)` does not
compile. The removal deletes this mixed-receiver inconsistency.

runconfig never imports a machine wire symbol. It calls
`machine.New` and `machine.Transition` in `runconfig/loader.go`'s
`buildMachine`. runconfig stays untouched.

### Removal list

Delete `machine/wire.go` in full. The lock diff removes four entries
from `api/machine.txt`:

- `(d *Definition).Encode(reg Registry) ([]byte, error)`
- `Decode(data []byte, reg Registry) (Definition, error)`
- `NewRegistry() (Registry)`
- `type Registry struct`

Delete the `names []transName` field from `Definition` in
`machine/definition.go`. Only `Decode` wrote it and only `Encode` read
it. Drop the `names` sentence from the `Definition` doc comment.

### Keep list

Keep every state-model symbol: `Status`, `Trigger`, `Guard`, `Action`,
`Transition`, `InOut`, `Definition`, `New`, `Initial`, `Transitions`,
`AllowedTransitions`, `AllowedTriggers`, `Validate`, `Fire`.

Keep `MoveEvent`. It is not part of the wire surface. Consumers:
`agent/agent_test/system_fixture_test.go` subscribes to it,
`events/events_test/cross_subsystem_integration_test.go` subscribes
and emits it, and `docs/examples/events-bus.md` uses it. Its constant
stays in `machine/events.go`. The `events` import stays.

### Code outside machine

No package code outside `machine` changes. Two comment-only
touch-ups are allowed in the same commit: `flow/wire.go`'s `Decode`
comment mirrors `machine.Decode`'s shape, and
`flow/flow_test/checkpoint_fuzz_test.go` names `FuzzDecode`. Update
or trim those references. No behavior change anywhere.

### Tests

Delete five wire test files from `machine/machine_test/`:
`wire_test.go`, `wire_integration_test.go`, `wire_bench_test.go`,
`conformance_test.go`, and `fuzz_test.go`. They test removed API.
This is API removal, not test weakening. Delete
`machine/testdata/vectors/` too. Only the deleted tests read those
vectors. `scripts/check_test_tampering.py` will flag the deletions.
The `Allow-Test-Change` commit trailer is the waiver.

No new tests. The surviving tests prove the remaining surface:
`go test ./machine/...` stays green after removal. Behavior outside
the deleted test files is unchanged. This is a pure surface removal.

### API lock

Run `make api-update`. Commit the `api/machine.txt` diff in the same
change. The diff holds deletions only. No other package lock changes.
`policy/layers.json` needs no edit: the removal deletes an import
(`encoding/json` is stdlib) and keeps the `events` edge.

### Docs, same change

- `docs/architecture.md`: the `machine/` bullet lists `Encode`,
  `Decode`, `Registry`, `NewRegistry`, and `MoveEvent`. Drop the wire
  names. Keep `MoveEvent`.
- `docs/packages/machine.md`: delete the wire sections and the
  `Registry`, `NewRegistry`, `Encode`, and `Decode` entries.
- `docs/README.md`: the machine blurb reads "the status model, the
  move dispatch, and the JSON wire form". Drop "and the JSON wire
  form" from that one line.
- `docs/plans/agentrun.md`: lines 186, 220, and 226 cite
  `machine.Decode` in present tense. Rewrite those passages without
  the machine half of the analogy. `flow.Decode` stays the live
  example. Historical plans are frozen records; this file is treated
  as live because it cites the removed API as current fact.
- `machine/doc.go`: drop the `wire.go` line from the package map.
- `docs/plans/machine.md`: this addendum.

### Verification

Run `make verify`. Coverage: the package now reports through
`coverpkg` at 100 percent with the wire code included. After removal,
`wire.go`'s statements leave the denominator and the numerator
together. The remaining code is covered by the surviving fire and
status tests. The builder must confirm the per-package floor holds:
run `go test -coverpkg=./machine ./machine/machine_test/` and read
the number before reporting. If it lands below 85, stop and
escalate; do not weaken the gate.

## Addendum: Fire gains two sentinels

Part of the maintenance addenda batch. See
docs/plans/agents/maintenance-addenda-batch.md, item 6b.

`Fire` returned two plain errors, so a caller could only match them by
substring. Add `machine/errors.go` with two sentinels:

- `ErrNoTransition = errors.New("machine: no transition")`
- `ErrGuardRejected = errors.New("machine: guard rejected move")`

`machine/definition.go:150` wraps each with `%w` and keeps the same
format verbs. The rendered text is byte-identical to today's text, so
no caller and no doc example changes behavior.

Flow and agentrun build their own similar strings at
`flow/wave.go:104` and `agentrun/matrix.go:332`. Neither is machine's
text, and neither changes. `docs/examples/flow-fallback-admission.md`
embeds machine's rendered text, which is unchanged.

`docs/packages/machine.md:80` names both sentinels.

### Addendum tests

- `machine/machine_test/fire_test.go` moves three `strings.Contains`
  assertions to `errors.Is` against the matching sentinel. The test
  names do not change, and no assertion is dropped.

### Addendum verification

- `make verify` passes. The `machine` coverage floor holds.
- `make api-update` adds `var ErrGuardRejected` and `var
  ErrNoTransition` to `api/machine.txt`. Commit that diff in the same
  change.
- No `policy/layers.json` diff. No conformance vector: this is not
  wire semantics.
