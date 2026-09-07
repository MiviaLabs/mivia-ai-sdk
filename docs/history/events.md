# Plan: events

Status: shipped through phases 17 and 18. This plan fixes
the boundary before any builder starts. It is the bus design contract.
See AGENTS.md's Building blocks section for the block composition
rule. The build phases live in docs/plans/agents/. Phase 17 ships the
leaf core.
Phase 18 proves the machine wiring on a caller-owned bus.

## Goal

React to a package's state change. A package emits a typed event after
a change. A consumer subscribes and runs one callback per event.
The dispatch runs in process, in order, one at a time. The caller owns
the bus it emits onto. The package imports nothing of this module; it
stays a leaf block.

## Scope

Inside: one event type, one subscription set, and a typed dispatch
loop. The event carries a change that the caller already saw in the
return value. The bus is caller-owned; the module has no single shared
bus. Each caller creates a bus and manages its lifetime.

Outside: persistence, replay, a distributed bus, ordering across
rooms, and a general publish-subscribe framework. The envelope
delivery path does not move into this package. Envelope carries no
dispatch; it only names a message's meaning. No envelope code and no
events code imports the other.

## API

The surface below is the lock target. It lands in `api/events.txt` via
make api-update.

- `type Name string` is the typed event kind that separates
  subscriptions. A domain owns its own `Name` constants, so a literal
  never survives in shared code.
- `type Event struct { Name Name; Data string }` holds the typed
  payload. A transition move uses `Name` for the event kind and
  `Data` for a description. The payload is opaque to the bus.
- `func (e Event) Validate() error` enforces the field rules. It
  rejects an empty `Name`. It rejects an empty `Data`.
- `type Handler func(ctx context.Context, e Event) error` is the
  subscriber callback.
- `type Bus struct` holds one subscription set. It is safe for
  concurrent use but it does not order goroutines. The name means one
  subscription set, not a general bus.
- `func New() *Bus` creates a bus with an empty subscription set. It
  has no error path. A fresh bus cannot reject the caller.
- `func (b *Bus) Subscribe(name Name, h Handler) error` adds a
  callback. It rejects an empty name and a nil handler.
- `func (b *Bus) Emit(ctx context.Context, e Event) error` validates
  the event, then it runs each handler for the event name. It rejects
  an unknown name with an error. It returns that error to the emitter.

`Subscribe` and `Emit` both return `error`. `Emit` copies the handler
slice under the mutex. It releases the mutex, then it runs each handler
unlocked. A handler may call `Subscribe` or `Emit`; such a call
dispatches on the inner bus state. Handlers for one event run in order.
A handler error does not stop `Emit`. All handlers still run even when
one fails. `Emit` does not propagate a handler error; its error covers
only unknown-name and `Event` validation. The caller logs a handler
failure. `Emit` never starts a goroutine.

The bus imports nothing of this module. It is a leaf block. The import
edge is one-directional; consumers import the bus, never the reverse.
The machine package imports the bus for its typed `MoveEvent` constant.
A caller at the composition layer owns a bus. It subscribes and emits
through the bus API. The `events` row in policy/layers.json is empty;
the `machine` row lists `events`.

## Tests

Tests live in `events/events_test/`. The API lock lands via make
api-update. There are no conformance vectors; the dispatch is in
process, not the wire form.

- `events_tdd_test.go` — red-green cases for `New`, `Subscribe`,
  `Emit`, and `Event.Validate`. Start with the assertions. Confirm they
  fail on the empty implementation. Implement and watch them pass.
- `events_integration_test.go` — subscribe and emit a flow. Prove the
  handler runs in order. Prove an unknown name returns an error.
  Prove a handler error does not stop later handlers. Prove a handler
  that calls `Subscribe` or `Emit` dispatches on the inner bus state.
- `events_concurrency_test.go` — concurrent `Subscribe` and `Emit` on
  one bus under `go test -race`. Atomic counters prove exact delivery.
  The watchdog bounds a regression deadlock.
- `cross_subsystem_integration_test.go` — the end-to-end composition
  test. It proves envelope, room, machine, and events compose through
  public APIs. A signed message passes room admission. A real machine
  move fires onto a caller-owned bus under `machine.MoveEvent`. A
  guard failure emits nothing.
- `events_perf_test.go` — benchmark `Emit` on one event with one
  handler. State the allocation budget.

Phase 17 table cases cover an empty name, a nil handler, an empty
`Event.Name`, an empty `Event.Data`, and an unknown name at `Emit`.
All non-test rejection branches are exercised by the table tests. The
module's own emitters always set both fields. Phase 18 proves a real
machine move arrives on a caller-owned bus.

## Verification

`make verify` passes. The coverage floor for `events` holds. The shape
lands in `api/events.txt` via make api-update. Concurrency uses
`go test -race ./...` for the mutex-guarded subscription set. A new
consumer decides when this package ships. Phase 18 is that consumer.
Do not build the package before the machine caller exists.

The build is phased. Phase 17 ships the leaf core. Phase 18 proves the
machine wiring on a caller-owned bus. Phase 19 wires the flow block
onto a caller-owned bus. Phase 20 wires the envelope delivery through
the composition layer. See the phase plans for each unit.

## Addendum: Emit accepts an unobserved event
Status: shipped.


Approved change. `Emit` no longer fails when no handler is subscribed.

### Goal

An observability side channel must not fail the main path. Today
`Emit` returns `events: no subscriber for name %q`. `agent.Run`
propagates that error into the step-failure path. One unobserved event
fails an agent step. The fix makes the no-subscriber case a no-op.

### New Emit contract

- `Emit` validates the event first. The `Event.Validate` error path
  stays exactly as it is.
- After validation, `Emit` copies the handler slice under the mutex.
- With zero handlers, `Emit` returns nil. The event is not stored.
  Nothing else changes.
- With handlers, behavior is unchanged. Handlers run in order,
  unlocked. A handler error is not propagated. `Emit` returns nil.
- `Emit` still never starts a goroutine.
- The doc comment on `Emit` is rewritten. It states the new rule: a
  name with no subscriber emits nothing and returns nil. Comments
  state rules code enforces.

### API

No signature changes. Behavior only. `api/events.txt` gets no diff.
No `make api-update` run is needed. `policy/layers.json` gets no
change.

### Migration set

Completeness is by grep for `no subscriber`, `.Emit(`, and `noop`
across the tree, `.go` and `.md` both. Worktree copies under
`.claude/worktrees/` are excluded.

Code:

- `events/bus.go:74-88` — remove the no-subscriber error branch.
  Rewrite the `Emit` doc comment.
- `agentrun/options.go:147-152` — delete the no-op subscribe loop over
  `MessageDeliveredEvent`, `MessageAckedEvent`, and
  `ThreadVerifiedEvent`, and the `noop` closure.
- `dispatch/options.go:171-179` — delete the no-op subscribe loop over
  `MessageDeliveredEvent` and `MessageAckedEvent`, and the `noop`
  closure. Rewrite the `New` doc comment that names the workaround.
- `agent/translator.go` — no change. It returns the raw `Emit` error.
  The "raw error" wording stays true.
- `agentloop/heartbeat.go:62-63` — the `emitEvent` doc comment says
  Emit errors "including no subscriber for name" are swallowed. That
  error no longer exists. Rewrite the sentence to drop the
  parenthetical.
- `agentloop/agentloop_test/heartbeat_test.go:245` —
  `TestRunHeartbeatEmitSwallowsNoSubscriberError` pins the removed
  error in its name and comment. This plan mandates the rename.
  Rename to `TestRunHeartbeatUnobservedEventKeepsRunAlive` and rewrite
  the comment. The assertion that `Run` completes with no
  heartbeat-name subscriber stays.
- `agentloop/heartbeat.go:70`, `ledger/ledger.go:36`,
  `scheduler/run.go:157`, `flow/wave.go:134`,
  `subagent/astool.go:119` — no change to code. They already discard
  the `Emit` error. Their behavior is identical under the new
  contract.

No production caller depends on the no-subscriber error. Only tests
pin it.

Tests rewritten or deleted. This plan mandates each rewrite. The gate
does not read plan prose: it waives a finding only through an
`Allow-Test-Change` commit-message trailer. Every commit in this
change that rewrites a test carries that trailer and names the
mandated rewrites. See Verification.

- `events/events_test/events_test.go:63` — `TestEmitRejectsUnknownName`
  pins the old error. Rewrite as `TestEmitAcceptsUnknownName`. It
  asserts `Emit` returns nil for an unsubscribed name.
- `events/events_test/events_test.go:73` —
  `TestZeroValueBusPinsConstructorOnly` asserts zero-value `Emit`
  errors. A zero-value bus has no subscribers, so it now returns nil.
  Rewrite the second half to assert nil. The `Subscribe` panic half
  stayed until the maintenance addendum at the end of this file
  removed it.
- `agent/agent_test/run_test.go:358` —
  `TestRunConfirmStepMessageDeliveredNoSubscriber` pins the propagated
  error. Rewrite to assert `Run` succeeds on a bare `events.New()`
  bus and `wait` runs once.
- `agent/agent_test/run_test.go:382` —
  `TestRunConfirmStepMessageAckedNoSubscriber`. Same rewrite.
- `agent/agent_test/run_test.go:405` —
  `TestRunThreadVerifiedNoSubscriber`. Same rewrite. The remaining
  `MessageDeliveredEvent` and `MessageAckedEvent` subscriptions in it
  become unnecessary; remove them.
- `agent/agent_test/run_test.go:4-8` — the package doc comment lists
  "the three no-subscriber cases". Rewrite it to name the rewritten
  cases. Rewrite it to name the rewritten cases.
- `agent/agent_test/run_test.go:27-28` — the `newRunBus` comment says
  the subscriptions keep "a real run" from failing. The reason is gone.
  Rewrite the comment or delete the no-op loop with it.
- `agent/agent_test/translator_test.go:303-338` — the table cases
  assert the bus's no-subscriber error surfaces unwrapped. Rewrite the
  no-subscriber cases to assert a nil error. Keep the invalid-input
  cases; those errors are unchanged.
- `agentrun/agentrun_test/options_test.go:185-235` — both tests emit
  the three agent names to prove `New` wired the bus. The emits still
  pass. Rewrite the stale comments that name the "no subscriber"
  fault. `TestNewUsesCallerBus` needs a new wiring proof; assert
  identity only, or subscribe a probe handler before `New` and check
  it survives.
- `dispatch/ladder_internal_test.go:22` —
  `TestProcessLine_EmitIsBestEffort` bypasses `New` to force the
  unsubscribed-name error. Under the new contract that premise is
  void. Rewrite to drop the error framing and keep the assertion that
  `processLine` answers on an unsubscribed bus.
- `agent/agent_test/system_fixture_test.go:36-56` — the comment claims
  the fixture bus must cover every name or `Run` fails. The claim is
  false after the fix. Rewrite the comment. Keep the fixture; the
  recorder subscriptions still serve observation.
- `agent/agent_test/repeat_thread_test.go:17-18` — same stale comment
  on `repeatBus`. Rewrite the comment. Keep or drop the no-op loop;
  either is harmless. Drop it for clarity.

### TDD test strategy

Failing tests first, against the old code.

- `events/events_test/events_tdd_test.go` or a new file —
  `TestEmitWithNoSubscriberReturnsNil`. The killing test. A fresh
  bus, one valid event, no subscriber. Assert nil. It fails on the
  current code.
- Handler dispatch keeps its coverage in
  `events/events_test/events_integration_test.go`:
  `TestSubscribeThenEmit` at line 13, `TestHandlersRunInOrder` at
  line 32. No new test is needed for that path.
- `TestEmitRejectsInvalidEvent` stays. It proves the validation
  contract survives.
- `agent/agent_test/run_test.go` —
  `TestRunSucceedsWithUnsubscribedBus`. One-step fixture, bare
  `events.New()` bus, confirming wait. Assert a nil error and one
  `wait` call. This is the defect-level end-to-end proof. It crosses
  `agent/run.go`'s `EmitMessageDelivered` and `EmitMessageAcked` calls
  with no subscriber anywhere.

### Related doc surfaces

Each states the old contract. Update in the same change:

- `docs/packages/events.md` lines 30, 48-56, 69, 83-84 — the Emit
  rule and the example comment.
- `docs/packages/agentrun.md:122` — names the "no subscriber" fault
  the default wiring avoids.
- `docs/packages/agentloop.md:446` — states `Run` swallows the
  "no subscriber" error.
- `docs/plans/agentloop.md:2163`, `docs/plans/agent.md:600`,
  `docs/plans/hooks.md:69` — same claim in plan prose.
- `docs/examples/agent-composition.md:161,342` and
  `docs/examples/_agentcomposition/main.go:138-146` — the example's
  `subscribeAll` helper exists only for the old error. Delete the
  helper and its prose.
- `docs/architecture.md` — grep shows no statement of the
  no-subscriber rule. No change required. The package map prose at
  line 223 lists the surface only.

Addenda in `docs/plans/agentrun.md` and `docs/plans/dispatch.md`
record the deleted loops in those packages' plans.

### Verification

- `make verify` passes.
- Coverage floors hold. `events` sits at 100%; the removed branch is
  replaced by the new nil-path tests. `agentrun` and `dispatch` lose
  the loop statements; their `New` tests still cover `New` end to end.
  Confirm the 85% floor holds per package in the coverage block.
- No `api/` diff. No `policy/layers.json` diff. No conformance vector:
  this is not wire semantics.
- Every commit that rewrites or renames a mandated test carries an
  `Allow-Test-Change` commit-message trailer. The trailer names the
  rewrites this addendum mandates. Without it the pre-commit hook
  blocks the change.

## Addendum: agentrun and dispatch carry the same rule
Status: shipped.


Each rewriting commit in `docs/plans/agentrun.md` and
`docs/plans/dispatch.md` scope carries the same `Allow-Test-Change`
trailer. It names the rewrites those addenda mandate.

## Addendum: the zero-value Bus is usable
Status: shipped.


Part of the maintenance addenda batch. See
docs/plans/agents/maintenance-addenda-batch.md, item 6d.

`Subscribe` assigns into `b.subs` at `events/bus.go:64`. On a zero
`Bus` that map is nil, so the call panics. Add the same two-line
nil-map guard `trigger/registry.go:77` uses, inside the lock and
before the append. `Emit` only reads the map, which is safe on nil.

### This reverses a documented invariant

State the necessity case plainly. This is a contract reversal, not a
bug fix.

- Three doc sites state the old rule: `events/bus.go:41`,
  `docs/packages/events.md:21`, and `docs/packages/events.md:59`.
- One test pins it on purpose. Its comment reads: the invariant is
  constructor-only, and `New` is the only sanctioned build.
- No in-tree caller can reach the panic. All 22 production
  `events.Bus` sites hold a `*events.Bus` built by `events.New`, or
  they nil-check first. So the change fixes no live defect.
- The gain is idiom alignment. `events.Bus` now behaves like
  `scheduler.Registry`, whose zero value is usable.
- The user ordered this change and its doc comment update directly.
  That instruction is the authority for the reversal and for the
  `TT01` override trailer. This plan does not self-authorize it.

The `Bus` doc comment and both `docs/packages/events.md` sites change
with the code, so no site keeps claiming the zero value is unusable.

### Addendum tests

- `events/events_test/events_test.go` replaces
  `TestZeroValueBusPinsConstructorOnly` with
  `TestZeroValueBusSubscribesAndEmits`. The old test asserted the
  panic this change removes.
- The replacement subscribes on a zero `Bus`, emits, and asserts the
  handler ran once. It keeps the old test's second half, which
  asserts `Emit` on an untouched zero `Bus` returns nil.

### Addendum verification

- `make verify` passes. The `events` coverage floor holds.
- No `api/` diff. No `policy/layers.json` diff. No conformance vector:
  this is not wire semantics.
- The commit carries one `Allow-Test-Change: TT01` trailer. Its reason
  names both test replacements in the batch, because a single trailer
  waives every `TT01` finding in the commit.

## Addendum: Error sentinel sweep

Status: shipped

This addendum classifies every error sentinel and invalid-argument
literal in `events` as CONFIG or RUNTIME, and merges the CONFIG cases
into one shared sentinel.

CONFIG means a caller-supplied argument fails a one-shot sanity check,
with no other caller decision besides fixing the call. RUNTIME means
the error reacts to live registry state or a handler's own decision.

| Sentinel | Classification | Disposition |
| --- | --- | --- |
| `ErrBlankName` | RUNTIME | `Add` runs repeatedly over a program's life, not once at construction. Kept unchanged. |
| `ErrNilHandler` | RUNTIME | Same reasoning as `ErrBlankName`. Kept unchanged. |
| `ErrDuplicateName` | RUNTIME | Depends on the registry's live contents at the call. Kept unchanged. |
| `ErrVetoed` | RUNTIME | Depends on a handler's own decision at `Fire` time. Kept unchanged. |
| `Point.Validate` invalid-point literal | CONFIG | A caller-supplied enum value with no live-state dependency. Merged into new `ErrInvalidOptions`. |

`ErrInvalidOptions = errors.New("events: invalid options")` is new in
`events/registry.go`. `Point.Validate` now wraps it:
`fmt.Errorf("%w: Point %d is not a valid point value", ErrInvalidOptions, int(p))`.

No existing test asserted the old unwrapped literal's text, so no test
assertion needed a rewrite for this change.

## Addendum: the Fire doc comment names the wrong error prefix

Status: planned, not yet built.

`Registry.Fire`'s doc comment pins the wrap format as a `hooks:`
prefix. The code emits an `events:` prefix on both the handler-error
path and the veto path. The comment is a leftover from the fold of the
`hooks` package into `events`.

Fix: correct the prefix in the comment. The code and the existing test
already agree, so no code changes and no new test.

### Addendum verification

- `go test ./events/...` passes unchanged.
- `python3 scripts/check_docs.py` passes.
