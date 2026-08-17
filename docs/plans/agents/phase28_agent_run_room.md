# Phase 28: agent run room stamping

Status: ready to build. Builds on phase 13's `Run` (docs/plans/agents/
phase13_agent_run.md), phase 26's heartbeat parameter
(docs/plans/agents/phase26_agent_heartbeat.md), and the shipped `room`
package (docs/plans/room.md). Independent of phase 27; the two phases
touch different packages and share no code.

## Goal

Close the room-admission gap `docs/examples/agent-dispatch.md`
discloses today: `Run` never sets `Message.Room` before it signs a
step message, so every message `Run` builds carries `Room == ""` and
`room.Room.Accepts` can never admit it. `Run` gains one trailing,
optional `room string` parameter. `confirmStep` stamps it onto each
built message before `a.id.Sign` runs, so a caller that supplies a
room name produces messages a real `room.Room` can admit.

`Room` must be set before signing, because `envelope.Sign` covers the
whole canonical-JSON payload, `Room` included. No step after `Sign`
can add or change `Room` without invalidating the signature. This is
not a transport-layer concern to defer; it is a value `Run` itself
must set, at the one point in `confirmStep` where the message still
exists unsigned.

## Scope

Inside: one new trailing parameter on `Run`, `room string`. One
assignment, `msg.Room = room`, inside `confirmStep`, before
`a.id.Sign(msg)`, guarded so an empty `room` reproduces today's
behavior exactly (`Message.Room` stays the zero value).

Outside: a `Room` field or a `*room.Room` reference on `Agent`. A
generic pre-sign decorator hook. Any change to `room.Room.Accepts` or
to `envelope.Sign`. See the three rejected alternatives below.

### Rejected: a `Room` field on `Agent` at `New` time

Contradicts the design intent phase 26 already states in
docs/plans/agent.md: per-run state belongs on `Run`'s parameters, not
on `Agent`'s fields, so one `Agent` can run against more than one
context without `New`'s signature changing. A room name is exactly
that kind of per-run state; `threadID` and `in` already carry it that
way. Putting `Room` on `Agent` would special-case one per-run value
onto the long-lived binding while every other per-run value stays a
`Run` parameter.

### Rejected: a generic pre-sign decorator hook

A hook shaped `func(envelope.Message) envelope.Message`, called by
`confirmStep` right before `Sign`, would let a caller stamp any field,
not only `Room`. This solves a broader, unconfirmed need. Only one
pre-sign field gap is confirmed today: `Room`. A hook adds an
indirection with a single, narrow caller need behind it, which
AGENTS.md's Building blocks rule rejects as speculative generality.
Revisit a hook shape only if a second pre-sign field gap is confirmed
later.

### Rejected: an `agent` to `room` import edge

`Room` is a plain `string` field, already declared on
`envelope.Message` and owned by `envelope`. `Run` needs no
`room.Room` value and no `room` import to set it; a caller passes the
room name as a `string`, the same shape `threadID` already uses.
`policy/layers.json`'s `agent` row does not change: `agent` already
imports `envelope`, and this phase adds no new import.

## API

The surface below lands in `api/agent.txt` via `make api-update`.

- `func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor, room string) (machine.Status, machine.InOut, error)`
  — the existing phase 26 signature, with one trailing `room string`
  parameter. Every existing behavior described in docs/plans/agent.md's
  phase 13 and phase 26 sections is unchanged. `room == ""` skips the
  assignment; `Message.Room` stays the zero value and `Run` behaves
  exactly as it does today. `room != ""` sets `msg.Room = room` inside
  `confirmStep`, on every gated step's built message, before
  `a.id.Sign(msg)` runs.

No new exported type, sentinel, or constant. `Run`'s existing sentinel
checks (`ErrNoWait`, `ErrNoBus`, `ErrNoThread`) are unchanged; `room`
gets no check of its own. An empty `room` is a valid, supported
"no room" choice, not a caller error, matching how `hb == nil` is a
valid "no telemetry" choice in phase 26.

The expected `api/agent.txt` diff, against the phase 26 block already
in docs/plans/agent.md:

```text
- func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor) (machine.Status, machine.InOut, error)
+ func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor, room string) (machine.Status, machine.InOut, error)
```

### Import change: none

`agent` already imports `envelope`, which already declares `Message.Room`.
No new import edge. `policy/layers.json`'s `agent` row stays
`["identity", "discovery", "flow", "envelope", "events", "machine",
"heartbeat"]`, unchanged.

### Every existing call site changes

This is a breaking signature change to an exported method, matching
how phase 26 changed `Run`. Every existing call site gains a trailing
`""` argument in this change, since no existing test supplies a room
name yet:

- `agent/agent_test/lifecycle_integration_test.go` — two call sites.
- `agent/agent_test/run_bench_test.go` — two call sites.
- `agent/agent_test/run_integration_test.go` — two call sites.
- `agent/agent_test/run_test.go` — thirteen call sites.
- `agent/agent_test/run_panel_integration_test.go` — two call sites.
- `agent/agent_test/liveness_test.go` and
  `agent/agent_test/liveness_integration_test.go` — every call site
  phase 26 added.

The builder confirms `go build ./...` catches any missed site before
the rest of `make verify` runs. The exact line numbers move once
earlier edits shift the files; the builder locates each site with
`grep -n "a.Run(\|\.Run(ctx" agent/agent_test/*.go` rather than
trusting a stale line list.

## Tests

New and changed test files live in `agent/agent_test/`, alongside the
phase 13 and phase 26 files:

- `run_test.go` gains two red-green cases in the existing `Run` test
  table:
  - A non-empty `room` argument, a confirmed one-step run: the test
    inspects the message `EmitMessageDelivered` received (through a
    bus subscriber that records the delivered `envelope.Message`) and
    asserts `Message.Room` equals the supplied room name. The test
    also asserts `Message.Validate()` and `Message.VerifySignature()`
    both return nil on that captured message, proving a non-empty
    `Room` still signs and validates cleanly.
  - An empty `room` argument (the zero value every other case in this
    file now passes): the captured message's `Room` is the empty
    string, reproducing today's exact behavior. This case pins the
    no-op path so a future change cannot silently start stamping a
    default room name.
- `run_room_integration_test.go` — the cross-package proof this phase
  exists to make. Build a real `identity.Identity` for the agent and a
  second one for a room founder. Build a real `room.Room` with
  `room.New` and `Admit` the agent's signer. Build a real two-step (or
  one-step) plan and a real `machine.Definition`. Call `Run` with a
  non-empty `room` argument equal to the `room.Room`'s `ID()`. Inside
  `wait`, call `rm.Accepts(msg)` on the signed message `Run` handed it
  and assert the call returns nil, proving a `Run`-built message, with
  `Room` set through the new parameter, is admitted by a real
  `room.Room`. A second case reruns the same setup with `room` left
  empty and asserts `Accepts` now returns a non-nil error, pinning the
  gap this phase closes as a regression check, not only a new-behavior
  check.
- Every existing call site to `a.Run(...)` across `run_test.go`,
  `run_integration_test.go`, `run_panel_integration_test.go`,
  `run_bench_test.go`, `lifecycle_integration_test.go`,
  `liveness_test.go`, and `liveness_integration_test.go` gains a
  trailing `""` argument, mirroring phase 26's mechanical rollout
  across the same files. No assertion in any of those existing cases
  changes.
- `run_bench_test.go` — no new benchmark. The existing benchmarks gain
  a trailing `""` argument only; a room-stamping assignment is one
  string-length string compare and one field write, not a distinct
  performance concern worth its own benchmark.

### Doc fix in the same phase: `docs/examples/agent-dispatch.md`

The example's `wait` closure calls `rm.Accepts(msg)` and only prints
the result; it never returns the mismatch as an error. Today's
program prints the room rejection on one line, then still prints
`final status: dispatched` one line later, as if the run had
succeeded. This phase fixes the doc in the same change, since the
phase's own fix is what lets the example pass a real `Accepts` call
cleanly:

- Thread the room string through the example's `a.Run(...)` call,
  using the real `rm.ID()` so the value the example signs with
  matches the value the example admits against.
- Change `wait` to return the `Accepts` error instead of only printing
  it, so a room mismatch halts the run and the final printed status
  honestly reflects failure instead of masking it.
- Re-run the example program against the real module before and after
  the edit, per the docs-maintenance skill's example-correctness rule,
  and update the "What the program shows" prose to state that
  `Accepts` now succeeds because `Run` stamps `Room` before signing.
- Update the call-order sequence diagram's `Run(ctx, threadID, m, in,
  wait, bus, hb)` label to `Run(ctx, threadID, m, in, wait, bus, hb,
  room)`.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `agent` and for the total, with
  the new room-stamping line counted in.
- `policy/layers.json` does not change. The `agent` row already lists
  `envelope`; this phase adds no new import edge. No gate diff to
  review for the import policy.
- `api/agent.txt` gains the changed `Run` line, through
  `make api-update` in the same change as the code. No other line in
  `api/agent.txt` changes. No other package's API lock changes.
- `go test -race ./agent/...` passes, covering the room integration
  case alongside the existing race-checked cases.
- `agent/doc.go`'s file map notes the room-stamping line living inside
  `confirmStep` in `run.go`, with no new file, matching the file's
  existing 500-line and 80-line budget in
  `scripts/check_structure.py`.
- `docs/architecture.md`'s `agent/` bullet gains one sentence
  describing the optional `room string` parameter, in the same change
  as the code.
- `docs/packages/agent.md` gains the updated `Run` signature under
  Functions and methods, and one new invariant line under `### Run`
  stating the room-stamping rule, in the same change as the code.
- `docs/protocol-design.md`'s Addressing bullet gains one sentence
  noting `agent.Run` can stamp a caller-chosen room name onto each
  step message before signing, so a plan whose caller supplies one
  produces messages a `room.Room` can admit. Required by AGENTS.md:
  message-semantics changes update `docs/protocol-design.md` in the
  same change as the code.
- `docs/examples/agent-dispatch.md` gains the room-string fix described
  in Tests above, verified by re-running the program against the real
  module. The final printed status must match the program's real
  outcome.
- This phase adds no conformance vector. `Message.Room` already has
  wire-level coverage in `envelope`'s own vectors; this phase composes
  that existing field, it does not add a new one.
