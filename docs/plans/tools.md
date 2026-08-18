# Plan: tools

Status: new. Build phase in docs/plans/agents/phase14_tools.md. This
plan adds one item over that phase contract: `Registry.Remove`, agreed
in an architecture review for symmetry with `room.Room.Admit`/`Remove`.

## Goal

Let an agent call a named action without knowing its concrete type. A
tool registers under a name. The registry resolves the name and runs
the tool. An unknown name fails the same way at lookup and at run.

## Scope

Inside: the `Tool` interface, the `Registry`, and tool execution.
Registration, lookup, removal, and run all live here.

Outside: the agent binding. A future phase wires a `Registry` into an
agent. A tool never sees the agent. Outside: the memory store. Phase
15 owns memory. The `tools` package does not import `agent` or a
future `memory` package.

Phase 16 runs the tool registry as a flow step. A panel step runs in
its own goroutine, so more than one goroutine can call `Add`, `Get`,
`Remove`, and `Run` on the same `Registry` at once, once that wiring
lands. This plan states the concurrency contract now, ahead of that
caller, matching how `room.Room`, `events.Bus`, and `heartbeat.Monitor`
each state their contract in their own plan before every caller
existed.

## API

- `type InOut struct { Value any }` — the tool input payload. A tool
  reads its typed argument through `Value` and asserts the concrete
  type it expects.
- `type Out struct { Value any }` — the tool output payload. A tool
  writes its typed result through `Value`.
- `type Tool interface { Name() string; Run(ctx context.Context, in InOut) (Out, error) }`
  — a named action. `Name` returns the registration key. `Run`
  performs the action and returns its result or an error.
- `type Registry struct` — holds tools by name. Unexported fields.
  Built only through `New`. Registry is safe for concurrent Add, Get,
  Remove, and Run; a sync.RWMutex guards the map.
- `New() *Registry` — builds an empty registry.
- `(*Registry).Add(t Tool) error` — registers `t` under `t.Name()`.
  Rejects a nil `t` with a sentinel error, before it calls `t.Name()`.
  Rejects a blank name (empty after `strings.TrimSpace`) with a
  sentinel error, matching `room.Room.Admit`'s id check. Rejects a
  duplicate name with a sentinel error.
- `(*Registry).Get(name string) (Tool, bool)` — resolves a name.
  Returns `false` when the name is absent.
- `(*Registry).Remove(name string) bool` — removes a name. Returns
  whether the name was present. Removing an absent name is not a
  fault; it returns `false` and changes nothing. After `Remove`,
  `Get` returns `false` for that name, and `Run` fails with the same
  error as any unknown name.
- `(*Registry).Run(ctx context.Context, name string, in InOut) (Out, error)`
  — resolves `name` through `Get` and calls the tool's `Run`. Returns
  the unknown-name error when `Get` reports `false`.

### InOut and Out: a new type, not a reused one

`machine.InOut` bundles one input and one output field in a single
struct, shaped for a transition that mutates a record in place. The
`Tool.Run` signature in the phase 14 contract takes one input value
and returns a separate output value: `Run(ctx, in InOut) (Out, error)`.
Reusing `machine.InOut` as the input type would leave its `Output`
field unused and would still need a distinct `Out` return type the
phase contract already names. It would also add a `tools` to `machine`
import edge that no other requirement in this plan or the phase
contract asks for. The `tools` package defines its own `InOut` and
`Out` types instead. Each wraps one `any` payload, matching the shape
`machine.InOut` uses for a single field, without pulling in
`machine`'s transition-specific `Output` field or its import edge.

`InOut` and `Out` are structs, not named aliases over `any`. A struct
field lets a later phase add a second field, such as a metadata map or
a typed error code, without changing the field name callers already
use or breaking every existing `Tool` implementation's call site. An
alias over `any` would force that same future change onto every
caller's type assertion instead.

### Errors

- `var ErrNilTool` — `Add` returns this for a nil `t`. `Add` checks
  `t == nil` before it calls any method on `t`, so a nil `Tool` never
  panics.
- `var ErrBlankName` — `Add` returns this when `t.Name()` is empty
  after `strings.TrimSpace`. A tool needs a real name to register
  under and to look up later.
- `var ErrDuplicateName` — `Add` returns this for a name already in
  the registry.
- `var ErrUnknownName` — `Get` reports `false` for an unknown name;
  `Run` returns this error when `Get` reports `false`.

## Tests

Test files live in `tools/tools_test/`, an external test package.

- `registry_test.go` — unit, red-green cases for `Add`, `Get`, `Run`,
  and `Remove`.
  - `Add(nil)` returns `ErrNilTool` and does not panic.
  - `Add` rejects a tool whose `Name()` is empty and a tool whose
    `Name()` is whitespace-only, both with `ErrBlankName`.
  - `Add` accepts a new name and rejects a duplicate name with
    `ErrDuplicateName`.
  - `Get` returns the tool and `true` for a registered name; returns
    `nil` and `false` for an unknown name.
  - `Run` calls the tool and returns its result for a registered
    name; returns `ErrUnknownName` for an unknown name.
  - `Remove` on a present name returns `true`; a following `Get`
    returns `false`, and a following `Run` fails with the same error
    as any unknown name.
  - `Remove` on an absent name returns `false` and leaves the
    registry unchanged; a follow-up `Get` for an unrelated registered
    name still succeeds.
- `registry_integration_test.go` — register two tools, resolve each by
  name, and run one. Prove a duplicate `Add` fails. Prove an unknown
  name fails `Run`. Extend with a remove-then-run case: register a
  tool, run it once to prove it works, remove it, then prove `Run`
  fails for that name the same way it fails for a name that was never
  registered.
- `registry_concurrent_test.go` — modeled on
  `heartbeat`'s `monitor_concurrent_test.go` pattern: N goroutines,
  a concrete outcome asserted, run under `go test -race`.
  1. N goroutines each call `Add` with a distinct name concurrently,
     then join. A following loop of `Get` calls must find every one
     of the N names, proving concurrent `Add` calls all land.
  2. One tool is registered up front. N goroutines call `Run` for its
     name concurrently while N other goroutines call `Add` for N
     distinct other names concurrently, then join. Every `Run` call
     must return the registered tool's result with no error, and a
     following `Get` loop must find all N added names, proving reads
     and writes on the map do not corrupt each other under `-race`.
  3. A tool is registered, then N goroutines race one `Remove` call
     for its name against N `Run` calls for the same name. Exactly one
     outcome is valid per `Run` call: either the tool's result (it ran
     before removal) or `ErrUnknownName` (it ran after removal). No
     call may panic or return any other error, proving `Remove` and
     `Run` serialize correctly against each other.
- `registry_bench_test.go` — benchmark `Run` on a registry of one
  hundred tools. Target under one microsecond per call. State the
  allocation budget for one `Run` call.

## Verification

`make verify` passes. The coverage floor for `tools` holds at or above
85 percent. The `tools` row in `policy/layers.json` lists its allowed
imports. `api/tools.txt` lands via `make api-update` and locks `Tool`,
`Registry`, `InOut`, `Out`, `New`, `Add`, `Get`, `Remove`, `Run`,
`ErrNilTool`, `ErrBlankName`, `ErrDuplicateName`, and `ErrUnknownName`.
`go test -race ./tools/...` passes, covering
`registry_concurrent_test.go`.
