# Phase 16: system integration

Status: ready to build. Builds on every prior phase: `identity`,
`discovery`, `flow`, `machine`, `envelope`, `events`, `a2a`, `room`,
`tools`, `memory`, and `agent`. All ten packages ship today. This
phase adds no package and no exported symbol. It adds two test files
under `agent/agent_test/` that wire the shipped blocks into one real
exchange.

## Goal

Prove the blocks compose into a working agent pair, in one process,
with no mock at the trust boundary. Agent A sends a signed request.
Agent B verifies the signature, checks room admission, runs a tool,
stores the shared context, and replies with a confirmed ack. The test
calls `envelope.VerifyThread` on the exchanged messages itself, not
only through the `agent` package's internal event.

## Scope

Inside: one two-agent exchange, one shared room, one flow step, one
tool call, one memory write, and one thread verification. The test
uses two real `identity.Identity` values, a real `room.Room`, a real
`tools.Registry`, a real `memory.Store`, and a real `events.Bus`. It
routes the request message through `a2a.ToPart`/`a2a.FromPart` once,
as an in-process stand-in for a transport hop; this is the "a2a
stub" the phase's original scope note names. No network call and no
`a2aclient` import: that package stays out of scope for this phase.

Outside: multiple concurrent rooms, a second flow step, retries, and
any change to an existing package's API. `agent.Agent` already
composes `identity`, `discovery`, and `flow` for the sending side;
this phase does not add a second `agent.Agent` for the receiving
side. Agent B is represented by its `identity.Identity`, its own
`tools.Registry`, and its own `memory.Store`, wired into the
`agent.AckWait` closure the test hands to Agent A's `Run`. This
matches the existing pattern in
`agent/agent_test/lifecycle_integration_test.go`: the receiver's
behavior is real code the test drives, not a mock. This phase differs
from that file only in what the closure does: it verifies a signature,
checks room membership, runs a tool, and writes to a memory store,
instead of building a canned `envelope.Ack` in place.

## API

No new exported symbol in any package. The phase confirms the ten
shipped packages work together as built. No row changes in
`policy/layers.json`. `scripts/check_deps.py`'s `package_dirs`
(scripts/check_deps.py:14-20) walks only the repo root's immediate
subdirectories and globs `*.go` in each, non-recursively. It never
descends into `agent/agent_test/`, so that directory is not a
"package" the gate inspects at all, independent of any test-file
exemption. `agent/agent_test/` is a test package nested inside
`agent/`, not a new top-level package, so `scripts/check_plan.py`
does not scan it for its own plan file.

## Tests

Test files live in `agent/agent_test/`, matching the flat layout in
`docs/plans/agents/PHASES.md`:

- `exchange_integration_test.go`
- `exchange_bench_test.go`

### exchange_integration_test.go

`exchangeFixture(t testing.TB)` builds the shared state both tests
use:

1. Two identities: `idA, _ := identity.New()` (the sender) and
   `idB, _ := identity.New()` (the receiver).
2. A `discovery.Card` for Agent A: `Name: "Requester"`,
   `Description: "..."`, `Capabilities: []string{"exchange"}`.
3. A one-step `flow.Definition`:
   `flow.New([]flow.Step{{ID: "request", To: "fulfilled", Payload:
   "run:echo:hello from agent A"}}, nil)`. One step, no panel: it
   gates on `Confirm`, matching `agent.Run`'s documented per-step
   signing path.
4. `agent.New(idA, card, plan)` builds Agent A.
5. A `machine.Definition`:
   `machine.New("start", machine.Transition{From: "start", To:
   "fulfilled", Trigger: "go-request"})`.
6. `room.New("exchange-room", idA.Signer())`, then
   `r.Admit(idB.Signer(), idA.Signer())`, so both signers are room
   members before any message moves.
7. `tools.New()` for Agent B's registry. `Add` one tool named
   `"echo"` whose `Run` reads `in.Value.(string)`, strips the
   `"run:echo:"` prefix, and returns `tools.Out{Value: <the
   remainder>}`. An input that carries no `"run:echo:"` prefix
   returns an error; the test never triggers that path. The
   malformed-input and budget-eviction paths are covered by tools'
   and memory's own suites; this phase asserts only the composed
   happy and rejected-admission paths.
8. `memory.New(4096)` for Agent B's shared context store.
9. `events.New()` for the bus `Run` takes.

`TestExchangeSignedRequestConfirmedAck` is the end-to-end case:

- The test's `agent.AckWait` closure represents Agent B. On each call
  it: (a) maps the received `envelope.Message` through `a2a.ToPart`,
  then immediately back through `a2a.FromPart` with the same
  `ContextID`/`MessageID` the mapping produced, standing in for one
  transport hop; (b) calls `VerifySignature` on the round-tripped
  message and fails the test on a non-nil error, since a broken
  signature after the hop would mean the trust boundary is not real;
  (c) calls `r.Accepts` on the round-tripped message, using the
  shared `room.Room`; (d) runs `registry.Run(ctx, "echo",
  tools.InOut{Value: msg.Payload})` and requires a nil error; (e)
  calls `store.Put([]byte(out.Value.(string)))` and keeps the
  returned ref; (f) appends the round-tripped message to a
  test-local slice, so the test can call `envelope.VerifyThread`
  itself after `Run` returns; (g) builds and confirms an
  `envelope.Ack`: `ack, err := envelope.NewAck(msg, idB.Signer(),
  out.Value.(string))`, returns `err` on a non-nil error (`NewAck`
  returns `(Ack, error)`), then returns `ack.Confirm(), nil`.
- The test calls `a.Run(ctx, "exchange-thread-1", m, machine.InOut{},
  wait, bus, nil, r.ID())`.
- Assertions: `Run` returns a nil error and
  `machine.Status("fulfilled")`. The captured message slice has
  exactly one entry, and it passes `.Validate()`. Calling
  `envelope.VerifyThread` on the captured slice returns nil: the test
  proves the thread verifies from its own vantage point, not only
  through `agent`'s internal `EmitThreadVerified` call. `store.Get`
  on the ref from step (e) returns `"hello from agent A"`, proving
  the shared context landed in Agent B's memory. The bus recorded
  `agent.MessageDeliveredEvent`, `agent.MessageAckedEvent`,
  `flow.StepCompletedEvent`, then `agent.ThreadVerifiedEvent`, in
  that order, matching the sequence
  `lifecycle_integration_test.go` already asserts for one gated step.

`TestExchangeRejectsUnadmittedReceiver` proves the trust boundary is
real, not decorative:

- Same fixture, except the test skips `r.Admit(idB.Signer(), ...)`,
  so Agent B's signer is not a room member.
- The `AckWait` closure runs the same round trip and calls
  `r.Accepts`. `Accepts` returns a non-nil error
  (`errors.Is(err, room.ErrNotMember)` after unwrapping, since
  `Accepts` wraps it). The closure returns that error instead of
  building an ack.
- `a.Run` returns a non-nil error and `machine.Status("")`. The bus
  never records `agent.ThreadVerifiedEvent`. The tool registry's
  `Run` and the memory store's `Put` are never called: the test
  asserts a counter incremented inside the tool's `Run` method stays
  at zero, proving `Accepts` gates the tool call, not just the ack.

### exchange_bench_test.go

`BenchmarkExchange` runs the full `exchangeFixture` setup once
outside the timed loop, then times `a.Run` alone, once per
iteration, with a fresh `threadID` per iteration (for example
`fmt.Sprintf("bench-thread-%d", i)`) so `PrevHash` chaining never
crosses iterations. `b.ReportAllocs()` runs; the benchmark states the
measured allocation count per run in a comment once the baseline
exists, matching the convention in `a2a/a2a_test/mapping_bench_test.go`
and `agent/agent_test/run_bench_test.go`. Target: under ten
milliseconds per exchange on the reference machine, matching the
phase's original target. The bench does not assert the target in
code; `b.N`'s wall time is read from `go test -bench` output, matching
every other benchmark file in this module.

The red-green file has no role in this phase. Both files are
integration-only, per the phase's original scope note.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- `go test -race ./...` passes across the module, with the two new
  files included.
- `go test -race ./agent/...` shows both new `Test` functions passing
  and `BenchmarkExchange` compiling and running under `go test -bench
  BenchmarkExchange -benchtime 1x ./agent/...`.
- No `api/*.txt` file changes: the phase adds no exported symbol. A
  diff in any `api/` file during this phase's review is a finding
  that the change strayed outside scope.
- No `policy/layers.json` change: `scripts/check_deps.py` passes
  unchanged, since the new files are `_test.go` files inside an
  existing package's test subdirectory.
- The coverage floor of 85 still holds for `agent` and for the total.
  The new tests exercise `agent.Run`, `agent.confirmStep`,
  `agent.EmitMessageDelivered`, `agent.EmitMessageAcked`, and
  `agent.EmitThreadVerified`, all already covered by earlier phases;
  this phase adds assertions, not new lines to cover.
- The phase adds no conformance vector. It defines no new wire
  schema; it composes `envelope.Message`, `envelope.Ack`, and
  `a2a.Mapped`, all already vector-covered in their own packages.
- The agent work is complete when this phase passes. No later phase
  in `docs/plans/agents/PHASES.md`'s "System" group follows it.
