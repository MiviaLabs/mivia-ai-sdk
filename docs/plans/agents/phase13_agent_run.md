# Phase 13: agent execution loop

Status: done. Builds on phase 12 and phase 20. This phase runs
the agent. The loop walks the agent plan through `flow.Run`, fires
each step, and waits on the ack gate. It routes an escalated step back
to the caller.

## Descope: in-process only, no a2a

The original sketch below sent each step "through the a2a adapter or
the in-process runner." The `a2a` package does not exist yet; its plan
in `docs/plans/a2a.md` stays status future, with no code. This phase
descopes to the in-process runner path only. `Run` drives `flow.Run`
directly. It signs and delivers each step as an in-process
`envelope.Message`; it never calls a network transport.

A future phase adds the a2a adapter. That phase supplies its own
`AckWait` implementation (see API) that resolves an ack over the wire
instead of in-process. `Run`'s signature does not change for that
future phase; only the caller-supplied `AckWait` value changes. This
keeps the transport binding outside `agent`, matching the Building
blocks rule in AGENTS.md: an agent imports the blocks; a block never
imports the agent, and the transport stays a caller concern.

## Original goal (unchanged)

Run an agent start to finish. Each step becomes an envelope message.
A request waits for the confirmed ack. An escalated step routes to a
human. The run returns the final status.

## Scope

Inside: the run entry point, the step-to-message translation, the ack
wait, and the escalation path, all in-process. Outside: the tool
registry and the memory store (phases 14 and 15). Outside: any network
transport; that is the future `a2a` package's job, not this phase's.

## The ack-resolution design

`flow.Run` already gates every non-panel (and one-member-panel) step
behind a `flow.Confirm` callback: "Run calls it after Fire moves the
status... A nil return means the ack confirmed; the walk advances."
See `flow/runner.go`. This phase does not invent a new ack mechanism.
It builds `Run`'s own `flow.Confirm` closure and asks the caller to
supply the one piece flow cannot know: how to obtain a receiver's real
`envelope.Ack` for one signed message, with no assumed transport.

`Run` takes a caller-supplied `AckWait` function:

```go
type AckWait func(ctx context.Context, msg envelope.Message) (envelope.Ack, error)
```

Inside `Run`'s `flow.Confirm` closure, for each gated step:

1. Build an `envelope.Message` from the step (`ID`, `ThreadID`,
   `Payload`), with `Version` set to `envelope.Version`, `Intent` set
   to `envelope.IntentRequest`, and `Epistemic` set to
   `envelope.EpistemicAssumed`. Sign it with the agent's own
   `*identity.Identity`.
2. Call `EmitMessageDelivered` (agent/translator.go). This runs the
   real `VerifySignature` cryptographic check before anything else
   happens; a step never proceeds on an unverified signature.
3. Call the caller's `AckWait` with the signed message. `AckWait`
   returns the receiver's real `envelope.Ack`, or an error. If `wait`
   returns a non-nil error, the closure short-circuits here: it
   returns that error unchanged (so `errors.Is` still matches a
   wrapped `ErrEscalated`, or any other sentinel the caller's `AckWait`
   wraps) and never calls `EmitMessageAcked`. A zero-value `Ack` fails
   `Ack.Validate()` with an unrelated "message id is required" error,
   so `EmitMessageAcked` never runs on the zero value the closure would
   otherwise hold after a failed wait.
4. Call `EmitMessageAcked` with the `Ack` `wait` returned. This runs
   the real `Ack.Validate()` check. This step runs only when step 3
   returned a nil error.
5. Reject the step unless `Ack.Status == envelope.AckConfirmed`. A
   `pending` or `corrected` ack halts the walk; it never counts as a
   pass.

Why the caller supplies `AckWait`, not `Run` itself: there is no
network hop today, so "wait for an ack" has no one true in-process
implementation. A test can supply a closure that hands the message to
a second in-process `Agent`, lets it build a real `envelope.Ack` with
`envelope.NewAck`, and resolves once that agent confirms or corrects.
A future `a2a`-backed caller supplies a closure that sends the message
over the wire and blocks on the reply. `Run` never short-circuits this
with a synchronous self-ack: it always runs `VerifySignature` on the
message and `Validate` on the ack, and it always requires
`AckConfirmed` before advancing. The one thing self-reported and
unchecked, by design, is the ack's content: `Ack` carries no
signature field in this protocol (see the Known limits section of
`docs/protocol-design.md`), so `EmitMessageAcked` verifies the ack's
shape, not its authorship. That limit is pre-existing and out of scope
here.

### Escalation

`Run` defines one sentinel, `ErrEscalated`. `AckWait` wraps it with
`%w` when a step needs a human instead of an automatic ack. `Run`
propagates the error unchanged (through `flow.Run`'s own `%w` wrap);
the caller checks `errors.Is(err, agent.ErrEscalated)` after `Run`
returns. `Run` carries no per-step escalate marker of its own: the
policy of when to escalate belongs to the `AckWait` the caller
supplies, not to `Run`. A later phase may add a per-step intent
selector if a real caller needs `Run` to originate an
`envelope.IntentEscalate` message on its own; no such caller exists
today, so this phase does not speculate on that shape.

### A known caveat: Fire runs before Confirm

`flow.Run`'s `runSingleton` fires the machine transition, then calls
`Confirm`. See `flow/runner.go`: `cur, rec, err = m.Fire(...)` happens
before the `confirm(ctx, step)` call. A rejected or escalated ack does
not roll the `machine.Status` or the `machine.InOut` record back to
the pre-step value; it only stops the walk from reaching a later step.
This is existing `flow` behavior, not a phase 13 change. The tests in
this phase assert the walk stops, not that the status reverts.

## API

The surface below lands in `api/agent.txt` via `make api-update`,
alongside the phase 12 and phase 20 surface already there.

- `type AckWait func(ctx context.Context, msg envelope.Message) (envelope.Ack, error)`
  is the caller-supplied ack resolver described above.
- `var ErrEscalated error` is the sentinel an `AckWait` wraps to route
  a step to a human. Test with `errors.Is`.
- `var ErrNoWait error` is the sentinel `Run` returns when `wait` is
  nil. `Run` checks this before it touches `m` or the agent's plan, so
  a nil `wait` never reaches a step build.
- `var ErrNoThread error` is the sentinel `Run` returns when
  `threadID` is empty. `Run` checks this before it touches `m`.
- `ErrNoBus`, already exported by the phase 20 translator, is reused:
  `Run` returns it when `bus` is nil, before it touches `m`.
- `func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus) (machine.Status, machine.InOut, error)`
  drives `a.plan` (the `*flow.Definition` bound at `New` time) through
  `flow.Run`. `threadID` names the one envelope thread this run's step
  messages share. `m` is the status model the plan's steps target; it
  is a `Run` parameter, not an `Agent` field, matching `flow.Run`'s own
  shape and keeping `Agent`'s declarative binding (`New`) free of
  per-run state. `in` is the starting record. `wait` resolves each
  step's ack. `bus` receives `MessageDeliveredEvent`,
  `MessageAckedEvent`, and, once per successful run with one or more
  gated steps, `ThreadVerifiedEvent`.

  For each gated step, `Run` builds the `envelope.Message` with
  `Version: envelope.Version`, `Intent: envelope.IntentRequest`, and
  `Epistemic: envelope.EpistemicAssumed`, alongside the step's `ID`,
  `threadID`, and `Payload`. `IntentRequest` matches the ack-gated
  design: `Message.RequiresAck` already treats every `IntentRequest`
  message as needing an ack. `EpistemicAssumed` needs no `Source` or
  `Evidence` in `Provenance`, unlike `EpistemicVerified`; `Run` sets no
  `Provenance` field. Both constants let the built message pass
  `Message.Validate()` on its own, which the closing
  `EmitThreadVerified` call requires: `envelope.VerifyThread` calls
  `Validate` on every message in the thread.

  Check order at entry: `wait == nil` first, then `bus == nil`, then
  `threadID == ""`. Each check returns `machine.Status("")`, the
  caller's `in` unchanged, and its sentinel. `Run` does not check `m`
  or `a.plan` itself; `flow.Run` already rejects a nil `m` or a nil
  `d`, and `Run` passes `a.plan` as `d`.

  On success `Run` returns the final `machine.Status` and
  `machine.InOut` from `flow.Run`, having emitted, per gated step,
  `MessageDeliveredEvent` then `MessageAckedEvent`, and, once at the
  end, `ThreadVerifiedEvent` over every step message built during the
  run, in step order.

  On an ack failure or an escalation, `Run` returns the error
  `flow.Run` returns (wrapped with `flow`'s own "ack not confirmed"
  text) and the status/record `flow.Run` returns for the failed step.
  No `ThreadVerifiedEvent` fires on a failed run.

### Caller contract: Payload is required per gated step

`envelope.Message.Validate` rejects an empty `Payload`. Every
`flow.Step` that `flow.Run` gates behind `Confirm` (a step named in no
panel, or the sole member of a one-member panel) must carry a
non-empty `Payload` for `Run` to build its message. A step that is a
member of a panel with two or more members needs no `Payload` for
`Run`'s purposes: `flow.Run` never calls `Confirm` for it, so `Run`
never builds an envelope message for it and emits no
`MessageDeliveredEvent` or `MessageAckedEvent` for that step. This
mirrors `flow`'s own documented rule; phase 13 does not change it.

## Tests

Test files live in `agent/agent_test/`:

- `run_test.go` — the red-green cases for `Run`. Start with the
  assertions; confirm they fail on the empty phase; implement and
  watch them pass. Table cases:
  - `wait` is nil, a valid `bus` and `threadID`: expect
    `errors.Is(err, agent.ErrNoWait)`.
  - `bus` is nil, a valid `wait` and `threadID`: expect
    `errors.Is(err, agent.ErrNoBus)`.
  - `threadID` is empty, a valid `wait` and `bus`: expect
    `errors.Is(err, agent.ErrNoThread)`.
  - `wait` nil together with `bus` nil: expect `ErrNoWait`, not
    `ErrNoBus`. Proves the check order.
  - `bus` nil together with `threadID` empty: expect `ErrNoBus`, not
    `ErrNoThread`. Proves the check order.
  - A one-step plan, an `AckWait` that confirms: `Run` returns a nil
    error and the target status.
  - A one-step plan, an `AckWait` that returns a `corrected` ack:
    `Run` returns a non-nil error; the caller does not need
    `errors.Is` here, `flow`'s wrap text is enough to assert non-nil.
  - A one-step plan, an `AckWait` that returns an error wrapping
    `ErrEscalated`: `Run` returns an error and
    `errors.Is(err, agent.ErrEscalated)` is true.
  - A one-step plan, an `AckWait` that returns a plain error built
    with `errors.New`, wrapping nothing: `Run` returns a non-nil
    error, and `errors.Is(err, wantErr)` is true against the exact
    sentinel `AckWait` returned. This proves the short-circuit from
    the ack-resolution design applies to any `wait` error, not only
    one wrapping `ErrEscalated`: `EmitMessageAcked` never runs, so no
    zero-value-`Ack` `Validate` error masks the real cause.
  - A zero-step plan (`&flow.Definition{}`, matching `agent.New`'s own
    documented zero-value acceptance): `Run` returns a nil error and
    the initial status, and calls `wait` zero times.
- `run_integration_test.go` — run a real `Agent`, built with
  `identity.New`, a real `discovery.Card`, and a real `flow.Definition`
  with two sequential steps (`a` needs nothing, `b` needs `a`), through
  a real `machine.Definition`. `wait` is a closure that builds a real
  `envelope.Ack` with `envelope.NewAck` and `Confirm`. The closure also
  captures each `envelope.Message` it receives. Prove:
  - step `a`'s ack confirms strictly before `wait` is called for step
    `b` (an ordered log inside the closure proves this, not a
    sleep-based race).
  - each captured message independently passes `Message.Validate()`
    with a nil error, confirming `Run` sets `Version`, `Intent`, and
    `Epistemic` to values that clear `Validate` on their own, not only
    values that happen to survive `EmitMessageDelivered`'s
    `VerifySignature` check.
  - the final status matches the machine's expected end state.
  - the bus receives, in order: `MessageDeliveredEvent(a)`,
    `MessageAckedEvent(a)`, `MessageDeliveredEvent(b)`,
    `MessageAckedEvent(b)`, `ThreadVerifiedEvent`.
  A second scenario in the same file feeds an `AckWait` that confirms
  step `a` and wraps `ErrEscalated` for step `b`. Prove `Run` returns
  `errors.Is(err, agent.ErrEscalated)`, the bus received no
  `ThreadVerifiedEvent`, and `wait` was called exactly twice: once for
  step `a`, once for step `b`, this plan's only two steps. Run every
  case under `go test -race`.
- `run_panel_integration_test.go` — the multi-member panel path, which
  `run_integration_test.go` and `lifecycle_integration_test.go` do not
  exercise (both use only a one-member panel or no panel). Build a
  real `Agent` and a real `machine.Definition`. Two scenarios:
  - a `flow.Definition` with one sequential gated step and one
    two-member panel step (both panel members named in the same
    panel, both with a `To` transition the machine model covers).
    `wait` is a closure that counts its own calls per step ID. Prove
    `Run` returns a nil error and the machine's final `Status`; `wait`
    is called exactly once, for the gated step only, zero times for
    either panel member; the bus receives
    `MessageDeliveredEvent`/`MessageAckedEvent` only for the gated
    step, never for either panel member; `ThreadVerifiedEvent` fires
    once, over the one message the gated step built.
  - a `flow.Definition` that is only a two-member panel, no other
    step, no gated step at all. Prove `Run` returns a nil error and
    the machine's final `Status`, `wait` is called zero times, the bus
    receives no `MessageDeliveredEvent` and no `MessageAckedEvent`,
    and `ThreadVerifiedEvent` never fires. This matches the plan's own
    rule: `ThreadVerifiedEvent` fires only once per run with one or
    more gated steps.
  Run both cases under `go test -race`.
- `run_bench_test.go` — benchmark a two-step run with a real,
  synchronous `AckWait` round trip (build and confirm an `envelope.Ack`
  in-process, no I/O). Target under two milliseconds per run.
  `AllocsPerRun` states the allocation budget; the builder records the
  measured baseline in this file.

### Follow-on: the full-lifecycle integration test

- `lifecycle_integration_test.go` — the end-to-end proof this phase
  adds beyond the original phase 13 test list. Build a real
  `identity.Identity` with `identity.New`, a real `discovery.Card` by
  struct literal, and a real `flow.Definition` with `flow.New`: one
  root step named in a one-member panel, and one sequential step that
  needs the root. `agent.New` binds the three into an `Agent`. Build a
  real `machine.Definition` whose transitions cover both steps' `To`
  values. Call `Run` with a shared `events.Bus` and an `AckWait` that
  builds and confirms a real `envelope.Ack` for each step.

  Assert:
  - `Run` returns a nil error and the machine's final `Status`.
  - the event sequence on the bus is exactly
    `MessageDeliveredEvent, MessageAckedEvent, StepCompletedEvent`
    for the panel step (a one-member panel runs through the same
    `Confirm` gate as a singleton, so it still emits the message
    events; `flow.StepCompletedEvent`, not an `agent` event, marks the
    step done), then the same three-event sequence for the sequential
    step, then one closing `ThreadVerifiedEvent`.
  - a second run of the same scenario, with `AckWait` returning a
    `corrected` ack for the sequential step, halts before that step's
    `StepCompletedEvent` fires, returns a non-nil error, and never
    emits `ThreadVerifiedEvent`. The root step's earlier
    `MessageDeliveredEvent`, `MessageAckedEvent`, and
    `StepCompletedEvent` still appear on the bus: the forced failure
    stops the walk: it does not erase what already happened.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `agent` and for the total, with
  `Run`'s new lines counted in.
- The `agent` row in `policy/layers.json` gains `machine`. The row
  change lands with this plan, before the code.
- `api/agent.txt` gains `AckWait`, `Run`, `ErrEscalated`, `ErrNoWait`,
  and `ErrNoThread`, through `make api-update` in the same change as
  the code. `ErrNoBus` is reused, not re-declared.
- `agent/doc.go`'s file map gains the new file name this phase adds,
  for example `run.go`.
- `docs/architecture.md`'s `agent/` bullet gains `Run`, `AckWait`, and
  the `machine` import edge, in the same change as the code.
- This phase adds no conformance vector. It composes `envelope.Message`
  and `envelope.Ack`, both already vector-covered in `envelope`, and it
  defines no new wire schema of its own.
- `docs/plans/a2a.md` stays status future; this phase does not depend
  on it and does not unblock it. A future phase revisits `a2a` to
  supply a wire-backed `AckWait`.
