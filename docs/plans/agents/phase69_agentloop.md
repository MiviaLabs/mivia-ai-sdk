# Phase 69: agentloop

Status: plan, ready for plan review. Ships as one new composition
package, `agentloop`. It depends on phase 62, which is plan-only
today. Phase 70 completes the composition; see "Both halves, or
neither" below.

## Why this phase exists

The SDK has every part of a model-driven tool loop except the loop.

- `provider.Response.ToolCalls` exists. No package outside `provider`
  reads it.
- `provider.Request.Tools` exists. No package builds it from a
  `tools.Registry`.
- `subagent.ProviderTool` sends one user message and returns
  `resp.Message.Content`. It drops `resp.ToolCalls` on the floor.
- `agentrun` binds a tool to a step by step identifier, from a
  developer-authored `flow.Definition`. The model picks nothing.

The result is a workflow engine with a model available as one step
type. That is a coherent product. It is not the product the term
"agent SDK" names to most readers.

Phases 62 through 68 do not close this gap. Phase 62 adds
`Message.ToolCalls`, which the loop needs to replay history. Phase 64
adds schema validation, which the loop needs to check arguments.
Phases 63, 65, 66, 67, and 68 add skills, context state, planning,
spooling, and file leaves. None of the seven contains a component that
reads `Response.ToolCalls` and dispatches to a `tools.Registry`. The
scaffolding is planned. The load-bearing piece is absent.

## Both halves, or neither

The product is an agent SDK with an orchestration engine. The model
chooses the next tool, and a chosen tool may itself be a graph. The
two composition models nest in both directions.

One direction already ships. `subagent.FlowTool` wraps a
`flow.Definition` as a `tools.Tool`. `subagent.AsTool` wraps a whole
`agentrun.Runner` as one. A model that can call tools can therefore
already call a flow and spawn a subagent, the moment a loop exists to
do the calling.

The other direction is this phase. `agentloop` runs the loop, and
phase 70 wraps a built loop as a `tools.Tool`, so a `flow.Step` can
bind it by step identifier.

One constraint makes this real, and it is why phase 70 is not
optional. `Definitions` skips a tool that publishes no parameter
schema, so the model is never offered a tool the loop cannot call.
Every one of `subagent`'s ten tools reads its input through the
unexported `stringValue` helper and publishes no schema. Without
phase 70 the model sees none of them. The orchestration half would
sit behind glass: present, documented, and unreachable by the model.
Ship this phase alone and the two halves do not connect.

`agentloop` changes no existing behavior. A `flow.Definition` caller
sees no difference.

## Documentation correction, independent of this phase

README line 242 reads "A model in the loop: swap the hand-written tool
for `subagent.ProviderTool`". `ProviderTool` runs one turn and returns
text. It is a model in a step, not a model in a loop. Correct that
line whichever product ships. Correct `docs/packages/subagent.md` in
the same change, and state on `ProviderTool`'s doc comment that it
discards tool calls.

## Goal

Let a caller run a model until it stops asking for tools. `Run` takes
a `provider.Completer`, a `tools.Registry`, and a starting message
list. It offers the registry's tools to the model, runs the tool calls
the model requests, appends the results as `RoleTool` messages, and
repeats until the model returns no tool call or a bound trips.

## Scope

Inside:

- `Run`, the loop, and its `Options` config struct.
- `Definitions`, which builds `[]provider.ToolDefinition` from a
  `tools.Registry`.
- The argument decode path: raw JSON bytes to `tools.InOut`.
- The result render path: `tools.Out` to `RoleTool` message content.
- Termination bounds: maximum iterations, maximum tool calls per
  turn, and context cancellation.
- The tool-failure policy: report the error to the model as a tool
  result, or end the run.
- A `Result` value holding the final message, the message history,
  the iteration count, and the summed `provider.Usage`.

Outside:

- Any change to `flow`, `agent`, `agentrun`, or `machine`. This phase
  adds a second composition path beside the graph, not a change to it.
- Any change to `tools.Tool`, `tools.InOut`, or `tools.Out`. The new
  schema and decode capabilities arrive as optional interfaces, which
  matches `ProfiledTool`, `ResultBudgetTool`, and `PrivilegedTool`.
- Concurrent execution of one turn's tool calls. The first build runs
  them in `ToolCall.Index` order. Concurrency is a later phase, and it
  needs a resource-key policy that `ExecutionProfile.ResourceKey`
  already anticipates.
- Context window management. Phase 66 owns that. `agentloop` accepts
  an optional caller hook to trim history and calls nothing itself.
- Any provider-specific forced-tool-call or structured-output field.

## The three structural problems

### A tool declares no parameter schema

`tools.Tool` is `Name() string` and
`Run(ctx, InOut{Value any}) (Out, error)`. A model needs a parameter
schema to call a tool. A dispatcher needs a decoder to turn the
model's JSON bytes into the `any` the tool expects.

Add one optional interface in `tools`, following the package's own
precedent:

```go
// SchemaTool is an optional interface. A Tool implements it to
// publish its parameter schema and decode raw argument bytes.
type SchemaTool interface {
    ParameterSchema() []byte
    DecodeArguments(raw []byte) (InOut, error)
}
```

Add `SchemaOf(t Tool) ([]byte, bool)` beside `ExecutionProfileOf`.
`Definitions` skips a tool that does not implement `SchemaTool`, so
the model is never offered a tool the loop cannot call. `Definitions`
returns the skipped names, so a caller can fail loudly instead.

`DecodeArguments` sits on the tool because only the tool knows its own
input type. The alternative, reflection in `agentloop`, is forbidden
in packages and would fork the type mapping.

`mcp` already maps a remote tool into a `tools.Tool`, and the MCP
protocol carries an input schema per tool. Make the `mcp` wrapper
implement `SchemaTool`. Every MCP tool then reaches the model with no
further caller work. This is the strongest single argument for the
interface's shape.

### A tool result is untyped

`Out.Value` is `any`. A `RoleTool` message carries a string.

Render in a fixed order. Use the value when it is a `string`. Use the
bytes when it is `[]byte` and valid UTF-8. Otherwise marshal to JSON.
Return a named error when marshalling fails. Apply
`tools.ResultBudgetOf` when the tool publishes a budget, and truncate
with a stated marker.

### Two types are named ToolCall

`provider.ToolCall` is the model's request. `tools.ToolCall` is the
approval-gate record. `agentloop` imports both. Import `tools.ToolCall`
under an alias in the one file that needs it, and name no new type
`ToolCall`.

## Placement and import policy

`agentloop` is a new top-level package. No package may import both
`provider` and `tools` today except `subagent` and `e2e`. Extending
`subagent` is the cheaper move and the wrong one: `subagent` is the
"SDK blocks as tools" package, and the loop is a caller of blocks, not
a block exposed as a tool.

Add one row to `policy/layers.json`:

```json
"agentloop": ["provider", "tools", "trace", "hooks", "usage", "events"]
```

`trace` opens one span per iteration and one per tool call, matching
`agentrun`'s tracer wiring. `hooks` fires `PointPreTool` and
`PointPostTool` per tool call, and `PointStop` once at the end, so the
existing veto vocabulary governs a model-chosen call exactly as it
governs a step-bound one. `usage` accumulates per iteration.
`events` carries the loop's own events. Each of the four is optional
at the `Options` level and unused when nil.

`agentloop` must not import `subagent`. `subagent` imports `agentloop`
in phase 70, and the edge runs one way only. `agentloop` imports no
package that imports it. The direction stays inward.

## Recursion is already bounded

A loop can call a flow tool, which can spawn a runner, which can bind
another loop. The recursion is real, and it needs no new guard.

`subagent.AsTool` carries a ctx-borne depth counter and stops at
`ErrMaxDepth`, three by default. That guard sits at the spawn point,
so it bounds the chain whatever runs between two spawns. `agentloop`
adds `MaxIterations`, which bounds one loop's turns. The two bounds
compose: depth caps how deep, iterations cap how long. This phase
adds no third counter and duplicates nothing.

## API

- `type Options struct` — fields: `Completer provider.Completer`,
  `Tools *tools.Registry`, `Scope *tools.Scope`, `Model string`,
  `MaxIterations int`, `MaxCallsPerTurn int`, `OnToolError ErrorPolicy`,
  `Hooks *hooks.Registry`, `Tracer *trace.Tracer`,
  `Usage *usage.Accumulator`, `SessionID string`, `Bus *events.Bus`,
  `Trim func([]provider.Message) []provider.Message`.
- `func (o Options) Validate() error` — enforces every invariant this
  plan states. `Completer` and `Tools` are required. `MaxIterations`
  must be positive. `Usage` requires `SessionID`.
- `func New(opts Options) (*Loop, error)` — validates, then binds.
- `type Loop struct` — built only through `New`.
- `func (l *Loop) Run(ctx context.Context, msgs []provider.Message) (Result, error)`
- `type Result struct` — `Final provider.Message`,
  `History []provider.Message`, `Iterations int`,
  `Usage provider.Usage`, `Stop StopReason`.
- `type StopReason string` and its constants: `StopNoToolCalls`,
  `StopMaxIterations`, `StopToolError`, `StopHookVeto`.
- `type ErrorPolicy string` and its constants: `ErrorPolicyReport`,
  `ErrorPolicyFail`. `ErrorPolicyReport` is the zero value and sends
  the tool's error text back as the tool result.
- `func Definitions(reg *tools.Registry, scope *tools.Scope) ([]provider.ToolDefinition, []string, error)`
  — the second return holds the names skipped for a missing schema.
- Sentinel errors: `ErrNoCompleter`, `ErrNoTools`, `ErrMaxIterations`,
  `ErrUnrenderableResult`, `ErrCallsPerTurnExceeded`.
- In `tools`: `type SchemaTool interface` and
  `func SchemaOf(t Tool) ([]byte, bool)`.

`Run` calls `Registry.RunScoped`, never `Registry.Run`. A
model-chosen call is the case tool scoping and approval exist for. A
model must not reach a privileged tool that a `Scope` allowlist
withholds.

Every entry lands in `api/agentloop.txt` and `api/tools.txt` through
`make api-update`.

## Sequencing

Phase 62 is a hard prerequisite. Without `Message.ToolCalls`, the loop
cannot place an assistant turn's tool calls into the next request, so
the model loses the calls it just made. Build phase 62 first.

Phase 64 is a soft prerequisite. Without it, `agentloop` decodes
arguments through the tool's own `DecodeArguments` and validates
nothing. That is a working first build. Wire `schema` into the decode
path once phase 64 ships, and turn a validation failure into a
corrective tool result.

Build order: phase 62, then this phase, then phase 70, then the
phase 64 wiring.

## Phase 70, stated here because this phase is incomplete without it

Phase 70 gets its own plan file before a builder starts. This section
states its contract, so plan review can judge the two together.

Phase 70 makes every shipped tool reachable by a model.

- `subagent` adopts `tools.SchemaTool` on all ten of its tools. Six
  of them already decode a JSON command struct through the unexported
  `decodeCommand` helper, so their schema exists in Go and is merely
  unpublished. `ParameterSchema` publishes it, and `DecodeArguments`
  replaces the `stringValue` cast for those six. The four
  string-payload tools publish a one-string-property schema.
- `mcp` adopts `tools.SchemaTool` on its remote-tool wrapper. The MCP
  protocol already carries an input schema per tool, so this maps a
  field that exists to a field that exists.
- `subagent.LoopTool` wraps a built `agentloop.Loop` as a
  `tools.Tool`, matching `FlowTool` and `ProviderTool` in naming and
  shape. It lands in `subagent`, not `agentloop`, because `subagent`
  is this module's one "blocks as tools" package. It adds `agentloop`
  to `subagent`'s `policy/layers.json` row.

An integration test then closes the circle: a model-driven loop calls
a `FlowTool`, whose graph binds a step to a `LoopTool`, whose inner
model calls a leaf tool. That test is the proof the two halves
connect. Without it, "agent SDK with an orchestration engine" is a
claim, not a verified property.

## Tests

`agentloop/agentloop_test/`:

- `options_test.go` — one case per invariant `Validate` claims.
- `definitions_test.go` — a registry with schema-bearing and
  schema-free tools yields the right definitions and skip list. A
  `Scope` denial removes a tool from the offered set.
- `loop_test.go` — the red-green cases. A response with no tool call
  ends the loop at one iteration. A response with one tool call runs
  the tool and appends a `RoleTool` message whose `ToolCallID` matches
  the call. Two calls in one turn run in `Index` order. An unknown
  tool name reports `tools.ErrUnknownName` under `ErrorPolicyReport`
  and fails under `ErrorPolicyFail`. A model that always calls a tool
  stops at `MaxIterations` with `StopMaxIterations`. A `PointPreTool`
  veto stops with `StopHookVeto` and runs no tool. A canceled ctx
  returns the ctx error.
- `render_test.go` — the render order, the JSON fallback, the
  unrenderable case, and the `ResultBudgetOf` truncation.
- `loop_integration_test.go` — a scripted `Completer` and a real
  `tools.Registry` run a two-tool, three-iteration task end to end.
  A second case runs the same loop wrapped as one `flow.Step` tool
  through `agentrun`, proving the two composition models nest.
- `loop_bench_test.go` — one iteration's allocation cost, with the
  baseline recorded in this plan before the phase closes.

In `tools/tools_test/`: `schema_test.go` covers `SchemaOf` on a tool
that implements `SchemaTool`, one that does not, and a typed nil.

Every scripted `Completer` lives in the test package. No concrete
model client ships in this SDK, and this phase adds none.

## Verification

`make verify` passes: gofmt, vet, the race detector, the coverage
floor at 85 percent for the new package and the total, the doc gate,
the structure gate, the plan gate, the deps gate against the new
`policy/layers.json` row, the API gate against the regenerated locks,
the Semgrep scan, and the probe suite.

`make api-update` runs, and the `api/agentloop.txt` and
`api/tools.txt` diffs land in the same change.

This phase adds no gate and weakens none.
