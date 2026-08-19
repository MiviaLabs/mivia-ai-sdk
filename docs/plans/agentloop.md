# Plan: agentloop

Status: plan, ready for plan review. See
`docs/plans/agents/phase69_agentloop.md` for the full design
rationale. This file is the declarative contract `check_plan.py` and
`make api-update` gate against.

## Goal

Let a caller run a model until it stops asking for tools. `Run` takes
a `provider.Completer`, a `tools.Registry`, and a starting message
list. It offers the registry's tools to the model, runs the tool
calls the model requests, appends the results as `RoleTool` messages,
and repeats until the model returns no tool call or a bound trips.

## Scope

Inside:

- `Run`, the loop, and its `Options` config struct.
- `Definitions`, which builds `[]provider.ToolDefinition` from a
  `tools.Registry`, skipping a tool with no published schema.
- The argument decode path: raw JSON bytes to `tools.InOut`, through
  the called tool's own `DecodeArguments`.
- The result render path: `tools.Out` to `RoleTool` message content.
- Termination bounds: maximum iterations, maximum tool calls per
  turn, a cumulative token ceiling across the whole run, and context
  cancellation.
- The tool-failure policy: report the error to the model as a tool
  result, or end the run.
- A `Result` value holding the final message, the message history,
  the iteration count, and the summed `provider.Usage`.
- One new optional interface in the existing `tools` package,
  `SchemaTool`, and one new helper, `SchemaOf`, so a `Tool` can
  publish a parameter schema and decode raw argument bytes without a
  change to `Tool`, `InOut`, or `Out`.
- One new enumeration method on the existing `tools.Registry`,
  `Tools() []Tool`, so `Definitions` can walk every registered tool
  and call `SchemaOf` on each without a second lookup.

Outside:

- Any change to `flow`, `agent`, `agentrun`, or `machine`. This
  package adds a second composition path beside the graph, not a
  change to it.
- Any change to `tools.Tool`, `tools.InOut`, or `tools.Out`.
- Concurrent execution of one turn's tool calls. The first build runs
  them in `ToolCall.Index` order.
- Context window management. `agentloop` accepts an optional caller
  hook to trim history and calls nothing itself. The hook receives
  ctx and can fail; see `Options.Trim` below.
- Any provider-specific forced-tool-call or structured-output field.
- Wrapping a built `Loop` as a `tools.Tool`. That is phase 70's
  `subagent.LoopTool`, a separate plan.
- Adopting `SchemaTool` on existing tools (`subagent`, `mcp`).
  That is phase 70's scope. One exception: `spool`'s `SpoolTool`
  wrapper gains schema forwarding in this change, because a wrapper
  that strips a capability silently is a defect, not a deferral. See
  `docs/plans/spool.md`.

## API

### New package `agentloop`

- `type Options struct` — `Completer provider.Completer`,
  `Tools *tools.Registry`, `Scope *tools.Scope`, `Model string`,
  `MaxIterations int`, `MaxCallsPerTurn int`, `MaxTotalTokens int`,
  `OnToolError ErrorPolicy`, `Hooks *hooks.Registry`,
  `Tracer *trace.Tracer`, `Usage *usage.Accumulator`,
  `SessionID string`, `Bus *events.Bus`,
  `Budget *contextbudget.Limits`,
  `Trim func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)`.
- `func (o Options) Validate() error` — `Completer` and `Tools` are
  required. `MaxIterations` must be positive. `Usage` requires
  `SessionID`. A non-nil `Budget` must pass
  `contextbudget.Limits.Validate`. `MaxCallsPerTurn` zero means
  unbounded, matching `flow.LoopPolicy.Max`'s zero-means-unbounded
  precedent. `MaxTotalTokens` must not be negative; zero means
  unbounded, the same precedent.
- `func New(opts Options) (*Loop, error)` — validates, then binds.
- `type Loop struct` — built only through `New`. Unexported fields.
- `func (l *Loop) Run(ctx context.Context, msgs []provider.Message) (Result, error)`
  — calls `Registry.RunScoped`, never `Registry.Run`, so a
  model-chosen call always passes through `l.Scope`.
- `type Result struct` — `Final provider.Message`,
  `History []provider.Message`, `Iterations int`,
  `Usage provider.Usage`, `Stop StopReason`.
- `type StopReason string` with constants `StopNoToolCalls`,
  `StopMaxIterations`, `StopToolError`, `StopHookVeto`.
- `type ErrorPolicy string` with constants `ErrorPolicyReport` (zero
  value; sends the tool's error text back as the tool result) and
  `ErrorPolicyFail`. A `DecodeArguments` failure on malformed
  model-supplied JSON arguments is a tool-run error and goes through
  the same `OnToolError` policy as a failed `Run`.
- `func Definitions(reg *tools.Registry, scope *tools.Scope) ([]provider.ToolDefinition, []string, error)`
  — the second return holds the names skipped for a missing schema.
  Definitions fails closed: when the offered set is empty and the
  skip list is not, it returns `ErrNoSchemas`. An empty Registry
  returns an empty set and no error. Before phase 70 every subagent
  tool is schema-less; a silent skip there would end the run with
  `StopNoToolCalls`, indistinguishable from success.
- Sentinel errors: `ErrNoCompleter`, `ErrNoTools`, `ErrMaxIterations`,
  `ErrUnrenderableResult`, `ErrCallsPerTurnExceeded`, `ErrNoSchemas`,
  `ErrOverBudget`, `ErrTokenBudgetExceeded`.

`Options.Trim` runs before each `Completer` call on the full message
history. Its signature is type-compatible with a closure over
`contextplan.Planner.Plan`: `Plan`'s signature is
`(ctx, *contextstate.Session, Window, provider.TokenEstimator) (PlanResult, error)`
and never consumes `Trim`'s `msgs []provider.Message` argument, so a
binding closure must discard `msgs` and read history from the
`Session` it closes over instead. Keeping that `Session` synchronized
with the loop's growing history is the caller's responsibility, out
of this plan's scope; context-window management belongs to phase 66
(see Scope, Outside). A non-nil trim error fails the run for that
iteration, wrapped with the iteration count. A nil `Trim` passes the
history through unchanged.

`Options.Budget` caps the loop's context, following `agent.Run`'s
`ErrOverBudget` precedent. Before each `Completer` call, `Run` sums
the message content bytes and the message count, and calls
`Budget.Fits`. A failure returns `ErrOverBudget` wrapped with the
iteration count. A nil `Budget` means uncapped.

A positive `MaxCallsPerTurn` trips when one turn's response requests
more calls than the bound: that turn fails the run
(`ErrCallsPerTurnExceeded` wrapped), before any call in the turn
runs. This trip always fails the run, regardless of `OnToolError`.
It is not routed through the report/fail policy: `OnToolError`
governs what `Run` does with a tool-run error that has a
`ToolCallID` to attach a report to, and the trip happens before any
call executes, so there is no call and no `ToolCallID` to report
against. A zero `MaxCallsPerTurn` means unbounded, matching
`flow.LoopPolicy.Max`'s precedent.

A positive `MaxTotalTokens` caps the run's cumulative spend, not one
call's context. `MaxIterations` bounds turns, `MaxCallsPerTurn`
bounds calls within a turn, and `Budget` bounds one call's message
size; none of the three bounds tokens actually billed across the
run. `Run` keeps its own running token total, seeded at zero and
independent of `Options.Usage` (which is optional, keyed by
`SessionID`, and persists across runs). After each `Completer` call
returns, `Run` adds `resp.Usage.TotalTokens` to that running total,
then checks it against `MaxTotalTokens`. A total over the cap fails
the run with `ErrTokenBudgetExceeded`, wrapped with the iteration
count; the response that tripped the cap is still appended to
history and still recorded onto `Options.Usage`, so a caller with a
`Usage` accumulator sees the exact spend that ended the run, not an
undercount. A zero `MaxTotalTokens` means unbounded.

Hitting `MaxIterations` is not an error: `Run` returns
`Result{Stop: StopMaxIterations}, nil`, a normal, graceful stop, the
same shape as `StopNoToolCalls`. `ErrMaxIterations` is reserved for
`Options.Validate()` rejecting a non-positive `MaxIterations` value —
a construction-time validation error, not a runtime stop.

`trace.Tracer` opens one span per iteration and one per tool call.
`hooks.Registry` fires `PointPreTool` and `PointPostTool` per tool
call, and `PointStop` once at the end. `usage.Accumulator` records
per iteration under `SessionID`. `events.Bus` carries the loop's own
events. Each of the four is optional and unused when nil.

`agentloop` names no new type `ToolCall`, since `provider.ToolCall`
already names the model's request; `agentloop` never constructs or
reads a `tools.ToolCall` itself, since `Registry.RunScoped` builds
one internally.

### Additions to the existing `tools` package

```go
// SchemaTool is an optional interface. A Tool implements it to
// publish its parameter schema and decode raw argument bytes.
type SchemaTool interface {
    ParameterSchema() []byte
    DecodeArguments(raw []byte) (InOut, error)
}

func SchemaOf(t Tool) ([]byte, bool)

// Tools returns a snapshot of every registered Tool, sorted by
// name. The result is a fresh slice; mutating it does not affect
// the Registry.
func (r *Registry) Tools() []Tool
```

`SchemaOf` returns `t.ParameterSchema()` and true when `t` implements
`SchemaTool`; else it returns `nil, false`. It follows the package's
own `ExecutionProfileOf` precedent: an optional marker, checked
through a type assertion, with a paired accessor. `DecodeArguments`
sits on the tool, not in `agentloop`, because only the tool knows its
own input type; reflection in `agentloop` is forbidden by this
module's no-third-party, no-reflection-mapping convention and would
fork the type mapping.

`Registry.Tools()` is the enumeration primitive `Definitions` needs:
it walks the returned slice and calls `SchemaOf` per tool, sorted so
the built `[]provider.ToolDefinition` order is deterministic and
test-stable. `Add`, `Get`, `Remove`, `Run`, and `RunScoped` are
unchanged.

`mcp/tools.go` already defines and locks an exported `SchemaTool`
interface (`InputSchema() any`), a different shape than this plan's
`tools.SchemaTool`; phase 70 must rename or remove `mcp.SchemaTool`
when `mcp` adopts `tools.SchemaTool`, so the name collision is a
tracked, deliberate decision and not an oversight.

`SchemaTool`, `SchemaOf`, and `Registry.Tools()` all land in
`api/tools.txt`; `Definitions` and the rest of the new package land
in `api/agentloop.txt`. Both diffs land in the same change through
`make api-update`.

## Placement and import policy

`agentloop` is a new top-level package. Add one row to
`policy/layers.json`:

```json
"agentloop": ["provider", "tools", "trace", "hooks", "usage", "events", "contextbudget"]
```

`agentloop` must not import `subagent`. `subagent` imports
`agentloop` starting in phase 70; the edge runs one way. `agentloop`
imports no package that imports it, so the dependency direction
stays inward.

`tools`'s row in `policy/layers.json` stays `[]`. `SchemaTool` and
`SchemaOf` use only `context`-free, standard-library-only code, the
same footprint the package already holds.

## Tests

`agentloop/agentloop_test/`, one external package:

- `options_test.go` — one case per invariant `Validate` claims,
  including `MaxCallsPerTurn == 0` passing validation (unbounded), a
  negative `Budget` field failing validation, `MaxTotalTokens == 0`
  passing validation (unbounded), and a negative `MaxTotalTokens`
  failing validation.
- `definitions_test.go` — a registry with schema-bearing and
  schema-free tools yields the right definitions and skip list. A
  `Scope` denial removes a tool from the offered set. A registry
  whose every tool lacks a schema fails with `ErrNoSchemas`.
- `loop_test.go` — the red-green cases. A response with no tool call
  ends the loop at one iteration. A response with one tool call runs
  the tool and appends a `RoleTool` message whose `ToolCallID`
  matches the call. Two calls in one turn run in `Index` order. An
  unknown tool name reports `tools.ErrUnknownName` under
  `ErrorPolicyReport` and fails under `ErrorPolicyFail`. A model that
  always calls a tool stops at `MaxIterations` with
  `StopMaxIterations`. A `PointPreTool` veto stops with
  `StopHookVeto` and runs no tool. A canceled ctx returns the ctx
  error. A tool whose `DecodeArguments` fails on malformed
  model-supplied JSON reports the decode error under
  `ErrorPolicyReport` and fails the run under `ErrorPolicyFail`, the
  same as a failed `Run`. A `Budget` that the history outgrows fails
  the run with `ErrOverBudget`. A `Trim` hook returning an error
  fails the run. A turn whose response requests more calls than a
  positive `MaxCallsPerTurn` fails the run before any call in that
  turn runs, asserting `ErrCallsPerTurnExceeded` under both
  `ErrorPolicyReport` and `ErrorPolicyFail`, since the trip is
  policy-independent. A model that always calls a tool with
  `MaxIterations` reached asserts
  `Result{Stop: StopMaxIterations}` returned with `err == nil`. A
  scripted `Completer` whose responses' summed `Usage.TotalTokens`
  crosses a positive `MaxTotalTokens` on the second iteration fails
  the run with `ErrTokenBudgetExceeded` wrapped with the iteration
  count, and a paired case with `Options.Usage` set asserts the
  tripping call's tokens still landed in the accumulator's total. A
  zero `MaxTotalTokens` with the same scripted responses runs to
  `StopMaxIterations` unaffected.
- `render_test.go` — the render order (string, then UTF-8 bytes,
  then JSON fallback), the unrenderable case, and the
  `ResultBudgetOf` truncation.
- `loop_integration_test.go` — a scripted `Completer` and a real
  `tools.Registry` run a two-tool, three-iteration task end to end.
  One case binds `Options.Trim` to a closure over
  `contextplan.Planner.Plan`, proving only that the two signatures are
  type-compatible and that the closure's ctx and error returns pass
  straight through `Trim`'s call site without being swallowed; the
  closure discards `msgs` and reads from a `Session` the test seeds
  once, since keeping that `Session` synchronized with the loop's
  history is out of this plan's scope.
  The nesting proof — a built `Loop` wrapped as one `flow.Step` tool
  through `agentrun`, showing the two composition models nest —
  belongs to phase 70, where `subagent.LoopTool` will exist to do the
  wrapping; this plan does not write that case.
- `loop_bench_test.go` — one iteration's allocation cost, with the
  baseline recorded in this plan before the phase closes.

`tools/tools_test/schema_test.go` — `SchemaOf` on a tool that
implements `SchemaTool`, one that does not, and a typed nil.

`tools/tools_test/registry_test.go` gains one case: `Tools()` on a
Registry holding several tools returns all of them sorted by name,
and `Tools()` on an empty Registry returns an empty, non-nil slice.

`tools/tools_test/registry_run_scoped_concurrent_test.go` (or a
sibling file matching that pattern) gains one race sub-case: N
goroutines call `Tools()` on a Registry concurrently with N goroutines
calling `Add`, under `go test -race`. Every `Tools()` call returns a
consistent, non-corrupt snapshot; no call panics. `Tools()` reads the
same map as `Add`/`Remove` under the same mutex, so it is in scope for
tools.md's "required for every method that touches the tools map"
concurrent-test policy.

Every scripted `Completer` lives in the test package. No concrete
model client ships in this SDK, and this plan adds none.

## Verification

`make verify` passes: gofmt, vet, the race detector, the coverage
floor at 85 percent for `agentloop` and the module total, the doc
gate, the structure gate, the plan gate, the deps gate against the
new `policy/layers.json` row, the API gate against the regenerated
locks, the Semgrep scan, and the probe suite.

`go test -race ./agentloop/... ./tools/...` passes.

`make api-update` runs, and the `api/agentloop.txt` and
`api/tools.txt` diffs land in the same change as the code.

This plan adds no conformance vector. `agentloop` carries no wire
format of its own; it composes `provider.Message` and `tools.InOut`,
both already covered by their own package's tests. No new gate is
added or weakened.
