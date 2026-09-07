# Plan: agentloop

Status: shipped. Phase 69's design rationale is folded into this
file; no standalone phase 69 plan file remains. This file is the
declarative contract `check_plan.py` and `make api-update` gate
against.

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
  `docs/history/spool.md`.

## API

### New package `agentloop`

- `type Options struct` — `Completer provider.Completer`,
  `Tools *tools.Registry`, `Scope *tools.Scope`, `Model string`,
  `MaxIterations int`, `MaxCallsPerTurn int`, `MaxTotalTokens int`,
  `OnToolError ErrorPolicy`, `Hooks *events.Registry`,
  `Tracer *trace.Tracer`, `Usage *provider.Accumulator`,
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
  `New` calls `Definitions(opts.Tools, opts.Scope)` once and stores
  the result on `Loop`; `Run` reuses that same
  `[]provider.ToolDefinition` slice for `provider.Request.Tools` on
  every iteration. `tools.Registry` and `tools.Scope` are not
  documented as mutating mid-run, and recomputing the set every
  iteration would waste work for no stated benefit.
- `type Loop struct` — built only through `New`. Unexported fields.
- `func (l *Loop) Run(ctx context.Context, msgs []provider.Message) (Result, error)`
  — calls `Registry.RunScoped`, never `Registry.Run`, so a
  model-chosen call always passes through `l.Scope`.
- `type Result struct` — `Final provider.Message`,
  `History []provider.Message`, `Iterations int`,
  `Usage provider.Usage`, `Stop StopReason`.
- `type StopReason string` with constants `StopNoToolCalls`,
  `StopMaxIterations`, `StopHookVeto`. No `StopToolError` constant
  exists: a tool error under `ErrorPolicyFail` is a hard failure, not
  a graceful stop. See the `ErrorPolicy` bullet below.
- `type ErrorPolicy string` with constants `ErrorPolicyReport` (zero
  value; sends the tool's error text back as the tool result) and
  `ErrorPolicyFail`. A `DecodeArguments` failure on malformed
  model-supplied JSON arguments is a tool-run error and goes through
  the same `OnToolError` policy as a failed `Run`. `ErrorPolicyFail`
  turns a tool-run error into `Run`'s own hard-fail return, wrapped
  with the failing `ToolCallID` and the iteration count, per the
  `Result`-shape rule below; `Run` never returns `StopToolError`,
  since a fail-policy stop is not graceful.
- `func Definitions(reg *tools.Registry, scope *tools.Scope) ([]provider.ToolDefinition, []string, error)`
  — the second return holds the names skipped for a missing schema.
  Definitions fails closed: it returns `ErrNoSchemas` whenever the
  registry is non-empty and the offered set ends up empty, whatever
  the cause — every tool lacking a schema, a `Scope` denying every
  tool, or both together. An empty Registry returns an empty set and
  no error. Before phase 70 every subagent tool is schema-less; a
  silent skip there would end the run with `StopNoToolCalls`,
  indistinguishable from success. The skip-list return value still
  reports which names were skipped for a missing schema, independent
  of whether `ErrNoSchemas` trips.
- Sentinel errors: `ErrNoCompleter`, `ErrNoTools`, `ErrMaxIterations`,
  `ErrUnrenderableResult`, `ErrCallsPerTurnExceeded`, `ErrNoSchemas`,
  `ErrOverBudget`, `ErrTokenBudgetExceeded`.

`Result`'s shape depends on how `Run` stops. On every graceful stop —
`StopNoToolCalls`, `StopMaxIterations`, `StopHookVeto` — `Run` returns
a fully populated `Result` and a nil error: `History` carries every
message appended so far, `Iterations` carries the completed iteration
count, and `Usage` carries the tokens summed so far. `Final` carries
the last message appended, or the zero value when the stop happened
before a new response arrived, as with `StopHookVeto`. On every
hard-fail error return — a canceled ctx, a `Completer.Chat` error,
`ErrOverBudget`, `ErrTokenBudgetExceeded`, `ErrCallsPerTurnExceeded`,
a `Trim` error, a post-`Trim` `provider.Message.Validate` error, a
tool error under `ErrorPolicyFail`, or a non-veto `events.Fire` error
— `Run` also returns the partial `Result` alongside the error, not
the zero value.
`History`, `Iterations`, and `Usage` carry the same partial state as
the graceful-stop case; `Final` and `Stop` stay the zero value, since
the run did not reach a stop condition — it failed one. This list is
closed: every hard-fail cause in this plan belongs to it, with no
exception. `Run` checks ctx at the start of each iteration, a step
that runs before any of the per-iteration bookkeeping the other
hard-fail causes share; a canceled ctx can also surface mid-`Completer`
call, before that call's `Response` ever arrives, in which case
`Final` and `Stop` stay the zero value along with `History`,
`Iterations`, and `Usage` staying at whatever they held before that
call, since no new state exists to add. When ctx cancellation is
caught before any iteration completes, `History`, `Iterations`, and
`Usage` are the zero value too, since no partial state has accumulated
yet — the general rule degrades to the zero-value `Result` on its own,
without a special case.

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
iteration, wrapped with the iteration count.

After a non-nil `Trim` runs, `Run` calls `provider.Message.Validate()`
on every message in `Trim`'s returned slice, before the next
`Completer` call, and fails closed on the first violation, wrapped
with the iteration count, per the `Result`-shape rule above.
`Message.Validate` checks only one message's own `Role` against its
own `ToolCallID` and `ToolCalls` fields; it does not check that an
assistant message's `ToolCalls` entry has a matching `RoleTool` reply
elsewhere in the slice. A `Trim` that drops a `RoleTool` reply while
keeping the assistant message that requested it passes this check and
reaches `Completer` unchanged: catching that cross-message break is
the caller's problem, since no primitive in this SDK validates
cross-message tool-call pairing today. A nil `Trim` passes the history
through unchanged, and skips this validation step.

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
returns, `Run` adds
`max(resp.Usage.TotalTokens, resp.Usage.PromptTokens + resp.Usage.CompletionTokens)`
to that running total, then checks it against `MaxTotalTokens`. See
the addendum below for why the running total does not trust
`TotalTokens` alone. A total over the cap fails the run with
`ErrTokenBudgetExceeded`, wrapped with the iteration count; the
response that tripped the cap is still appended to history and still
recorded onto `Options.Usage`, so a caller with a `Usage` accumulator
sees the response that tripped the cap. `Options.Usage` recording
itself still sums the `Completer`'s raw reported `TotalTokens`, not
the corrected `max()` figure, and carries its own, separate
under-reporting gap for a `Completer` that leaves `TotalTokens` at
zero; see the addendum's Outside bullet on `provider.Accumulator`. A
zero `MaxTotalTokens` means unbounded.

Hitting `MaxIterations` is not an error: `Run` returns
`Result{Stop: StopMaxIterations}, nil`, a normal, graceful stop,
following the `Result`-shape rule above. `ErrMaxIterations` is
reserved for `Options.Validate()` rejecting a non-positive
`MaxIterations` value — a construction-time validation error, not a
runtime stop.

`trace.Tracer` opens one span per iteration and one per tool call.
`events.Registry` fires `PointPreTool` and `PointPostTool` per tool
call, and `PointStop` once at the end. When a `Fire` call returns a
non-nil error, `Run` checks `errors.Is(err, events.ErrVetoed)`: a veto
is the graceful `StopHookVeto` stop described above — nil error, no
tool run. Any other `Fire` error, a handler-returned error that is not
a veto, is a hard failure: `Run` returns the wrapped error and the
partial `Result` per the rule above, and the tool does not run.
`provider.Accumulator` records per iteration under `SessionID`.
`events.Bus` carries the loop's own events. Each of the four is
optional and unused when nil.

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
  failing validation. Every failing case asserts `errors.Is` against
  its specific sentinel (`ErrNoCompleter`, `ErrNoTools`, or
  `ErrMaxIterations`), not a plain non-nil check.
- `definitions_test.go` — a registry with schema-bearing and
  schema-free tools yields the right definitions and skip list. A
  `Scope` denial removes a tool from the offered set. A registry
  whose every tool lacks a schema fails with `ErrNoSchemas`. A
  schema-bearing registry whose `Scope` denies every tool also fails
  with `ErrNoSchemas`, proving the broadened fail-closed condition
  trips even when the skip list stays empty.
- `loop_test.go` — the red-green cases. A response with no tool call
  ends the loop at one iteration. A response with one tool call runs
  the tool and appends a `RoleTool` message whose `ToolCallID`
  matches the call. Two calls in one turn run in `Index` order. An
  unknown tool name reports `tools.ErrUnknownName` under
  `ErrorPolicyReport` and fails under `ErrorPolicyFail`. A
  `PointPreTool` veto stops with `StopHookVeto` and runs no tool. A tool whose `DecodeArguments` fails on malformed
  model-supplied JSON reports the decode error under
  `ErrorPolicyReport` and fails the run under `ErrorPolicyFail`, the
  same as a failed `Run`. A `Budget` that the history outgrows fails
  the run with `ErrOverBudget`. A `Trim` hook returning an error
  fails the run. A turn whose response requests more calls than a
  positive `MaxCallsPerTurn` fails the run before any call in that
  turn runs, asserting `ErrCallsPerTurnExceeded` under both
  `ErrorPolicyReport` and `ErrorPolicyFail`, since the trip is
  policy-independent. A model that always calls a tool stops at
  `MaxIterations`, asserting `err == nil` and the returned `Result`
  equal to `Result{Stop: StopMaxIterations}`. A scripted `Completer`
  whose responses' summed `Usage.TotalTokens`
  crosses a positive `MaxTotalTokens` on the second iteration fails
  the run with `ErrTokenBudgetExceeded` wrapped with the iteration
  count, and a paired case with `Options.Usage` set asserts the
  tripping call's tokens still landed in the accumulator's total. A
  zero `MaxTotalTokens` with the same scripted responses runs to
  `StopMaxIterations` unaffected. A tool registered after `New` but
  before `Run` is never offered to the model, proving `Definitions`
  runs once at `New` and `Run` reuses the cached result. A
  `PointPreTool` handler that returns a non-veto error fails the run
  with the wrapped handler error, asserts
  `errors.Is(err, events.ErrVetoed)` is false to distinguish it from a
  veto, and asserts the returned `Result` carries the accumulated
  `History`, `Iterations`, and `Usage` at the point of failure, not
  the zero value. A `Trim` hook returning a slice with one invalid
  message — a
  `RoleTool` message with a blank `ToolCallID` — fails the run with
  the wrapped `provider.Message.Validate` error, before the next
  `Completer` call. A `Trim` hook that drops a `RoleTool` reply while
  keeping the assistant message's matching `ToolCalls` entry passes
  `Run`'s per-message validation unchanged and reaches `Completer`,
  documenting that cross-message pairing stays the caller's
  responsibility. A ctx canceled before the first iteration completes
  returns the ctx error alongside the zero-value `Result`, since no
  partial state has accumulated yet. A ctx canceled at the start of a
  later iteration, after at least one prior iteration already
  appended a `RoleTool` message and completed, returns the ctx error
  alongside a `Result` whose `History`, `Iterations`, and `Usage`
  carry that prior iteration's accumulated state, and whose `Final`
  and `Stop` stay the zero value. Every hard-fail case above — the
  canceled ctx, the `Completer.Chat` error, `ErrOverBudget`,
  `ErrTokenBudgetExceeded`, `ErrCallsPerTurnExceeded`, the `Trim`
  error, the post-`Trim` validation error, the `ErrorPolicyFail` tool
  error, and the non-veto hook error — asserts the returned `Result`
  carries the accumulated `History`, `Iterations`, and `Usage` at the
  point of failure, not the zero value when any iteration already
  completed.
- `render_test.go` — the render order (string, then UTF-8 bytes,
  then JSON fallback), the unrenderable case asserting
  `errors.Is(err, ErrUnrenderableResult)`, and the `ResultBudgetOf`
  truncation.
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

`agentloop` holds a mutation-kill floor of 98, in
`scripts/mutation_denylist/agentloop.json`. Run `make mutation-gate`
to check it.

## Addendum: argument validation, an audit hook, and an untrusted
error marker

Status: shipped. This addendum covers three hardening fixes found in
an adversarial review of the shipped `agentloop` code. It changes
`toolcall.go`, `options.go`, `wire.go`, and `run.go`. It adds no new
package.

### Addendum goal

Close three gaps: model-supplied tool arguments reach `RunScoped`
with no schema check; a run produces no audit trail a caller can
sign; and a reported tool error looks identical to a normal tool
result in the model-facing transcript.

### Addendum scope

Inside:

- Compiling each `Scope`-offered, schema-bearing tool's
  `ParameterSchema()` once, at `New`, through the existing `schema`
  package, and validating every model-supplied `call.Arguments`
  against its tool's compiled schema before `DecodeArguments` runs.
- One optional `Options.Audit AuditFunc` hook `Run` calls once per
  completed Completer turn and once per tool call whose result
  reaches history, carrying enough structured data for a caller to
  build and sign its own `envelope.Message` chain outside `agentloop`.
- One exported constant, `ToolErrorPrefix`, marking error-path
  `RoleTool` content as untrusted, applied at every `runOneToolCall`
  and `decodeAndRun` error-report site.

Outside:

- Signing, hashing, or any `envelope`/`identity`/`contextstate`
  import inside `agentloop`. `agentloop` stays a block; the block
  never sees a signing identity, matching how `flow` never imports
  `envelope` and only the composition layer (`agent`, `agentrun`)
  does. A caller wanting signed audit records builds them from
  `AuditRecord` values itself, the same way `agent.confirmStep`
  builds and signs an `envelope.Message` from a `flow.Confirm`
  payload today.
- Any change to `tools.SchemaTool`, `tools.DecodeArguments`, or the
  `schema` package. This addendum is a caller of `schema`, not a
  change to it.
- Retrying a tool call after an argument-validation failure.
  `OnToolError` already decides what happens next; this addendum adds
  no new retry policy.
- Redacting or masking tool-call `Arguments` bytes in `AuditRecord`.
  A caller that needs redaction applies it before signing; `agentloop`
  passes the bytes it already holds, unchanged.
- Adding an `Epistemic`-style field to `provider.Message`. See the
  decision below.
- Preserving `New`'s current always-succeeds behavior for a registry
  that carries a latent, never-called, malformed schema. `New` now
  fails closed with `ErrInvalidSchema` on any `Scope`-offered schema
  defect. This is deliberate: an existing caller whose registry
  happens to carry a `SchemaTool` with a previously inert malformed
  `ParameterSchema()` now sees that defect at construction time,
  instead of never.

### Fix 7 decision: a text marker, not a `provider.Message` field

Recommendation: keep the `ToolErrorPrefix` text-constant design.
Reject adding an epistemic field to `provider.Message`. Reasoning
follows.

Two designs were weighed for distinguishing error-report `RoleTool`
content from a normal tool result.

- Option A (recommended): a named constant, `ToolErrorPrefix`, that
  `agentloop` prepends to `Content` under `ErrorPolicyReport`. No
  change to `provider.Message`.
- Option B: a new `Epistemic`-shaped field on `provider.Message`
  itself, mirroring `envelope.Epistemic` and its
  `EpistemicUntrustedInput` value, set structurally instead of
  string-sniffed.

`provider`'s own row in `policy/layers.json` is `[]`: `provider`
imports no internal package today, and `AGENTS.md` states this
plainly — `provider` is "a leaf package; no internal imports." Adding
`envelope.Epistemic` to `provider.Message` would import `envelope`
into `provider`, breaking that documented invariant. `envelope`
itself imports `contextstate`, so the edge would not stop at one hop.

The blast radius argues the same way. `provider.Message` is
`usage`, `providerregistry`, `contextplan`, `agentloop`, `memory`,
`subagent`, and `e2e`'s shared currency; every one of those, plus
every external `provider.Completer` implementation, would need an
opinion on a field only `agentloop`'s error-report path populates
today. A leaf package should not grow a field for one caller's
concern.

A local, `envelope`-free enum defined inside `provider` avoids the
import problem but not the blast radius one: it still changes
`provider.Message`'s shape, still touches `api/provider.txt`, and
still asks every `Completer` implementation and every `Message`
consumer to account for a field only one caller sets.

The `ToolErrorPrefix` marker also is not a naked magic string: it is
a single named constant, defined once, referenced everywhere the code
builds error-report content. The "no string literals where constants
exist" rule targets scattered literal duplication of an enum value; a
one-constant marker checked and rendered in one file does not
reproduce that problem.

The prefix also serves a real constraint the field design cannot
remove: the model itself only ever reads `Content` as text. No
provider wire format carries a side-channel provenance field a model
can see, so the untrusted signal a model needs has to live in
`Content` regardless of what `provider.Message` gains. A struct field
would only help a Go-level consumer, not the model.

For a Go-level consumer that needs a structural signal without
string-sniffing, this addendum already provides one: `AuditRecord.Err`
is non-nil exactly when `ToolResult.Content` carries a
`ToolErrorPrefix`-marked report, and `AuditKind` distinguishes the
audited event kind. A caller wired through `Options.Audit` never
parses `Content` to learn a call failed.

No second, independent caller needs an `Epistemic` field on
`provider.Message` today. Per the Building blocks rule, this addendum
does not add abstraction without a caller. If a future concrete
`Completer` or a second consumer needs structural provenance on every
message, not only agentloop's error reports, that is its own plan
against `provider`, weighed against `provider`'s leaf-package
contract at that time.

### Addendum API

New in `agentloop`:

```go
// ErrInvalidSchema is New's error when a SchemaTool's
// ParameterSchema() fails schema.Compile. Test with errors.Is.
var ErrInvalidSchema = errors.New("agentloop: tool parameter schema does not compile")

// ErrArgumentValidation is decodeAndRun's error when call.Arguments
// fails schema.Compiled.Validate against the called tool's compiled
// parameter schema, before DecodeArguments runs. Wraps the
// underlying schema error (schema.ErrValidation,
// schema.ErrMalformedPayload, or schema.ErrAdmission). Routed through
// OnToolError exactly like a DecodeArguments failure. Test with
// errors.Is.
var ErrArgumentValidation = errors.New("agentloop: tool call arguments failed schema validation")

// ToolErrorPrefix marks RoleTool message Content as an untrusted
// error report. runOneToolCall and decodeAndRun's validation-failure
// path both prefix error-report Content with it under
// ErrorPolicyReport, so the model-facing transcript distinguishes a
// reported failure from a normal tool result without a
// provider.Message schema change.
const ToolErrorPrefix = "[tool-error] "

// AuditKind names which of Run's two audit-relevant events an
// AuditRecord describes.
type AuditKind string

const (
	// AuditKindCompletion is one completed Completer.Chat call.
	AuditKindCompletion AuditKind = "completion"
	// AuditKindToolCall is one tool call whose RoleTool result
	// message reached history.
	AuditKindToolCall AuditKind = "tool_call"
)

// AuditRecord is one audit-relevant event from a Run call, passed to
// Options.Audit. A caller builds and signs its own envelope.Message
// from the fields it needs; agentloop signs nothing itself.
type AuditRecord struct {
	// Iteration is the 1-based Completer-call count this record
	// belongs to, matching Result.Iterations at the same point.
	Iteration int
	// Kind names which event this record describes.
	Kind AuditKind
	// Request is the exact provider.Request sent to Completer.Chat
	// this iteration. Set only when Kind == AuditKindCompletion.
	Request provider.Request
	// Response is the provider.Response Completer.Chat returned this
	// iteration. Set only when Kind == AuditKindCompletion.
	Response provider.Response
	// ToolCall is the model-requested call this record describes.
	// Set only when Kind == AuditKindToolCall.
	ToolCall provider.ToolCall
	// ToolResult is the RoleTool message runOneToolCall appended to
	// history for ToolCall, including any ToolErrorPrefix marker.
	// Set only when Kind == AuditKindToolCall.
	ToolResult provider.Message
	// Err is the tool-run error runOneToolCall reported, or nil on a
	// successful call. Set only when Kind == AuditKindToolCall.
	Err error
}

// AuditFunc receives one AuditRecord per audited event, in the order
// Run produces them. A non-nil return is a hard failure: Run wraps it
// with the iteration count and returns it exactly like a Trim error,
// per the Result-shape rule.
type AuditFunc func(ctx context.Context, rec AuditRecord) error
```

Changed in `agentloop`:

- `Options` gains one new field: `Audit AuditFunc`. Optional; a nil
  `Audit` means Run performs no audit call, at no added cost.
- `New` compiles the parameter schema of every tool that
  `Definitions` already put in the offered `defs` slice, keyed by
  `defs[i].Name`, and stores the result on `Loop` as
  `schemas map[string]*schema.Compiled`. `New` calls `Definitions`
  once, exactly as the base plan already does, and reuses that same
  return value for the compile loop instead of walking `reg.Tools()`
  again: the compiled-schema set is always exactly the `Scope`-offered
  set, never wider. A compile failure fails `New` with
  `ErrInvalidSchema`, wrapped with the tool name and the underlying
  `schema.ErrCompile`/`schema.ErrAdmission` reason. Scoping the
  compile loop this way closes the shared-registry blast radius a
  wider, `Scope`-independent compile would carry: one
  `*tools.Registry` is shared across every `Loop` built over it, each
  with its own `Options.Scope`, so a malformed schema on a tool
  entirely outside one `Loop`'s `Scope` must not fail that `Loop`'s
  `New` call.
- `decodeAndRun` already checks `l.scope != nil && !l.scope.Allowed(call.Name, t)`
  immediately after `reg.Get` resolves `call.Name`, before the
  `SchemaTool` type assertion, and returns `tools.ErrScopeDenied`,
  wrapped with `call.ID`, on a denial; this addendum does not move
  that check. Keeping it ahead of the type assertion, unchanged,
  matters for this addendum specifically: it rejects a call naming a
  tool that is both `Scope`-denied and not a `SchemaTool` with
  `ErrScopeDenied`, not with the generic "publishes no schema" error,
  since the type assertion never runs for a name the `Scope` check
  already rejected. `tools.Scope.Allowed` is not nil-safe — calling
  it on a nil `*tools.Scope` panics, since `Allowed` dereferences its
  own `deny` field on its first line — so the existing
  `l.scope != nil` guard stays, exactly as every current call site
  (`decodeAndRun` and `Definitions`) already requires.
- After the `Scope` check passes and the `SchemaTool` type assertion
  succeeds, `decodeAndRun` runs
  `schema.Compiled.Validate(call.Arguments)` against `call.Name`'s
  compiled schema, looked up on `Loop`, before `st.DecodeArguments`.
  A validation failure returns `ErrArgumentValidation`, wrapped with
  `call.ID` and the underlying `schema` error; it never reaches
  `DecodeArguments`. `schema.Compiled.Validate` already enforces
  `schema.MaxPayloadBytes` before it unmarshals `call.Arguments`, so
  `decodeAndRun` adds no separate byte cap of its own.
  `l.schemas[call.Name]` is guaranteed to hit whenever control reaches
  this point: reaching it requires `call.Name` to have passed both
  the `Scope` check and the `SchemaTool` type assertion, the same two
  conditions, applied in either order, that decide `defs` membership
  inside `Definitions`; `l.schemas` is keyed by that same `defs` set.
  `schema.Compiled.Validate` is also not nil-safe, so a map miss would
  panic on the lookup's result; the base plan's `New` description
  above already assumes `tools.Registry` and `tools.Scope` do not
  mutate mid-run, which rules that miss out, and `decodeAndRun` adds
  no defensive not-found branch for it.
- `runOneToolCall`'s two existing error-report branches (a
  `decodeAndRun` failure and a `render` failure) prefix their
  `Content` with `ToolErrorPrefix` under `ErrorPolicyReport`. An
  `ErrArgumentValidation` failure additionally renders through
  `schema.Corrective(err)` instead of `err.Error()`, so the
  model-facing text is the bounded, schema-derived corrective message
  `schema.Corrective` already builds for exactly this purpose, not an
  internal Go error string.
- `runOneToolCall`'s unexported signature widens from
  `(provider.Message, bool, error)` to
  `(provider.Message, bool, error, error)`: the third return,
  `reported`, carries the pre-render tool-run error — the same error
  `decodeAndRun` or `render` produced — on every `ErrorPolicyReport`
  branch, where the fourth return (`err`, `runOneToolCall`'s own
  hard-fail signal) stays nil. `reported` is nil on a successful call
  and on every `ErrorPolicyFail` branch, where `err` instead carries
  the wrapped failure and `runOneToolCall` returns before building a
  `RoleTool` message. Today `runOneToolCall` collapses `runErr` into
  `Content` via `runErr.Error()` and returns a nil own-error under
  `ErrorPolicyReport`, discarding the typed error before any caller
  could read it; `reported` is the fix, carrying that same typed
  error one level up. `runToolCalls` receives `reported` alongside
  `msg` and `veto` and forwards it, unchanged, to the
  `AuditKindToolCall` call described below.
- `runToolCalls` calls `l.audit` once per tool call whose `RoleTool`
  message reaches history — every call for which `runOneToolCall`
  returns `veto == false` and a nil own-error — right after appending
  that call's `msg` onto `history`, with `Kind: AuditKindToolCall`,
  the call, `msg` as `ToolResult`, and `reported` as `Err`. This is
  the exact point in the `run` → `runToolCalls` → `runOneToolCall`
  chain where the audit call for a tool result fires: inside
  `runToolCalls`'s per-call loop, not in `run` after the whole turn's
  calls finish. A `PointPreTool` veto produces no history entry or
  audit record for the vetoed call itself: `StopHookVeto` ends the run
  immediately, so there is no tool result to attest to for that call.
  Under `MaxConcurrentTools > 1`, a call the worker pool already ran
  to completion before the veto landed is a different case: it did
  produce a tool result, and `recordRanOutcomes` appends its history
  entry and audit record before the veto's own short-circuit returns.
  See "Addendum: audit an in-flight call the worker pool already ran
  when a veto lands" below. An `ErrorPolicyFail` tool error is
  not audited either, since `runOneToolCall` returns a non-nil own
  `err` and no `msg` on that path, and `runToolCalls` propagates that
  `err` straight to `run` as a hard failure without appending
  anything; the wrapped hard-fail error is `Run`'s own audit trail
  for that case. A non-nil `l.audit` error from an `AuditKindToolCall`
  call is itself a hard failure: `runToolCalls` returns it to `run`
  the same way a `runOneToolCall` `err` return already propagates.
- `run` calls `l.audit` once per iteration, immediately after
  appending `resp.Message` to `history` and updating `totalUsage` and
  `Options.Usage`, and strictly before the `MaxTotalTokens` check, the
  no-tool-calls stop, the `MaxCallsPerTurn` check, and the
  `runToolCalls` call, with `Kind: AuditKindCompletion`, the exact
  `provider.Request` built for `Completer.Chat` this iteration, and
  `resp`. Firing the completion audit call before those three
  downstream checks is deliberate: `Completer.Chat` already succeeded
  for this iteration by the time any of them run, so an iteration
  that goes on to hard-fail on `ErrTokenBudgetExceeded` or
  `ErrCallsPerTurnExceeded` still emits its `AuditKindCompletion`
  record before `run` returns the wrapped error. A non-nil `l.audit`
  error from an `AuditKindCompletion` call is a hard failure: `run`
  wraps it with the iteration count and returns the partial `Result`,
  per the existing Result-shape rule, the same as a `Trim` error, and
  this hard-fail path returns before the `MaxTotalTokens` check would
  otherwise run.

`AuditKind`, `AuditRecord`, `AuditFunc`, `ErrInvalidSchema`, and
`ErrArgumentValidation` land in `api/agentloop.txt` via
`make api-update`, in the same change as the code. `runOneToolCall`'s
widened signature is unexported and touches no lock file.

### Addendum placement and import policy

No new package. `agentloop`'s `policy/layers.json` row gains one
entry:

```json
"agentloop": ["provider", "tools", "trace", "hooks", "usage", "events", "contextbudget", "schema"]
```

No other row changes. `agentloop` still does not import `envelope`,
`identity`, or `contextstate`; the audit hook keeps `agentloop`
envelope-agnostic, matching `flow`'s precedent and the Building
blocks rule that a block never imports the agent composition layer.

### Addendum tests

Migrating the base plan's existing fixtures: the new
`schema.Compiled.Validate(call.Arguments)` step runs before
`DecodeArguments`, so every pre-existing `provider.ToolCall.Arguments`
fixture in `agentloop_test` that reaches a `Scope`-offered
`SchemaTool` must carry schema-passing JSON, not the unset or
non-JSON byte slices the base plan's tests use today
(`[]byte("in")`, `[]byte("bad")`, `[]byte("query")`,
`[]byte("url")`, and nil `Arguments`, across `loop_test.go`,
`loop_bounds_test.go`, `loop_wiring_test.go`,
`loop_integration_test.go`, `loop_trim_test.go`, and
`render_test.go`). This addendum moves every such fixture to
`[]byte("{}")`, which already passes every test tool's permissive
parameter schema (`loop_bounds_test.go` and `loop_wiring_test.go`
already use this literal in their newer cases).
`TestRunTrimErrorLaterIteration` (`loop_trim_test.go:21`) and
`TestRunTrimInvalidMessageLaterIteration`
(`loop_trim_test.go:58`) each call a registered, `Scope`-offered
`schemaEchoTool` through a `provider.ToolCall{...}` literal with an
implicit-nil `Arguments` field; neither test's assertion depends on
that call succeeding or on `DecodeArguments` running, so the move to
`[]byte("{}")` is for suite-wide consistency, not to fix a breaking
case. `TestRunDecodeArgumentsFailure`
(`loop_test.go:212-246`) keeps its `DecodeArguments`-failure intent
by pairing `[]byte("{}")` `Arguments` with the test tool's
already-supported, unconditional `decodeErr` field
(`helper_test.go:81-89`), instead of relying on non-parseable bytes:
the new schema-validation step would otherwise intercept a malformed
payload before `DecodeArguments` ever ran, changing the failure this
test exercises from a decode failure to an
`ErrArgumentValidation` one.

- `argument_validation_test.go` — a tool whose `ParameterSchema()`
  requires a field; a call missing that field fails with
  `errors.Is(err, ErrArgumentValidation)` under `ErrorPolicyFail`,
  and reports a `ToolErrorPrefix`-marked, `schema.Corrective`-shaped
  message under `ErrorPolicyReport`. A call whose `Arguments` satisfy
  the schema reaches `DecodeArguments` and runs normally. A tool
  whose `ParameterSchema()` returns bytes that do not compile fails
  `New` with `errors.Is(err, ErrInvalidSchema)`, before any `Run`
  call. A second `Loop`, built with a narrower `Scope` over the same
  shared `*tools.Registry`, still constructs successfully through
  `New` even though the wider `Loop`'s excluded tool carries that
  same malformed schema, proving the compile loop is scoped, not
  registry-wide. A call naming a tool outside `Scope` but present in
  the registry fails with `errors.Is(err, tools.ErrScopeDenied)`,
  before `DecodeArguments` or schema `Validate` ever run, proving
  `decodeAndRun`'s own `Scope` check is the defense-in-depth gate for
  a compile loop that no longer covers the full registry. A call with
  oversized `Arguments`, over `schema.MaxPayloadBytes`, fails
  `ErrArgumentValidation` wrapping `schema.ErrAdmission`, without
  ever calling `DecodeArguments`. A race sub-case runs N goroutines
  calling `Run` concurrently on one shared `*Loop`, each driving a
  scripted `Completer` through several schema-validated tool calls,
  under `go test -race`; every call resolves without a panic or a
  race, confirming the immutable, `New`-time `schemas` cache and
  `schema.Compiled.Validate`'s own documented concurrent-use safety
  hold under concurrent `Run` calls, matching
  `registry_run_scoped_concurrent_test.go`'s precedent for concurrent
  `tools.Registry` access.
- `audit_test.go` — a two-iteration, two-tool-call run with
  `Options.Audit` set records one `AuditKindCompletion` record per
  iteration and one `AuditKindToolCall` record per tool call, in
  order, each carrying the same `Request`/`Response`/`ToolCall`/
  `ToolResult` values the run itself produced. An `AuditKindToolCall`
  record for a reported `ErrorPolicyReport` `decodeAndRun` failure
  carries the original, unrendered error in `Err`, not the
  `ToolErrorPrefix`-marked `Content` string, proving `reported`
  survives the collapse `runOneToolCall` applies to `Content`. A
  second case runs `render_test.go`'s existing unrenderable-result
  tool under `ErrorPolicyReport` and asserts the matching
  `AuditKindToolCall` record's `Err` wraps `ErrUnrenderableResult`,
  proving `reported` also carries a `render` failure, not only a
  `decodeAndRun` failure. A `PointPreTool` veto
  produces no `AuditKindToolCall` record for the vetoed call. An
  `ErrorPolicyFail` tool error produces no `AuditKindToolCall` record
  for the failing call. A scripted `Completer` whose second
  iteration's response trips `ErrCallsPerTurnExceeded` still yields
  an `AuditKindCompletion` record for that second iteration before
  `Run` returns the wrapped error, pinning that a hard-failing
  iteration's completion is still audited; a paired case does the
  same for a response that trips `ErrTokenBudgetExceeded`. An `Audit`
  func returning an error fails the run with the wrapped error,
  `errors.Is`-checkable back to the `Audit` func's own sentinel, and
  the returned `Result` carries the accumulated `History`,
  `Iterations`, and `Usage` at the point of failure, matching every
  other hard-fail case in this plan. A nil `Options.Audit` runs
  unchanged from the base plan's existing cases.
- `render_test.go` gains cases: an `ErrorPolicyReport` tool failure's
  `Content` starts with `ToolErrorPrefix`, and an argument-validation
  failure's `Content` starts with `ToolErrorPrefix` followed by a
  `schema.Corrective`-shaped message, not a raw Go error string.

`tools/tools_test/` is unchanged: this addendum adds no new `tools`
symbol.

### Addendum verification

`make verify` passes, including the deps gate against the widened
`agentloop` row and the API gate against the regenerated
`api/agentloop.txt`.

`go test -race ./agentloop/...` passes.

`make api-update` runs, and the `api/agentloop.txt` diff lands in the
same change as the code.

This addendum adds no conformance vector. It adds no new gate. The
coverage floor stays at 85 percent for `agentloop` and the module
total.

## Addendum: a trustworthy MaxTotalTokens cap

Status: shipped. This addendum fixes a correctness bug in `run.go`'s
`MaxTotalTokens` enforcement. It changes `run.go` only. It adds no new
package and no new exported symbol.

### Addendum goal

Stop a `Completer` that under-reports `provider.Usage.TotalTokens`
from silently bypassing `Options.MaxTotalTokens`.

### Addendum bug

`run` enforces `MaxTotalTokens` with one line:

```go
runningTokens += resp.Usage.TotalTokens
if l.maxTotalTokens > 0 && runningTokens > l.maxTotalTokens {
    return l.hardFail(history, iterations, totalUsage),
        fmt.Errorf("agentloop: iteration %d: %w", iterations, ErrTokenBudgetExceeded)
}
```

`provider.Usage` documents `TotalTokens` and `CachedTokens` but states
no relationship between the four fields, and `provider.Usage` has no
`Validate` method to enforce one. Nothing requires a `Completer` to
fill `TotalTokens`. A `Completer` that fills only `PromptTokens` and
`CompletionTokens`, and leaves `TotalTokens` at its zero value, drives
`runningTokens` to stay at zero every iteration. The cap never trips,
however many iterations run and however many tokens the run actually
bills.

No shipped code in this repository fills `Usage` this way today: every
`provider.Usage{...}` literal in the tree, test or production, either
sets `TotalTokens` consistent with `PromptTokens + CompletionTokens`
or sets `TotalTokens` alone. But `agentloop` accepts any caller-built
`provider.Completer`, and the SDK ships no concrete client; the gap is
a latent trust-boundary defect in the primitive, not a defect in a
fixture.

### Addendum decision: max(), not Validate, not a doc-only fix

Three options were weighed.

- Option A (recommended): `run` computes each response's billed
  tokens as `max(resp.Usage.TotalTokens, resp.Usage.PromptTokens +
  resp.Usage.CompletionTokens)` and sums that onto `runningTokens`,
  instead of summing `resp.Usage.TotalTokens` alone.
- Option B: give `provider.Usage` a `Validate` method that rejects a
  `TotalTokens` inconsistent with `PromptTokens + CompletionTokens`,
  and have `run` fail the turn when `resp.Usage.Validate()` errors.
- Option C: document the trust boundary in `Options.MaxTotalTokens`'s
  doc comment and change no code.

Option C is rejected. AGENTS.md states invariants live in `Validate`
methods, not comments alone; a documented limit that a caller can
silently violate is exactly the gap that rule exists to close, and
`MaxTotalTokens` is a stated safety cap, not an advisory setting.

Option B is rejected for `provider.Usage` specifically, though the
general rule is sound elsewhere. `provider.Usage`'s own doc comment
says `CachedTokens` "counts prompt tokens served from a
provider-side cache" — a subset of `PromptTokens`, not an addend — so
`TotalTokens == PromptTokens + CompletionTokens` is the only
relationship the documented field semantics support; `CachedTokens`
does not enter the equation for or against it. Given that, a
`Validate` check against exactly that equality would reject a
`Completer` that reports `TotalTokens` correctly but leaves
`CompletionTokens` at zero mid-stream, or any other partial-fill
shape a real vendor response might carry before a turn finishes. This
SDK ships no concrete `Completer`, so no real vendor payload shape is
available to confirm the equality always holds; asserting it as a hard
`Validate` rule risks failing a legitimate, merely partial, `Usage`
value the same way the bug under-counts one. A `Validate` method
belongs on `provider.Usage` when a real shape needs one; this addendum
does not add a speculative rule with no confirmed vendor case behind
it, per the Building blocks rule against abstraction without a caller.

Option A is recommended because it never rejects a well-formed
`Usage` value and never depends on an equality that might not hold for
every vendor. `max` degrades to today's exact behavior whenever
`TotalTokens` is already filled correctly or is the larger of the two
readings; it only changes the outcome when `TotalTokens` under-reports
relative to the field sum, which is exactly the bypass this addendum
closes. It touches one enforcement site and adds no new type, no new
`Validate` method, and no new exported symbol.

### Addendum scope

Inside:

- `run.go`: replace the `runningTokens += resp.Usage.TotalTokens` line
  with a call to a new unexported helper, `billedTokens(u
  provider.Usage) int`, returning
  `max(u.TotalTokens, u.PromptTokens+u.CompletionTokens)`.
- The `MaxTotalTokens` doc comment in `options.go` and this plan's
  existing "A positive `MaxTotalTokens` caps..." paragraph, updated to
  state the `max()` accounting instead of "adds
  `resp.Usage.TotalTokens`".

Outside:

- `sumUsage` and `totalUsage`, `Result.Usage`, and
  `Options.Usage`/`provider.Accumulator` recording. Those three continue
  to record the raw `resp.Usage` a `Completer` reports, unchanged: they
  are a caller-facing report of what the `Completer` said, not a
  safety cap, and correcting a `Completer`'s own under-reporting there
  would silently rewrite data the caller may need to reconcile against
  a vendor invoice. Only the cap-enforcement running total changes.
- `provider.Usage` and `provider/types.go`. This addendum adds no
  `Validate` method there; see the addendum decision above. No
  `docs/history/provider.md` change.
- `provider.Accumulator.Record` in `usage/accumulator.go`. It sums
  `TotalTokens` the same trust-assuming way `run.go` did, so
  `Accumulator.Total` under-reports for the same
  `TotalTokens`-left-zero `Completer` shape. This is a real, smaller
  gap in a different package, not fixed in this change. It needs its
  own plan review against `docs/history/usage.md`, since `provider.Record`
  is a reporting primitive, not a safety cap, and the correct fix
  there — reporting the caller's raw numbers, or reporting a corrected
  `max()` total, or adding a `PartialUsage`-style flag — is its own
  design decision, not a mechanical copy of this addendum's fix.
- Every other `.TotalTokens` read in the tree
  (`contextplan/calibrated.go`'s `Observe` doc comment, and every
  `usage_test`/`agentloop_test` fixture) carries no enforcement
  decision of its own; none is in scope.

### Addendum API

No exported symbol changes. `billedTokens` is unexported. No
`api/agentloop.txt` or `api/provider.txt` diff, and no `make
api-update` run for this addendum.

### Addendum tests

The four cases below, and `TestRunMaxTotalTokens` moved verbatim from
`loop_bounds_test.go`, live in a new sibling file,
`agentloop/agentloop_test/loop_bounds_tokens_test.go`. Adding them to
`loop_bounds_test.go` in place pushed it past the 500-line structure
gate; splitting by concern into a new file keeps every file under the
limit without raising it.

- `TestRunMaxTotalTokensUnderReportedTotal` (the reproduction): two
  scripted `provider.Response` values, each built with the existing
  `toolCallResponse` helper (so each response carries a `ToolCall` and
  the loop reaches a second `Completer.Chat` call, not
  `StopNoToolCalls` after iteration one), each with `Usage:
  provider.Usage{PromptTokens: 30, CompletionTokens: 30}` and
  `TotalTokens` left at its zero value, run against
  `MaxTotalTokens: 100`, mirroring `TestRunMaxTotalTokens`
  (moved to `loop_bounds_tokens_test.go`) in every field except the
  `Usage` shape. `billedTokens` per response is
  `max(0, 30+30) = 60`: `60` alone does not trip the `100` cap, so the
  first iteration completes normally, but `60 + 60 = 120` trips it on
  the second iteration, the same `res.Iterations == 2` shape
  `TestRunMaxTotalTokens` already asserts. Before this fix,
  `runningTokens` sums `TotalTokens` alone and stays `0` after both
  responses, so the run never returns `ErrTokenBudgetExceeded` and
  this case fails; asserting `errors.Is(err, ErrTokenBudgetExceeded)`
  and `res.Iterations == 2` is the reproduction, and it kills the
  mutation that reverts `billedTokens` back to summing
  `resp.Usage.TotalTokens` alone.
- The same scripted shape (`toolCallResponse`, `PromptTokens: 30,
  CompletionTokens: 30`, `TotalTokens: 0`, two responses) with
  `MaxTotalTokens: 0` (unbounded) runs to its normal stop, unaffected,
  proving the fix adds no cap where none is configured.
- `TestRunMaxTotalTokens`, moved to `loop_bounds_tokens_test.go`, is
  itself unchanged: its `Usage: provider.Usage{TotalTokens: 60}` fixture
  already sets `TotalTokens` with no `PromptTokens`/`CompletionTokens`
  set, so `billedTokens` reads `max(60, 0) = 60` per response, the
  same `60` the pre-fix code read, and the case still trips on the
  same second iteration with the same `120` total, proving `max()`
  does not change behavior for this well-formed, `TotalTokens`-only
  `Usage` shape.
- `TestRunMaxTotalTokensSurchargedTotal`: one scripted `Completer`
  response, built with `toolCallResponse`, whose `Usage` sets
  `PromptTokens: 20, CompletionTokens: 20, TotalTokens: 50` — a
  provider that bills a surcharge outside the two counted fields —
  run against `MaxTotalTokens: 40`. `billedTokens` reads
  `max(50, 40) = 50`, over the `40` cap on the first response alone,
  proving `max()` picks the larger, `TotalTokens` reading in the
  surcharge direction too, not only the under-reporting one.

### Addendum verification

`make verify` passes: gofmt, vet, the race detector, the coverage
floor at 85 percent for `agentloop` and the module total, the doc
gate, the structure gate, the plan gate, the deps gate (unchanged
`policy/layers.json`), the Semgrep scan, and the probe suite.

`go test -race ./agentloop/...` passes, including the new
reproduction case failing on the pre-fix code and passing after.

This addendum adds no conformance vector, no new gate, and no
`policy/layers.json` change. It runs no `make api-update`, since it
adds no exported symbol.

## Addendum: a nil-schema panic on a tool registered after New

Status: shipped. This addendum fixes a nil-pointer panic an
adversarial logic review found in `toolcall.go`. It changes
`toolcall.go` and `options.go`. It adds one new sentinel error and no
new package.

### Addendum bug

`New` compiles `l.schemas`, a `map[string]*schema.Compiled`, once, at
construction time, keyed by the `Scope`-offered `defs` set
`Definitions` returns at that same moment. `decodeAndRun`
(`toolcall.go:154`) then indexes `l.schemas[call.Name]` and calls
`.Validate` on the result, unguarded:

```go
if err := l.schemas[call.Name].Validate(call.Arguments); err != nil {
```

The surrounding doc comment states this lookup "is guaranteed to hit
once `call.Name` has passed both the `l.scope` check and the
`tools.SchemaTool` assertion." That claim holds only while the
`*tools.Registry` a `Loop` was built over stays fixed after `New`
runs. `reg.Get` (`toolcall.go:143`) and `l.scope.Allowed`
(`toolcall.go:147`) both read the live registry and the live scope,
not the `New`-time snapshot. A caller that adds a schema-bearing,
scope-allowed tool to the shared registry after `New` has already
run — a realistic pattern for a long-running agent with dynamic tool
registration — breaks the invariant: the model can later call that
tool by name, `reg.Get` and `l.scope.Allowed` both admit the call, and
`l.schemas[call.Name]` returns the zero value, a nil `*schema.
Compiled`. `(*schema.Compiled).Validate` is not nil-safe
(`schema/validate.go:22`); calling it on a nil receiver panics, rather
than returning a typed error `OnToolError` could route.

No existing test reproduces this. `TestRunDefinitionsCachedOnce`
(`agentloop_test/loop_bounds_test.go:135`) proves a tool registered
after `New` is never offered to the model, in `Request.Tools`; it
never scripts the completer to call that tool by name anyway, so the
panic path is unexercised.

### Addendum decision: a runtime guard, not a documentation-only contract

Two options were weighed.

- Option A (recommended): guard the `l.schemas` lookup in
  `decodeAndRun` and return a new sentinel error, routed through the
  existing `OnToolError` policy, on a miss.
- Option B: document that a caller must not mutate `Tools` after
  `New`, and enforce nothing.

Option A is recommended. Every other tool-call failure this function
already handles — an unresolved `call.Name`
(`tools.ErrUnknownName`), a scope-denied `call.Name`
(`tools.ErrScopeDenied`), a missing `SchemaTool` implementation, an
argument-validation failure (`ErrArgumentValidation`) — degrades to a
typed error routed through `OnToolError`, never a panic. A
documentation-only contract leaves this one lookup as the sole path
in `decodeAndRun` that can crash the process on a model-facing input,
which is inconsistent with the function's own established shape and
with a tool-calling loop's threat model: `call.Name` and
`call.Arguments` are both model-supplied, and a model can be made to
name a tool the caller added late, whether by an intentional dynamic
registration pattern or by chance. A runtime guard closes the gap
without asking every caller to police a mutation rule the type system
does not enforce.

### Addendum scope

Inside:

- Guarding `l.schemas[call.Name]` in `decodeAndRun` with a
  comma-ok lookup, returning a new sentinel error on a miss.
- One new sentinel error, `ErrToolNotOffered`.
- Updating `decodeAndRun`'s doc comment in `toolcall.go` to state the
  guarded behavior instead of the now-corrected "guaranteed to hit"
  claim.
- A regression test proving the guarded path runs without a panic,
  under both `ErrorPolicyReport` and `ErrorPolicyFail`.
- A second, unrelated regression test closing an uncovered branch:
  the `AuditKindToolCall` audit-error branch inside `runToolCalls`
  (`toolcall.go:39-49`), which no existing test exercises.

Outside:

- Enforcing registry immutability after `New`. Rejected above.
- Recomputing `l.schemas` per call or per iteration. The base plan's
  `New` doc comment already states `Tools` and `Scope` are "not
  documented as mutating mid-run," and recomputing would reintroduce
  the shared-registry blast-radius problem the argument-validation
  addendum's scoped compile loop already solved. This addendum keeps
  `l.schemas` a fixed, `New`-time snapshot; it only makes a miss on
  that snapshot fail safely instead of panicking.
- Any change to `tools.Registry`, `tools.Scope`, or `schema.Compiled`.
  The fix is entirely inside `agentloop`.
- Any change to `Definitions` or `compileSchemas`. Both already build
  the correct `New`-time set; the bug is `decodeAndRun` trusting that
  set stays complete forever, not a defect in how the set is built.

### Addendum API

New in `agentloop`:

```go
// ErrToolNotOffered is decodeAndRun's error when a model-chosen call
// names a tool with no entry in l.schemas, the schema set New
// compiled once from the Scope-offered tools at construction time.
// This happens when a caller registers a schema-bearing,
// Scope-allowed tool on the shared *tools.Registry after New already
// ran: Registry.Get and Scope.Allowed both read the live registry and
// the live scope, so the call still reaches decodeAndRun, but
// l.schemas, frozen at New, carries no entry for it. Routed through
// OnToolError exactly like ErrArgumentValidation and
// tools.ErrUnknownName. Test with errors.Is.
var ErrToolNotOffered = errors.New("agentloop: tool call names a tool not offered when New ran")
```

`ErrToolNotOffered` lands in `api/agentloop.txt` via `make
api-update`, in the same change as the code. No other exported symbol
changes.

### Addendum fix, exact shape

`toolcall.go:154`, before:

```go
if err := l.schemas[call.Name].Validate(call.Arguments); err != nil {
    return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w: %w", call.ID, ErrArgumentValidation, err)
}
```

After:

```go
compiled, ok := l.schemas[call.Name]
if !ok {
    return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w", call.ID, ErrToolNotOffered)
}
if err := compiled.Validate(call.Arguments); err != nil {
    return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w: %w", call.ID, ErrArgumentValidation, err)
}
```

This sits between the existing `SchemaTool` type-assertion branch
(`toolcall.go:150-153`) and the existing `DecodeArguments` call
(`toolcall.go:157`), matching the shape of the two branches directly
above it: `reg.Get`'s `!ok` branch and the `l.scope.Allowed` `false`
branch. Every branch in `decodeAndRun` now returns a wrapped, typed
error on a mismatch and never panics.

`decodeAndRun`'s doc comment (`toolcall.go:130-141`) changes its
closing sentence from asserting the lookup "is guaranteed to hit" to
stating the guarded contract: `l.schemas[call.Name]` hits whenever
`call.Name` was in the `Scope`-offered set `New` compiled from; a miss
means a tool the caller registered on the shared registry after `New`
ran, and `decodeAndRun` returns `ErrToolNotOffered` instead of
indexing a nil `*schema.Compiled`.

### Addendum tests

New file `agentloop/agentloop_test/schema_drift_test.go`. A dedicated
file, not an addition to `loop_bounds_test.go`
(393 lines) or `argument_validation_test.go`: the concern is registry
drift after `New`, distinct from both the bounds/cache-reuse cases in
`loop_bounds_test.go` and the schema-compile cases in
`argument_validation_test.go`, and keeping it separate matches this
plan's existing precedent of splitting a new concern into its own
file (`loop_bounds_tokens_test.go`).

- `TestDecodeAndRunToolRegisteredAfterNewReportsUnderReportPolicy` —
  builds a `Loop` over an empty registry, then registers a
  schema-bearing tool named `"late"` on the same `*tools.Registry`
  after `New` returns. A scripted `Completer` calls `"late"` on
  iteration one and returns a no-tool-call response on iteration two.
  `Options.Audit` records every `AuditRecord`. Asserts `Run` returns
  `nil` error (default `ErrorPolicyReport`), the `AuditKindToolCall`
  record for the call carries `errors.Is(rec.Err,
  agentloop.ErrToolNotOffered)`, and the appended `RoleTool` message's
  `Content` starts with `ToolErrorPrefix`. This is the primary
  reproduction: on the unguarded code, this case panics instead of
  returning a value, crashing the whole test binary rather than
  merely failing.
- `TestDecodeAndRunToolRegisteredAfterNewFailsUnderFailPolicy` — same
  setup, `OnToolError: ErrorPolicyFail`. Asserts `Run` returns a
  non-nil error satisfying `errors.Is(err,
  agentloop.ErrToolNotOffered)`, and the returned `Result` carries
  the accumulated `History` and `Iterations` at the point of failure,
  matching every other hard-fail case in this plan's Result-shape
  rule.

Closing the uncovered `AuditKindToolCall`-audit-error branch: one new
case, `TestAuditFuncErrorOnToolCallFailsRun`, added to the existing
`agentloop_test/audit_test.go` (328 lines; stays under the 500-line
structure gate). It belongs beside `TestAuditFuncErrorFailsRun`, which
exercises only the sibling `AuditKindCompletion` branch in `run.go`;
the new case exercises `toolcall.go:39-49`'s `AuditKindToolCall`
branch inside `runToolCalls`, the one no existing test reaches. A
scripted `Completer` requests one successful tool call, then an
`Audit` func that returns `nil` for `AuditKindCompletion` and a
sentinel error for `AuditKindToolCall`. Asserts `Run` returns a
non-nil error satisfying `errors.Is(err, errAudit)` (the file's
existing sentinel), and the returned `Result`'s `History` already
contains the tool call's `RoleTool` message, matching
`runToolCalls`'s documented append-then-audit order.

### Addendum verification

`make verify` passes: gofmt, vet, the race detector, the coverage
floor at 85 percent for `agentloop` and the module total, the doc
gate, the structure gate, the plan gate, the deps gate (unchanged
`policy/layers.json`), the API gate against the regenerated
`api/agentloop.txt`, the Semgrep scan, and the probe suite.

`go test -race ./agentloop/...` passes, including both new
`schema_drift_test.go` cases, which crash the test binary on the
pre-fix code and pass cleanly after.

`make api-update` runs, and the `api/agentloop.txt` diff, adding
`ErrToolNotOffered`, lands in the same change as the code.

This addendum adds no conformance vector and no new gate. It changes
no other package: `l.reg.Get`, `l.scope.Allowed`, and
`schema.Compiled.Validate` all stay unchanged, and
`policy/layers.json`'s `agentloop` row is unchanged, since the fix
adds no new import.

## Addendum: context planning and prompt-too-long recovery

Status: shipped. This addendum wires the ported
compaction policy into `Run`. It changes `options.go` and `run.go`.
It adds two internal imports and no new package.

### Addendum goal

Plan every iteration against a `contextplan.Window` before the request
is built, run compaction through the LLM summarizer whenever the
trigger trips, keep the EWMA correction live, and recover once from a
prompt-too-long rejection. Compaction is LLM-only: no fallback path
exists anywhere in `Run`.

### Addendum scope

Inside:

- Per-iteration planning: estimate the history, compare against
  `Window.CompactTrigger`, and compact through `contextsummary` when
  the trigger trips.
- Per-iteration observation: after every `Chat` that returns, call
  `Calibrated.Observe` with the response's `Usage.TotalTokens`.
- Prompt-too-long recovery: on `provider.ErrPromptTooLong`, compact to
  a fixed 16K-token target, or `Budget` over four when smaller, append
  one model-visible notice, and retry that iteration exactly once.
- Three new `Options` fields and five new sentinels.

Outside:

- Any structural-only compaction, manual compact, or force path. The
  task forbids all three. When the summarizer call cannot be made or
  fails, the iteration fails and nothing is sent.
- Sending an over-budget prompt as a fallback. The final estimate
  check fails the iteration instead.
- Changes to `Options.Trim`. `Trim` stays exactly as locked; a nil
  `Window` keeps today's `Trim` behavior unchanged.
- Changes to `contextplan.Plan` or the session-store planner. The
  wiring uses the new `contextplan.Compact` over the loop's own
  history.
- Any second retry of a rejected prompt. A second rejection
  propagates.

### Addendum API

New `Options` fields:

```go
// Window plans every iteration against a token budget. A nil Window
// disables planning; the loop then runs exactly as before. A non-nil
// Window requires Summarizer and Calibrated, and excludes Trim.
Window *contextplan.Window

// Summarizer runs the LLM summary every compaction requires. Required
// when Window is set.
Summarizer *contextplan.Summarizer

// Calibrated estimates tokens for planning and receives one Observe
// call after every Chat. Required when Window is set.
Calibrated *contextplan.Calibrated
```

New sentinels:

```go
// ErrPlanFailed is Run's error when the planning step cannot produce
// an estimate or a plan: an estimator error or an invalid Window at
// iteration time. Test with errors.Is.
var ErrPlanFailed = errors.New("agentloop: context planning failed")

// ErrCompactionFailed is Run's error when a required compaction
// cannot complete: the retention set alone exceeds the window
// (wrapping contextplan.ErrRetentionOverflow), the summarizer call
// failed (wrapping the contextsummary sentinel), or the compacted
// history still exceeds the window. Test with errors.Is.
var ErrCompactionFailed = errors.New("agentloop: compaction failed")

// ErrSummarizerRequired is Options.Validate's error when Window is
// set and Summarizer is nil. Test with errors.Is.
var ErrSummarizerRequired = errors.New("agentloop: Window requires Summarizer")

// ErrEstimatorRequired is Options.Validate's error when Window is set
// and Calibrated is nil. Test with errors.Is.
var ErrEstimatorRequired = errors.New("agentloop: Window requires Calibrated")

// ErrTrimExcluded is Options.Validate's error when both Window and
// Trim are set. Test with errors.Is.
var ErrTrimExcluded = errors.New("agentloop: Window and Trim are mutually exclusive")
```

New constants:

```go
// RecoveryTargetTokens is the fixed compaction target of the
// prompt-too-long recovery path.
const RecoveryTargetTokens = 16384

// CompactionNotice is the user-role message content Run appends after
// a recovery compaction, so the model sees that compaction occurred.
const CompactionNotice = "Earlier messages were compacted into a context summary. Some detail was dropped."
```

`Options.Validate` gains four rules, in order after the existing ones:
a non-nil `Window` passes `Window.Validate`, wrapping any
`contextplan` validation error; a non-nil `Window` with a nil
`Summarizer` fails `ErrSummarizerRequired`; a non-nil `Window` with
a nil `Calibrated` fails `ErrEstimatorRequired`; a non-nil `Window`
with a non-nil `Trim` fails `ErrTrimExcluded`.

### Planning step, exact

Before each `Completer.Chat`, when `l.window` is non-nil, `Run`:

- Calls `l.calibrated.EstimateTokens` over a `provider.Request`
  carrying the history. An estimate error fails the iteration with
  `ErrPlanFailed`, wrapped with the iteration count.
- Passes through unchanged when the estimate is under
  `l.window.CompactTrigger()`.
- Otherwise runs the compaction sequence below, then proceeds with the
  compacted history.

The compaction sequence:

- Copies the caller's `Window` value and appends
  `contextplan.SummaryMessageName` to the copy's
  `Compaction.PreserveNames` only when absent, into a freshly
  allocated slice. The append never mutates the caller's backing
  array, and a caller already listing the name never trips
  `Compaction.Validate`'s duplicate rule. The injected summary
  therefore survives every later compaction whatever the caller
  configured.
- Removes any prior message named `SummaryMessageName` from the
  history and holds it aside. At most one summary message exists at
  any time; the prior one becomes summarizer input, not silently
  dropped content.
- Calls `contextplan.Compact` with the adjusted window and
  `l.calibrated`. `Compact` itself rejects an invalid window through
  `Window.Validate`, which runs `Compaction.Validate`. A `Compact`
  error fails the iteration with `ErrCompactionFailed`, wrapped with
  the iteration count and the underlying sentinel.
- Skips the summarizer call and the injection when `Compact`'s
  `Dropped` is empty and no prior summary message was held aside.
  Nothing droppable means nothing to summarize; the run proceeds with
  the retained history.
- Otherwise calls `l.summarizer.Summarize` over the prior summary
  message, when one was held aside, prepended to `Compact`'s
  `Dropped`. Any summarizer error fails the iteration with
  `ErrCompactionFailed`, wrapped with the iteration count and the
  contextsummary sentinel. This is the hard rule: no request, no
  messages, no tool calls are sent for that iteration.
- Injects `contextplan.SummaryMessage(s)` directly after the
  leading system message, or at index zero when none leads.
- Re-estimates the rebuilt history. Above the effective window's
  `Budget()`, the iteration fails with `ErrCompactionFailed`
  wrapping `contextplan.ErrRetentionOverflow`. An over-budget prompt
  is never sent.
- Replaces `history` with the compacted history only after the whole
  sequence succeeds. A failed compaction returns the pre-compaction
  history in `Result.History`, per the Result-shape rule.

The compacted history serves this iteration and every later one;
`Result.History` carries it after the run.

### Observation step, exact

After every `Completer.Chat` that returns a response, `Run` calls
`l.calibrated.Observe(resp.Usage.TotalTokens)` when `l.calibrated` is
non-nil. A non-positive `TotalTokens` is a no-op inside `Observe`, so
an under-reporting `Completer` cannot corrupt the correction factor.
The observation runs before the `MaxTotalTokens` check, on both the
normal path and a recovery retry.

This call shape is historical. The "Pair Observe's estimate to its
own request" addendum, later in this file, changes it to a
two-argument `Observe(estimated, actual)` call. Read that addendum
for the current shape.

### Recovery path, exact

When `Completer.Chat` returns an error matching
`errors.Is(err, provider.ErrPromptTooLong)` and `l.window` is
non-nil, `Run`:

- Builds a recovery window: a copy of `l.window` whose
  `Compaction.TriggerPercent` is 1, the minimum legal value, and
  whose `Compaction.TargetTokens` is `max(1, min(
  RecoveryTargetTokens, l.window.Budget()/4))`. The trigger override
  makes recovery compact even when the pre-Chat estimate sat below
  the configured trigger, which is exactly the rejected case
  recovery exists for. The floor keeps `TargetTokens` positive, so
  `Compaction.Validate` skips the percent comparison and the window
  stays valid for every budget of two tokens or more. A budget of
  one token hosts no legal target; recovery there fails closed with
  `ErrCompactionFailed` wrapping the window error.
- Runs the compaction sequence above against the recovery window,
  with one addition: one `RoleUser` message whose content is
  `CompactionNotice` is appended directly after the summary
  injection, before the sequence's final re-estimate. The budget
  check therefore prices the notice bytes too. A summarizer failure
  here propagates the same way, per the same hard rule.
- Retries the same iteration's `Chat` exactly once with the rebuilt
  history. Any error from the retry, including a second
  `ErrPromptTooLong`, propagates as a hard failure.
- Treats a `Compact` result with `Compacted` false as unrecoverable:
  the history estimates under one percent of `Budget()`, so no
  compaction the policy allows can shrink it. `Run` returns the
  original `ErrPromptTooLong` error unchanged, with no retry and no
  notice.

A nil `l.window` propagates the rejection unchanged; recovery needs
the window, the summarizer, and the estimator together.

These failures join the plan's closed hard-fail list: an
`ErrPlanFailed` estimate failure, an `ErrCompactionFailed` compaction
failure, and a propagated second rejection each return the partial
`Result` with `History`, `Iterations`, and `Usage` accumulated so far,
per the existing Result-shape rule.

### Addendum placement and import policy

`agentloop`'s `policy/layers.json` row gains two entries:

```json
"agentloop": ["provider", "tools", "trace", "hooks", "usage", "events", "contextbudget", "schema", "contextplan", "contextsummary"]
```

`contextplan` and `contextsummary` are declared before any code lands.
`agentloop` still imports no package that imports it.

### Addendum tests

In `agentloop/agentloop_test/`, new file `compaction_test.go` with one
scripted `Completer` and one scripted `Summarizer` per case:

- `Options.Validate`: a `Window` without `Summarizer` fails with
  `ErrSummarizerRequired`; without `Calibrated` fails with
  `ErrEstimatorRequired`; with both, and with `Trim` nil, passes;
  a `Window` and a `Trim` together fail `ErrTrimExcluded`; an invalid
  `Window` fails, wrapping the `contextplan` validation error. Every
  failing case asserts `errors.Is` against its sentinel.
- Under trigger: no summarizer call, history unchanged, `Observe`
  recorded after `Chat`.
- Over trigger: `Compact` ran, one summarizer call over the dropped
  messages, the summary message sits after the system message, its
  `Name` is `contextplan.SummaryMessageName`, and the request the
  `Completer` received carries the compacted history.
- At trigger with nothing droppable: an all-mandatory history at the
  trigger yields an empty `Dropped`; the summarizer is never called,
  no summary is injected, and the run sends the retained history
  normally.
- Summarizer failure: `Run` fails with `errors.Is(err,
  ErrCompactionFailed)` reaching the contextsummary sentinel; the
  `Completer` was never called that iteration; no tool call ran. The
  returned `Result.History` holds the pre-compaction history,
  unchanged, proving the replacement happens only on success.
- Retention overflow: a `Window` smaller than the mandatory set fails
  with `ErrCompactionFailed` reaching `contextplan.ErrRetentionOverflow`
  before any request.
- Prior summary replacement: a second compaction removes the earlier
  summary message, passes it inside the summarizer input, and leaves
  exactly one summary message in the sent history.
- Preserve-name injection: a caller `Window` whose `PreserveNames`
  omits the summary name still keeps the injected summary after a
  later compaction, proving `Run` appended it.
- Preserve-name duplicate: a caller `Window` whose `PreserveNames`
  already lists the summary name compacts successfully and preserves
  exactly one summary message, proving the append is
  duplicate-safe and never mutates the caller's slice.
- Recovery: the `Completer` returns `provider.ErrPromptTooLong` once,
  then succeeds. `Run` retries exactly once, sends the notice message,
  and the retried request's estimated size lands at or under the
  recovery target, notice bytes included. `Observe` ran for the
  successful retry.
- Recovery with a low estimator: an estimator that under-reports,
  below the configured trigger, still compacts on recovery down to
  `max(1, min(RecoveryTargetTokens, Budget over four))` and retries
  once, proving the trigger override works. A tiny `Budget()` clamps
  the target to one token and recovery still proceeds.
- Recovery with a tiny history: a history estimated under one percent
  of `Budget()` returns the original `ErrPromptTooLong` with no
  notice and no retry.
- Recovery twice: the `Completer` returns `ErrPromptTooLong` twice;
  the second rejection propagates with no third call.
- Recovery summarizer failure: the summarizer fails on the recovery
  path; `Run` fails with `ErrCompactionFailed` and no retry.
- Recovery without `Window`: the rejection propagates unchanged.
- Concurrency: goroutines call `Run` on one shared `*Loop` with
  `Window`, `Summarizer`, and `Calibrated` set, under
  `go test -race`. No race and no panic; the `Calibrated` mutex this
  change window adds in `docs/history/contextplan.md` carries the
  shared estimator.
- Nil `Window`: the full existing suite passes unchanged, proving the
  planning path adds no behavior when disabled.

### Addendum verification

- `make verify` passes, including the deps gate against the widened
  `agentloop` row and the API gate against the regenerated
  `api/agentloop.txt`.
- `go test -race ./agentloop/...` passes.
- `make api-update` runs; the `api/agentloop.txt` diff, the three
  `Options` fields, five sentinels, and two constants land in the same
  change as the code.
- Coverage floor of 85 holds for `agentloop` and the total.
- `docs/packages/agentloop.md` gains the planning and recovery surface
  in the same change as the code.
- This addendum lands with or after the `contextplan` compaction
  change, the `contextsummary` package, and the `provider`
  `ErrPromptTooLong` sentinel; `agentloop` compiles against all three.

## Addendum: graceful work-limit conclude

Status: shipped. A gap analysis against `internal/agent.Loop`, a
production caller in a separate, external repository, found a
capability `agentloop` lacked: a nudge toward a usable final answer as
`MaxIterations` approaches. This addendum closes that gap, then closes
one bug a post-ship logic review found in the same change window. It
changes `options.go`, `loop.go`, and `run.go`. It adds no new package
and no `policy/layers.json` row.

### Addendum goal

Nudge the model to produce a usable final answer as `MaxIterations`
approaches, instead of hard-stopping at `StopMaxIterations` with
whatever partial, mid-task state the transcript happens to hold.

### Addendum scope

Inside:

- Two new `Options` fields, `ConcludeMargin` and `ConcludeNotice`.
- One new `StopReason`, `StopConcluded`, for the case the nudge
  worked: the model returned no tool call on an iteration whose sent
  request still carried the notice.
- One new exported constant, `DefaultConcludeNotice`.
- One new sentinel, `ErrConcludeMargin`, for a negative
  `ConcludeMargin`.

Outside:

- Nudging as `MaxTotalTokens` approaches. Estimating "one more turn's
  headroom" against a token cap is a harder estimation problem than an
  iteration count, and needs its own review once this addendum's
  iteration-only nudge proves its shape.
- Stripping `Request.Tools` on the nudged iteration to force a
  text-only reply. This addendum only appends a text nudge; a model
  that still requests a tool call on the nudged iteration is not
  blocked, and `Run` still ends with `StopMaxIterations`, unchanged,
  if the limit is hit.
- Retrying the nudge more than once. `ConcludeMargin` fires the notice
  exactly once, on the first iteration it applies to.
- Interaction with `Options.Window`. `Window`'s compaction step may
  drop, reorder, or summarize away `ConcludeNotice` before the nudged
  `Completer` call sees it, the same class of risk as the `Trim` case
  below. No test covers `ConcludeMargin` combined with `Window`: no
  current caller pairs the two, and `Window` already excludes `Trim`
  per `Options.Validate`, so the combination has one fewer variable
  than the general case. A future addendum adds coverage once a
  caller needs both together.

### ConcludeMargin trigger formula

`run.go`'s loop tracks a 0-based `iterations` counter, incremented
after each `Completer` call, and checked `iterations >= l.maxIterations`
at the top of the loop before the next call. Number each `Completer`
call with a 1-based index `k`, so the call that runs while
`iterations` holds `k-1` is call `k`. `k` ranges from 1 to
`MaxIterations`.

`Run` appends `ConcludeNotice` to history, once, immediately before
the `Completer` call at the first `k` for which:

```
MaxIterations - k < ConcludeMargin
```

Zero `ConcludeMargin` never satisfies this inequality, since `k` never
exceeds `MaxIterations`, so nudging stays disabled. A `ConcludeMargin`
greater than or equal to `MaxIterations` satisfies it at `k = 1`, so
the nudge fires on `Run`'s first iteration.

Worked table, `MaxIterations = 5`:

| ConcludeMargin | First qualifying k | Call nudged | Notes |
| --- | --- | --- | --- |
| 0 | none | none | nudging disabled |
| 1 | 5 | the last allowed call only | `MaxIterations-5 = 0 < 1` |
| 2 | 4 | the next-to-last call | `k=4` and `k=5` both qualify; the notice appends once, at the first qualifying `k` |

### Addendum API

```go
// Options gains:

// ConcludeMargin nudges the model to produce a final answer as
// MaxIterations approaches, appending ConcludeNotice once, instead of
// hard-stopping at MaxIterations with no notice. Zero disables
// nudging. Run appends the notice before the Completer call at
// 1-based iteration k the first time MaxIterations-k < ConcludeMargin
// holds; k ranges from 1 to MaxIterations, so a positive ConcludeMargin
// greater than or equal to MaxIterations fires the nudge on Run's
// first iteration. See this addendum's worked table.
ConcludeMargin int

// ConcludeNotice is the RoleUser content Run appends once nudging
// starts. Empty ConcludeNotice with a positive ConcludeMargin uses
// DefaultConcludeNotice. Run appends the notice at the tail of
// history, as the last message in the nudged iteration's
// Request.Messages, not spliced near the system message the way
// CompactionNotice is. A tail append puts the "final answer now"
// instruction directly before the model's next response. The append
// runs after this iteration's Trim, Budget, and Window steps,
// immediately before the Completer call.
ConcludeNotice string
```

```go
// DefaultConcludeNotice is Options.ConcludeNotice's fallback text.
const DefaultConcludeNotice = "You are close to the iteration limit. Provide your best final answer now."

// StopConcluded is Run's stop reason when the model returns no tool
// call on an iteration whose sent request still carried
// ConcludeNotice. Graceful, same Result-shape rule as
// StopNoToolCalls.
const StopConcluded StopReason = "concluded"
```

```go
// ErrConcludeMargin is Validate's error when ConcludeMargin is
// negative. Test with errors.Is.
var ErrConcludeMargin = errors.New("agentloop: ConcludeMargin must not be negative")
```

`Options.Validate` gains one rule: a negative `ConcludeMargin` fails
validation with `ErrConcludeMargin`.

### Addendum bug: StopConcluded misattributed from stale notice text

A logic review run after this addendum first shipped found that
`run.go`'s original `noticeInRequest := noticePresent(history,
l.concludeNotice)` scanned the entire current history for the notice
text, with no check that this run's own `ConcludeMargin` logic ever
appended it. `loop.go`'s `resolveConcludeNotice` resolves
`concludeNotice` to `DefaultConcludeNotice` unconditionally in `New`,
even when `ConcludeMargin` is zero, so that exported string is always
a live match target.

Two reproductions confirmed the bug: `ConcludeMargin=0` (fully
disabled) with a caller-supplied initial message equal to
`DefaultConcludeNotice` returned `StopConcluded` instead of
`StopNoToolCalls`; `ConcludeMargin=2` with the threshold never reached
this run, same coincidental initial message, returned the same wrong
result. A realistic trigger: an app that feeds a prior `Run` call's
leftover `Result.History` into a fresh `Run` call carries the old
notice text forward, even when the new call disables or never reaches
the nudge.

Fix: `noticeInRequest := noticeSent && noticePresent(history,
l.concludeNotice)`, gating presence on this run's own causal flag.
This preserves the original fix for the `Trim`-drops-the-notice case
(`noticeSent` true, `noticePresent` false after `Trim` strips it,
still resolves to `StopNoToolCalls`) while closing the false positive
when this run never appended anything itself.

### Addendum tests

- `Options.Validate`: a negative `ConcludeMargin` fails with
  `errors.Is(err, ErrConcludeMargin)`. A zero or positive
  `ConcludeMargin` passes.
- A scripted `Completer` set to run past `MaxIterations` without
  `ConcludeMargin` stops at `StopMaxIterations`, unchanged from the
  base plan.
- `MaxIterations=5`, `ConcludeMargin=2`, a scripted `Completer` that
  returns tool calls through iteration 3, and returns no tool call at
  iteration 4: the sent `Request.Messages` for the iteration-4 call
  ends with the notice, and `Run` stops at `StopConcluded` with
  `Result.Iterations == 4`. This is the next-to-last-call row of the
  worked table above.
- `MaxIterations=5`, `ConcludeMargin=1`, the same scripted `Completer`
  returning tool calls through iteration 4, and no tool call at
  iteration 5: the notice appends only before the iteration-5 call,
  not iteration 4, matching the worked table's last-call-only row.
- The request sent to `Completer` on the nudged iteration carries the
  notice message as the last element of `Request.Messages`.
- `MaxIterations=5`, `ConcludeMargin=2`, a scripted `Completer` that
  returns no tool call at iteration 1, strictly before the first
  qualifying `k=4`: `Run` stops at `StopNoToolCalls`, not
  `StopConcluded`, and the iteration-1 `Request.Messages` carries no
  notice. This pins the boundary between an early, non-nudged stop and
  a nudged one.
- `MaxIterations=1`, `ConcludeMargin=1`: `k=1` satisfies
  `MaxIterations-k < ConcludeMargin` on Run's first iteration, so the
  first `Request.Messages` sent to `Completer` ends with the notice.
  This pins the doc comment's claim that a `ConcludeMargin` greater
  than or equal to `MaxIterations` fires the nudge on Run's first
  iteration.
- A `ConcludeMargin` set, but the model still requests a tool call on
  the nudged iteration and every iteration after, still stops at
  `StopMaxIterations` once the limit is reached; the sent request on
  the nudged iteration still carries the notice.
- An empty `ConcludeNotice` with a positive `ConcludeMargin` sends
  `DefaultConcludeNotice`. A caller-set `ConcludeNotice` sends that
  text instead.
- The nudge message appends exactly once across a multi-iteration run,
  even when several iterations pass while inside the margin
  (`ConcludeMargin=2` case above already covers this: `k=4` and `k=5`
  both qualify, but only one notice appends).
- A `Trim` hook that drops every `RoleUser` message, run with
  `ConcludeMargin` set: the notice appends to history, then the next
  iteration's `Trim` call drops it before the following `Completer`
  call sees it. `Run` still reaches `StopMaxIterations`, since the
  model was never actually nudged. This is a documented, accepted
  limit, not a guarantee this addendum makes: `Options.Trim` already
  runs on the full history before every `Completer` call and may drop
  any message, per the base plan's `Trim` contract; `ConcludeNotice`
  gets no special protection from that contract.
- The bug-fix regression: `MaxIterations=3`, `ConcludeMargin=2`, `Trim`
  drops every `RoleUser` message, the model calls tools through
  iteration 2 and returns no tool call at iteration 3. The notice
  reaches iteration 2's request, `Trim` strips it before iteration 3's
  request, and `Run` stops at `StopNoToolCalls`, not `StopConcluded`,
  since the model never saw the notice on the iteration it stopped.
- Two more bug-fix regressions: `ConcludeMargin=0` with a caller
  initial message equal to `DefaultConcludeNotice` stops at
  `StopNoToolCalls`; `ConcludeMargin=2` with the threshold unreached
  this run and the same coincidental initial message also stops at
  `StopNoToolCalls`.

### Addendum verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change. `docs/packages/agentloop.md` gains the
`ConcludeMargin`/`ConcludeNotice` surface and the "Graceful conclude
near MaxIterations" section in the same change as the code.

## Addendum: duplicate-call dedup within a turn

Status: shipped. A gap analysis against `internal/agent.Loop`, a
production caller in a separate, external repository, found a
capability `agentloop` lacked: detection of a duplicate tool call
within one turn, before it runs twice. This addendum closes that gap.
It changes `options.go`, `loop.go`, and `toolcall.go`, and adds
`wire.go`. It adds no new package and no `policy/layers.json` row.

### Addendum goal

Detect an identical `(tool name, arguments)` call already served
earlier in the same turn, and serve a fixed notice instead of running
the tool a second time. Avoid a duplicate side effect from a model
that requests the same call twice in one turn's response.

### Addendum scope

Inside:

- One new `Options` field, `DedupWithinTurn`.
- Comparing calls by tool name and a canonical form of `Arguments`,
  scoped to one turn: the set resets every call to `runToolCalls`, one
  call per turn. One new unexported helper in `wire.go`,
  `canonicalizeArgs(raw json.RawMessage) (string, error)`, builds the
  canonical form: decode `raw` with a `json.Decoder` configured with
  `UseNumber()`, into an `any`, then `json.Marshal` the result.
  `UseNumber()` decodes each JSON number token into a `json.Number`, a
  string type that keeps the source digits verbatim, instead of
  collapsing every number into a Go `float64`. Plain `float64`
  decoding loses precision above 2^53 and would silently canonicalize
  two distinct large integers (IDs, nanosecond timestamps) to the same
  value; `UseNumber()` avoids that collapse. `encoding/json` sorts
  object keys on marshal, so this normalizes key order without a
  repo-specific canonicalization utility.
- Trailing-data check after decode. `json.Decoder.Decode` consumes
  only one JSON value and does not check for bytes left over, unlike
  `json.Unmarshal`. `canonicalizeArgs` calls `dec.More()` right after
  `dec.Decode(&v)` succeeds. `dec.More()` true means bytes remain
  after the first value: trailing garbage (`{"a":1}garbage`) or a
  second concatenated JSON value (`{"a":1}{"b":2}`).
  `canonicalizeArgs` treats a true `dec.More()` as a canonicalization
  error (`errTrailingArgsData`) and returns it, so the call falls
  under the fail-open contract below instead of canonicalizing from a
  partial, misleading prefix.
- Fail-open error contract for `canonicalizeArgs`. `call.Arguments` is
  raw wire bytes assembled from streaming deltas and can be malformed
  JSON before schema validation runs. When `canonicalizeArgs` returns
  an error, the call is excluded from the dedup set: it is never
  treated as a duplicate, and it always runs. A canonicalization error
  never blocks a call and never fails the turn.
- Hook-firing contract for a deduped call. The dedup check runs before
  `PointPreTool` and short-circuits: a call identified as a duplicate
  never reaches `PointPreTool` or `PointPostTool`. Those hook points
  fire once per turn for a given `(tool, canonical-argument)` pair, on
  the call that actually runs.
- Dedup-set seeding for an errored call. `runOneToolCall` can append a
  `RoleTool` error message via `ErrorPolicyReport` without the
  underlying tool ever running. Any `RoleTool` message reaching
  history for a call, success or error, seeds the dedup set for that
  call's `(tool, canonical-argument)` pair. A later byte-identical
  retry in the same turn is deduped either way, since the goal is to
  avoid re-triggering a call already resolved one way or another.
- One new exported constant, `DuplicateCallNotice`, served as the
  `RoleTool` content for a detected duplicate.

Outside:

- Any interaction with a turn-level result-size shaping type. No such
  type exists in `agentloop`. This addendum's dedup check produces one
  `RoleTool` message per call, deduped or not, like any other tool
  result; it defines no contract with a future result-shaping pass.
- Cross-turn dedup: a call repeated on a later iteration is not
  detected. `agentloop` holds no cross-iteration call history for this
  purpose, and adding one is a larger design question about how long a
  served call stays "recent" across a long-running loop.
- True in-flight, concurrent dedup. Tool calls run sequentially, one
  at a time; "already in flight" and "already served" are the same
  condition. A future concurrent-tool-call design needs its own dedup
  review.
- Reusing the first call's actual result for the duplicate. The
  duplicate always gets `DuplicateCallNotice`, a fixed notice, not the
  original result replayed. Replaying a stale result risks the model
  treating it as fresh, a correctness problem this addendum does not
  take on.

### Addendum API

```go
// Options gains:

// DedupWithinTurn detects a duplicate (tool, canonical-argument) call
// already served earlier in the same turn, and serves
// DuplicateCallNotice instead of running the tool again. False, the
// zero value, runs every call, unchanged from the base plan.
DedupWithinTurn bool
```

```go
// DuplicateCallNotice replaces a tool result's content when
// DedupWithinTurn detects the same (tool, canonical-argument) call
// already served earlier in the same turn.
const DuplicateCallNotice = "[duplicate-call] This exact tool call was already served earlier in this turn; skipped to avoid a repeated side effect."
```

`canonicalizeArgs` stays unexported: `canonicalizeArgs(raw
json.RawMessage) (string, error)`. It is not part of the locked
surface. Its contract, enforced by the caller in `agentloop`:

- On success, it returns a canonical string form of `raw`: numbers
  keep their source digits via `json.Number`, object keys sort by
  `encoding/json`'s marshal order. Success requires `raw` to decode as
  exactly one JSON value with no bytes left over; `canonicalizeArgs`
  checks this with `dec.More()` after `dec.Decode`.
- On error (malformed `Arguments`, or a valid JSON prefix followed by
  trailing bytes), the caller treats the call as never dedup-eligible:
  it runs unconditionally and is never recorded in, or matched
  against, the dedup set. This is a fail-open contract; a
  canonicalization error never blocks a call and never fails the turn.
- The dedup check, including a call to `canonicalizeArgs`, runs before
  `PointPreTool`. A call identified as a duplicate short-circuits
  there: it never reaches `PointPreTool` or `PointPostTool`, and the
  underlying tool never runs for it.

### Addendum tests

- `DedupWithinTurn` false runs two identical calls in one turn twice,
  unchanged from the base plan.
- `DedupWithinTurn` true, two identical calls (same name, byte-equal
  `Arguments`) in one turn, runs the first and serves
  `DuplicateCallNotice` for the second, without a second `RunScoped`
  call reaching the underlying tool.
- Two calls with the same tool name but semantically identical
  `Arguments` in a different key order both compare equal under
  canonicalization, and the second is deduped.
- Two calls with the same tool name and genuinely different
  `Arguments` both run; neither is treated as a duplicate.
- `canonicalizeArgs` adversarial cases:
  - A numeric literal written as `1` in one call and `1.0` in the
    other: `UseNumber()` keeps each source digit string as a distinct
    `json.Number`, so the two calls are not deduped against each
    other.
  - Two large distinct integers that collide under naive `float64`
    decoding, `9007199254740992` (2^53) and `9007199254740993`: with
    `UseNumber()`, `canonicalizeArgs` keeps their exact digit strings
    distinct, so the two calls compare unequal and both run. This
    proves the fix closes the numeric-precision false-positive that
    plain `float64` decoding would have caused.
  - Raw JSON with a duplicate key, for example `{"a":1,"a":2}`:
    `encoding/json` keeps the last value on decode, so
    `canonicalizeArgs` sees only `{"a":2}`. A caller that sends
    ambiguous duplicate-key JSON gets that encoding/json behavior, not
    a dedup guarantee.
  - Two calls whose string values differ only by Unicode-escape form,
    for example `"café"` versus `"café"`: `encoding/json` decodes
    both into the same Go string, so the second call is deduped.
  - Malformed `Arguments`, for example truncated or non-JSON bytes:
    `canonicalizeArgs` returns an error. The call still runs, is never
    treated as a duplicate, and the turn does not fail.
  - `Arguments` holding a valid JSON value followed by trailing bytes,
    for example `{"a":1}garbage` or two concatenated JSON values
    `{"a":1}{"b":2}`: `canonicalizeArgs` detects the leftover bytes
    with `dec.More()` and returns an error instead of silently
    ignoring them. A second call carrying the same leading fragment
    but different trailing bytes is never falsely deduped against the
    first.
- The synthesized `RoleTool` message for a deduped call carries the
  duplicate call's own `ToolCallID`, not the first call's `ToolCallID`.
- `PointPreTool` and `PointPostTool` invocation counts for a turn with
  two identical calls: both hook points fire exactly once for the
  pair, on the first call only.
- A `PointPreTool` veto on the first of two identical calls stops the
  turn before the second call is ever reached.
- The dedup set resets between iterations: a call repeated on a later
  iteration runs again, proving no cross-turn dedup happens.
- `Options.Audit`'s `AuditKindToolCall` record for a deduped call
  carries a nil `Err`, since a served duplicate is not a tool-run
  error.
- A first call that resolves as an `ErrorPolicyReport`-appended
  `RoleTool` error message, without the underlying tool running,
  still seeds the dedup set: a byte-identical retry of that same call
  later in the same turn is deduped and serves `DuplicateCallNotice`.
- An `Options.Audit` error returned on a deduped call's own
  `AuditKindToolCall` record fails `Run`, exactly like an audit error
  on a normally-run call: the `DuplicateCallNotice` message reaches
  history before the audit call runs, so it survives in the returned
  partial `Result`.
- `FuzzCanonicalizeArgs`, in `agentloop` itself since
  `canonicalizeArgs` is unexported: arbitrary bytes never panic
  `canonicalizeArgs`, and a successful canonicalization is idempotent,
  re-canonicalizing the returned form yields the same string.

### Addendum verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change. `docs/packages/agentloop.md` gains the
`DedupWithinTurn`/`DuplicateCallNotice` surface and a "Duplicate-call
dedup within a turn" section in the same change as the code.

## Addendum: heartbeat and progress events

Status: shipped. This addendum was phase 83
(`docs/history/agents/PHASES.md`); no standalone phase 83 plan file
remains. It adds `Options.HeartbeatInterval` and six `events.Name`
progress events to `Run`. It changes `heartbeat.go` (new),
`options.go`, `run.go`, `loop.go`, and `toolcall.go`. It adds no new
package and no new `policy/layers.json` edge: `events` was already an
allowed `agentloop` import.

### Addendum goal

Emit periodic events on `Options.Bus`, wired since the base plan but
never used, so a caller can show progress during a long `Completer`
call or a long tool batch.

### Addendum scope

Inside:

- One new `Options` field, `HeartbeatInterval time.Duration`.
- Six new `events.Name` constants for iteration start/end, a periodic
  completion heartbeat, a tool-call start/end, and a periodic
  per-tool-call heartbeat.
- Emitting on `Bus` at each of those points. A `Bus.Emit` error is
  swallowed, the same way `Run` already swallows a
  `events.Registry.Fire` error from `PointStop`: a heartbeat is
  observability, not a control-flow gate. The gate is decoupled
  per event class: the four lifecycle names (iteration start/end,
  tool-call start/end) fire whenever `Bus` is non-nil, while the
  two heartbeat names tick only when `HeartbeatInterval` is
  positive. The original revision gated every name on a positive
  `HeartbeatInterval`; that forced lifecycle-only consumers to
  pick a large-but-finite interval purely to keep the ticks quiet,
  a cadence value doing gating work it does not name.
- `Options.Validate` gains one rule, appended after the `Window`
  block: a positive `HeartbeatInterval` with a nil `Bus` fails with
  `ErrHeartbeatRequiresBus`.
- An extraction in `run.go`: `runIteration` holds the single-iteration
  body `run`'s `for` loop used to inline, so `run` stays under the
  80-line function cap while bracketing every iteration exit path
  with `EventIterationStart`/`EventIterationEnd`.
- A shared ticker helper, `startHeartbeat`, used by both the
  completion-heartbeat site in `runIteration` and the tool-call-
  heartbeat site in `runOneToolCall`.

Outside:

- Any change to the `events` package. `events.Name` is already
  `type Name string`; `agentloop` defines its own constants of that
  type. `events.Bus`, `events.Event`, and `events.Handler` need no
  change.
- Carrying structured data richer than `events.Event.Data string`
  allows. Every heartbeat event's `Data` is a short, human-readable
  string.

### Addendum API

New `Options` field:

```go
// HeartbeatInterval emits a heartbeat Event on Bus every interval
// while one Completer call or one tool call is in flight. Zero
// disables heartbeats. A positive HeartbeatInterval requires a
// non-nil Bus.
HeartbeatInterval time.Duration
```

New event names:

```go
const (
    EventIterationStart      events.Name = "agentloop.iteration.start"
    EventCompletionHeartbeat events.Name = "agentloop.completion.heartbeat"
    EventToolCallStart       events.Name = "agentloop.tool_call.start"
    EventToolCallHeartbeat   events.Name = "agentloop.tool_call.heartbeat"
    EventToolCallEnd         events.Name = "agentloop.tool_call.end"
    EventIterationEnd        events.Name = "agentloop.iteration.end"
)
```

New sentinel:

```go
// ErrHeartbeatRequiresBus is Options.Validate's error when
// HeartbeatInterval is positive and Bus is nil. Test with errors.Is.
var ErrHeartbeatRequiresBus = errors.New("agentloop: HeartbeatInterval requires a non-nil Bus")
```

`runOneToolCall` (`toolcall.go`) returns before `RunScoped` on a
`PointPreTool` veto. `EventToolCallStart` fires on entry, before the
`PointPreTool` fire, and `EventToolCallEnd` fires from a deferred
closure covering every return path, including the veto path and the
`PointPreTool` hook-error path. The `EventToolCallHeartbeat` ticker
starts only after the veto check passes, since a vetoed call never
reaches the blocking `RunScoped` work a heartbeat reports progress
on.

See [packages/agentloop.md](packages/agentloop.md)'s "Events" section
for the full field list, the failure mode, and the gating rule.

### Addendum tests

- `Options.Validate`: a positive `HeartbeatInterval` with a nil `Bus`
  fails with `errors.Is(err, ErrHeartbeatRequiresBus)`, only after
  every earlier check passes. A zero `HeartbeatInterval` passes
  regardless of `Bus`.
- A scripted `Completer` that blocks past two heartbeat intervals,
  run with a `Bus` subscribed to `EventCompletionHeartbeat`, receives
  at least two heartbeat events before the call returns. A zero
  `HeartbeatInterval` receives none.
- `EventIterationStart`/`EventIterationEnd` fire once per iteration,
  in order, on every exit path from `runIteration`: ctx cancellation,
  a trim error, a budget error, a `planHistory` error, an audit
  error, the token-budget-exceeded error, a tool-call error, and both
  graceful stops.
- Heartbeat ticker goroutine cleanup: no leak when ctx is canceled
  mid-call, and no leak when the call returns before the first tick
  fires.
- `PointPreTool` veto and hook-error paths: `EventToolCallStart`
  followed immediately by `EventToolCallEnd`, with no
  `EventToolCallHeartbeat` in between.
- Tool-call heartbeat, positive path: `EventToolCallStart`, at least
  two `EventToolCallHeartbeat` events, then `EventToolCallEnd`, in
  order.
- `Bus.Emit` errors are swallowed, matching
  the `PointStop`/`events.Registry.Fire` swallow precedent; a name
  with no subscriber emits nothing; `Run` completes normally.
- A race sub-case: heartbeat emission from the ticking goroutine and
  the main loop's own state changes run concurrently, under
  `go test -race`, with no data race.

### Addendum verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change: `events` was already an allowed import.

## Addendum: per-batch tool-result size shaping

Status: superseded. Shipped as phase 80
(`docs/history/agents/PHASES.md`), then removed: the only intended
consumer, the sibling consumer repo's CLI adapter, rejected the omit-over-budget
semantics and ships its own degrade-with-notice shaping wrapper. The
field, the notice constant, and the sentinel are gone. See
`docs/packages/agentloop.md` for the current `Bounds` surface.

### Addendum goal

Cap one turn's tool results as a set, by summed byte size, instead of
capping only one call's result at a time. `tools.ResultBudgetOf`
already caps one call's own rendered content; nothing capped the
combined size of several results returned in the same turn.

### Addendum scope

Inside:

- One new `Options` field, `TurnResultBudget int`.
- Shaping every call's already-rendered content, in `ToolCall.Index`
  order, against a running total for the turn, after each call's own
  `tools.ResultBudgetOf` bound already applied.
- One new exported constant, `BatchTruncationNotice`, marking a
  batch-truncated result, distinct from `wire.go`'s existing
  per-call `truncationMarker`.
- `Options.Validate` gains one rule: a negative `TurnResultBudget`
  fails with a new sentinel, `ErrTurnResultBudget`.

Outside:

- Any change to `tools.ResultBudgetOf`, `tools.ResultBudgetTool`, or
  any other `tools` symbol. This shapes already-rendered `string`
  content inside `agentloop`; it adds no new `tools` API.
- Skipping or refusing to run a call because the turn's budget is
  already exhausted. Every call in the turn still runs, and every
  call's hooks still fire, unchanged; only the rendered content that
  reaches history is shaped.
- Redistributing budget by call importance or size. The policy is
  first-come: earlier calls, in `Index` order, keep their content
  whole while budget remains; once the running total exhausts the
  budget, each later call's content is replaced with a fixed notice.

### Addendum API

```go
// TurnResultBudget caps the summed byte size of one turn's rendered
// tool results, across every call in that turn, before they append to
// history. Zero means uncapped: the budget comparison and any shaping
// are skipped entirely, and every call's content passes through
// whole. Distinct from a Tool's own tools.ResultBudgetOf bound, which
// caps one call's content alone; TurnResultBudget shapes the batch as
// a set, after each call's own bound already applied, in
// ToolCall.Index order. Hard cap when positive: a call's content
// stays whole only when the running total plus that content's byte
// length does not exceed TurnResultBudget; otherwise the content is
// replaced with BatchTruncationNotice and the running total does not
// grow for it. The running total never exceeds TurnResultBudget.
// Applies to every appended RoleTool content, including a reported
// tool-run error under ErrorPolicyReport.
TurnResultBudget int
```

```go
// BatchTruncationNotice replaces a tool result's content when
// TurnResultBudget is exhausted before that call's turn in Index
// order.
const BatchTruncationNotice = "[batch-truncated] Turn tool-result budget exhausted; this result was omitted."
```

```go
// ErrTurnResultBudget is Validate's error when TurnResultBudget is
// negative. Test with errors.Is.
var ErrTurnResultBudget = errors.New("agentloop: TurnResultBudget must not be negative")
```

`runToolCalls` (`toolcall.go`) tracks the running total as a local
`int`, reset to zero once per call, at the start of each turn's
batch. The shaping step runs after `runOneToolCall` returns and
before both the `history` append and the audit call, so
`AuditRecord.Err` always reports the true per-call outcome,
independent of the shaping applied to `AuditRecord.ToolResult`.
`runToolCalls`'s sort switched from `sort.Slice` to `sort.SliceStable`
in the same change, so a duplicate `Index` value's tie-order, now
externally observable through which call's content survives, stays
deterministic.

See [packages/agentloop.md](packages/agentloop.md)'s "Turn result
budget" section for the full field description and failure mode.

### Addendum tests

- `Options.Validate`: a negative `TurnResultBudget` fails with
  `errors.Is(err, ErrTurnResultBudget)`. Zero and positive values
  pass.
- A turn with two tool calls, a `TurnResultBudget` sized to fit the
  first call's content but not both, keeps the first call's content
  whole and replaces the second call's content with
  `BatchTruncationNotice`.
- The same setup with `TurnResultBudget` zero appends both calls'
  content whole, unchanged from the base plan.
- A turn where the first call's own `tools.ResultBudgetOf` bound
  already truncates it proves the two truncation layers apply in the
  documented order: the per-call bound first, the batch bound second.
- An exact-boundary case (running total plus one call's content
  length equal to `TurnResultBudget`) keeps that call's content
  whole, pinning the `<=` comparison against an off-by-one.
- A reported tool-run error under `ErrorPolicyReport` counts toward
  the running total exactly like a successful result, and gets
  replaced with `BatchTruncationNotice` when the batch budget is
  exhausted, while `AuditRecord.Err` still carries the true error.
- `Options.Audit`'s `AuditKindToolCall` record for a batch-truncated
  call carries the shaped `BatchTruncationNotice` content in
  `ToolResult`, with `Err` unaffected by the shaping either way.
- A `PointPreTool` veto partway through a turn stops later calls from
  running, unchanged from the base plan; the shaping pass only
  considers the calls that ran before the veto.
- Duplicate-`Index` calls resolve in input-slice order,
  deterministically, proving the `sort.SliceStable` switch.

### Addendum verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change.

## Addendum: Budget checks the post-compaction history

Status: shipped. This addendum fixes an ordering
defect in `run.go`'s iteration loop. It changes `run.go` and
`options.go`. It adds no new file, no new package, and no
`policy/layers.json` row.

### Addendum goal

Make `Options.Budget` check the history a `Completer.Chat` call will
actually receive. Today `checkBudget` runs before window compaction,
so a caller who sets both `Budget` and `Window` can get `ErrOverBudget`
on a pre-compaction history that `Window` would have compacted well
under `Budget`. After this addendum, `checkBudget` runs after window
compaction, on the same history the request carries.

### Addendum bug

`run`'s per-iteration order today is: `ctx.Err`, `MaxIterations`,
`applyTrim`, `checkBudget`, then, only when `l.window` is non-nil,
`planHistory`. `checkBudget` sums `history`'s content bytes and
message count and checks them against `l.budget.Fits`, ahead of any
compaction. `Options.Validate` does not exclude `Budget` and `Window`
together, unlike `Trim` and `Window`, which fail `ErrTrimExcluded`
when combined. A caller who wires both expects `Window` to run first:
`Budget` bounds raw byte and message-count size, `Window` bounds token
count through LLM-driven compaction, and compaction predictably
shrinks both bytes and message count. Checking `Budget` first defeats
that expectation and fails runs a caller configured `Window` to
rescue.

### Addendum decision: reorder, do not double-check

Two shapes could fix this. Move `checkBudget` to run after
`planHistory`, so it checks whatever history the request will carry,
compacted or not. Or keep the existing pre-compaction check and add a
second post-compaction check. The second shape adds a call site and a
comment explaining why two checks exist, for no behavior a single
post-compaction check does not already give: `contextbudget.Limits`
has no concept of "pre-compaction" versus "post-compaction" budget, so
a caller has no way to set different limits for the two checks, and a
history that would fail before compaction but pass after is exactly
the case `Window` exists to fix.

A single post-compaction check does drop one thing: an early exit
before compaction runs, for a pathologically large history that
`Budget` alone can reject without ever calling the estimator, the
summarizer, or the compaction sequence. This is not a correctness
loss. `planHistory`'s own trigger check is cheap, one
`Calibrated.EstimateTokens` call over the uncompacted history, and
`Window`'s own `Compact` call fails closed with
`ErrCompactionFailed`/`contextplan.ErrRetentionOverflow` when even the
mandatory retention set cannot fit the window. A caller who wants a
pre-compaction ceiling for cost control, not correctness, can size
`Budget` for that purpose or add a `Trim` step; `Trim` and `Window`
stay mutually exclusive, so this addendum does not reopen that
question.

Move `checkBudget` to run once, after `planHistory`. This is the
smallest change: a call-site move in `run`, plus doc-comment
corrections. No new sentinel, no new field, no new exported symbol.

This reorder changes real behavior for one caller shape: a
`Budget`-and-`Window` caller whose history stays over budget even
after compaction. Today that caller fails fast with `ErrOverBudget`
before compaction runs. After this addendum, that caller pays for one
compaction attempt, including a summarizer call, before the same
`ErrOverBudget` failure. A caller relying on the pre-compaction fail-
fast path for cost control sees one extra LLM call before the run
fails.

### Addendum scope

Inside:

- Reordering `run`'s per-iteration body: `applyTrim`, then, when
  `l.window` is non-nil, `planHistory`, then `checkBudget`, then
  `runChat`. When `l.window` is nil, `checkBudget` runs directly after
  `applyTrim`, exactly where it runs today; a caller who sets `Budget`
  without `Window` sees no behavior change.
- Correcting `Options.Budget`'s and `Options.Window`'s doc comments in
  `options.go` to state the corrected order.
- Rewriting `TestRunBudgetChecksBeforeWindowCompaction` in
  `agentloop/agentloop_test/compaction_test.go` to assert the
  corrected behavior, and adding one sibling test proving `Budget`
  still trips when the post-compaction history remains over budget.

Outside:

- Any change to `contextbudget.Limits`, `Fits`, or `Validate`. The
  fix is ordering only; the budget-check math does not change.
- Any change to `checkCompactedBudget` in `compaction.go`. That
  function already checks the window's own token budget against the
  post-compaction history; it is unrelated to `Options.Budget` and
  `contextbudget.Limits`, and this addendum does not touch it.
- A second `Budget` check, pre- and post-compaction. Rejected above.
- Any change to `MaxTotalTokens`, `MaxCallsPerTurn`, or any other
  `Options` field's check order relative to `checkBudget`.

### Addendum API

No exported symbol changes. `Options.Budget` and `Options.Window` keep
their field names, types, and positions; only their doc comments
change text. `ErrOverBudget` keeps its meaning and trigger condition;
only the history it inspects, when `Window` is set, changes.
`api/agentloop.txt` locks field signatures, not doc comments (see the
existing `Budget *contextbudget.Limits` and `Window
*contextplan.Window` lines), so this addendum needs no
`api/agentloop.txt` diff and no `make api-update` run.

`Options.Budget`'s doc comment changes from:

```go
// Budget caps one Completer call's message history by byte count
// and message count. A nil Budget means uncapped.
Budget *contextbudget.Limits
```

to:

```go
// Budget caps one Completer call's message history by byte count
// and message count. A nil Budget means uncapped. When Window is
// also set, Budget checks the history after window compaction runs,
// so a history Window would compact under Budget never fails here.
// When Window is nil, Budget checks history exactly as sent.
Budget *contextbudget.Limits
```

`Options.Window`'s doc comment changes from:

```go
// Window plans every iteration against a token budget. A nil Window
// disables planning; the loop then runs exactly as before. A non-nil
// Window requires Summarizer and Calibrated, and excludes Trim.
Window *contextplan.Window
```

to:

```go
// Window plans every iteration against a token budget. A nil Window
// disables planning; the loop then runs exactly as before. A non-nil
// Window requires Summarizer and Calibrated, and excludes Trim. When
// Budget is also set, Window's compaction runs before the Budget
// check, so Budget sees the compacted history, not the raw one.
Window *contextplan.Window
```

### Addendum mechanics, exact

In `run.go`'s `run` function, move the `checkBudget` call from
directly after `applyTrim` to directly after the `l.window != nil`
block that calls `planHistory`. The moved call keeps its exact
signature, `l.checkBudget(history, iterations)`, and keeps checking
whatever `history` currently holds at that point in the loop: the
trimmed and, when `l.window` is set, compacted history. No change to
`checkBudget` itself in `compaction.go`'s sibling file or to
`contextbudget.Limits`. The resulting order in `run`:

1. `ctx.Err()` check.
2. `MaxIterations` check.
3. `applyTrim` (when `l.trim` is set).
4. `planHistory` (when `l.window` is set), which may itself fail with
   `ErrPlanFailed` or `ErrCompactionFailed` before `checkBudget` ever
   runs.
5. `checkBudget` (when `l.budget` is set), against the
   post-`applyTrim`, post-`planHistory` history.
6. `runChat`.

### Addendum tests

In `agentloop/agentloop_test/compaction_test.go`:

- Rename `TestRunBudgetChecksBeforeWindowCompaction` to
  `TestRunBudgetChecksAfterWindowCompaction`. Keep the doc comment
  above it, rewritten to describe the corrected assertion.
  `newPlanningFixture` takes no `Budget` parameter (see
  `compaction_test.go:76-98`). This test builds `agentloop.Options`
  directly instead, the way the original test did
  (`compaction_test.go:372-380`), wiring `Window`, `Summarizer`, and
  `Calibrated` by hand alongside `Budget`. The test scripts one
  completer response, `{Message: provider.Message{Role:
  provider.RoleAssistant, Content: "done"}}`. That response lets the
  run reach `StopNoToolCalls` right after the compacted, under-budget
  history reaches the completer. Fixture:
  `msgs` is four messages, in order: `{RoleSystem, "s"}`,
  `{RoleUser, strings.Repeat("o", 5000)}`, `{RoleAssistant, "a"}`,
  `{RoleUser, "l"}`. `w := contextplan.Window{MaxTokens: 400,
  Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens:
  20}}`, matching `TestRunOverTriggerCompactsThroughSummarizer`'s
  proven shape so the same mandatory-retention and drop pattern
  applies: the trigger trips, `Compact` drops only the 5000-byte user
  message, keeps `system`, `assistant`, and the trailing `user`, and
  the summarizer injects one `SummaryMessageName` message after the
  system message. `Options.Budget = &contextbudget.Limits{MaxBytes:
  200}` (`MaxEvents` left zero, uncapped: message count does not
  change across compaction in this fixture, so only `MaxBytes`
  isolates the pre/post difference). Pre-compaction content bytes sum
  to 5003, over the 200-byte cap; post-compaction content bytes sum to
  the fixed-size JSON summary reply (see `summaryReplyJSON`, 97 bytes)
  plus the three one-byte messages, 100 bytes, under the cap. Assert:
  `loop.Run` returns a nil error. `res.Stop ==
  agentloop.StopNoToolCalls`. `completer.callCount() == 1`. The
  summarizer's `stats()` call count is 1. The request the completer
  received (`f.completer`'s recorded request) does not contain the
  5000-byte message's content.
- Add `TestRunBudgetTripsAfterCompactionStillOverBudget`. Same `msgs`
  and `w` as the renamed test above, so the same compaction pattern
  applies and the summarizer runs once. `Options.Budget =
  &contextbudget.Limits{MaxBytes: 50}`: smaller than the 100-byte
  post-compaction size, so the post-compaction history still exceeds
  it. Assert: `errors.Is(err, agentloop.ErrOverBudget)`.
  `completer.callCount() == 0`: the budget check must trip after
  compaction runs but before any `Completer.Chat` call. The
  summarizer's `stats()` call count is 1: proving compaction ran, and
  the trip happens on the compacted history, not a bypass of
  compaction. `isZeroResult(res)` is true, matching `hardFail`'s
  zero-iterations rule.
- No other existing test in this file changes. `TestRunUnderTriggerNoCompaction`,
  `TestRunAtExactTriggerCompacts`,
  `TestRunOverTriggerCompactsThroughSummarizer`,
  `TestRunAtTriggerNothingDroppableSkipsSummarizer`,
  `TestRunSummarizerFailureFailsBeforeRequest`, and
  `TestRunRetentionOverflowFailsBeforeRequest` set no `Budget`, so
  reordering `checkBudget` changes nothing they assert.
- `TestRunBudgetExceededLaterIteration` in
  `agentloop/agentloop_test/loop_bounds_test.go` sets `Budget` without
  `Window`. The reorder is a no-op on that path: `checkBudget` still
  runs directly after `applyTrim`, since the `l.window != nil` block
  it now follows never executes. That test needs no change and must
  keep passing unmodified, pinning that a `Budget`-only caller sees no
  behavior change.

### Addendum verification

`make verify` passes. `go test -race ./agentloop/...` passes,
including the rewritten and the added compaction test. No `api/`
diff: `api/agentloop.txt` locks field signatures, not doc-comment
text, and this addendum changes no signature. No `policy/layers.json`
change: this addendum reorders two existing calls in `run.go` and
adds no import. The coverage floor stays at 85 percent for `agentloop`
and the module total.

## Addendum: steering and interruption

Status: shipped. This addendum
ports phase 78,
`docs/history/agents/phase78_steering_and_interruption.md`, into this
file. It adds one new file, `agentloop/steer.go`, and changes
`run.go` and `options.go`. It adds no new package and no
`policy/layers.json` row.

### Addendum goal

Let a caller stop the current iteration's in-flight `Completer.Chat`
call without a hard `ctx` cancellation. A hard `ctx` cancellation ends
`Run` at its hard-fail path: `Final` and `Stop` stay the zero value. A
steer request ends the run at the next iteration boundary instead,
with `Stop == StopSteered`, and it leaves every already-appended
history entry untouched, including a completed tool call's `RoleTool`
message.

### Addendum scope

Inside:

- One new type, `Steer`, a caller-held handle that requests a
  soft-cancel of the in-flight `Completer.Chat` call for one
  `RunSteerable` call.
- One new method, `RunSteerable`, alongside `Run`. `Run`'s signature
  and behavior do not change: `Run` becomes a one-line call to
  `RunSteerable` with a nil `steer`, so `Run(ctx, msgs)` stays
  identical to `RunSteerable(ctx, msgs, nil)`.
- One new `StopReason`, `StopSteered`.
- Threading a `steer *Steer` parameter through `run` (the unexported
  loop body) and `runChat` (the unexported per-iteration `Completer`
  call), so the soft-cancel scopes to one iteration's
  `l.completer.Chat` call.
- Extracting the new steer-versus-hard-fail classification into its
  own unexported helper, `isSteerStop`, and extracting enough of
  `run`'s existing body into a second unexported helper so `run`
  itself stays at or under the 80-line structure-gate ceiling. See
  Addendum mechanics.

Outside:

- Interrupting a running tool call. `runToolCalls` runs calls
  sequentially, strictly after `runChat` returns; no tool call is ever
  in flight when a steer request can bind a cancel func. A future
  concurrent-tool-call design needs its own review of what
  "interrupt a tool call" means.
- Any change to `provider.Completer`. `RunSteerable` relies only on
  the existing convention that a `Completer.Chat` implementation
  observes the `context.Context` it receives, the same reliance `Run`
  already has for `ctx.Err()` to surface mid-call on a hard
  cancellation.
- Any change to `Options` or `New`. `Steer` is a per-`RunSteerable`-call
  value, not a per-`Loop` value: a `Loop` already supports concurrent
  `Run` calls, and an `Options.Steer` field would let one caller's
  steer request stop another caller's unrelated call sharing the same
  `Loop`.
- A steer request during the prompt-too-long recovery retry.
  `recoverPromptTooLong` issues its own second `Completer.Chat` call
  on the plain outer `ctx`, not on a steer-derived context. A
  `Trigger` call fired while that retry is in flight has no effect
  until the next iteration boundary. This matches the already-adopted
  mid-tool-batch rule: a `Trigger` call has no effect on work already
  dispatched: it only binds at the start of the next
  `l.completer.Chat` call. `compaction.go` needs no change; this
  addendum only documents the limitation.
- A runtime guard against sharing one `Steer` value across two
  concurrent `RunSteerable` calls. The doc comment forbids it and a
  test documents the forbidden case; see Addendum API and Addendum
  tests. No mutex-based cross-call guard ships in this addendum.

### Addendum mechanics

`runChat(ctx, history, iterations)` gains a `steer *Steer` parameter.
When `steer` is nil, `runChat` is unchanged: it calls
`l.completer.Chat(ctx, req)` on the tracer-annotated `ctx` exactly as
today. When `steer` is non-nil, `runChat` derives a per-call context,
`chatCtx, cancel := context.WithCancel(ctx)`, arms `steer` with
`cancel`, calls `l.completer.Chat(chatCtx, req)`, then disarms
`steer` and calls `cancel()` before returning, so no derived context
leaks past its `Completer.Chat` call. `chatAttempt.iterCtx` keeps
carrying the tracer-annotated `ctx`, not `chatCtx`: a later tool call
in this turn must not run under a context a completed
`Completer.Chat` call already canceled. `recoverPromptTooLong`'s own
retry call keeps using the plain `ctx` it already receives; `runChat`
passes no `chatCtx` into it. That retry call is unreachable by a
steer request, matching the Addendum scope's documented limitation.

`Steer` tracks one boolean, `triggered`, and one `context.CancelFunc`,
`cancel`, under a `sync.Mutex`. `Trigger` sets `triggered` true and
calls `cancel` when non-nil; both effects are idempotent, so a second
`Trigger` call, or a `Trigger` call with no bound `cancel`, changes
nothing further. `runChat`'s arm step stores the derived `cancel` and
returns whether `triggered` was already true; when it was, `runChat`
calls the just-stored `cancel` immediately, before
`l.completer.Chat` runs, so a steer request fired during the previous
iteration's tool-call batch takes effect at the very start of the
next iteration's `Completer.Chat` call, not only during it. `runChat`'s
disarm step clears `cancel` back to nil; it never clears `triggered`.

`Steer` also exposes an unexported accessor, `wasTriggered() bool`,
that reads `triggered` under the same `sync.Mutex` `Trigger` uses.
`isSteerStop` is the only caller. This accessor is what lets
classification tell a genuine steer-triggered cancellation apart from
a `Completer` that independently returns or wraps `context.Canceled`
for its own reason while a `Steer` happens to be present but was
never triggered.

`RunSteerable` resets `steer` at the call's own start, before the loop
begins: `triggered` false, `cancel` nil. This reset is what makes a
`Trigger` call before `RunSteerable` starts, or one left over from a
prior `RunSteerable` call on a reused `Steer`, a no-op: the run begins
as if `Trigger` had never fired. `RunSteerable` does not reset `steer`
again at return; a `Trigger` call after `RunSteerable` has already
returned has nothing left to cancel, since no iteration is in flight
and no future call will observe today's `triggered` value without
first going through the same start-of-call reset.

`isSteerStop(err error, ctx context.Context, steer *Steer, fromRecovery bool) bool`
is the unexported helper `run` calls to classify a `runChat` error.
It returns true only when all four conditions hold together:
`steer` is non-nil; `steer.wasTriggered()` is true; `fromRecovery` is
false; and `errors.Is(err, context.Canceled)` holds while `ctx.Err()`
is still nil. The `wasTriggered` condition is what closes the finding
this addendum's plan review raised: without it, any `Completer` that
independently wraps `context.Canceled` while a non-triggered `Steer`
happens to be present would be misclassified as a steer stop instead
of a hard failure. The `ctx.Err() == nil` condition is what
distinguishes a steer-triggered cancellation of the derived `chatCtx`
from a caller's direct `ctx` cancellation, which also propagates into
`chatCtx` since `context.WithCancel`'s child observes its parent's
cancellation. A direct `ctx` cancellation always leaves `ctx.Err()`
non-nil, so it keeps falling through to the existing hard-fail path,
unchanged. On a classified steer stop, `run` returns
`Result{History: history, Iterations: iterations, Usage: totalUsage, Stop: StopSteered}, nil`
— the same partial-state shape every other graceful stop before a new
response arrives already uses, matching `StopHookVeto`.

`run`'s loop body is already at the 80-line structure-gate ceiling
before this addendum (`agentloop/run.go`, the `run` function). Calling
`isSteerStop` from `run` adds a branch `run` has no line budget left
for. The implementation must also extract a second unexported helper
from `run`'s existing body to make room, sized so both `run` and the
new helper stay at or under 80 lines each. The natural candidate is
the tool-call dispatch and veto-check block at the end of the loop
(the `runToolCalls` call, its `veto` check, and the `StopHookVeto`
return), moved into an unexported helper such as
`runToolPhase(ctx, history, resp, iterations, totalUsage) (Result, bool, error)`,
where the returned `bool` reports whether the loop should return the
`Result` as-is or continue. The builder chooses the exact extraction
boundary; the requirement is `run` at or under 80 lines after adding
the `isSteerStop` branch, verified by
`python3 scripts/check_structure.py`.

### Addendum API

```go
// Steer is a caller-held handle that requests a soft-cancel of one
// RunSteerable call's in-flight Completer.Chat call. Trigger is safe
// to call from another goroutine, any number of times, before,
// during, or after the RunSteerable call it is passed to. A Steer
// triggered before RunSteerable starts, or after it already returned,
// is a no-op: RunSteerable resets Steer's internal state at the start
// of its own call. One Steer value must not be passed to two
// concurrent RunSteerable calls: both calls would arm and disarm the
// same triggered flag and cancel func, and one caller's Trigger could
// stop the other caller's unrelated run.
type Steer struct {
    // unexported: a sync.Mutex, a triggered bool, and a bound
    // context.CancelFunc.
}

// NewSteer returns a ready Steer, unbound to any RunSteerable call
// until passed to one.
func NewSteer() *Steer

// Trigger requests the soft-cancel this Steer is bound to for its
// current RunSteerable call, if any. Trigger fired mid-tool-call-batch
// has no effect on the calls already dispatched in that batch; it
// takes effect at the start of the next iteration's Completer.Chat
// call instead. A Trigger call fired during a prompt-too-long recovery
// retry has no effect until the following iteration boundary, for the
// same reason. Calling Trigger more than once, or with no
// RunSteerable call in progress, has no additional effect.
func (s *Steer) Trigger()

// RunSteerable is Run with one addition: a non-nil steer lets the
// caller request a soft-cancel of the current iteration's in-flight
// Completer.Chat call from another goroutine, through steer.Trigger.
// ctx cancellation still ends the run as a hard failure, unchanged
// from Run. A triggered steer ends the run gracefully instead, at the
// next iteration boundary, with Stop == StopSteered and Final holding
// the zero value: the run stops before a new response arrives, the
// same rule every other pre-response graceful stop already follows
// (see Result-shape rule above). This is a deliberate correction of
// the origin brief, docs/history/agents/phase78_steering_and_interruption.md,
// which stated Final would carry the last message appended before the
// steer fired; that statement conflicted with the base Result-shape
// rule and this addendum overrides it. History, Iterations, and Usage
// carry every already-completed iteration's state. Run(ctx, msgs) is
// equivalent to RunSteerable(ctx, msgs, nil).
func (l *Loop) RunSteerable(ctx context.Context, msgs []provider.Message, steer *Steer) (Result, error)
```

New `StopReason` constant, in `options.go`'s existing `StopReason`
block:

```go
// StopSteered is Run's stop reason when a Steer.Trigger call requests
// a soft-cancel of the in-flight Completer.Chat call. Graceful: nil
// error, the same Result-shape rule as every other graceful stop that
// happens before a new response arrives.
const StopSteered StopReason = "steered"
```

Changed in `agentloop`:

- `Run`'s body becomes `return l.RunSteerable(ctx, msgs, nil)`. Its
  doc comment gains one sentence pointing to `RunSteerable` for a
  graceful, in-flight stop. Its exported signature does not change.
- `run` (unexported) gains a `steer *Steer` parameter, threaded
  through from `RunSteerable`. `Run` passes `nil`. `run` calls the new
  `isSteerStop` helper and the extracted tool-phase helper named in
  Addendum mechanics, to stay at or under 80 lines.
- `runChat` (unexported) gains a `steer *Steer` parameter, per the
  Addendum mechanics section above.
- `Result.Final`'s doc comment in `agentloop/loop.go` gains
  `StopSteered` to its list of stop reasons that leave `Final` at the
  zero value, alongside `StopHookVeto` and `StopMaxIterations`.

`Steer`, `NewSteer`, `(*Steer).Trigger`, `RunSteerable`, and
`StopSteered` land in `api/agentloop.txt` via `make api-update`, in
the same change as the code. The `run`, `runChat`, `isSteerStop`, and
the extracted tool-phase helper are unexported and touch no lock file.

### Addendum tests

`Steer` lives in a new sibling file, `agentloop/steer.go`, because it
is a caller-facing type with its own invariants, not loop-body logic;
keeping it out of `run.go` keeps that file's growth in check under the
500-line structure gate. `RunSteerable` lives in `run.go`, next to
`Run`, since the two share one loop body and one doc-comment
cross-reference. `isSteerStop` also lives in `run.go`, next to `run`,
since it is loop-body classification logic with no caller-facing
role.

Tests live in a new file, `agentloop/agentloop_test/steer_test.go`,
because they exercise a new type and a new entry point, not an
existing suite's concern. A test-review and logic-review pass after
the initial build split the suite across three files, once
`steer_test.go` and its fixtures pushed past the 500-line structure
gate: `steer_test.go` keeps the core cases below; the shared scripted
`Completer`s, tools, and the `newSteerLoop` helper moved to
`agentloop/agentloop_test/steer_fixtures_test.go`; the two
prompt-too-long recovery-boundary cases moved to
`agentloop/agentloop_test/steer_recovery_test.go`. That pass also
added two cases beyond the list below:
`TestSteerTriggeredButErrNotCanceled`, in `steer_test.go`, proves
`isSteerStop` still hard-fails a triggered `Steer` whose `Completer`
error does not wrap `context.Canceled`; and
`TestSteerTriggeredDuringFailingRecoveryRetry`, in
`steer_recovery_test.go`, proves the `fromRecovery` guard stops a
triggered `Steer` from masking a failing prompt-too-long recovery
retry as `StopSteered`.

- `TestSteerTriggerMidCompleter`: a scripted `Completer` whose `Chat`
  blocks on its own `ctx.Done()` before returning. A second goroutine
  calls `steer.Trigger()` shortly after `RunSteerable` starts.
  `RunSteerable` returns with `Stop == StopSteered`, a nil error, and
  a zero-value `Final`.
- `TestSteerTriggerAfterPriorIteration`: the same setup, but the
  scripted `Completer` returns a normal tool-call response on its
  first call and blocks on its second. `Trigger` fires mid-second-call.
  `Result.History`, `Iterations`, and `Usage` carry the first
  iteration's state; `Final` stays the zero value.
- `TestSteerNeverTriggered`: a `Steer` passed to `RunSteerable` but
  never triggered runs to its normal `StopNoToolCalls` stop, identical
  to a plain `Run` call over the same scripted `Completer`.
- `TestSteerCtxCanceledDirectly`: `ctx` canceled directly, with a
  `Steer` present but never triggered, still hard-fails with the zero
  `Final` and `Stop`, exactly like `Run`, proving `RunSteerable` does
  not change hard-cancellation behavior.
- `TestSteerNotTriggeredSpontaneousCancel`: a scripted `Completer`
  whose `Chat` returns an error wrapping `context.Canceled` on its
  own, for a reason unrelated to steering. `steer` is present, bound
  to the call, but `Trigger` is never called, and the outer `ctx` is
  never canceled. `RunSteerable` returns a non-nil error and
  `Stop != StopSteered`, proving `isSteerStop` does not misclassify a
  `Completer`'s own `context.Canceled` error as a steer stop just
  because a `Steer` was passed in.
- `TestSteerTriggerTwiceAndBeforeStart`: `Trigger` called twice in a
  row does not panic and has the same single effect as one call.
  `Trigger` called on a fresh `Steer`, before it is ever passed to
  `RunSteerable`, then passed to a `RunSteerable` call that never
  triggers it again, runs to its normal stop, proving the pre-start
  trigger was a no-op.
- `TestSteerTriggerConcurrent`: `N` goroutines call `Trigger`
  concurrently on one `Steer` bound to one in-flight `RunSteerable`
  call, under `go test -race`. No panic, no race, and `RunSteerable`
  still returns `Stop == StopSteered` exactly once.
- `TestSteerTriggerMidToolBatch`: a scripted multi-call tool batch
  (three tool calls in one turn), with `steer.Trigger()` called while
  the second call is executing. The third call still runs and its
  `RoleTool` message still reaches `History`. The following iteration's
  `Completer.Chat` call is steered immediately: `RunSteerable` returns
  `Stop == StopSteered` without waiting on that call to block, proving
  the "triggered before bind" carry-over inside one `RunSteerable`
  call works.
- `TestSteerTriggerMidPromptTooLongRecovery`: a scripted `Completer`
  that rejects the first call with `provider.ErrPromptTooLong` and
  blocks on the recovery retry's call. `steer.Trigger()` fires while
  the retry is blocked. The retry still completes and its response
  still reaches `History`; the following iteration is where the steer
  takes effect, proving `Trigger` fired during a prompt-too-long retry
  has no effect until the next iteration boundary, per the documented
  limitation.
- `TestSteerSharedAcrossConcurrentRuns` (documentation test, not a
  guard test): one `Steer` passed to two concurrent `RunSteerable`
  calls on the same `*Loop`, over scripted `Completer`s that both
  block. Triggering the shared `Steer` stops one or both runs, and the
  outcome is deliberately unspecified beyond "no panic, no race under
  `go test -race`". The test's comment records that sharing one
  `Steer` across concurrent `RunSteerable` calls is forbidden by the
  `Steer` doc comment, and this test exists to pin the forbidden
  behavior, not to certify it as supported.

### Addendum verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt` and the structure gate against `run.go`'s new
helpers. `go test -race ./agentloop/...` passes, including the
concurrent-`Trigger` case and the shared-`Steer` documentation case.
No `policy/layers.json` change: this addendum needs only `context`,
already imported elsewhere in `agentloop`, and `sync`, which is new to
`agentloop` but is a standard-library import, so it needs no
`policy/layers.json` row; that file governs imports between packages
of this module, not standard-library imports. The coverage floor
stays at 85 percent for `agentloop` and the module total. `make
api-update` runs, and the `api/agentloop.txt` diff lands in the same
change as the code.

## Addendum: planHistory failure routes through hardFail

Status: shipped. This addendum fixes a Result-shape defect in
`run.go`. It changes `run.go` and two test files. It adds no new
file, no new package, and no `policy/layers.json` row.

### Addendum goal

Make the `planHistory` failure return in `run` follow the
Result-shape rule the base plan and the window-compaction addendum
both already state: at `iterations == 0`, a hard-fail return carries
the zero-value `Result`, with no special case.

### Addendum bug

`run`'s `l.window != nil` block, at `run.go:76-82`, calls
`l.planHistory` and, on error, returns:

```go
return Result{History: history, Iterations: iterations, Usage: totalUsage}, err
```

Every other hard-fail return in `run` and `runToolStage` calls
`l.hardFail(history, iterations, totalUsage)` instead of building this
literal by hand. `hardFail`, at `run.go:252-257`, degrades to
`Result{}` when `iterations == 0`, matching its own doc comment: "when
none has [completed], no partial state exists yet, and the rule
degrades to the zero-value `Result` on its own, with no special case."
The plan's base Result-shape paragraph states the same rule at
`docs/history/agentloop.md:145-149`, and the window-compaction addendum
folds `ErrPlanFailed` and `ErrCompactionFailed` failures into the same
closed hard-fail list at `docs/history/agentloop.md:1512-1516`, "per the
existing Result-shape rule."

The bespoke literal at `run.go:79` does not call `hardFail`, so at
`iterations == 0` it returns `Result{History: history}` — `History`
set to the pre-compaction input messages, a non-nil slice, not the
documented zero value. `Iterations` and `Usage` already read zero at
`iterations == 0` regardless, since neither field has accumulated
anything yet; only `History` diverges from `hardFail`'s output. At
`iterations >= 1`, the literal and `l.hardFail(history, iterations,
totalUsage)` build the identical `Result`, so the bug is confined to
the `iterations == 0` case.

### Addendum scope

Inside:

- Replacing the inline `Result{...}` literal at `run.go`'s
  `planHistory` failure return with `l.hardFail(history, iterations,
  totalUsage)`, keeping the same `err` as the second return value.
- Correcting `TestRunSummarizerFailureFailsBeforeRequest` in
  `agentloop/agentloop_test/compaction_test.go` to assert the
  zero-value `Result`, replacing its current wrong assertion.
- Strengthening `TestRunPlanHistoryEstimatorErrorFailsWithErrPlanFailed`
  in `agentloop/agentloop_test/compaction_estimator_test.go` to capture
  and assert the returned `Result`, instead of discarding it.
- Adding `TestRunPlanHistoryFailureLaterIterationPreservesPartialResult`
  to `agentloop/agentloop_test/compaction_reentry_test.go`, proving the
  other side of the boundary: a `planHistory` failure after at least
  one completed iteration still carries the partial `History`,
  `Iterations`, and `Usage`, unaffected by this fix.

Outside:

- Any change to `hardFail` itself. Its behavior is already correct and
  already documented; this addendum only routes one more call site
  through it.
- Any change to `ErrPlanFailed`, `ErrCompactionFailed`, or any other
  sentinel, wrapping, or error message.
- Any change to the runToolStage, runChat, or recovery hard-fail
  paths. Every other hard-fail return in `run.go` already calls
  `hardFail`; only the `planHistory` site was bespoke.
- `TestRunRetentionOverflowFailsBeforeRequest`
  (`compaction_test.go:451-468`) discards its `Result` with `_, err :=
  loop.Run(...)` and is unaffected either way. This addendum does not
  touch it; strengthening it is not required to close this gap, since
  the two tests this addendum does change already cover both the
  `ErrPlanFailed` and the `ErrCompactionFailed` hard-fail causes at
  `iterations == 0`.

### Addendum API

No exported symbol changes. `hardFail` is unexported and already
exists; `Result`'s fields and `Run`'s signature do not change. No
`api/agentloop.txt` diff and no `make api-update` run.

### Addendum mechanics, exact

In `run.go`, inside the `if l.window != nil { ... }` block, change:

```go
planned, err := l.planHistory(ctx, history, iterations)
if err != nil {
	return Result{History: history, Iterations: iterations, Usage: totalUsage}, err
}
```

to:

```go
planned, err := l.planHistory(ctx, history, iterations)
if err != nil {
	return l.hardFail(history, iterations, totalUsage), err
}
```

One line changes. No other line in `run.go` changes.

### Addendum tests

In `agentloop/agentloop_test/compaction_test.go`:

- `TestRunSummarizerFailureFailsBeforeRequest` (currently at
  `compaction_test.go:324-346`) keeps its `msgs`, `w`, fixture setup,
  and its `ErrCompactionFailed`/`contextplan.ErrCallFailed`/
  `completer.callCount() == 0` assertions unchanged. Replace only its
  final assertion:

  ```go
  if len(res.History) != len(msgs) {
  	t.Fatalf("Result.History = %d messages, want the pre-compaction %d", len(res.History), len(msgs))
  }
  ```

  with:

  ```go
  if !isZeroResult(res) {
  	t.Fatalf("Result = %+v, want the zero Result: no iteration completed before the summarizer failed", res)
  }
  ```

  matching the pattern already proven at
  `compaction_test.go:446-448`'s `TestRunBudgetTripsAfterCompactionStillOverBudget`.

In `agentloop/agentloop_test/compaction_estimator_test.go`:

- `TestRunPlanHistoryEstimatorErrorFailsWithErrPlanFailed` (currently
  at `compaction_estimator_test.go:64-95`) keeps its fixture unchanged.
  Change its `Run` call from:

  ```go
  _, err = loop.Run(context.Background(), msgs)
  ```

  to:

  ```go
  res, err := loop.Run(context.Background(), msgs)
  ```

  and add, alongside the existing `ErrPlanFailed`/`estErr`/
  `callCount() == 0` assertions:

  ```go
  if !isZeroResult(res) {
  	t.Fatalf("Result = %+v, want the zero Result: no iteration completed before the estimate failed", res)
  }
  ```

In `agentloop/agentloop_test/compaction_reentry_test.go`, add
`TestRunPlanHistoryFailureLaterIterationPreservesPartialResult`. This
test proves a `planHistory` failure on the second iteration, after the
first iteration's compaction and `Completer.Chat` call already
completed, carries the first iteration's partial state instead of the
zero value. Fixture, built on
`TestRunBudgetWindowSecondCompactionAcrossIterations`'s proven
two-compaction shape (`compaction_reentry_test.go:173-225`), with the
second compaction's summarizer call failing instead of succeeding:

- `msgs`: the same four messages as
  `TestRunBudgetWindowSecondCompactionAcrossIterations`: `{RoleSystem,
  "s"}`, `{RoleUser, strings.Repeat("o", 80)}`, `{RoleAssistant, "a"}`,
  `{RoleUser, "l"}`.
- `w := contextplan.Window{MaxTokens: 500, Compaction:
  contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}`, the
  same window: `TriggerPercent: 1` trips compaction on every
  iteration, proven to trigger twice across two iterations in the
  sibling test.
- A tool registry with one `schemaEchoTool{name: "search", schema:
  []byte(`{"type":"object"}`), result: strings.Repeat("z", 80)}`, the
  same tool that grows the history enough to trip the second
  iteration's compaction in the sibling test.
- A new unexported type, `summaryFailsOnSecondCall`, in this file,
  modeled on `cancelDuringChat`'s shape (`compaction_reentry_test.go:151-162`):
  a `contextsummary`-compatible `Completer` (`Name`, `Chat`,
  `ChatStream`) with a mutex-guarded call counter. Its first `Chat`
  call returns `provider.Response{Message: provider.Message{Role:
  provider.RoleAssistant, Content: summaryReplyJSON}}` and a nil
  error, the same success shape as `summaryScript`. Its second and
  every later `Chat` call returns `provider.Response{}` and a fixed
  sentinel error, `errors.New("summary boom second")`. `ChatStream`
  returns an error, unsupported, the same as `summaryScript`.
- `completer := &scriptedCompleter{responses: []provider.Response{
  toolCallResponse(provider.ToolCall{ID: "c1", Name: "search",
  Arguments: []byte("{}")}) with Usage: provider.Usage{TotalTokens:
  30} set on that response's `Message`-carrying struct}}`. Exactly one
  scripted response: the run must hard-fail on iteration two, before
  a second `Completer.Chat` call, so a second scripted response is
  never consumed.
- `agentloop.Options{Completer: completer, Tools: reg, MaxIterations:
  4, Window: &w, Summarizer: <NewSummarizer over
  summaryFailsOnSecondCall>, Calibrated: contextplan.Calibrate(scaleEstimator{div:
  1}, 1.0)}`. No `Budget`: irrelevant to this test's boundary.
- Call `loop.Run(context.Background(), msgs)`.
- Assert `errors.Is(err, agentloop.ErrCompactionFailed)`.
- Assert `errors.Is(err, contextplan.ErrCallFailed)`.
- Assert `completer.callCount() == 1`: the first iteration's
  `Completer.Chat` call ran; the second iteration hard-fails inside
  `planHistory`, before any second `Completer.Chat` call.
- Assert `res.Iterations == 1`: exactly one iteration completed before
  the failure.
- Assert `res.Usage == provider.Usage{TotalTokens: 30}`: the first
  iteration's usage carries forward, unchanged by the failed second
  iteration.
- Assert `res.Final == provider.Message{}` and `res.Stop ==
  agentloop.StopReason("")`: the run did not reach a stop condition;
  it failed one, per the Result-shape rule's hard-fail case.
- Assert `len(res.History) > 0` and `summaryNamed(res.History) == 1`:
  the first iteration's compaction result, including its injected
  summary message, survives into the failure `Result`, proving
  `History` carries the accumulated partial state rather than the
  zero value `hardFail` would return at `iterations == 0`.

### Addendum verification

`make verify` passes. `go test -race ./agentloop/...` passes,
including the corrected `TestRunSummarizerFailureFailsBeforeRequest`,
the strengthened
`TestRunPlanHistoryEstimatorErrorFailsWithErrPlanFailed`, and the new
`TestRunPlanHistoryFailureLaterIterationPreservesPartialResult`. No
`api/agentloop.txt` diff: no exported symbol changes. No
`policy/layers.json` change: the fix reuses an existing unexported
helper and adds no import. The coverage floor stays at 85 percent for
`agentloop` and the module total.

## Addendum: pair Observe's estimate to its own request

Status: shipped. Companion to
`docs/history/contextplan.md`'s "Correctness fix: Calibrated.Observe
drops the shared-lastEst pairing". That fix changes
`contextplan.Calibrated.Observe`'s signature from `Observe(actual
int)` to `Observe(estimated, actual int)`, a breaking change with
`agentloop/run.go` as the only internal caller. Both plans land in
one commit; `agentloop` cannot compile against the new signature
until this addendum's code lands too.

### Addendum goal

Give `Observe` the exact estimate that describes the request whose
response it now scores, so `agentloop` stops relying on `Calibrated`'s
own internal state to pair the two. Today's single call site,
`l.calibrated.Observe(resp.Usage.TotalTokens)` inside `afterChat`,
implicitly pairs the response against whatever `Calibrated.lastEst`
holds at that moment. `Calibrated.lastEst` is unaffected by which
goroutine or which iteration produced it once concurrent `Run` calls
share one `*Loop` (`TestRunConcurrentSharedLoopWithPlanning`,
`agentloop/agentloop_test/compaction_recovery_test.go`), so the
pairing can silently cross goroutines. This addendum makes each
iteration carry its own estimate end to end.

### Addendum scope

Inside:

- `chatAttempt` (`agentloop/run.go`) gains one field,
  `estimatedTokens int`.
- `runChat` computes the primary request's estimate before the first
  `Completer.Chat` call: when `l.calibrated != nil`, it calls
  `l.calibrated.EstimateTokens(req)` right after building `req`, and
  stores the result on the returned `chatAttempt.estimatedTokens`. An
  `EstimateTokens` error here is non-fatal: `Chat` still runs,
  `estimatedTokens` stays at its zero value, and the later `Observe`
  call sees a non-positive `estimated` and no-ops, matching today's
  silent-degrade behavior for an estimator failure outside planning.
- `runChat` computes the recovery request's own estimate too, when
  `recoverPromptTooLong` returns successfully. After `retryReq` comes
  back, and when `l.calibrated != nil`, `runChat` calls
  `l.calibrated.EstimateTokens(retryReq)` and stores the result on
  the recovered `chatAttempt.estimatedTokens`, the same non-fatal
  rule as the primary path. This corrects a gap in
  `docs/history/contextplan.md`'s companion-change note, which names
  only "the point `req` is built" without separating `runChat`'s two
  branches. `recoverPromptTooLong` (`agentloop/compaction.go`) calls
  `Completer.Chat` on its own `retryReq` before `runChat` regains
  control, so `EstimateTokens(retryReq)` runs after that `Chat` call
  returns, not before it. `EstimateTokens` depends only on `req`, not
  on the response, so computing it after `Chat` returns yields the
  same value computing it before would; the ordering has no effect on
  correctness. Without this second call, every recovered iteration's
  `Observe` call would pair against the zero value, a permanent
  no-op, and the correction factor would never learn from a recovered
  turn's real usage.
- `afterChat`'s Observe call site changes from
  `l.calibrated.Observe(resp.Usage.TotalTokens)` to
  `l.calibrated.Observe(at.estimatedTokens, resp.Usage.TotalTokens)`.
- A `Calibrated` set without a `Window` now also gets one
  `EstimateTokens` call per iteration, inside `runChat`, a call this
  configuration never made before this addendum. `Options.Validate`'s
  `ErrEstimatorRequired` rule only requires `Calibrated` when `Window`
  is set; it does not forbid `Calibrated` alone. Gating the new call
  on `l.calibrated != nil`, with no `l.window` check, is correct and
  covers this configuration too. The added cost is one estimator call
  per iteration, far cheaper than the one `Completer.Chat` call it
  measures.

Outside:

- `agentloop/compaction.go`'s `checkCompactedBudget` and
  `planHistory`'s own `EstimateTokens` calls. Those are planning-time
  estimates that decide whether to compact; they never feed `Observe`,
  so this addendum does not touch them.
- `Options.Validate`. `ErrEstimatorRequired` already requires
  `Calibrated` whenever `Window` is set; no new rule is needed for
  this addendum's `l.calibrated != nil` gate to be safe.
- `policy/layers.json`. `agentloop` already imports `contextplan`; no
  edge changes.

### Addendum API

No exported symbol changes: `chatAttempt` is unexported, and
`Result`, `Loop`, and `Options` keep their locked shape. `make
api-update` must produce no `agentloop` diff.

### Addendum mechanics, exact

In `run.go`, `chatAttempt` gains one field:

```go
type chatAttempt struct {
	resp            provider.Response
	req             provider.Request
	history         []provider.Message
	iterCtx         context.Context
	err             error
	fromRecovery    bool
	estimatedTokens int
}
```

`runChat` computes the primary request's estimate immediately after
building `req`, strictly before `steerableChat` sends it, so
`estimatedTokens` always reflects the factor that was live at send
time, never a later, possibly drifted one:

```go
req := provider.Request{Model: l.model, Messages: history, Tools: l.defs}
estimated := l.estimateTokens(req)
resp, err := l.steerableChat(ctx, req, steer)
if span != nil {
	span.End()
}
if err == nil {
	return chatAttempt{resp: resp, req: req, history: history, iterCtx: ctx,
		estimatedTokens: estimated}
}
if l.window == nil || !errors.Is(err, provider.ErrPromptTooLong) {
	return chatAttempt{err: err, iterCtx: ctx}
}
recovered, rebuilt, retryReq, rerr := l.recoverPromptTooLong(ctx, err, history, iterations)
if rerr != nil {
	return chatAttempt{err: rerr, fromRecovery: true, iterCtx: ctx}
}
return chatAttempt{resp: recovered, req: retryReq, history: rebuilt, iterCtx: ctx,
	estimatedTokens: l.estimateTokens(retryReq)}
```

The recovery branch keeps its estimate call after `recoverPromptTooLong`
returns, since `retryReq` does not exist before that call: the function
builds `retryReq` and calls `Completer.Chat` on it internally, in one
step neither `runChat` nor this addendum touches. This means the
recovery branch reads a possibly later `factor` snapshot than the one
live when `retryReq` was actually sent, unlike the primary path above.
This gap does not reintroduce the pairing bug this addendum fixes: a
mispairing means `Observe` scores one iteration's actual usage against
a different iteration's estimate. Here, `EstimateTokens(retryReq)`
always measures `retryReq`'s own messages, tools, and model, the exact
request `Chat` received, whichever `factor` snapshot it happens to
read. A stale `factor` changes how tightly `estimated` tracks the
model's real token count for that call, a precision cost, not a
pairing error: `estimated` and `actual` still describe the same
request. `Calibrated`'s own contract already tolerates this: any
concurrent `Observe` call reads whatever `factor` is live at that
instant, by design, per the Fix scope bullet on shared-`factor`
correctness in `docs/history/contextplan.md`.

`estimateTokens` is a new unexported helper on `*Loop`, factoring the
shared non-fatal-error rule out of both call sites:

```go
// estimateTokens returns l.calibrated.EstimateTokens(req), or zero
// when l.calibrated is nil or the estimate call fails. Zero is a
// safe default: Calibrated.Observe no-ops on a non-positive estimated
// value, so an estimator failure here degrades silently, the same
// rule EstimateTokens failures already followed outside planning.
func (l *Loop) estimateTokens(req provider.Request) int {
	if l.calibrated == nil {
		return 0
	}
	est, err := l.calibrated.EstimateTokens(req)
	if err != nil {
		return 0
	}
	return est
}
```

In `afterChat`, the Observe call site changes:

```go
if l.calibrated != nil {
	l.calibrated.Observe(at.estimatedTokens, resp.Usage.TotalTokens)
}
```

The `l.calibrated != nil` guard stays: `Observe` on a nil
`*Calibrated` would panic, and `at.estimatedTokens` is meaningless
when no `Calibrated` produced it.

### Addendum tests

In `agentloop/agentloop_test/`:

- A deterministic-estimate `Completer` and estimator pair, where
  `runChat` triggers `recoverPromptTooLong` for one iteration and
  succeeds normally for the rest. Assert the `Observe` call for the
  recovered iteration pairs with the recovery path's own estimate,
  not the pre-recovery estimate, by asserting the resulting `factor`
  matches `contextplan`'s reference formula (see
  `docs/history/contextplan.md`'s Fix tests section) applied to the
  known `(estimated, actual)` pairs. This fails against today's code,
  which has no `estimatedTokens` field and cannot compile against the
  new `Observe` signature; once `agentloop` is updated to compile,
  it fails against a version that only estimates the primary `req`
  and leaves the recovered branch's `estimatedTokens` at zero.
- `TestRunConcurrentSharedLoopWithPlanning`, existing in
  `compaction_recovery_test.go`, needs a fixture change before its
  assertion can distinguish correct pairing from the bug it exists to
  catch. Today all 4 goroutines share one `msgs` slice, one
  deterministic `scaleEstimator`, and one `final` response reused for
  every call with `Usage` at its zero value, so `actual` is `0` for
  every call: already a permanent no-op under both the old and the new
  `Observe` rule. Correct pairing and cross-goroutine-contaminated
  pairing are numerically indistinguishable under that fixture; an
  assertion against it would pass equally against fixed and still-buggy
  code.

  A free-running design, where every goroutine estimates, sends, and
  observes on its own schedule with no test-controlled ordering, cannot
  fix this. With `alpha == 1.0`, `Observe` computes `next := factor_live
  * sample`, where `factor_live` is read fresh at `Observe`-call time
  and `sample` embeds `factor_capture`, the value read earlier at
  `EstimateTokens`-call time. Both are only guaranteed inside `[0.5,
  2.0]` independently; their ratio can reach `4.0` or `0.25`, outside
  `[0.5, 2.0]`, from legitimate concurrent drift between two *correctly
  paired* calls, with no mispairing at all. A clamp-to-boundary result
  is then not a reliable mispairing signal, and no fixed byte-length
  spacing can rule this out: the number of `Observe` calls that can
  interleave between one goroutine's own capture and its own observe is
  scheduler-dependent, not bounded at test-authoring time. This plan
  replaces the free-running design with a checkpointed one instead,
  matching the fallback this addendum's round of review offered: a
  smaller test with a fully known interleaving beats a larger one whose
  soundness depends on an unprovable bound.

  The checkpointed fixture, replacing today's `newRecoveryFixture`
  wiring for this one test:
  - A `Loop` built with `Calibrated: cal` (`cal := contextplan.Calibrate
    (scaleEstimator{div: 1}, 1.0)`, held by the test) and `Window: nil`.
    A nil `Window` skips `planHistory` and `Compact` entirely
    (`agentloop/run.go:106`), so no `Budget()` or retention-overflow risk
    exists for any message size this test picks; the earlier, rejected
    design's fixture-cannot-run problem does not recur.
  - Four goroutines, each running `loop.Run(ctx, msgs[g])` once
    (`MaxIterations: 1`), each `msgs[g]` a single `RoleUser` message
    whose content is `strings.Repeat` of a distinct rune, sized so
    `scaleEstimator{div: 1}` reports a distinct raw estimate per
    goroutine: 100, 200, 300, and 400 bytes.
  - A new completer test double, `checkpointCompleter`, replaces
    `scriptedCompleter` for this one test. It holds one gate per
    goroutine, keyed by that goroutine's distinct message content. Its
    `Chat` looks up the calling goroutine's gate by matching
    `req.Messages[0].Content`, signals a `reached` channel the test
    reads, then blocks on a `release` channel the test closes, then
    returns that gate's scripted `provider.Response`.
  - The test first waits for all four gates' `reached` signal before
    closing any `release` channel. `runChat` (per this addendum's own
    mechanics section) always calls `l.estimateTokens(req)` before
    `steerableChat`, so a goroutine cannot reach its `Chat` gate before
    its own `EstimateTokens` call has already returned. Since no
    `release` channel has closed yet, no `Chat` call has returned, so
    no `afterChat` and no `Observe` call has run for any goroutine.
    `cal`'s `factor` is therefore still exactly `1.0` when every one of
    the four captures happens, proven by this barrier, not assumed.
  - The test then closes `release[1]`, waits for goroutine 1's `Run` to
    return, closes `release[2]`, waits for goroutine 2's `Run`, and so
    on through goroutine 4, one at a time. Each `Run` return proves that
    goroutine's own `Observe` call has already completed, since `Observe`
    runs synchronously inside `afterChat`, before `Run` can return, on
    the single call path a response with no tool calls follows. This
    fixes the exact order of the four `Observe` calls: 1, then 2, then
    3, then 4, fully known, not merely bounded.
  - The scripted `Usage.TotalTokens` for goroutines 1 through 4 are 110,
    180, 360, and 320. With every `estimated` equal to that goroutine's
    own raw value (`factor == 1.0` at every capture, from the barrier
    above), the reference formula (`sample := actual/estimated; next :=
    factor * ((1-alpha) + alpha*sample)`, `alpha == 1.0`) folds, in the
    fixed release order, to:
    - `factor0 = 1.0`
    - `factor1 = factor0 * (110/100) = 1.10`
    - `factor2 = factor1 * (180/200) = 1.10 * 0.90 = 0.99`
    - `factor3 = factor2 * (360/300) = 0.99 * 1.20 = 1.188`
    - `factor4 = factor3 * (320/400) = 1.188 * 0.80 = 0.9504`
    No clamp fires; `0.9504` is the exact, fully derived expected final
    factor.
  - Contrast: under the reverted, single-`lastEst` pairing, the same
    four captures still all run before any `Observe`, per the same
    barrier, so `lastEst` ends at goroutine 4's raw value, `400`, once
    all four captures complete. Every `Observe(actual)` call, in the
    same fixed release order, then divides by `400` instead of its own
    goroutine's raw value:
    - `Observe(110)`: `sample = 110/400 = 0.275`, clamped to `0.5`.
    - `Observe(180)`: `sample = 180/400 = 0.45`, `next = 0.5*0.45 =
      0.225`, clamped to `0.5`.
    - `Observe(360)`: `sample = 360/400 = 0.9`, `next = 0.5*0.9 = 0.45`,
      clamped to `0.5`.
    - `Observe(320)`: `sample = 320/400 = 0.8`, `next = 0.5*0.8 = 0.4`,
      clamped to `0.5`.
    The reverted pairing pins the factor at exactly `0.5`, sharply
    diverging from the correctly paired `0.9504`.
  - The test asserts the exact predicted value, not a range: after
    goroutine 4's `Run` returns, call `cal.EstimateTokens` on a request
    with exactly 10,000 bytes of content and assert the result is `9504`,
    within a tolerance of `2` for accumulated float rounding.
    `int(float64(raw) * factor)` truncates
    (`contextplan/calibrated.go`), so the tolerance only needs to cover
    floating-point drift across four multiplications, not the formula
    itself.
  - `go test -race` covers this test like every other: the four `Run`
    calls are genuine concurrent goroutines sharing one `*Calibrated`,
    and the checkpoint channels are the only synchronization between
    them, so a data race in the shared `factor` field would still
    surface under the race detector even though the *logical* order of
    `Observe` calls is fixed by the test.
- A `Calibrated`-without-`Window` configuration: a `Loop` built with
  `Calibrated` set and `Window` nil. Assert `l.calibrated`'s factor
  changes after one `Run` call, proving `runChat` now calls
  `EstimateTokens` for this configuration. This fails against today's
  code, which never calls `EstimateTokens` when `l.window` is nil, so
  the factor never moves off its `1.0` starting value.
- An estimator that fails on `EstimateTokens`, with `Calibrated` set:
  `Run` still completes normally, the response reaches history
  unchanged, and the `Completer.Chat` call still ran. This proves the
  non-fatal-error rule: an estimate failure never fails the run. This
  fails against a version that returns the `EstimateTokens` error
  instead of degrading to zero.

### Addendum verification

- Both plans land in one commit: this addendum's `agentloop` code and
  `docs/history/contextplan.md`'s `Calibrated.Observe` signature change
  land together. `agentloop` does not compile against the new
  `Observe` signature until `chatAttempt.estimatedTokens` and the
  updated call site land, so a split commit leaves the tree
  non-building.
- `make verify` passes; `agentloop` and `contextplan` both hold the 85
  coverage floor.
- `go test -race ./agentloop/... ./contextplan/...` passes.
- `make api-update` produces no `api/agentloop.txt` diff: confirm this
  explicitly, since `chatAttempt` and its new field stay unexported.
  `api/contextplan.txt` changes, per `docs/history/contextplan.md`'s
  Fix verification.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass. No `policy/layers.json` change:
  `agentloop` already imports `contextplan`.
- `docs/packages/agentloop.md`'s `Calibrated.Observe(resp.Usage.
  TotalTokens)` reference updates to the two-argument call, in the
  same commit as the code.

## Addendum: pull-based steer injector (commit d914611)
Status: shipped.


This addendum is the fix spec for the six review findings of commit
d914611. The original commit is "feat(agentloop): add pull-based
steer injector" (originally 8f86264). The code in commit d914611
stays. The four follow-ups land together. Tests are
added without removing any existing case. Cross-references: the
architecture description in `docs/architecture.md` and the package
reference in `docs/packages/agentloop.md`. The commit message
cross-references this addendum so reviewers can find it.

The four follow-ups are:

- `HasActiveCall` export — `Steer.HasActiveCall` lands in
  `api/agentloop.txt:26`. A bridge that polls continuously across
  iterations guards each `Trigger` on this method, so a `Trigger`
  fired when no `Completer.Chat` is in flight does not poison the
  next arm.
- Downgrade-point no-drain — the builder removes `drainInjected` from
  the `hasInjector()` soft-continue branch in `runIteration` so the
  downgrade path returns immediately after `ackTriggered()`. The
  iteration-top boundary already drains on the next loop. Drain at
  the downgrade point was redundant and made the deliver-once shape
  ambiguous; the addendum's `TestInjectorDeliversOncePerIteration`
  pins the new shape.
- `RoleTool` `Name` addition — `runOneToolCall` and the dedup
  short-circuit in `runToolCalls` both set `Message.Name` on the
  appended `RoleTool` to the `call.Name` they served. A
  model-supplied `RoleTool` message now carries the tool name the
  model asked for, on the dedup path and on the success path.
- Soft-continue split on `hasInjector()` — a `Steer` with an
  installed injector soft-continues every steer, even on an empty
  drain. A `Steer` with no injector keeps the original single-shot
  `StopSteered` shape. The split is load-bearing for bridges that
  poll across iterations.

### Addendum goal

Document the steer injector and its soft-continue shape so a future
reviewer can confirm the contract without reading the implementation.

### Addendum scope

Inside:

- The five test cases listed under "Addendum tests" below.
- A doc-comment rewrite of `run.go`'s `// Steered-stop branch` block
  at `agentloop/run.go:147-173`, matching the downgrade-point
  no-drain property the four follow-ups describe. The rewritten
  comment must say: case (a) returns immediately after
  `ackTriggered()`, the downgrade path does not call `drainInjected`,
  and the next iteration-top boundary at `run.go:71-85` drains the
  injector. The rewritten comment must remove the false claim that
  the downgrade point drains the injector.

Outside:

- Any new exported symbol. The lock at `api/agentloop.txt:26-27` is
  correct.
- Any change to `decodeAndRun`, `runOneToolCall`, or any other
  tool-call dispatch path beyond adding the `Name` field on the
  appended `RoleTool` message.
- Any change to `Steer`'s mutex shape or `reset()` ordering.
- The four code changes themselves. This addendum is the spec; the
  builder implements them.

### Addendum API

No new exported symbol. `Steer.SetInjector` and `Steer.HasActiveCall`
land in `api/agentloop.txt:26-27` already, in commit d914611.

Reasoning: the lock is correct because the injector surface needs no
  caller-built struct. A single `func() []provider.Message` closure
  carries everything the loop needs.

### Addendum tests

A new file `agentloop/agentloop_test/steer_injector_review_test.go`
holds these five cases. The file matches the existing
`steer_injector_*_test.go` naming pattern.

- `TestSteerHasActiveCallFalseBeforeArm` — builds a fresh `Steer`,
  asserts `HasActiveCall()` returns `false` before any `RunSteerable`
  call arms. Kills the mutation that returns true unconditionally.
- `TestSteerHasActiveCallTrueDuringChat` — drives a `blockingCompleter`
  through `RunSteerable`, asserts `HasActiveCall()` returns `true`
  while the goroutine waits inside `Completer.Chat`. The case arms
  the same way `TestInjectorTriggeredAndEmptyStopsSteered` does.
- `TestHasActiveCallGuardsNoopBridgeTrigger` — installs an injector,
  drives a `Trigger` between iterations. Asserts: (a) the bridge
  goroutine calls `HasActiveCall()` in a loop and only `Trigger()`s
  when the method returns true; (b) the run reaches
  `StopNoToolCalls`, and the history does not contain a
  "would-have-triggered" sentinel proving the no-op trigger did not
  poison the next iteration's `Chat` arm; (c) the test counts
  `HasActiveCall()` calls to prove the guard ran on each poll. The
  bridge pattern guards each `Trigger` on `HasActiveCall`, proving the
  guard closes the no-op-trigger loop the finding describes.
- `TestRoleToolMessageCarriesToolName_Success` — drives a normal
  tool call, asserts the appended `RoleTool` message carries
  `Name == call.Name` on the success path.
- `TestRoleToolMessageCarriesToolName_Dedup` — feeds a duplicate call
  through `runToolCalls`, asserts the synthesized `RoleTool` message's
  `Name` equals the duplicate call's name.
- `TestInjectorDeliversOncePerIteration` — installs an injector whose
  drain returns the queue `[nil, payload1, payload2]` across two
  steered iterations. Asserts: (a) `payload1` appears in history
  exactly once after the first steered iteration's top drain; (b)
  `payload2` appears in history exactly once after the second steered
  iteration's top drain; (c) the injector call count is exactly 3
  (one per iter top), proving the downgrade path does not re-call
  `drainInjected`. The case pins the deliver-once shape across
  consecutive payloads.

### Addendum verification

- `make verify` passes.
- `go test -race -count=1 ./agentloop/...` passes.
- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_prose.py` passes.

No `make api-update`, no `policy/layers.json` change, no
`api/agentloop.txt` diff, no coverage-floor change.

## Addendum: loop extensions and concurrency hardening

Status: shipped. This addendum documents the loop extensions: WorkBudget,
ToolBudget, Surface rotation, ConcludeDeadline, and ConcludeStepsLeft;
ConcludeStepsLeft was later removed; see the closing maintenance
addendum.
It also documents OnToolCallError, MaxConcurrentTools, and toolcallctx.

### Addendum goal

Provide host control over token budgets, tool budgets, dynamic surfaces,
and concurrent execution.

### Addendum scope

Inside:

- `WorkBudget` token reservation hooks before and after completion calls.
- `ToolBudget` tool-call reservation hook before tool-call execution.
- `Surface` rotation hook from iteration two onward.
- Conclude thresholds: `ConcludeDeadline` and `ConcludeStepsLeft`;
  `ConcludeStepsLeft` was later removed; see the closing maintenance
  addendum.
- `OnToolCallError` custom error-response shaping hook.
- `MaxConcurrentTools` worker pool for parallel tool execution.
- `StreamingWriter` capture for partial replies on steered stop.
- `toolcallctx` integration for attaching tool calls to contexts.
- Events for assistant turns, thinking brackets, cache usage, and parallel dispatches.

Outside:

- Any change to `flow`, `agent`, or `agentrun`.
- Any external third-party dependency.

### Addendum API

Exported symbols added to `api/agentloop.txt`:

- `StopEmptyResponse` constant.
- `WorkBudget` struct and `ErrIncompleteWorkBudget`.
- `ToolBudget` struct and `ErrIncompleteToolBudget`.
- `Surface` struct.
- `ErrorFunc` type.
- `Options` fields: `WorkBudget`, `ToolBudget`, `Surface`, `ConcludeDeadline`, `ConcludeStepsLeft`, `ConcludeToolCallsLeft`, `StartTime`, `OnToolCallError`, `MaxConcurrentTools`, and `StreamingWriter`. `ConcludeStepsLeft` was later removed; see the closing maintenance addendum.
- Event constants: `EventAssistant`, `EventThinkingStart`, `EventThinkingDelta`, `EventThinkingEnd`, `EventCacheUsage`, `EventCalibrationDelta`, and `EventToolParallel`.

### Addendum tests

- TestRunEmptyResponseEndsWithStopEmptyResponse proves blank content stops with StopEmptyResponse.
- TestWorkBudgetReserveThenRefundOnSuccessfulTurn proves reserve and refund sequencing.
- TestWorkBudgetNilHookStillRuns proves a nil budget hook does not fail.
- TestWorkBudgetReserveErrorFailsClosed proves a reserve error aborts the run.
- TestWorkBudgetRefundsZeroUsageOnChatError proves refund on failed completion.
- TestWorkBudgetValidateRequiresBothFuncs validates hook completeness.
- TestWorkBudgetReserveAndRefundOnPromptTooLongRecovery proves recovery reserve and refund.
- TestToolBudgetReserveRunsWithFullRawCountBeforeDispatch proves tool budget reservation count.
- TestToolBudgetNilHookStillRuns proves a nil tool budget hook does not fail.
- TestToolBudgetReserveErrorFailsClosedBeforeAnyToolRuns proves tool reserve failure aborts.
- TestToolBudgetValidateRequiresReserve proves validation requires Reserve hook.
- TestSurfaceHookRotatesAdvertisedSetFromSecondIteration proves tool rotation.
- TestSurfaceHookNilReturnKeepsPrior proves nil hook retains prior surface.
- TestSurfaceHookPanicFailsRunClosed proves panic recovery.
- TestSurfaceConcurrentRunDoesNotRace proves concurrent surface safety.
- TestRunConcludeDeadlineThresholdFires proves deadline expiration conclude nudge.
- TestRunConcludeDeadlineFutureDoesNotFire proves future deadline does not nudge.
- The turn-cap test, `TestRunConcludeToolCallsLeftThresholdFires`, was later removed with its dead reserved option; see the closing maintenance addendum.
- `TestRunConcludeMarginThresholdFires` proves the margin conclude nudge. It was named `TestRunConcludeStepsLeftThresholdFires` until the steps-left option was later removed; see the closing maintenance addendum.
- TestRunConcludeTermsOREDTogether proves combination of conclude triggers.
- TestOptionsValidateCompleterBeforeConclude proves option validation ordering.

### Addendum verification

- `make verify` passes.
- `go test -race -count=1 ./agentloop/...` passes.
- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_prose.py` passes.

## Addendum: repeated tool failures early stop
Status: shipped.


Stop runs early when consecutive tool-calling turns repeatedly fail all tool calls.

### Addendum goal

Provide defense-in-depth against models stuck in repeated tool-calling failure loops.

### Addendum scope

Inside:

- `MaxConsecutiveToolFailures` option in `Options`.
- `StopRepeatedToolFailures` graceful stop reason.
- `ErrMaxConsecutiveToolFailures` validation error.
- Failure counting across consecutive turns where all dispatched calls carry a reported tool error under `ErrorPolicyReport` (unknown tool name, argument validation failure, or tool execution error alike; see the addendum "widen the consecutive-tool-failure counter").
- Counter reset on mixed turns or successful tool dispatches.

Outside:

- Any change to `flow`, `agent`, or `agentrun`.

### Addendum API

Exported symbols added to `api/agentloop.txt`:

- `StopRepeatedToolFailures` constant.
- `ErrMaxConsecutiveToolFailures` error variable.
- `MaxConsecutiveToolFailures` field on `Options`.

### Addendum tests

- `TestRepeatedToolFailuresStopsEarly` proves consecutive unknown-tool turns stop at threshold.
- `TestRepeatedToolFailuresResetsOnSuccess` proves successful calls reset the failure counter.
- `TestRepeatedToolFailuresResetsOnMixedTurn` proves mixed turns reset the failure counter.
- `TestRepeatedToolFailuresExcludesArgValidationAndToolError` (name kept for history) now proves argument validation and reported tool-run errors also count, alongside unknown tool names; see the addendum "widen the consecutive-tool-failure counter".
- `TestRepeatedToolFailuresDefaultZeroUnbounded` proves zero preserves unbounded behavior.
- `TestRepeatedToolFailuresValidateNegative` proves negative bound fails validation.

### Addendum verification

- `make verify` passes.
- `go test -race -count=1 ./agentloop/...` passes.
- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_prose.py` passes.

## Addendum: a stop-decision hook
Status: shipped.


Let a caller observe a graceful stop and continue the run from inside
the loop.

### Addendum goal

The loop decides on its own when to stop. A caller injects messages
before an iteration through `Steer.SetInjector`. A caller asks the loop
to wind down through `ConcludeMargin` and `ConcludeNotice`. No seam lets
a caller read the response that ended the run and continue from it.

`runToolStage` holds the whole picture at the moment it stops. It holds
the assistant message, an empty tool-call list, and the iteration count.
It turns that into `StopNoToolCalls`, `StopEmptyResponse`, or
`StopConcluded` and returns. The caller receives a finished `Result`
with no way back in.

A host that wants to continue such a turn runs a second loop over a
longer message list. A second loop resets state this package owns.
`Trim` re-runs over the whole history, so a long turn pays preparation
twice. `MaxTotalTokens` and the internal running token count restart, so
a per-turn ceiling becomes a per-attempt ceiling.

The case for the hook rests on three points. It offers
observe-then-decide at a stop point, which no existing seam offers. It
collapses a host's two re-run sites into one loop. It removes the
re-preparation cost, because a continuation stays inside the run.

`Steer.SetInjector` does not cover the case. The loop drains it at the
top of an iteration and at a steered-stop downgrade. Both points come
before the response exists. A caller cannot decide to continue from
evidence it has not seen.

`ConcludeMargin`, `ConcludeNotice`, and `StopConcluded` do not cover it
either. The nudge fires on proximity to a bound, not on what the model
did. The notice pushes the model toward a final answer. That is the
opposite operation.

### Addendum scope

Inside:

- `Options.ContinueOnStop`, one optional hook.
- `StopDecision`, the evidence the loop had when it decided to stop.
- The three graceful stop sites in `runToolStage`.
- `safeContinue`, the panic wrapper that fails the run closed.
- The two file moves the structure gate forces.

Outside:

- Any judgement about when to continue. The loop offers the decision
  point; the policy belongs to the caller.
- Natural-language patterns. This package must not read a model's prose
  to decide anything.
- Any change to `Steer`, `ConcludeMargin`, `ConcludeNotice`, or
  `StopConcluded`. Each keeps its current meaning.
- The consuming migration in the sibling consumer repo, which is a
  separate repository.

### Addendum API

Exported symbols added to `api/agentloop.txt`:

- `ContinueOnStop` field on `Options`, declared last in the struct.
- `StopDecision` struct with `Stop`, `Message`, `ToolCalls`,
  `Iterations`, and `History` fields. `ToolCalls` was later removed;
  see the closing maintenance addendum.

No existing symbol changes. A caller that leaves `ContinueOnStop` nil
gets the current behavior unchanged. The change is additive and needs no
major version.

`Options.Validate` gains no case. A function field carries no invariant
to enforce, so no new sentinel error enters the package.

Declare the field last in `Options`, after `ToolBudget`, so the lock
diff stays a pure append:

```go
// ContinueOnStop is consulted when the loop is about to stop
// gracefully. A non-empty return appends those messages to the run
// history and continues the loop. A nil or empty return stops the run
// unchanged. A nil hook changes no behavior. Runs on the loop
// goroutine, like Surface; a panic fails the run closed. A
// continuation is an ordinary iteration and obeys every bound the
// loop owns. The loop adds no bound of its own for this hook: a
// caller that sets neither MaxIterations nor MaxTotalTokens and
// always returns messages gets an unbounded run. That is the
// caller's choice. See docs/history/agentloop.md.
ContinueOnStop func(ctx context.Context, d StopDecision) []provider.Message
```

Declare `StopDecision` in the new `agentloop/stop.go`:

```go
// StopDecision is the evidence the loop had when it decided to stop.
// Consulted only on a graceful stop; see Options.ContinueOnStop.
type StopDecision struct {
	// Stop is the graceful reason the loop picked.
	Stop StopReason
	// Message is the assistant turn that ended the run.
	Message provider.Message
	// ToolCalls is the response's tool-call list, empty at every
	// call site the loop consults the hook from.
	ToolCalls []provider.ToolCall // later removed; see the closing maintenance addendum
	// Iterations counts the Completer calls that completed.
	Iterations int
	// History carries every message appended so far.
	History []provider.Message
}
```

### Addendum mechanics, exact

Consult the hook at the three graceful stops in `runToolStage`:
`StopNoToolCalls`, `StopEmptyResponse`, and `StopConcluded`.

Never consult the hook for `StopSteered`, `StopHookVeto`,
`StopMaxIterations`, or `StopRepeatedToolFailures`. Each is a bound or
an explicit caller decision doing its job. Never consult the hook on a
`hardFail` path. An error is not a stop.

Restructure the no-tool-call branch to pick the reason, then delegate.
The empty-content case still wins over the notice case, unchanged:

```go
if len(resp.ToolCalls) == 0 {
	*consecutiveFailures = 0
	stop := StopNoToolCalls
	if strings.TrimSpace(resp.Message.Content) == "" {
		stop = StopEmptyResponse
	} else if noticeInRequest {
		stop = StopConcluded
	}
	return l.gracefulStop(ctx, history, resp, iterations, totalUsage, stop)
}
```

Add `gracefulStop` to `agentloop/stop.go`:

```go
// gracefulStop consults l.continueOnStop and returns runToolStage's
// three values. A non-empty hook return continues the loop with the
// grown history; a nil hook, an empty return, or a panic keeps the
// stop or fails the run closed.
func (l *Loop) gracefulStop(ctx context.Context, history []provider.Message, resp provider.Response, iterations int, totalUsage provider.Usage, stop StopReason) (Result, bool, error) {
	if l.continueOnStop != nil {
		msgs, err := safeContinue(ctx, l.continueOnStop, StopDecision{
			Stop:       stop,
			Message:    resp.Message,
			ToolCalls:  resp.ToolCalls, // later removed; see the closing maintenance addendum
			Iterations: iterations,
			History:    history,
		})
		if err != nil {
			return l.hardFail(history, iterations, totalUsage), true, err
		}
		if len(msgs) > 0 {
			return Result{History: append(history, msgs...)}, false, nil
		}
	}
	return Result{Final: resp.Message, History: history, Iterations: iterations, Usage: totalUsage, Stop: stop}, true, nil
}
```

Control flow, exactly. `runToolStage` returns `(Result, bool done,
error)`. `afterChat` returns the result when `done` is true. `afterChat`
sets `*history = res.History` when `done` is false, then reports `done`
false to `runIteration`. `runIteration` reports it to `run`, and `run`
iterates again.

A continuation therefore returns a `Result` whose `History` is the
appended slice, with `done` false. That one return threads the grown
history back through `afterChat`. The next iteration reads it as its own
starting history. This is the same path a completed tool turn already
takes, so no new control-flow shape enters the loop.

Bounds, exactly. `afterChat` increments `iterations` and adds the
response's billed tokens before it calls `runToolStage`. A continuation
therefore already counts as a completed iteration. The next pass through
`run` hits the `iterations >= l.maxIterations` check. The next
`runIteration` pass runs `applyTrim`, the window plan, and `checkBudget`
over the grown history. The `ctx.Err()` check at the top of `run` ends a
canceled run. The `MaxTotalTokens` check in `afterChat` fires once the
next response arrives.

The runaway bound is conditional, not absolute. State it honestly.
`New` maps `MaxIterations: 0` through `unboundedOrSet` to
`math.MaxInt32`, and `TestSDKValidateAllowsUnboundedMaxIterations` pins
that zero-means-uncapped contract. `MaxTotalTokens: 0` is likewise
uncapped. So a bound catches a runaway hook only when the caller sets
`MaxIterations` or `MaxTotalTokens`. With both unset, an always-continue
hook runs without end.

Today that same configuration is safe because the model's own
no-tool-call turn ends the run. `ContinueOnStop` removes exactly that
terminator. Name the consequence in the doc comment and leave the choice
with the caller.

Add no second bound for a runaway hook. A new cap would duplicate
`MaxIterations` and would break the zero-means-uncapped contract that
callers rely on. The fix is the documented caveat plus
`TestContinueOnStopUnboundedWithoutBounds`.

`consecutiveFailures` stays where it is. `runToolStage` sets it to zero
on the no-tool-call branch before the hook runs. Keep that line in
place. A turn with no tool call is not a tool failure, so the reset
already holds on the stop path. A continuation must not change failure
accounting.

`noticeSent` stays where it is. It lives in `run` and reaches
`runIteration` by pointer. `afterChat` and `runToolStage` never see it,
so a continuation cannot reset it. That is correct: the notice fires
once per run, and a continuation is one more iteration of the same run.
The later stop reason depends on whether the notice survives.
`runIteration` recomputes `noticeInRequest` every iteration, and
`noticePresent` scans the current history for the notice text. `Trim`
or `Window` may strip the notice from a later iteration's history; see
the `noticePresent` doc comment in `agentloop/conclude.go`. So a later
graceful stop reports `StopConcluded` again only while the notice
survives in history. Once it is stripped, the same stop reports
`StopNoToolCalls`. `noticeSent` stays true either way, so the notice
never fires a second time.

Panic handling. Wrap the hook in `safeContinue`, mirroring
`safeSurface`:

```go
// safeContinue invokes fn, converting a panic into a plain error so a
// hostile host hook fails the run closed instead of half-continuing.
func safeContinue(ctx context.Context, fn func(context.Context, StopDecision) []provider.Message, d StopDecision) (msgs []provider.Message, err error) {
	defer func() {
		if r := recover(); r != nil {
			msgs = nil
			err = fmt.Errorf("agentloop: ContinueOnStop hook panicked: %v", r)
		}
	}()
	return fn(ctx, d), nil
}
```

`gracefulStop` turns that error into a `hardFail` return. The run fails
closed and never half-continues. No new exported error type enters the
package, matching `safeSurface`.

Message handling. Append the returned messages verbatim. Do not label,
wrap, or validate them. A host that appends a `RoleUser` message owns
making it distinguishable from a real user turn.

Known consequence at the `StopEmptyResponse` site. `afterChat` appends
`resp.Message` verbatim and unvalidated. A response whose `Message` has
an empty `Role` stops at `StopEmptyResponse` today, and nothing ever
validates it. A continuation sends that message into the next
`applyTrim` pass, which validates every trimmed message. `Message.Validate`
returns `ErrUnknownRole` for an empty `Role`. So continuing past a
`Role`-less empty response fails the run when `Trim` is set.

Accept this behavior; do not paper over it. Validating the hook's own
return would not help, because the offending message came from the
completer. Validating `resp.Message` would change the stop behavior of
runs that never use this hook. A caller that continues past an empty
response owns the provider responses it feeds the loop.

Wiring. Add a `continueOnStop` field to `Loop` in `agentloop/loop.go`.
Set it from `opts.ContinueOnStop` in `New`.

The hook receives the iteration context `runToolStage` already runs tool
calls under. Cancellation of the outer context still ends the run at the
top of the next iteration.

### Addendum file and function budget

`agentloop/run.go` holds 497 lines and `agentloop/options.go` holds 494.
The structure gate caps a file at 500 lines. Both files need room before
this change lands. Move code; do not raise the limit.

- Move `billedTokens` and `sumUsage` out of `agentloop/run.go` into a
  new `agentloop/tokens.go`. That is 22 lines with their comments, so
  `run.go` drops to about 475.
- Move `StopReason`, its doc comment, and its seven constants out of
  `agentloop/options.go` into the new `agentloop/stop.go`. That is 34
  lines, so `options.go` drops to about 460 before the new field lands.
- Put `StopDecision`, `gracefulStop`, and `safeContinue` in
  `agentloop/stop.go`, beside the stop vocabulary they serve.

Both moves relocate code inside one package. No exported symbol changes,
so the moves alone add nothing to `api/agentloop.txt`.

`gracefulStop` holds about 20 lines and `safeContinue` about 9.
`runToolStage` drops two return statements and gains one call, so it
stays near its current 36 lines. Every function stays under the 80-line
cap.

### Addendum documentation

- The plan deletes `docs/history/agents/proposal_stop_decision_hook.md`.
  This addendum carries its content, and a duplicate invites drift.
- The plan drops that file's "Sequencing" section rather than folding it
  in. The repository owner decided to build the seam on its own merits.
- No file calls this hook a proposal, and no file calls it not
  implemented.
- A grep for the deleted filename hits only this documentation section.
  A grep for `ContinueOnStop` and `StopDecision` hits only this plan
  today. After the change it hits `agentloop/`, `api/agentloop.txt`,
  and the package reference `docs/packages/agentloop.md`.
- `docs/README.md`, `docs/architecture.md`, and `agentloop/doc.go` name
  neither the proposal nor the hook, so none of them changes.

### Addendum tests

Put the cases in a new file under `agentloop/agentloop_test/`, named
`continue_on_stop_test.go`. Split the bound cases into
`continue_on_stop_bounds_test.go` when the first file passes 500 lines.

Every entry names its class. A regression fails against the baseline
where `ContinueOnStop` exists but `gracefulStop` never consults it. A
guard passes against that baseline and against the change.

- `TestContinueOnStopContinuesNoToolCalls` (regression) proves a
  non-empty return on `StopNoToolCalls` reaches the completer again.
- `TestContinueOnStopContinuesEmptyResponse` (regression) proves the
  same at the `StopEmptyResponse` site. Set `Role:
  provider.RoleAssistant` on the empty-content response, so the case
  tests the continuation and not the `Role`-less trim failure.
- `TestContinueOnStopEmptyResponseRoleLessTrimFails` (regression) pins
  the known consequence above. A response with an empty `Role` and empty
  content, a set `Trim`, and a continuing hook must fail the run with
  `ErrUnknownRole`. Classify it as documented behavior, not a defect.
- `TestContinueOnStopContinuesConcluded` (regression) proves the same at
  the `StopConcluded` site.
- `TestContinueOnStopNilReturnStopsUnchanged` (regression) proves a nil
  return yields today's `Result` field for field. Assert also that the
  hook ran exactly once and observed `StopDecision.Stop ==
  StopNoToolCalls`. Without both assertions the case passes on a
  build that never consults the hook. The invocation-count assertion
  is what makes the case fail against the baseline.
- `TestContinueOnStopEmptyReturnStopsUnchanged` (regression) proves an
  empty non-nil return behaves as a nil return. Assert the same
  invocation count of one and the same observed `Stop` value, for the
  same reason, with the same regression consequence.
- `TestContinueOnStopNilHookRequestsIdentical` (guard) proves a nil hook
  produces the same request sequence as the current loop.
- `TestContinueOnStopBoundedByMaxIterations` (regression) proves an
  always-continue hook ends with `StopMaxIterations`, not a hang.
- `TestContinueOnStopBoundedByMaxTotalTokens` (regression) proves the
  same hook ends with `ErrTokenBudgetExceeded` when the ceiling is set.
- `TestContinueOnStopUnboundedWithoutBounds` (regression) proves the
  loop adds no bound of its own. Set `MaxIterations: 0` and
  `MaxTotalTokens: 0`. Let the hook continue a fixed count of times,
  then return nil. Assert the completer ran that many times plus one,
  and that the run ended only because the hook stopped continuing.
- `TestContinueOnStopReceivesStopEvidence` (regression) proves the hook
  sees the assistant message and the empty tool-call list.
- `TestContinueOnStopTrimAppliesToGrownHistory` (regression) proves
  `Trim` runs over the grown history on the continued iteration.
- `TestContinueOnStopTrimStripsNoticeReportsNoToolCalls` (regression)
  proves the conditional notice claim above. A continued run whose
  `Trim` drops the `ConcludeNotice` reports `StopNoToolCalls`, not
  `StopConcluded`. Assert also that the notice is never appended a
  second time, proving `noticeSent` stays true.
- `TestContinueOnStopPanicFailsClosed` (regression) proves a panicking
  hook fails the run and appends no continuation.
- `TestContinueOnStopConcludedKeepsOneNotice` (regression) proves a
  continued run appends `ConcludeNotice` exactly once. Assert the
  completer call count is at least two. Without that assertion the case
  passes against the baseline, where an un-continued run appends the
  notice once anyway.
- `TestContinueOnStopSkippedOnSteered` (guard) proves `StopSteered`
  never consults the hook.
- `TestContinueOnStopSkippedOnHookVeto` (guard) proves `StopHookVeto`
  never consults the hook.
- `TestContinueOnStopSkippedOnMaxIterations` (guard) proves
  `StopMaxIterations` never consults the hook.
- `TestContinueOnStopSkippedOnRepeatedToolFailures` (guard) proves
  `StopRepeatedToolFailures` never consults the hook.
- `TestContinueOnStopSkippedOnTokenBudgetHardFail` (guard) proves the
  one `hardFail` that competes with a graceful stop over the same
  response skips the hook. The `MaxTotalTokens` check in `afterChat`
  fires after the response is recorded and before `runToolStage` runs.
  Fixture: a zero-tool-call response that trips the ceiling. Assert the
  run fails with `ErrTokenBudgetExceeded` and the hook ran zero times.
  Claim only this path. `hardFail` has nine call sites, and most return
  before `runToolStage` is reachable, so one case cannot prove the
  general rule.
- `TestContinueOnStopCancelledContextEndsRun` (regression) proves a
  canceled context ends a continued run at the next iteration top.
  Proving this needs at least one continuation, which the baseline never
  performs, so the case is a regression and not a guard.

Every skip case counts hook invocations. Assert that the count is zero,
not only that the stop reason matched.

Completer fixtures for the always-continue cases. `scriptedCompleter`
in `helper_test.go` returns an error once its scripted list runs out.
An always-continue hook then hard-fails on "no response scripted"
instead of hitting the bound under test. Script exactly `MaxIterations`
responses in those fixtures, or reuse `replayCompleter` from
`unbounded_max_iterations_test.go`, which repeats a terminal response
after its scripted turns. `TestContinueOnStopBoundedByMaxTotalTokens`
must not use `replayCompleter`; it needs scripted responses carrying
non-zero `Usage`, because `replayCompleter`'s repeated fallback reports
zero tokens and can never trip the ceiling. Reuse `scriptedCompleter`
for the request-sequence and call-count assertions, where the turn
count is known.

### Addendum verification

- `make api-update`, then commit the `api/agentloop.txt` diff in this
  same change.
- `make verify` passes, including the coverage floor for `agentloop`.
- `go test -race -count=1 ./agentloop/...` passes.
- `python3 scripts/check_structure.py` passes, proving both moved-from
  files stay under 500 lines.
- `python3 scripts/check_deps.py` passes. `agentloop` needs no new row
  in `policy/layers.json`. The hook uses `context` from the standard
  library and `provider`, an edge the policy already allows.
- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_prose.py` passes.

## Addendum: steer ack generation counter
Status: shipped.


### Goal

Close the steer trigger-drop window in the injector soft-continue
path. `ackTriggered` clears `triggered` unconditionally. A `Trigger`
fired after `disarm` but before `ackTriggered` is wiped. The caller's
steer intent vanishes. This contradicts the rule in
`Steer.HasActiveCall`'s doc comment: a trigger with no call in flight
still sets the flag for the next arm to observe.

### Scope

Inside:

- One new unexported field pair on `Steer`: `gen uint` and
  `ackedGen uint`.
- `Trigger` increments `gen` under the existing mutex.
- `wasTriggered` records `gen` into `ackedGen` when it returns true.
- `ackTriggered` clears `triggered` only when `gen == ackedGen`.
- `reset` zeroes `gen` and `ackedGen` with the rest.
- One new internal test file, `agentloop/steer_ack_test.go`, package
  `agentloop`, for the deterministic pairing test.
- One new external test file, `agentloop/agentloop_test/steer_ack_regression_test.go`,
  for the loop-level regression rows.

Outside:

- Any exported symbol. `api/agentloop.txt` stays byte-identical.
- The plain steered-stop path. It never calls `ackTriggered`; see
  `run.go`'s steered-stop branch. Case (b) semantics stay pinned by
  the existing `steer_test.go` rows.
- The Trigger-while-chat-in-flight cancel path. That trigger is the
  observed one; the ack consumes it exactly as before.
- `SetInjector` concurrency caveats. Its doc comment declares those
  caller-unprotected. Out of scope.
- Any production-code hook for tests.

### API

No exported change. `Steer` keeps `Trigger`, `SetInjector`, and
`HasActiveCall`; `NewSteer` stays. The invariant: an ack clears only
the trigger generation the loop observed, never a newer one. After the
fix, the code enforces what the `HasActiveCall` doc comment claims.
The doc comment needs no edit. Extend the `ackTriggered` doc comment
by one sentence: it clears the flag only when no newer `Trigger`
fired since `wasTriggered` observed one.

### Mechanism

Sequence today, injector installed: stop trigger sets `triggered` and
cancels the chat. Chat returns. `disarm` clears `cancel`.
`isSteerStop` calls `wasTriggered`. The injector branch calls
`ackTriggered`, which clears the flag unconditionally. A `Trigger` in
that tail is a logic race under correct locking, the same shape as the
prior `Calibrated.Observe` mispairing.

Fix: `Trigger` does `s.gen++` beside `s.triggered = true`.
`wasTriggered` sets `s.ackedGen = s.gen` when it returns true. This is
the loop's observation of one trigger generation. `ackTriggered` runs
`s.triggered = false` only under `if s.gen == s.ackedGen`. A trigger
fired after the observation raises `gen`; the ack leaves the flag set.
The next arm sees it and cancels instantly, honoring the late steer.
The steered stop after that instant cancel observes the same
generation, so its ack clears, and the loop converges with no spin.

### Determinism note

The production window from `wasTriggered` to `ackTriggered` is a few
statements on the loop goroutine. No blocking point exists between
them. A loop-level test cannot interleave into that window without a
sleep. The pairing lives entirely inside `Steer`, so the killing test
drives `Steer`'s own methods sequentially in an internal test. This is
deterministic and needs no production hook. The loop-level fixtures
cover the adjacent reachable windows with channel barriers, not
sleeps. The `observe_pairing_test.go` precedent supports pairing-level
proof for a mutex-protected mispairing.

### Addendum tests

New internal file `agentloop/steer_ack_test.go`, package `agentloop`.
New external file `agentloop/agentloop_test/steer_ack_regression_test.go`,
package `agentloop_test`.

- `TestSteerAckSparesUnobservedTrigger` is the killing test. Fixture:
  `Trigger`; `wasTriggered` returns true; `Trigger` again;
  `ackTriggered`; `wasTriggered` must stay true. Against the old
  `ackTriggered`, whose body is the unconditional `s.triggered = false`
  at `steer.go`, the final read reports false and the test fails.
  Against the new conditional clear, it passes. Same fixture, both
  directions, no goroutines.
- `TestSteerAckClearsObservedTrigger` pins the happy path: observe
  then ack with no newer trigger clears the flag once.
- `TestSteerAckWithoutObserveClearsNothing` pins the safe default: an
  ack with `ackedGen` behind `gen` never clears.
- `TestSteerDoubleTriggerSingleGenerationAck` covers a double
  `Trigger` before the observe: one ack consumes both only after an
  observe of the later generation; a stale observe does not.
- `TestSteerTriggerAfterResetNewGeneration` covers `reset` zeroing
  `gen` and `ackedGen`, then a fresh trigger observe-and-ack cycle.
- `TestInjectorAckSparesTriggerBeforeNextArm` is the loop-level
  window probe. Fixture: injector installed; a blocking fake
  Completer whose first Chat blocks on a channel. Test triggers while
  Chat blocks, so the stop trigger is deterministic. The fake's second
  Chat signals entry and blocks, so the test observes whether the
  second iteration armed clean or canceled instantly. With the fix and
  a trigger raised between the first ack and the second arm, the
  second Chat is canceled instantly and the run reports the second
  stop. The trigger-before-arm side of the window is reachable here;
  the trigger-inside-ack-window side is covered by the internal test
  above.
- `TestInjectorTriggerDuringDrainIsHonored` is a regression row: the
  injector blocks at the iteration-top drain; the test triggers;
  release; the drain delivers and the next chat cancels instantly.
  Works under old and new code; pins the non-ack paths.
- Existing rows keep passing unchanged:
  `TestInjectorTriggeredAndEmptyInjectorSoftContinues`,
  `TestSteerTriggerMidCompleter`, `TestSteerTriggerTwiceAndBeforeStart`,
  `TestSteerTriggerConcurrent`, and the recovery rows in
  `steer_recovery_test.go`. They pin the non-injector path, which
  never acks.

### What this does not change

- A trigger while a chat is in flight: same cancel, same stop, same
  ack consumption. That trigger is the observed generation.
- `reset` semantics: still zero-valued start per `RunSteerable`; now
  also zeroes the two counters.
- `SetInjector` mid-run caveats: unchanged, caller-unprotected.
- The non-injector steered stop: no ack on that path today, none
  after.

### Addendum verification

- `make verify` passes. The `agentloop` coverage floor stays at or
  above 85; the new tests only add covered statements.
- `make api-update` produces no diff in `api/agentloop.txt`. State:
  no API lock change is expected or permitted.
- `policy/layers.json` gets no new edge; both new test files import
  only what `agentloop` tests already import.
- `go test -race -count=1 ./agentloop/...` passes.
- `python3 scripts/check_structure.py` passes: `steer.go` grows under
  ten lines and stays well under the 500-line cap.
- `python3 scripts/check_plan.py`, `scripts/check_prose.py`, and
  `scripts/check_labels.py` pass.

## Addendum: maintenance batch — ConcludeToolCallsLeft removal and runIteration shape note
Status: shipped.


### Goal

- Two maintenance items in one batch. First, remove the dead
  `ConcludeToolCallsLeft` option. Second, record a refactoring rule
  for `runIteration`.

### Scope

- Item one, verified: `Options.ConcludeToolCallsLeft` is declared at
  `agentloop/options.go:269-270`, validated at `options.go:449-451`,
  and stored at `loop.go:152`. No code reads
  `loop.concludeToolCallsLeft`. Grep finds no consumer outside
  `agentloop` and its tests. Direction: remove the field. Removing a
  dead exported field beats keeping a documented reservation, because
  no caller exists to break and the lock can re-add the name later.
- Exact change: delete the field, its `Validate` row, the
  `loop.go:152` assignment, and the `loop.go:71` struct field.
- Item two, verified: `agentloop/run.go` is 477 lines,
  `options.go` 474, `toolcall.go` 458; all sit near the 500-line cap.
  `runIteration` at `run.go:135` takes 10 parameters, 6 of them
  pointers. Direction: no code change today.

### Addendum tests

- Delete `TestRunConcludeToolCallsLeftThresholdFires` in
  `agentloop/agentloop_test/conclude_terms_test.go:152`: its subject,
  the field's inertness, disappears with the field. The commit message
  carries an `Allow-Test-Change` trailer, so
  `scripts/check_test_tampering.py` waives the deletion.
- Delete the three `ConcludeToolCallsLeft` rows in
  `agentloop/agentloop_test/options_test.go` and the row at
  `options_test.go:266`; same trailer covers them.
- No new test: the change deletes surface, it adds none. Existing
  conclude tests pin the other terms unchanged.

### Addendum verification

- `make api-update` removes the `ConcludeToolCallsLeft int` line from
  `api/agentloop.txt`; commit the diff in the same change. This is
  the batch's only API delta.
- `make verify` passes, with two known-foreign failures in
  `agentloop/steer_ack_test.go` from a parallel change, not this
  batch.
- Coverage floor of 85 holds for `agentloop`.
- For `runIteration`: at the next feature that touches it, introduce
  an unexported `runState` struct that carries the six pointer
  parameters. Record this as a caller gate, not a scheduled change.

## Addendum: maintenance batch — duplicate conclude term, dead stop field, sentinel promotion
Status: shipped.


### Prior item closed

- The previous addendum ordered the `Options.ConcludeToolCallsLeft`
  removal. Commits `fafd815` and `c3e6d98` completed it.
- `command grep -rn ConcludeToolCallsLeft --include='*.go'
  --include='*.md' . | command grep -v '^\./\.claude/'` returns eleven
  hits. Every hit sits in this plan file. Each one is historical plan
  text, the prior addendum, or this addendum's own back-references.
- No live code site remains. The item is closed. The builder does
  nothing more about it.

### Addendum goal

Three maintenance items ship as one commit. Item A removes the
duplicate `Options.ConcludeStepsLeft` term. Item B removes the dead
`StopDecision.ToolCalls` field. Item C promotes the last three inline
`errors.New` rules in `Options.Validate` to package sentinels.

### Addendum scope

Inside:

- Item A: `Options.ConcludeStepsLeft`, its `Loop` field, its
  `Validate` row, its `shouldConclude` branch, its tests, its docs.
- Item B: `StopDecision.ToolCalls`, its one assertion, its docs.
- Item C: three new sentinel errors and the three tests that assert
  them with `errors.Is`.
- The stale `Options.Validate` doc comment at
  `agentloop/options.go:395-405`.

Outside:

- `ConcludeMargin`, `ConcludeDeadline`, and `ConcludeNotice`. All
  three keep today's behavior.
- Any other package. `policy/layers.json` gets no change, because no
  import edge moves.

### Doc-edit convention

Two document classes take two different edits. This rule covers every
doc site in items A and B.

- A historical addendum in `docs/history/agentloop.md` records a past
  decision. Never delete its text. Annotate it. A prose line gets the
  appended clause "later removed; see the closing maintenance
  addendum". A line inside a Go code fence gets the same clause as a
  trailing `//` comment on the declaring line.
- `docs/packages/agentloop.md` is the live reference. It must state
  today's truth. Drop the removed name outright.
- The precedent for the annotate rule is
  `docs/history/agentloop.md:3654`, written when
  `ConcludeToolCallsLeft` went away.

### Item A: remove Options.ConcludeStepsLeft

- Verified duplication: `agentloop/conclude.go:39-50` OR-s
  `maxIterations-k < concludeMargin` and
  `maxIterations-k < concludeStepsLeft`. The two predicates are
  identical in form.
- Verified wrong comment: `agentloop/options.go:269-273` claims "the
  smaller of the two decides". An OR of two `<` predicates fires at
  the larger threshold, not the smaller.
- `agentloop/agentloop_test/conclude_terms_test.go:221-222` already
  shows the larger threshold winning.
- Direction: delete `ConcludeStepsLeft` and keep `ConcludeMargin`.
  One field with a correct comment beats two fields with a wrong one.

Code sites to delete, each confirmed by grep:

- `agentloop/options.go:269-273`: the field and its comment.
- `agentloop/options.go:402`: the `ConcludeStepsLeft is not` clause
  and the following `negative,` word inside the `Validate` doc
  comment. See "Doc comment repair" below.
- `agentloop/options.go:447-449`: the `Validate` row and its inline
  `errors.New`.
- `agentloop/conclude.go:30`: change "Three terms are OR-ed" to "Two
  terms are OR-ed".
- `agentloop/conclude.go:37-38`: the doc-comment bullet.
- `agentloop/conclude.go:48-50`: the predicate branch.
- `agentloop/loop.go:71`: the `concludeStepsLeft int` struct field.
- `agentloop/loop.go:151`: the `concludeStepsLeft: opts.ConcludeStepsLeft,`
  assignment.

Doc comment repair at `agentloop/options.go:395-405`:

- Delete the words `ConcludeStepsLeft is not` and the following
  `negative,`.
- The comment today stops at "and finally a positive
  HeartbeatInterval requires a non-nil Bus". That is wrong. Two
  checks run after it: `o.WorkBudget.validate()` at
  `agentloop/options.go:462` and `o.ToolBudget.validate()` at `:465`.
- Move the word "finally" off the heartbeat clause. End the comment
  with a clause naming both budget checks in that order.
- The comment must list the checks in the order `Validate` runs them.
  This is the invariant the comment claims. Re-read the function body
  from `agentloop/options.go:407` to `:469` after the edit and confirm
  every check appears once, in order.

### Item A test rewrites

Every scenario survives. No scenario is deleted. Two test functions
are renamed. Neither new name exists today; `command grep -rn "func
TestRunConcludeMargin" agentloop/` confirms it.

- `agentloop/agentloop_test/conclude_terms_test.go:149-183`: rename
  `TestRunConcludeStepsLeftThresholdFires` to
  `TestRunConcludeMarginThresholdFires`. Replace `ConcludeStepsLeft: 4`
  with `ConcludeMargin: 4`. Keep `MaxIterations: 5`. Keep every
  assertion. Rewrite the doc comment to name `ConcludeMargin` and keep
  the same worked arithmetic.
- `agentloop/agentloop_test/conclude_terms_test.go:260-290`: rename
  `TestRunConcludeStepsLeftBoundary` to
  `TestRunConcludeMarginBoundary`. Replace `ConcludeStepsLeft: 3` with
  `ConcludeMargin: 3`. Keep `MaxIterations: 5`. Keep every assertion.
- `agentloop/agentloop_test/conclude_terms_test.go:231-258`
  (`TestRunConcludeZeroTermsDoNotFire`): delete the
  `ConcludeStepsLeft: 0` line at `:240`. Drop `ConcludeStepsLeft` from
  the doc comment at `:230`.
- `agentloop/agentloop_test/conclude_terms_test.go:292-329`
  (`TestRunConcludeZeroTermsKExceedsMaxIterations`): delete the
  `ConcludeStepsLeft: 0` line at `:307`.
- `agentloop/agentloop_test/options_test.go:249-259`
  (`TestOptionsValidateCompleterBeforeConclude`): delete the
  `o.ConcludeStepsLeft = -1` line at `:253`. Change the doc comment
  "negative ConcludeDeadline and StepsLeft" to "negative
  ConcludeDeadline". The test keeps its subject: `ErrNoCompleter` wins
  over a later conclude check.

### Item A: the OR test needs a reachable second term

`TestRunConcludeTermsOREDTogether` at
`agentloop/agentloop_test/conclude_terms_test.go:187-227` needs care.
Its in-body comment claims that several terms fire at different
points. That claim must stay true after the change.

- The fixture is `twoIterCompleter`, so the run ends at iteration 2.
  Only k=1 and k=2 ever reach `shouldConclude`.
- Today `ConcludeMargin: 1` with `MaxIterations: 5` first qualifies at
  k=5. The in-body comment at `:190` says so. That k never runs.
- So the margin disjunct is unreachable in this test. Dropping
  `ConcludeStepsLeft: 4` and keeping `ConcludeMargin: 1` would leave
  one live term.
- The notice-count assertion at `:225` would still hold in that state.
  The past deadline qualifies at both k=1 and k=2 on its own, so a
  broken dedup guard still drives the count to 2. The assertion is not
  the reason to change the value.
- The reason is the comment. A relabelled "two OR-ed terms" comment
  over one live term is a false claim about control flow. See
  `.agents/memories/comment_claim_equals_promise.md`.
- Fix: change `ConcludeMargin: 1` at `:201` to `ConcludeMargin: 4`,
  the same arithmetic the deleted `ConcludeStepsLeft: 4` carried.
  Margin then qualifies at k=2, because 5-2=3 is less than 4.
- Delete the `ConcludeStepsLeft: 4` line at `:203`. Keep
  `ConcludeDeadline: time.Hour` and the past `StartTime`, which
  qualify at k=1.
- Both terms now qualify inside the run, at different k. The comment
  and the code agree again.
- Rewrite the in-body comment block at `:190-196`. State that Deadline
  qualifies at k=1 and Margin at k=2. Change "three OR-ed terms" to
  "two OR-ed terms" in the doc comment at `:187`.
- The block at `:193-196` carries a bare `StepsLeft` token, not the
  full field name. Delete it with the rest of the block. A grep for
  `ConcludeStepsLeft` alone does not see it.

Collision decision for `agentloop/agentloop_test/options_test.go:149-160`:

- The three `ConcludeStepsLeft` rows are "negative fails", "zero
  passes", and "positive passes".
- `options_test.go:125-136` already carries the same three rows for
  `ConcludeMargin`. A rewrite of the `ConcludeStepsLeft` rows produces
  a duplicate of those rows.
- Decision: fold. Delete the three `ConcludeStepsLeft` rows. Do not
  add a distinguishing threshold. A second copy of the same table row
  proves nothing the first copy does not.
- The `ConcludeMargin` rows at `:125-136` stay unchanged. They are the
  surviving coverage for this invariant.

Collision check for the two renames:

- `TestRunConcludeMarginThresholdFires` uses `twoIterCompleter` and
  runs two iterations. Every existing `ConcludeMargin` test in
  `conclude_test.go` uses `scriptedCompleter`. No body becomes
  byte-identical.
- `TestRunConcludeMarginBoundary` keeps its own fixture and its own
  arithmetic. It is not the only proof of the strict `<`:
  `conclude_test.go:101-105` already asserts that k=3 carries no
  notice when `ConcludeMargin` is 2, because 5-3=2 is not less than 2.
  Keep the renamed test. Claim no novelty for it.

### Item B: remove StopDecision.ToolCalls

- Verified dead: `agentloop/stop.go:52-54` declares the field.
  `gracefulStop` fills it from `resp.ToolCalls`.
- Verified single call site: `command grep -rn "gracefulStop"
  --include='*.go' .` returns one call, at `agentloop/run.go:289`.
  That call sits inside the `if len(resp.ToolCalls) == 0` branch
  opened at `run.go:281`. The field is therefore always empty.
- Direction: delete the field. A field that can only ever be empty is
  a false promise to the hook author.

Code sites:

- `agentloop/stop.go:52-54`: delete the field and its comment.
- `agentloop/stop.go:70`: delete the `ToolCalls: resp.ToolCalls,`
  line in the `StopDecision` literal.
- `agentloop/agentloop_test/continue_on_stop_test.go:381-383`: delete
  that one `if len(d.ToolCalls) != 0` assertion and its `t.Fatalf`
  body. The surrounding test stays. Every other assertion in it
  stays.

### Addendum docs

Item A, historical plan sites. Annotate, do not delete:

- `docs/history/agentloop.md:3596`, `:3611`, and `:3632`: append the
  clause "later removed; see the closing maintenance addendum" to each
  `ConcludeStepsLeft` mention.
- `docs/history/agentloop.md:3655`: rewrite the line to name
  `TestRunConcludeMarginThresholdFires` and the margin term, and
  append the same clause about the old name.
- Backtick rule for that line. Line 3655 sits in the `### Addendum
  tests` section that opens at `:3635`. `scripts/check_plan.py`
  cross-checks such a section: every bare `Test[A-Z]\w+` token in it
  must name a test the package declares. `_strip_code_spans` at
  `check_plan.py:74` scrubs single-backtick spans first.
- So write both the new name and the old
  `TestRunConcludeStepsLeftThresholdFires` inside backticks. A bare
  removed or renamed test name in a cross-checked Tests section fails
  the gate. Commit `fafd815` hit this. The precedent at `:3654`
  backticks the removed name for the same reason.
- The same rule governs every other Tests section this commit edits.

Item A, live reference sites. Drop outright:

- `docs/packages/agentloop.md:20`: drop `ConcludeStepsLeft` from the
  `Options` field list. Keep `ConcludeNotice` on the same line.
- `docs/packages/agentloop.md:93`: drop `ConcludeStepsLeft` only. The
  `HeartbeatInterval`, `WorkBudget`, and `ToolBudget` clauses at
  `:95-97` are already correct. Do not touch them.

Item B, historical plan sites. Annotate, do not delete:

- `docs/history/agentloop.md:3778`: the prose bullet listing the
  `StopDecision` fields. Append the clause about `ToolCalls`.
- `docs/history/agentloop.md:3815-3817`: the `StopDecision` code fence.
  Put the clause as a trailing `//` comment on the `ToolCalls
  []provider.ToolCall` line at `:3817`.
- `docs/history/agentloop.md:3863`: the `gracefulStop` code fence. Put
  the clause as a trailing `//` comment on the `ToolCalls:
  resp.ToolCalls,` line.
- `docs/history/agentloop.md:3839` reads `if len(resp.ToolCalls) == 0`.
  That is the response field, not the struct field. Leave it alone.

Item B, live reference sites. Drop outright:

- `docs/packages/agentloop.md:47`: drop `ToolCalls` from the
  `StopDecision` field list.
- `docs/packages/agentloop.md:320-321`: change "the assistant turn,
  the tool-call list, the iteration count, and the history" to "the
  assistant turn, the iteration count, and the history".

No change needed:

- `docs/architecture.md` names neither symbol.

Closing greps. Both must return zero hits:

- `command grep -rni 'stepsleft' --include='*.go' . | command grep -v
  '^\./\.claude/'`. The case-insensitive form catches the bare
  `StepsLeft` token that the full field name misses.
- `command grep -rn 'ToolCalls' --include='*.go' agentloop/stop.go`.

### Item C: promote three inline errors to sentinels

Three inline `errors.New` calls remain in `Options.Validate` after
item A. Each becomes a package sentinel. Every message string stays
byte-identical.

- `agentloop/options.go:417` becomes `ErrSessionIDRequired`, with the
  message `agentloop: Usage requires a non-blank SessionID`.
- `agentloop/options.go:425` becomes `ErrMaxTotalTokens`, with the
  message `agentloop: MaxTotalTokens must not be negative`.
- `agentloop/options.go:445` becomes `ErrConcludeDeadline`, with the
  message `agentloop: ConcludeDeadline must be non-negative`.

Declaration style and placement:

- Declare all three inside the existing `var (` block that opens at
  `agentloop/options.go:24`. That block already holds every other
  `Options.Validate` sentinel, including `ErrConcludeMargin` at
  `:101` and `ErrMaxIterations` at `:35`.
- Append the three after `ErrHeartbeatRequiresBus` at
  `agentloop/options.go:117`, in the order `ErrSessionIDRequired`,
  `ErrMaxTotalTokens`, `ErrConcludeDeadline`.
- Give each a doc comment starting with the symbol name and ending
  with `Test with errors.Is.`, matching `ErrConcludeMargin`.
- Source order does not change the lock. The lock sorts variables by
  name.

### Item C test changes

Three existing table rows in
`agentloop/agentloop_test/options_test.go` currently pass `nil` as
`wantErr`. A `nil` `wantErr` only asserts that some error came back.
Each row must now name its sentinel, which `runValidateCases`
compares with `errors.Is` at `options_test.go:50`.

- `options_test.go:95-98`, row `"negative MaxTotalTokens fails"`:
  change the `wantErr` field from `nil` to
  `agentloop.ErrMaxTotalTokens`.
- `options_test.go:107-110`, row `"Usage without SessionID fails"`:
  change the `wantErr` field from `nil` to
  `agentloop.ErrSessionIDRequired`.
- `options_test.go:137-140`, row `"negative ConcludeDeadline fails"`:
  change the `wantErr` field from `nil` to
  `agentloop.ErrConcludeDeadline`.

No row is deleted by item C. Each row keeps its name and its
`mutate` function.

### Addendum API

Removed from `api/agentloop.txt`:

- `  ConcludeStepsLeft int`, today at `api/agentloop.txt:84`, inside
  `type Options struct`.
- `  ToolCalls []provider.ToolCall`, today at `api/agentloop.txt:107`,
  inside `type StopDecision struct`.

Added to `api/agentloop.txt`, in the lock's alphabetical variable
order:

- `  var ErrConcludeDeadline`, after `var ErrCompactionFailed`.
- `  var ErrMaxTotalTokens`, after `var ErrMaxIterations`.
- `  var ErrSessionIDRequired`, after `var ErrPlanFailed`.

The lock changes by five lines and nothing else. Run `make
api-update` and commit the `api/agentloop.txt` diff in the same
commit. Do not hand-edit the lock.

`policy/layers.json` gets no change. No package gains or loses an
import.

### Addendum tests

- No new test function. The change removes surface and adds three
  sentinels for rules the tests already exercise.
- Two renamed tests keep their fixtures, their thresholds, and their
  assertions. Only the option name changes.
- `TestRunConcludeTermsOREDTogether` gains a reachable second term.
  Its notice-count assertion becomes meaningful.
- Three `Validate` table rows gain an `errors.Is` assertion they did
  not have.
- `go test -race -count=1 ./agentloop/...` must pass.

### Addendum verification

`make verify` is not clean on this change until the commit carries
its trailers. State the expected result plainly.

- `make verify` reports two `TT01` findings from
  `scripts/check_test_tampering.py`, which `Makefile:24` runs inside
  `verify-fast`. `scripts/test_tampering_override.py` waives a
  finding only through an `Allow-Test-Change:` trailer on the commit.
- One `Allow-Test-Change: TT01 <reason>` trailer clears both
  findings. The reason needs six significant words at least, per
  `test_tampering_override.py:62-71`.
- Do not add a second `TT01` trailer. `resolve_overrides` builds
  `first_by_id` with `setdefault` at
  `test_tampering_override.py:85-87`, so the first trailer per ID
  wins and a second one of the same ID is parsed and ignored.
- Commit `fafd815` is the precedent for the trailer form. Its two
  trailers carry two different IDs, not one ID twice.
- The ORCHESTRATOR authors and verifies that trailer. The builder
  never adds one.
- Every other gate must be clean with no trailer and no waiver.

Gate commands the builder runs:

- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_deps.py` passes. No edge changed.
- `python3 scripts/check_api.py` passes after `make api-update`.
- `python3 scripts/check_docs.py` passes. Each new sentinel carries a
  doc comment starting with its own name.
- `python3 scripts/check_prose.py` passes.
- `python3 scripts/check_structure.py` passes. No touched file grows
  past its limit. `agentloop/options.go` loses about nine lines to
  item A and gains about nine to item C, so it stays near 469 lines,
  well under the 500-line cap.
- The coverage floor of 85 holds for `agentloop`. The removed
  `Validate` row and the removed `shouldConclude` branch each take
  their own covered lines away with them.

### Predicted test-tampering findings

`scripts/check_test_tampering.py` matches a removed test function by
body hash. Both renames change the body, so no hash matches.

- TT01 on `agentloop/agentloop_test/conclude_terms_test.go`, for
  `TestRunConcludeStepsLeftThresholdFires`. Justification: renamed to
  `TestRunConcludeMarginThresholdFires` with the same fixture, the
  same threshold of 4, and every assertion kept. The option it named
  no longer exists.
- TT01 on `agentloop/agentloop_test/conclude_terms_test.go`, for
  `TestRunConcludeStepsLeftBoundary`. Justification: renamed to
  `TestRunConcludeMarginBoundary` with the same fixture, the same
  threshold of 3, and every assertion kept.
- TT04 is expected to stay silent. Both renamed functions register as
  new test functions, which suppresses the assertion-count rule at
  `scripts/test_tampering_rules.py:175`. If it fires, the
  justification is the deleted `StopDecision.ToolCalls` assertion at
  `continue_on_stop_test.go:381`, whose subject is a field the commit
  removes.
- TT05 is expected to stay silent. The deleted assertion's condition
  is `len(d.ToolCalls) != 0`, which the rule's operand pattern does
  not match, and the commit adds no bare `err` check in that hunk.
- The builder reports these findings and this justification text. The
  builder does not add a trailer. The orchestrator verifies each
  finding and decides the trailer.

## Addendum: reserveTools drops its redundant Reserve check
Status: shipped.


### Addendum goal

Delete one redundant condition in `reserveTools`. The construction
path already rejects the state it tests. No behavior, API, or policy
changes.

### Addendum scope

At `agentloop/budget.go:143`, replace

```go
	if l.toolBudget == nil || l.toolBudget.Reserve == nil {
```

with

```go
	if l.toolBudget == nil {
```

Extend `reserveTools`'s doc comment with the invariant it now relies
on:

```go
// reserveTools runs the ToolBudget's Reserve for one turn's tool-call
// count. A nil l.toolBudget is a no-op; Options.Validate rejects a
// non-nil ToolBudget with a nil Reserve, so Reserve is never nil here.
// A hook error is wrapped with the 1-based iteration count so the
// hard fail names its cause, mirroring reserveWork.
```

Outside the addendum: `ToolBudget.validate`, `Options.Validate`, and
every `WorkBudget` function.

### Addendum proof

`ToolBudget.validate` at `agentloop/budget.go:127` returns
`ErrIncompleteToolBudget` for a non-nil budget with a nil `Reserve`.
`Options.Validate` calls it unconditionally at
`agentloop/options.go:465`. `New` calls `opts.Validate` as its first
statement, at `agentloop/loop.go:114`, and returns on any error.

`New` is the only constructor. `&Loop{` appears once in the module, at
`agentloop/loop.go:125`. `Loop.toolBudget` (`loop.go:97`) and
`Loop.workBudget` (`loop.go:93`) are unexported, so no caller outside
the package builds a `Loop`, and no internal test builds one.
`reserveTools` has one caller, `agentloop/run.go:295`, which runs on a
`Loop` that `New` returned.

`reserveWork` at `agentloop/budget.go:61` and `refundWork` at
`agentloop/budget.go:74` each carry the single nil check only. The
deletion makes the three functions agree.

Residual risk, stated and accepted: `l.toolBudget` aliases the
caller's `*ToolBudget`, so a caller that nils `Reserve` after `New`
moves from a silent no-op to a nil-func panic. `reserveWork` and
`refundWork` already carry that same hazard, so this is a consistency
fix and not a new class of risk. The mutation also races the
documented "safe for concurrent use" contract on `ToolBudget.Reserve`.

### Addendum tests

Add `TestToolBudgetNewRejectsNilReserve` to
`agentloop/agentloop_test/tool_budget_test.go`. It calls
`agentloop.New` with a `ToolBudget` whose `Reserve` is nil, and
asserts `errors.Is(err, agentloop.ErrIncompleteToolBudget)` and a nil
`Loop`.

This test is the positive control for the deletion. Both existing
cases reach the sentinel through `Options.Validate` directly:
`TestToolBudgetValidateRequiresReserve` at
`tool_budget_test.go:133`, and the "incomplete ToolBudget fails" row
at `options_test.go:191-194`, which `runValidateCases` drives. Neither
proves that `New` runs `Validate`, which is the precondition the
deletion depends on.

### Addendum coverage

Measured on a scratch copy: `agentloop` stays at 98.6%. The 85% floor
holds.

`scripts/mutation_denylist/agentloop.json` holds a mutation floor of
96. `make verify` runs `check_mutation.py --probe` only
(`Makefile:59`), so that floor is not re-checked by verify. Run
`make mutation-gate` as a follow-up if the score sits near the floor.

### Addendum verification

`make verify` passes. `go test ./agentloop/...` passes.
`make api-update` produces no diff: `reserveTools` and
`ToolBudget.validate` are unexported. A non-empty `api/` diff is a
failure, not a lock refresh. `policy/layers.json` needs no row.

Predicted `scripts/check_test_tampering.py` findings: none. The change
deletes no test and adds one. The builder does not add a trailer.

## Addendum: the agentloop composition example and the Conclude and Bounds groups

Status: shipped. Both parts of this addendum ship as one change.
Commit 112d0ad later removed `TurnResultBudget`; see the addendum
"per-batch tool-result size shaping" above.

### Addendum goal

- Two parts, one change.
- Part one adds the agentloop composition example:
  `docs/examples/_agentloop/main.go` and `docs/examples/agentloop.md`.
  The example wires a complete `Options` and runs offline. It is the
  oracle for part two: it sets every grouped field, so its shape fixes
  the target API before the regrouping lands.
- Part two folds nine flat `Options` fields into two groups,
  `Conclude` and `Bounds`, and folds `runIteration`'s six pointer
  parameters into one `runState` struct.
- Working order: the builder writes the example first, against the
  grouped shape. The example compiles only after part two lands. Both
  parts ship in one commit.

### The field map and the grep that produced it

- Command:

```sh
sed -n '/^type Options struct/,/^}/p' agentloop/options.go \
  | grep -c '^[[:space:]]*[A-Z]'
```

- It counts 34 flat fields in `Options` today.
- The map. Nine fields move; the other 25 stay flat and unmoved.

| Flat field | Group | Member |
|---|---|---|
| `ConcludeMargin` | `Conclude` | `Margin` |
| `ConcludeDeadline` | `Conclude` | `Deadline` |
| `ConcludeNotice` | `Conclude` | `Notice` |
| `MaxIterations` | `Bounds` | `MaxIterations` |
| `MaxCallsPerTurn` | `Bounds` | `MaxCallsPerTurn` |
| `MaxTotalTokens` | `Bounds` | `MaxTotalTokens` |
| `MaxConcurrentTools` | `Bounds` | `MaxConcurrentTools` |
| `MaxConsecutiveToolFailures` | `Bounds` | `MaxConsecutiveToolFailures` |

### The new types and the Validate placement

The exact declarations. `Conclude` and its method land in
`agentloop/conclude.go`. `Bounds` and its method land in a new file,
`agentloop/bounds.go`.

```go
// Conclude groups the graceful-conclude options: when the loop starts
// nudging the model toward a final answer, and what it says.
type Conclude struct {
	// Margin nudges the model once MaxIterations-k < Margin holds.
	// Zero disables the step-count term.
	Margin int
	// Deadline, when positive, fires the nudge once
	// StartTime.Add(Deadline) has passed. Zero disables the term.
	Deadline time.Duration
	// Notice is the RoleUser content Run appends once nudging starts.
	// Empty Notice uses DefaultConcludeNotice.
	Notice string
}

// Validate checks the group in a fixed order and returns the first
// failure: Margin is not negative, then Deadline is not negative.
func (c Conclude) Validate() error {
	if c.Margin < 0 {
		return ErrConcludeMargin
	}
	if c.Deadline < 0 {
		return ErrConcludeDeadline
	}
	return nil
}
```

```go
// Bounds groups the loop's numeric caps. Zero means uncapped or
// serial, per the member's own doc comment.
type Bounds struct {
	// MaxIterations bounds the Completer-call count of one Run.
	MaxIterations int
	// MaxCallsPerTurn bounds one turn's model-requested tool calls.
	// Zero means unbounded.
	MaxCallsPerTurn int
	// MaxTotalTokens caps the run's cumulative billed tokens. Zero
	// means unbounded.
	MaxTotalTokens int
	// MaxConcurrentTools bounds one turn's parallel tool calls. Zero
	// and one both mean serial.
	MaxConcurrentTools int
	// MaxConsecutiveToolFailures bounds consecutive all-failing turns.
	// Zero means unbounded.
	MaxConsecutiveToolFailures int
}

// Validate checks the caps in a fixed order and returns the first
// failure: MaxIterations, MaxTotalTokens,
// MaxConcurrentTools, then MaxConsecutiveToolFailures, each not
// negative.
func (b Bounds) Validate() error {
	if b.MaxIterations < 0 {
		return ErrMaxIterations
	}
	if b.MaxTotalTokens < 0 {
		return ErrMaxTotalTokens
	}
	if b.MaxConcurrentTools < 0 {
		return ErrMaxConcurrentTools
	}
	if b.MaxConsecutiveToolFailures < 0 {
		return ErrMaxConsecutiveToolFailures
	}
	return nil
}
```

Placement decisions:

- `agentloop/options.go` sits at 470 lines. The swap of nine fields
  for two, plus two type blocks and two methods, would land near the
  500-line cap. The declarations move out instead. `stop.go` holds
  `StopReason` for the same reason; see its comment in
  `options.go:158-160`.
- `agentloop/bounds.go` is a new file in the same package. It needs no
  `policy/layers.json` row, because no import edge moves.
- `Options` gains two fields: `Bounds Bounds` where `MaxIterations`
  sat, and `Conclude Conclude` where `ConcludeMargin` sat.
- Both methods use value receivers, matching `Options.Validate`.
- New `Options.Validate` order: `ErrNoCompleter`, `ErrNoTools`,
  `o.Bounds.Validate()`, `ErrSessionIDRequired`, the `Budget` check,
  the `Window` block, `o.Conclude.Validate()`,
  `ErrHeartbeatRequiresBus`, `WorkBudget.validate`,
  `ToolBudget.validate`. Bounds first, then Conclude.
- The flat interleaving cannot survive the split exactly. State the
  consequence: `o.Bounds.Validate()` now runs right after the Tools
  check. `ErrMaxTotalTokens` therefore wins over an invalid Budget,
  where the flat order ran the Budget check first and reached
  MaxTotalTokens after it. `ErrMaxConcurrentTools` and
  `ErrMaxConsecutiveToolFailures` move
  with it: they used to run after the Window block, and they now
  return before the SessionID, Budget, and Window checks.
  `ErrConcludeMargin` keeps its place after the Window block.
- No test pins a cross-group first-error. Verified by reading
  `agentloop/agentloop_test/options_test.go`: every table row mutates
  exactly one field. The only order tests are
  `TestOptionsValidateHeartbeatOrder` (Completer before Heartbeat) and
  `TestOptionsValidateCompleterBeforeConclude` (Completer before
  ConcludeDeadline). Both stay green, because the Completer check
  stays first in `Options.Validate`.
- Rewrite the `Validate` doc comment at `options.go:399-409` to list
  the new order. Precedent: the doc-comment repair in the
  duplicate-conclude-term addendum.

### The StartTime decision

- Grep, run in the worktree:

```sh
grep -n 'StartTime' agentloop/*.go | grep -v _test
```

- Output: the declaration and comment at `options.go:264-271`, the
  comment at `loop.go:71-76` and `loop.go:174-176`, and one code
  consumer, `loop.go:150`:
  `deadlineAt: computeDeadlineAt(opts.StartTime, opts.ConcludeDeadline)`.
- `shouldConclude` at `conclude.go:37-47` reads `l.concludeMargin`,
  `l.maxIterations`, `l.deadlineAt`, and `l.concludeDeadline`. It does
  not read StartTime.
- StartTime is the deadline anchor, not part of the conclude decision.
  It stays flat on `Options`. It does not join `Conclude`.
- `New` keeps `computeDeadlineAt(opts.StartTime, opts.Conclude.Deadline)`.

### The Loop embed and New's transforms

- `Loop` replaces nine unexported fields with two, both by value:
  `conclude Conclude` and `bounds Bounds`.
- The nine today: `maxIterations`, `maxCallsPerTurn`, `maxTotalTokens`,
  `concludeMargin`, `concludeDeadline`, `concludeNotice`,
  `maxConcurrent`, `maxConsecutiveFailures`, `turnResultBudget`.
- `deadlineAt` stays its own field on `Loop`, with its comment.
- `New` must keep two transforms on its copies. Dropping either is a
  silent behavior change:
  - `bounds.MaxIterations = unboundedOrSet(bounds.MaxIterations)`:
    zero becomes `math.MaxInt32`.
  - `conclude.Notice = resolveConcludeNotice(conclude.Notice)`: empty
    becomes `DefaultConcludeNotice`.
- Concrete form: copy `opts.Bounds` and `opts.Conclude` into locals,
  apply the two transforms, then build the `&Loop{...}` literal from
  the locals.

### Sentinel preservation

- Six sentinels keep both name and wording byte-identical:
  `ErrMaxIterations`, `ErrMaxTotalTokens`, `ErrConcludeMargin`,
  `ErrConcludeDeadline`, `ErrMaxConcurrentTools`,
  `ErrMaxConsecutiveToolFailures`.
- The message `agentloop: ConcludeMargin must not be negative` stays,
  although the field path becomes `Conclude.Margin`. Only the field
  path that triggers each sentinel moves.

### The mechanical rename

Method:

- A one-off rewrite script runs over the exact token set, in two
  forms: the nine `Field:` literal keys and the nine `o.Field =`
  assignment forms. Never hand-edit the ~300 test literals.
- Literal form: the script merges same-group members within one
  `Options` literal into one composite literal. A literal setting
  `MaxIterations` and `MaxCallsPerTurn` gets one
  `Bounds: agentloop.Bounds{...}` key, not two. The merged key sits
  where the group's first member sat in that literal. Member renames
  apply inside it: `ConcludeMargin:` to `Margin:`,
  `ConcludeDeadline:` to `Deadline:`, `ConcludeNotice:` to `Notice:`.
- The merge is required, not cosmetic. 61 existing test literals set
  two or three members of one group. Wrap-per-occurrence would emit
  duplicate `Bounds:` or `Conclude:` keys there, a compile error.
  Grep-verified examples: `conclude_terms_test.go:196`, `:232`,
  `:298`, and `conclude_test.go:265`.
- Assignment form: `o.ConcludeMargin = -1` becomes
  `o.Conclude = agentloop.Conclude{Margin: -1}`. The same rule maps
  the six Bounds members onto `o.Bounds`. `options_test.go` carries
  all 18 assignment sites; grep finds none in the other test files.
- Six of the nine member names are unchanged, so the literal renames
  are the three conclude members only.
- The builder runs the script from outside the tree and deletes it
  after. It is not committed.
- Closing greps. Both must return zero hits:
  - The three renamed keys as whole words, everywhere in Go. The
    leading `\b` is load-bearing: without it the pattern matches
    inside `DefaultConcludeNotice:`, which a t.Fatalf message at
    `conclude_test.go:367` names and this change must not touch:

```sh
grep -rnE '\b(ConcludeMargin|ConcludeDeadline|ConcludeNotice):' \
  --include='*.go' . | grep -v '^\./\.claude/'
```

  - The nine names in assignment form, in the test trees. After the
    rewrite, grouped members sit inside `{...}` as keys, so no
    dotted assignment form survives:

```sh
grep -rnE '\.(MaxIterations|MaxCallsPerTurn|MaxTotalTokens|ConcludeMargin|ConcludeDeadline|ConcludeNotice|MaxConcurrentTools|MaxConsecutiveToolFailures)[[:space:]]*=' \
  agentloop/agentloop_test/ e2e/ --include='*.go'
```

Code scope, each site grep-verified in this worktree:

- `agentloop/options.go`: the struct fields and `Validate`.
- `agentloop/loop.go`: the `Loop` struct and `New`'s copy block.
- `agentloop/run.go`: six field reads, at lines 89, 156, 160, 238,
  268, and 285.
- `agentloop/toolcall.go`: four field reads, at lines 160, 174, 243,
  and 244.
- `agentloop/conclude.go`: two field reads, at lines 39 and 42.
- Discovery grep for the reads:

```sh
grep -rnE 'l\.(maxIterations|maxCallsPerTurn|maxTotalTokens|concludeMargin|concludeDeadline|concludeNotice|maxConcurrent|maxConsecutiveFailures|turnResultBudget)\b' agentloop/ --include='*.go' | grep -v _test
```

- `agentloop/loop.go` and `agentloop/toolcall.go` also match flat
  names in comments. `loop.go:71-76` names `opts.ConcludeDeadline`;
  `loop.go:174-178` names `ConcludeDeadline` four times, at lines
  174, 175, 176, and 178; `loop.go:208` names `l.maxIterations`;
  `toolcall.go:51`, `:55`, and `:60` name `l.turnResultBudget`;
  `toolcall.go:133-134` name `l.maxConcurrent`. `agentloop/budget.go`,
  `wire.go`, `tokens.go`, and `stop.go` match in comments only.
- Three more comment sites carry the old conclude keys in key form,
  and closing grep one still sees them after the rename clears the
  literal keys. The builder updates each:
  - `agentloop/conclude.go:32-34`: shouldConclude's doc comment
    labels its two OR-ed terms `- ConcludeMargin:` and
    `- ConcludeDeadline:`. The fields move, so the builder rewords
    both labels to the member names, `Margin` and `Deadline`, and
    updates the same comment's field paths to `bounds.MaxIterations`,
    `conclude.Margin`, and `conclude.Deadline`.
  - `agentloop/agentloop_test/conclude_terms_test.go:324`: the doc
    comment reads `ConcludeDeadline: 0`. The builder rewords it to
    the grouped form the rewritten literal sets.
  - `agentloop/agentloop_test/conclude_test.go:460`: the doc comment
    reads `a positive ConcludeMargin:`. The builder rewords it to the
    member name, `Margin`.
- The builder updates every other comment mention that names a moved
  field path, so `opts.ConcludeDeadline` reads
  `opts.Conclude.Deadline` and `l.maxIterations` reads
  `l.bounds.MaxIterations`. Prose mentions of the bound concepts
  stay.
- One hit needs no edit: `agentloop/agentloop_test/conclude_test.go:367`
  writes `want DefaultConcludeNotice:` inside a t.Fatalf message.
  That is assertion prose naming the unchanged DefaultConcludeNotice
  const. The builder does not touch it. The leading word boundary in
  closing grep one removes it from the match set.
- The discovery grep above doubles as the comment-aware closing
  check. After the rename and these comment updates it returns zero
  hits: none of the old path forms match, and the new forms
  (`l.bounds.MaxIterations`, `l.conclude.Margin`) match none of its
  alternatives.
- The test package `agentloop/agentloop_test/` holds 49 files with 322
  `Field:` literal references. The script covers every one. The test
  package never touches the unexported `Loop` fields; grep confirms
  zero hits.
- One file outside `agentloop`:
  `e2e/e2e_test/anthropic_compaction_test.go` line 128,
  `MaxIterations: 5,`.
- Docs: `docs/packages/agentloop.md` carries 48 flat-name lines. The
  builder moves live references to the grouped form. Sentinel message
  quotes stay byte-identical. `docs/architecture.md` lines 358-359
  name `MaxIterations`, `MaxCallsPerTurn`, and `MaxTotalTokens` in a
  bound list; that line names the `Bounds` group after the change.
- Historical plan text in `docs/history/agentloop.md` is not rewritten.
  This addendum is the only description of the new shape. Precedent:
  the ConcludeToolCallsLeft addendum kept historical mentions.

### The runState fold

- The caller gate above, at the ConcludeToolCallsLeft addendum, fires:
  this change touches `runIteration`'s body, so the fold ships now.
- New unexported struct in `agentloop/run.go`:

```go
// runState carries the six pointer parameters runIteration mutated
// in place. run constructs one per Run call and passes it down.
type runState struct {
	history             *[]provider.Message
	iterations          *int
	totalUsage          *provider.Usage
	runningTokens       *int
	consecutiveFailures *int
	noticeSent          *bool
}
```

- `run` allocates the six locals as today, builds one `*runState`,
  and passes it.
- `runIteration` drops from ten parameters to five: ctx, st, steer,
  stream, surface. Its body swaps `*history` for `*st.history` and so
  on. No behavior change.
- Decision: `afterChat` switches too. Its signature becomes ctx, at,
  st, noticeInRequest, surface. Five parameters replace nine. The body
  keeps the same statements; it loses no line and gains none. It stays
  well under the 80-line cap, so the fold is safe there.
- `runToolStage` keeps its signature. It takes snapshot values plus
  `*int`; its caller now passes `st.consecutiveFailures`. Nothing else
  about it changes.
- Scope stops here. `runChat`, `runToolCalls`, and the rest keep their
  signatures. `steer`, `stream`, and `surface` stay separate
  parameters; they are not per-run mutable state of the six-pointer
  kind.

### Part one: the composition example

File list:

- `docs/examples/_agentloop/main.go`, new, package main. The
  underscore prefix keeps `go build ./...` and `go vet ./...` blind to
  it. Explicit paths work on underscore directories.
- `docs/examples/agentloop.md`, new. It matches
  `docs/examples/agentrun.md`'s shape: intro prose, the
  `## The program` header, one fenced Go block, closing prose.
- `scripts/check_examples_sync.py`: append one PAIRS entry,
  `("docs/examples/agentloop.md", "## The program",
  "docs/examples/_agentloop/main.go")`. The fence must be
  byte-identical to main.go.
- `Makefile`: add one standing line to `verify-fast`:
  `go vet ./docs/examples/_agentloop/`. Decision: yes, add it.
  `go vet ./...` skips underscore directories, so without the explicit
  path the example rots silently at the next API change. That gap is
  what this slice closes. `docs/examples/_agentrun` has no Go test
  today; the byte gate is its only standing check. The vet line gives
  `_agentloop` the compile-level check the byte gate cannot. It costs
  sub-second time in the pre-commit hook.

main.go design:

- House style follows `docs/examples/_agentrun/main.go`: a
  `// Command agentloop` doc comment, small named builders, one
  `Options` literal in main.
- `cannedCompleter` (in-file) implements `provider.Completer`:
  `Name()` returns `canned`; `Chat` returns one scripted response per
  call, in order; `ChatStream` returns an error. Shape follows
  `agentloop/agentloop_test/helper_test.go`'s scripted completer.
- Scripted exchange, two turns, one tool call:
  - Turn one: a response carrying one `provider.ToolCall`, Name
    `upper`, Arguments `{"text":"hello"}`, with
    `Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}`.
  - Turn two: a response whose assistant `Message` carries the final
    text, with a `Usage` in the same scale.
  - The run ends with `StopNoToolCalls` after two iterations. The
    output is deterministic.
- `upperTool` implements `tools.Tool` and `tools.SchemaTool`: `Name`,
  `Run`, `ParameterSchema` returning a JSON object that requires the
  string property `text`, and `DecodeArguments` unmarshaling into a
  struct. `Run` uppercases the text. The scope must allow it, or
  `New` fails with `ErrNoSchemas`.
- `shoutTool` implements `tools.Tool` only: `Name` and `Run`. It shows
  the plain-tool path beside the schema tool. Registered and allowed,
  but not called by the script.
- An in-file `provider.TokenEstimator` returns a bytes-over-four
  count. It feeds `contextplan.Calibrate(est, 0.25)`.
- The `Options` literal sets every field this addendum touches:
  - `Completer`, `Tools`.
  - `Scope`: `tools.NewScope(tools.ScopeOptions{Allowlist:
    []string{"upper", "shout"}})`.
  - `Bounds`: `agentloop.Bounds{MaxIterations: 4, MaxCallsPerTurn: 4,
    MaxTotalTokens: 100000, MaxConcurrentTools: 2,
    MaxConsecutiveToolFailures: 2}`.
  - `Conclude`: `agentloop.Conclude{Margin: 1, Deadline: time.Minute,
    Notice: "Wrap up with your best answer now."}`.
  - `Window`: `&contextplan.Window{MaxTokens: 512,
    Compaction: contextplan.Compaction{TriggerPercent: 80,
    TargetPercent: 50}}`.
  - `Summarizer`: `contextplan.NewSummarizer(canned)`. It returns
    an error; main prints and returns on it.
  - `Calibrated`: `contextplan.Calibrate(est, 0.25)`.
  - `Tracer`: `trace.New()`.
  - `Hooks`: `events.New()` plus one handler at `events.PointPostTool`.
    The handler prints the payload and returns true, nil, so `Fire`
    continues.
  - `Usage`: `provider.New()`; `SessionID`: `agentloop-example`.
  - `WorkBudget`: `Reserve` and `Refund` both non-nil no-op closures.
    A half-wired budget fails `Validate`.
  - `ToolBudget`: `Reserve` non-nil.
  - `Audit`: an `AuditFunc` that prints the record's kind and
    iteration.
- main calls `loop.RunSteerable(ctx, msgs, steer)` with
  `steer := agentloop.NewSteer()`. No trigger fires; the call shows
  the steerable entry point.
- main prints `res.Stop`, `res.Iterations`, `res.Usage`, and
  `res.Final.Content`.

### Addendum tests

- No test is deleted, renamed, or skipped. Every test name and every
  assertion survives the mechanical rename.
- Predicted `scripts/check_test_tampering.py` findings: none. No test
  function is removed, so TT01 stays silent, and assertion counts do
  not decrease, so the aggregate rules stay silent. The builder does
  not add a trailer.
- Existing `Validate` rows keep their names and their sentinels.
  `o.ConcludeMargin = -1` becomes
  `o.Conclude = agentloop.Conclude{Margin: -1}`; the `wantErr` value
  is unchanged.
- One new test, `TestConcludeValidate`, lives in
  `agentloop/agentloop_test/options_test.go`. It calls
  `agentloop.Conclude.Validate` directly on a `Conclude` value. It
  does not route through `Options.Validate`. It follows the file's
  `validateCase` table style. Rows: a negative Margin fails with
  `agentloop.ErrConcludeMargin`; zero Margin passes; a negative
  Deadline fails with `agentloop.ErrConcludeDeadline`; zero Deadline
  passes; a valid group passes. Each failing row asserts with
  `errors.Is`, so the test pins that the grouped method returns the
  unchanged sentinels.
- `Bounds.Validate` gains no direct test. Its five invariants stay
  covered through the existing `Options.Validate` rows, which now
  reach them through the `Bounds.Validate` call.
- `go test -race -count=1 ./agentloop/... ./e2e/...` passes.

### policy/pending_wiring.json

- Rewrite only the agentloop row's reason. Keep `permanent: true` and
  the target line unchanged.
- Verified gap statuses behind the text: steering closed
  (`agentloop/steer.go` exists), dedup closed (`Options.DedupWithinTurn`
  in the live struct), work and tool budgets closed (`WorkBudget` and
  `ToolBudget` in the live struct), conclude closed
  (`agentloop/conclude.go`), injection-safe framing still plan-only
  (`docs/history/agents/phase82_injection_safe_framing.md` reads
  `Status: plan, not scheduled`).
- Full proposed reason text:

```json
"reason": "Model tool-calling loop, a composition package meant for an external agent implementation, not another SDK package. A 2026-08-20 gap analysis against the sibling consumer repo's internal/agent.Loop (the candidate first caller) found five gaps; four are now closed in code: steering (agentloop/steer.go), per-batch tool-result dedup (DedupWithinTurn), work and tool budgets (WorkBudget, ToolBudget), and graceful conclude (agentloop/conclude.go). One gap stays open: hook-injection-safe framing, still plan-only at docs/history/agents/phase82_injection_safe_framing.md. The docs/examples/_agentloop composition example is now the in-repo positive control: it wires every Options group offline, runs a scripted two-turn tool exchange through RunSteerable, and verify-fast vets it. agentloop in turn has Scope-gated schema validation, a documented Result-shape contract, structured Audit, Tracer spans, and an explicit ErrorPolicy switch that the sibling consumer repo lacks. Adoption needs the sibling consumer repo to build an adapter closing its side of that gap, not an SDK-internal caller."
```

- The provider/anthropic row already anticipates "the agentloop
  composition example". This change makes that true. Do not edit that
  row.

### Addendum API

- Run `make api-update`. Commit the `api/agentloop.txt` diff in the
  same change. Do not hand-edit the lock.
- The `Options` block loses the nine flat field lines and gains
  `Bounds Bounds` and `Conclude Conclude`.
- The lock gains two type blocks, `Conclude` and `Bounds`, with their
  member lines, plus two method lines,
  `func (b Bounds) Validate() (error)` and
  `func (c Conclude) Validate() (error)`.
- `scripts/check_docs.py` needs doc comments on `Conclude`, `Bounds`,
  and every exported field. Each starts with its own symbol name.
- `policy/layers.json` gets no change. No import edge moves.
  `agentloop/bounds.go` is a file, not a package.

### Addendum verification

- `go vet ./docs/examples/_agentloop/` passes, after part two lands.
- `go run ./docs/examples/_agentloop/` runs offline and prints the
  stop reason, the iteration count, the usage, and the final content.
  The builder captures both outputs as proof.
- `make verify` passes. `verify-fast` now carries the new explicit vet
  line, so the standing gates cover the example from then on.
- `python3 scripts/check_plan.py`, `scripts/check_prose.py`,
  `scripts/check_labels.py`, and `scripts/check_names.py` pass.
- The coverage floor of 85 holds for every package. The moved checks
  keep their tests.

## Addendum: audit an in-flight call the worker pool already ran when a veto lands
Status: shipped. This addendum fixes a defect commit e5e99e9 found and
closed: under `MaxConcurrentTools > 1`, a call claimed before another
call's veto was stored ran to completion, but the collect pass dropped
its result from history and its record from the audit trail.

### Addendum goal

Make `collectCalls` record every call the worker pool actually ran,
not only the calls before the index that short-circuits the batch. An
executed side effect must stay traceable even when a later index vetoes
or hard-fails first.

### Addendum scope

`collectCalls` (`agentloop/toolcall.go`) now calls the new
`recordRanOutcomes` helper on both short-circuit paths, `out.err !=
nil` and `out.veto`, before returning. `recordRanOutcomes` walks the
remaining plans and outcomes past the short-circuit index, skips a
duplicate plan and a zero-value outcome (a call the abort stopped
before it ran), and appends history and an `AuditKindToolCall` record
for every call that produced a real `msg`. `plans` and `outcomes` stay
aligned slices; the walk is order-preserving.

### Addendum proof

`agentloop/agentloop_test/veto_concurrent_test.go` holds a slow call in
flight until a second call's veto lands, then asserts the slow call's
result reaches `history` and its audit record exists. `go test -race
-count=1 ./agentloop/...` passes.

## Addendum: minimal entry surface

Status: shipped. The package gains `DefaultBounds` and
`EnableCompaction`, the two functions the raw entry path was missing.
The raw entry cost was already low, but no example showed it, and the
compaction triple (`Window`, `Summarizer`, `Calibrated`) demanded
three coupled fields from every caller.

### Addendum goal

Cut the smallest useful entry to five lines of wiring:
`anthropic.New`, `tools.New`, `Options{Completer, Tools, Bounds:
DefaultBounds()}`, `EnableCompaction`, `Run`.

### Addendum scope

Inside:

- `DefaultBounds() Bounds`: every cap set to a sensible production
  default (24 iterations, 8 calls per turn, 200k tokens, 4-way tool
  parallelism, 3-turn failure tripwire).
- `EnableCompaction(o *Options, completer provider.Completer, window
  contextplan.Window, alpha float64) error`: builds the
  `contextplan.Summarizer` from the completer, the
  `contextplan.Calibrated` from its `provider.TokenEstimator`
  capability, and sets all three `Options` fields. A completer
  without the estimator capability fails with `ErrNoTokenEstimator`
  and leaves `Options` untouched.
- `docs/examples/_agentloop_minimal`, a runnable five-line-wiring
  example; `verify-fast` runs it and asserts its output.

Outside:

- No new package. `contextplan` cannot host the helper: it imports
  `provider` alone, and the summarizer type lives in
  `contextsummary`. `agentloop` already imports both, so the helper
  lives here.
- No change to `Options.Validate` or any existing rule.

### Addendum tests

`agentloop_test/enable_compaction_test.go`:

TestEnableCompactionRequiresEstimator proves the no-estimator
failure and the untouched-Options guarantee.

TestEnableCompactionSetsTriple proves all three fields land
populated and the `Options` pass `Validate`.

TestDefaultBoundsPassesValidate pins `DefaultBounds` against
`Validate` and positive-cap drift.

### Verification

- `make verify` passes, including the API gate against the
  regenerated `api/agentloop.txt`.
- `verify-fast` runs `docs/examples/_agentloop_minimal` and asserts
  its final output.

## Addendum: widen the consecutive-tool-failure counter

Status: shipped.

### Addendum goal

`collectCalls` counted a failing dispatched call toward
`consecutiveFailures` only when its reported error was
`tools.ErrUnknownName`. An argument validation failure or a reported
tool execution error, both routed through the same
`ErrorPolicyReport` path, never counted. A model stuck retrying a
malformed-argument or always-failing tool call bypassed
`MaxConsecutiveToolFailures` entirely. Widen the counter to any
reported tool error.

### Addendum scope

Inside:

- `agentloop/toolcall.go`'s `collectCalls`: count `out.reported != nil`
  instead of `errors.Is(out.reported, tools.ErrUnknownName)`.
- `Bounds.MaxConsecutiveToolFailures`'s doc comment, updated to state
  the counted condition precisely.

Outside:

- The `ErrorPolicyFail` path: a hard tool error there short-circuits
  `Run` before reaching `collectCalls`'s failure count, unchanged.
- Any change to `MaxConsecutiveToolFailures`'s reset rule: a turn with
  at least one non-failing dispatched call still resets the counter.

### Addendum tests

`agentloop_test/repeated_tool_failures_test.go`:

`TestRepeatedToolFailuresExcludesArgValidationAndToolError` (name kept
for history; it now proves the opposite of what it once did) proves an
argument validation failure and a reported tool execution error, in
consecutive turns, trip `MaxConsecutiveToolFailures` and stop the run
with `StopRepeatedToolFailures`, exercising the full `Run` loop
through a scripted `Completer` and real tools rather than a synthetic
`Result` fed to a helper.

### Addendum verification

- `go test -race ./agentloop/...` passes.

## Addendum: capability derivation from the Completer

Status: shipped.

### Addendum goal

`New` (commit 157a978) derives two `Options` defaults from
`opts.Completer` capability interfaces, so a completer that reports
its own context window and reasoning policy needs no per-request
wiring for them. Document the derivation and its Trim exclusion,
which had no coverage in `docs/packages/agentloop.md` or this plan.

### Addendum scope

Inside:

- `deriveWindow` (`agentloop/adoption.go`): when `opts.Window` and
  `opts.Trim` are both nil and `opts.Summarizer` and `opts.Calibrated`
  are both set, `New` builds a default `contextplan.Window` from
  `provider.ContextAccountant.ContextWindow()` when the Completer
  implements that capability and reports a positive window.
  `MaxTokens` is the reported window, `Reserve` is one fifth of it,
  `Compaction` triggers at 80% and targets 50%.
- `deriveReasoningEffort` (`agentloop/adoption.go`): `New` reads
  `provider.ReasoningPolicy.ReasoningEffort()` as the loop's default
  reasoning effort when the Completer implements that capability.
- The Trim exclusion: derivation stands down whenever `opts.Trim` is
  set, because `Options.Validate` rejects `Window` and `Trim`
  together (`ErrTrimExcluded`); a derived Window must not manufacture
  that rejection. Commit c5696d9 added this exclusion, the
  `opts.Trim == nil` term in `agentloop/loop.go`'s `New`, and its
  covering case in `agentloop/capability_derivation_test.go`, fixing a
  gap in the capability-derivation commit above: derivation there had
  no Trim check, so a derived Window could manufacture
  `ErrTrimExcluded` for one commit before this fix landed.
- `docs/packages/agentloop.md`'s new "Capability derivation from the
  Completer" section.

Outside:

- Any change to `deriveWindow`, `deriveReasoningEffort`, or the Trim
  exclusion's behavior.
- Any new exported symbol.

### Addendum proof

- `agentloop/capability_derivation_test.go` exercises: a Completer
  implementing both capabilities derives `Window` and the default
  effort; a Completer implementing neither leaves both untouched; a
  Completer with `Trim` set does not derive `Window` even when
  `ContextAccountant` is implemented, proving the exclusion.
- `go test -race ./agentloop/...` passes.
- `python3 scripts/check_docs.py` and `python3 scripts/check_plan.py`
  pass with the new section and addendum in place.

## Addendum: summarizer interface, request observer, summary skip, and shape repair

Status: shipped.

Note: this addendum predates the `contextplan` to `context/plan`
rename (docs/history/context/plan.md). Every `contextsummary.` prose
reference below is the historical package name; the shipped code
already uses `plan.`, the current import name for `context/plan`.

### Addendum goal

Four changes land in one commit. `Options.Summarizer` becomes an
interface owned by `agentloop`. Compaction survives a summarizer that
declines to summarize. A host hook observes every request before the
`Completer` call runs. The loop repairs a shape-empty assistant turn
before planning or sending.

### Addendum scope

Inside:

- The `Summarizer` interface in `agentloop/options.go`. The
  `Options.Summarizer` field and the unexported `Loop.summarizer`
  field (`agentloop/loop.go`) change type to it.
  `*contextsummary.Summarizer` satisfies it, pinned by a compile-time
  assertion in `options.go`.
- Doc-comment rules for the interface, the field, and
  `EnableCompaction`: sanctioned constructors, the
  `contextsummary.ErrSummarySkipped` contract, and the typed-nil
  warning.
- `compactHistory` skip handling through a new `summarizeDropped`
  bool. See "Skip handling, exact" below.
- The `Options.ObserveRequest` hook and its `observeRequest` helper in
  `agentloop/budget.go`. See "Request observer, exact" below.
- New file `agentloop/shape.go`, matching the per-concern file style
  of `steer.go` and `budget.go`. See "Shape repair, exact" below.
- `docs/packages/agentloop.md` same-change updates. See
  "Same-commit bookkeeping" below.

Outside:

- `Options.Trim` is unchanged, including its `Window` exclusion
  (`ErrTrimExcluded`).
- No pre-compaction rewrite hook is added. No loop-path caller needs
  one. An ordered hook list would be speculative.
- `contextsummary` is unchanged: no signature change and no new
  exported symbol. `ErrSummarySkipped` already exists on main.
- `policy/layers.json` is unchanged. `agentloop` already imports
  `contextsummary`.
- `docs/architecture.md` is unchanged. The module-map bullet
  enumerates no exhaustive `Options` field list, and the package count
  does not change.
- The `afterChat` append and the `injectAfterSystem`, `injectNotice`,
  `splitSummary`, `preserveSummaryName`, `recoveryWindow`, and
  `checkCompactedBudget` bodies are unchanged.
- `docs/examples/_agentloop` and `_agentloop_adoption` compile
  unchanged. They assign a `*contextsummary.Summarizer`, a plain
  interface conversion.

### Addendum API

New interface, defined in `agentloop`:

```go
// Summarizer generates the summary one compaction requires. An
// implementation returns contextsummary.ErrSummarySkipped to decline
// summary generation; compactHistory then reuses the prior summary or
// proceeds without one. Build the field's value only through
// EnableCompaction or contextsummary.NewSummarizer. Warning: a typed
// nil (*contextsummary.Summarizer)(nil) stored in the field is not
// nil as an interface, so Validate's nil check passes and the first
// Summarize call panics.
type Summarizer interface {
	Summarize(ctx context.Context, msgs []provider.Message) (contextsummary.Summary, error)
}

// Compile-time proof, pinned in options.go under the interface.
var _ Summarizer = (*contextsummary.Summarizer)(nil)
```

New `Options` field:

```go
// ObserveRequest runs after reserveWork and before every
// Completer.Chat call, including the prompt-too-long recovery retry's
// call. A non-nil error fails the iteration before the call runs.
// This is not Options.Audit: Audit records after the fact and cannot
// fail a call. A nil hook is a no-op.
ObserveRequest func(ctx context.Context, req provider.Request) error
```

Changed field, same name, new type:

```go
// Summarizer runs the LLM summary every compaction requires.
// Required when Window is set. See the Summarizer interface for the
// sanctioned constructors and the typed-nil warning.
Summarizer Summarizer
```

`EnableCompaction` behavior is unchanged. Its assignment is a plain
interface conversion. `New`'s `deriveWindow` gate reads
`opts.Summarizer != nil` on the interface. The gate works unchanged.
`Options.Validate` keeps its check shape. `ErrSummarizerRequired` is
unchanged.

### Skip handling, exact

`summarizeDropped` (`agentloop/compaction.go`) returns
`(contextsummary.Summary, bool, error)`. The bool reports a skip.
`errors.Is(err, contextsummary.ErrSummarySkipped)` maps to
`(Summary{}, true, nil)`. Any other error keeps today's wrap:
`ErrCompactionFailed` over the sentinel. `compactHistory` branches on
the bool, after the existing `!res.Compacted && notice` early return.

- Skip with a prior summary held aside: re-inject the prior message
  unchanged, through `injectAfterSystem`, the fresh summary's
  placement, and set `injected`. No summarizer error surfaces.
- Skip with no prior, planning path: inject nothing. The dropped
  messages stay dropped. The run proceeds with the kept history.
- Skip with no prior, recovery path: `compactHistory` returns
  `(nil, false, nil)`. `recoverPromptTooLong` then returns the
  original `ErrPromptTooLong`, the same treatment as the existing
  `!compacted` case. No retry and no notice. A recovery that cannot
  produce or reuse any summary must not silently retry the same
  oversized prompt.
- Skip with a prior, recovery path: proceed. The notice is appended;
  messages were dropped, and the model must see the notice.
  `injectNotice` needs no change: the re-injected prior keeps
  `contextsummary.SummaryMessageName`, so the notice lands directly
  after it.
- Every skip path still passes `checkCompactedBudget` before
  `compactHistory` returns the rebuilt history.

### Request observer, exact

- `observeRequest` (`agentloop/budget.go`, beside `reserveWork`) is a
  nil-hook no-op.
- Call point one: `runChat` (`agentloop/run.go`), after
  `reserveWork`, immediately before `steerableChat`.
- Call point two: `recoverPromptTooLong`
  (`agentloop/compaction.go`), after its `reserveWork`, immediately
  before the retry's `Chat`.
- A non-nil error wraps as
  `agentloop: iteration %d: observe request: %w`, with the count the
  adjacent `reserveWork` call received (`iterations+1` in `runChat`,
  `iteration+1` in `recoverPromptTooLong`).
- Primary route: the attempt fails like a `reserveWork` error, a hard
  fail in `runIteration`.
- Recovery route: the error returns through
  `chatAttempt{err: rerr, fromRecovery: true}`, the `fromRecovery`
  route in `runIteration`.
- An observer error runs `refundWork` with zero `Usage` before the
  attempt error returns, on both routes. `Reserve` had succeeded, and
  the call never consumed the reservation.
- The doc comment states the `Options.Audit` distinction: `Audit`
  records after the fact and cannot fail a call.

### Shape repair, exact

- The invariant: the loop never carries a shape-empty assistant
  message into planning or a request.
- Shape-empty means `Role == provider.RoleAssistant`, `Content` blank
  after `TrimSpace`, zero `ToolCalls`, and zero `ReasoningBlocks`.
  The blank check matches the sibling predicate and `runIteration`'s
  own `StopEmptyResponse` check.
- `isEmptyAssistantTurn` mirrors a caller's provider adapter
  `DropEmptyAssistantTurns` predicate, adapted to this module's
  `provider.Message`: `ReasoningBlocks` here, `ReasoningContent`
  there. The doc comment cites the caller's adapter.
- `dropEmptyAssistantTurns` returns a filtered copy when any empty
  turn exists and the input unchanged otherwise, mirroring the
  sibling's `needsWork` fast path.
- Call point one: `run`, right after the msgs copy, so the
  caller-supplied initial history is repaired before the loop starts.
- Call point two: the top of `runIteration`, before `applyTrim`, so
  trim, planning, conclude, and rewrite detection see the filtered
  history as the baseline.
- The `afterChat` append of `resp.Message` is untouched. A graceful
  stop keeps today's shape: an empty assistant turn produced by the
  final turn stays in `Result.History`.
- The dropped turns carry no reasoning blocks by definition, so
  filtering never sets `DisableProviderReplay`. The filter runs before
  the `historyRewritten` comparisons, so the comparisons never see a
  pre-filter baseline.

### Effective thresholds for host-style configs

- `CompactTrigger` and `CompactTarget` price against `Budget`, never
  `MaxTokens` (`contextplan.Window.CompactTrigger`).
- `deriveWindow` sets `Reserve` to `MaxTokens` over five, so `Budget`
  is `MaxTokens` minus `MaxTokens/5`.
- A host-style trigger at 80% of `MaxTokens` is `TriggerPercent` 100:
  `Budget × 100%` is `4·MaxTokens/5`. The identity is exact when
  `MaxTokens` is a multiple of five, the host case. The derived
  `Reserve` floors otherwise. The zero `TriggerPercent` resolves to
  the same 100 through `DefaultTriggerPercent`.
- A host-style target at 50% of `MaxTokens` is `TargetTokens` set to
  `MaxTokens/2`. `TargetPercent` is relative to `Budget`, so no
  integer percent expresses `0.5·MaxTokens`: it is 62.5% of `Budget`.
  `MaxTokens/2` is below `Budget`, so `Window.Validate` accepts it.

### Growth conditions

- `ObserveRequest` gains a second consumer or is absorbed into
  `Audit`.
- `ErrSummarySkipped` gains a second producer or is inlined as adapter
  behavior.

### Same-commit bookkeeping

- `make api-update` runs with the code. `api/agentloop.txt` gains the
  `Summarizer` interface and the `ObserveRequest` field, and shows the
  changed `Summarizer Summarizer` field type. Land the diff in the
  same commit.
- `policy/pending_symbols.json`: remove the
  `contextsummary.ErrSummarySkipped` entry. This change is the
  consumer the entry was waiting for.
- `policy/layers.json` is unchanged.
- `docs/architecture.md` is unchanged.
- `docs/packages/agentloop.md` updates, pinned sites at the current
  line numbers:
  - Lines 14-23, the `Options` entry: add `ObserveRequest` to the
    field list. State that the `Summarizer` field's type is the
    `Summarizer` interface.
  - Lines 77-85, the `EnableCompaction` entry: name it and
    `contextsummary.NewSummarizer` as the only sanctioned
    constructors. Carry the typed-nil warning.
  - Lines 105-115, the `Options.Validate` entry: state the interface
    nil check and the typed-nil caveat.
  - Lines 179-188, the `ErrCompactionFailed` and
    `ErrSummarizerRequired` entries: cross-reference the skip rules.
  - Lines 202-240, "Context planning and prompt-too-long recovery":
    add the skip rules for the planning path and the recovery path.
  - Line 248, "Capability derivation from the Completer": the
    `opts.Summarizer != nil` gate reads an interface. The gate's
    behavior is unchanged.
  - New sections, "Request observer" and "Shape repair". The latter
    notes the `Result.History` rule for the final turn.

### Addendum tests

In `agentloop/agentloop_test/`. Skip tests use a fake implementing the
one-method interface directly. No `contextsummary` adapter is needed.
The existing fixtures keep compiling unchanged:
`newPlanningFixture` assigns a `*contextsummary.Summarizer` to the
interface field, a plain conversion.

- `TestCompactionSkipWithPriorReinjectsPrior` — a fake summarizer
  returns `contextsummary.ErrSummarySkipped`; a prior summary message
  sat in history. The prior is re-injected unchanged after the system
  message, no summarizer error surfaces, and the run proceeds.
- `TestCompactionSkipWithoutPriorDropsQuietly` — skip with no prior:
  the dropped messages stay dropped, no summary message is injected,
  and the run proceeds with the kept history.
- `TestRecoverySkipWithPriorRetriesWithNotice` — recovery path with a
  prior: the retry proceeds, and the notice sits directly after the
  re-injected prior.
- `TestRecoverySkipWithoutPriorReturnsOriginalErr` — recovery path
  with no prior: `Run` returns the original `provider.ErrPromptTooLong`
  under `errors.Is`, and the completer call count stays at one.
- `TestObserveRequestErrorFailsBeforeChat` — primary path: the
  observer errors, the completer call count is 0, and the error wraps
  with the iteration count and hard-fails. Recovery path: the observer
  errors on the retry, and the error returns through the
  `fromRecovery` route. With `WorkBudget` wired, `Refund` saw zero
  `Usage` on both routes.
- `TestObserveRequestSeesRetryRequest` — a completer that rejects once
  with `provider.ErrPromptTooLong`: the observer is called twice, and
  the second request carries the rebuilt history.
- `TestShapeRepairDropsEmptyTurnAtIterationStart` — an empty assistant
  turn in the middle of history is gone from the sent request. The
  reachable route: a `ContinueOnStop` continuation after
  `StopEmptyResponse`. `gracefulStop` appends the empty turn to
  history and continues, so the next iteration-top filter catches it.
- `TestShapeRepairFiltersInitialHistory` — an empty assistant turn in
  `Run`'s input never reaches the completer.
- `TestValidateSummarizerInterfaceNilChecks` — `Window` set with a nil
  `Summarizer` fails `ErrSummarizerRequired`. A typed nil
  `(*contextsummary.Summarizer)(nil)` passes `Validate`, documenting
  the warning.

### Addendum verification

- `make verify` passes, including the API gate against the
  regenerated `api/agentloop.txt` and the symbol-wiring gate against
  the removed `pending_symbols.json` entry.
- `go test -race ./agentloop/...` passes.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass.
- The coverage floor of 85 holds for `agentloop` and the total.

## Addendum: the Options and Extensions split

Status: shipped. The struct split, the Bounds defaulting, the
zero-Window derivation fix, the loud schema failure, and the API lock
refresh land as one change.

### Addendum goal

`Options` carried 27 fields. Nine of them, counted by field,
existed to mirror one external caller's legacy loop. The
`policy/pending_wiring.json` agentloop row names that gap analysis.
Several mirrored knobs, `DedupWithinTurn` and `Conclude` among them,
the consumer then declined to adopt. Peer SDKs carry less surface:
Anthropic's tool runner adds one field; eino's agent config carries
13 fields.

This addendum does four things:

- Moves the nine host-mirror fields behind one optional pointer,
  `Options.Extensions`. The main struct reads like the product
  surface it is.
- Folds `Window`, `Summarizer`, and `Calibrated` into one
  `Options.Compaction` value group.
- Applies `DefaultBounds()` at `New` when `Bounds` is the zero
  value, so a zero `Bounds` is no longer an unbounded run.
- Fails loudly when a registered tool publishes no schema.

Two fixes ride along. `EnableCompaction` now accepts a zero `Window`
and lets `New` derive it, so the helper composes with derivation.
`New`'s defaulting order is pinned in this addendum.

### The field map

Every one of today's 27 `Options` fields moves to exactly one place.
Fifteen stay on `Options`. Nine move to `Extensions`. Three fold into
the `Compaction` group.

| Today's field | Destination | Why, one line |
|---|---|---|
| `Completer` | `Options` | Required core. |
| `Tools` | `Options` | Required core. |
| `Scope` | `Options` | Core admission policy. |
| `Model` | `Options` | Core request field. |
| `Bounds` | `Options` | Numeric caps every user needs. |
| `OnToolError` | `Options` | The tool-failure policy switch. |
| `Hooks` | `Options` | Product observability surface. |
| `Tracer` | `Options` | Product observability surface. |
| `Usage` | `Options` | Product telemetry; pairs with `SessionID`. |
| `SessionID` | `Options` | Keys `Usage`. |
| `Bus` | `Options` | Product event surface. |
| `HeartbeatInterval` | `Options` | Pairs with `Bus`; the rule stays intra-struct. |
| `Budget` | `Options` | Context bounding every user needs. |
| `Trim` | `Options` | Context bounding; `Compaction.Window` excludes it. |
| `Audit` | `Options` | Product audit surface. |
| `Window` | `Options.Compaction.Window` | One third of the co-required planning triple. |
| `Summarizer` | `Options.Compaction.Summarizer` | One third of the co-required planning triple. |
| `Calibrated` | `Options.Compaction.Calibrated` | One third of the co-required planning triple. |
| `OnToolCallError` | `Extensions` | Host mirror: report-path response shaping. |
| `Surface` | `Extensions` | Host mirror: per-iteration tool rotation. |
| `StreamingWriter` | `Extensions` | Host mirror: steered-stop stream capture. |
| `Conclude` | `Extensions` | Host mirror wrap-up terms; the consumer leaves it unset. |
| `StartTime` | `Extensions` | Anchors `Conclude.Deadline`; moves with it. |
| `DedupWithinTurn` | `Extensions` | Host mirror; the consumer leaves it unset. |
| `WorkBudget` | `Extensions` | Host mirror: token reservation around `Chat`. |
| `ToolBudget` | `Extensions` | Host mirror: reservation before dispatch. |
| `ContinueOnStop` | `Extensions` | Host mirror; the host's wrap-up budget rides it. |

### Addendum scope

Inside:

- `agentloop/options.go`: the split struct, the `Compaction` field,
  the `Extensions` pointer, `Validate`, `ErrMaxIterations`'s doc
  comment, and the new `ErrNoSchema` sentinel.
- `agentloop/extensions.go`, a new file in the package: the
  `Extensions` type. Named by feature, not by process.
- `agentloop/compaction.go`: the `Compaction` type and
  `EnableCompaction`'s zero-Window rule.
- `agentloop/loop.go`: `New`'s order, the zero-Bounds defaulting,
  `unboundedOrSet`'s comment, and the nil-Extensions local.
- `agentloop/bounds.go`: the member comments that currently say zero
  means unbounded.
- `agentloop/definitions.go`: the loud schema failure.
- Every in-repo caller; see the migration list.

Outside:

- No new package. `policy/layers.json` needs no row; no import edge
  moves. No `policy/thirdparty.json` change.
- No change to `Loop`'s unexported fields. `New` still fills the same
  fields; only the read paths move to `opts.Extensions` and
  `opts.Compaction`.
- No code change to `spool`, `contextplan`, `contextsummary`,
  `contextbudget`, `tools`, `agentrun`, or `runconfig`. The spool
  change is comments and docs only; see the spool resolution.
- The external consumer's own migration to `Extensions`. That is the
  consumer repo's change. Only `policy/pending_wiring.json`'s agentloop
  reason text updates in this repo, so the row does not go stale.

### The new declarations

`agentloop/options.go` holds `Options` and `Validate`.
`agentloop/extensions.go` holds `Extensions`. `agentloop/compaction.go`
holds `Compaction` beside `EnableCompaction`. Field order is exactly
as below; the API lock mirrors it.

```go
// Options declares the blocks one New call wires into a Loop.
// Completer and Tools are required; the rest are optional. The host
// integration knobs live behind Extensions, the last field.
type Options struct {
	// Completer runs each chat turn. Required.
	Completer provider.Completer
	// Tools is the registry Definitions builds the offered tool set
	// from, and RunScoped resolves a model-chosen call against.
	// Required.
	Tools *tools.Registry
	// Scope narrows which tools a model-chosen call may invoke. Run
	// always calls Registry.RunScoped, never Registry.Run.
	Scope *tools.Scope
	// Model names the model Request.Model carries. An empty Model
	// means the Completer's own default.
	Model string
	// Bounds groups the loop's numeric caps. The zero value receives
	// DefaultBounds at New; see this addendum's defaulting rule.
	Bounds Bounds
	// OnToolError governs what Run does with a tool-run error.
	OnToolError ErrorPolicy
	// Hooks fires PointPreTool and PointPostTool per tool call, and
	// PointStop once at the end. Optional.
	Hooks *hooks.Registry
	// Tracer opens one span per iteration and one per tool call.
	// Optional.
	Tracer *trace.Tracer
	// Usage records per-iteration provider.Usage under SessionID.
	// Requires SessionID. Optional.
	Usage *usage.Accumulator
	// SessionID keys Usage's running total. Required when Usage is
	// set.
	SessionID string
	// Bus receives Run's iteration, heartbeat, and tool-call events.
	// Required when HeartbeatInterval is positive. Optional.
	Bus *events.Bus
	// HeartbeatInterval emits a heartbeat Event on Bus while one
	// Completer or tool call is in flight. Zero disables heartbeats.
	HeartbeatInterval time.Duration
	// Budget caps one Completer call's message history by byte count
	// and message count. A nil Budget means uncapped. When
	// Compaction.Window is set, Budget checks the compacted history.
	Budget *contextbudget.Limits
	// Trim runs before each Completer call on the full history. A nil
	// Trim passes history through unchanged. Trim and
	// Compaction.Window are mutually exclusive.
	Trim func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)
	// Audit receives one AuditRecord per audited event. A nil Audit
	// means Run performs no audit call. Optional.
	Audit AuditFunc
	// Compaction groups the context-window planning triple. The zero
	// value disables planning. See the Compaction type.
	Compaction Compaction
	// Extensions holds the host-integration knobs. A nil Extensions
	// means every knob at its zero value. Optional.
	Extensions *Extensions
}
```

```go
// Extensions groups the host-integration knobs one external loop
// mirror needs. Reached only through Options.Extensions; a nil
// pointer means every member at its zero value.
type Extensions struct {
	// OnToolCallError runs on the ErrorPolicyReport path after a
	// decodeAndRun or render failure. See ErrorFunc for the contract.
	OnToolCallError ErrorFunc
	// Surface, when non-nil, replaces the iteration's advertised
	// definitions, registry, and scope from iteration two onward.
	Surface func() *Surface
	// StreamingWriter, when non-nil, mirrors what the Completer
	// writes; on a Steered stop the buffered bytes become
	// Result.Final.Content. Must be safe for concurrent use.
	StreamingWriter io.Writer
	// Conclude groups the graceful-conclude terms; see the Conclude
	// type for Margin, Deadline, and Notice.
	Conclude Conclude
	// StartTime is the wall-clock anchor for Conclude.Deadline. Zero
	// falls back to the time of New.
	StartTime time.Time
	// DedupWithinTurn serves DuplicateCallNotice for a repeated
	// (tool, canonical-argument) call within one turn.
	DedupWithinTurn bool
	// WorkBudget, when non-nil, is the token-reservation surface the
	// loop invokes around each Completer call.
	WorkBudget *WorkBudget
	// ToolBudget, when non-nil, is the cumulative tool-call budget
	// invoked once per turn before dispatch.
	ToolBudget *ToolBudget
	// ContinueOnStop is consulted on every graceful stop; a non-empty
	// return continues the loop. See StopDecision.
	ContinueOnStop func(ctx context.Context, d StopDecision) []provider.Message
}
```

```go
// Compaction groups the context-window planning triple. The zero
// value disables planning; the loop then runs unplanned. Window nil
// with Summarizer and Calibrated set asks New to derive the window
// from the Completer's ContextAccountant capability.
type Compaction struct {
	// Window plans every iteration against a token budget. Nil asks
	// for derivation at New. Requires Summarizer and Calibrated, and
	// excludes Options.Trim.
	Window *contextplan.Window
	// Summarizer runs the LLM summary every compaction requires.
	// Required when Window is set. See the Summarizer interface for
	// the sanctioned constructors and the typed-nil warning.
	Summarizer Summarizer
	// Calibrated estimates tokens and receives one Observe call after
	// every Chat. Required when Window is set.
	Calibrated *contextplan.Calibrated
}
```

Allocation decisions, one line each:

- `Compaction` earns its keep: the three fields are co-required by
  `Validate`, excluded together with `Trim`, written together by
  `EnableCompaction`, and read together by `New`'s derivation. One
  named slot deletes a three-field coupling from the main struct and
  from every caller literal. It follows the shipped `Bounds` and
  `Conclude` value-group precedent. This is grouping, not framework.
- `HeartbeatInterval` stays on `Options`: `Bus` stays on `Options`,
  so `ErrHeartbeatRequiresBus` stays an intra-struct rule. It is
  heartbeat-addendum product surface, not a mirror knob.
- `StartTime` moves with `Conclude`: it anchors `Conclude.Deadline`,
  and the pair shares one struct.
- `Usage`, `SessionID`, `Budget`, and `Trim` stay: generic product
  surface, not mirror knobs.
- `Extensions` is a pointer so an unset group is visible at the call
  site: a nil pointer means every knob is off.
- New keeps an `ext := opts.Extensions` local, nil replaced by
  `&Extensions{}` once, after `Validate`. Every `Loop` field the
  group feeds reads through that local.

### Validate order after the change

`Options.Validate` checks in this fixed order and returns the first
failure:

1. `Completer` nil, `ErrNoCompleter`.
2. `Tools` nil, `ErrNoTools`.
3. `Bounds.Validate`: `MaxIterations`, `MaxTotalTokens`,
   `MaxCallsPerTurn`, `MaxConcurrentTools`,
   `MaxConsecutiveToolFailures`, each not negative.
4. `Usage` set with a blank `SessionID`, `ErrSessionIDRequired`.
5. `Budget` non-nil, wrapped `Budget.Validate`.
6. `Compaction.Window` non-nil: wrapped `Window.Validate`, then
   `ErrSummarizerRequired`, then `ErrEstimatorRequired`, then
   `ErrTrimExcluded` when `Trim` is also set.
7. `Extensions`, first block, skipped whole when the pointer is nil:
   `Conclude.Validate`.
8. `HeartbeatInterval` positive with nil `Bus`,
   `ErrHeartbeatRequiresBus`.
9. `Extensions`, second block, same nil guard:
   `WorkBudget.validate`, then `ToolBudget.validate`.

The split preserves today's precedence exactly: Conclude before
Heartbeat before WorkBudget and ToolBudget. `Validate` reads the
`Extensions` pointer twice for this; a nil pointer skips both blocks.
No first-error among today's rules changes.

`TestOptionsValidateHeartbeatOrder` stays green; its "runs last"
comment is reworded to "runs before the WorkBudget and ToolBudget
checks".

### New's defaulting order

`New` runs these steps in this order:

1. `opts.Validate()`.
2. `Definitions(opts.Tools, opts.Scope)`. A wrapped `ErrNoSchema`
   fails `New` here.
3. `compileSchemas(defs)`. `ErrInvalidSchema` unchanged.
4. Bounds defaulting on a copy: a fully zero `opts.Bounds` becomes
   `DefaultBounds()`; then `unboundedOrSet` maps a zero
   `MaxIterations` to `math.MaxInt32`.
5. Capability adoption: `deriveWindow` when `Compaction.Window` is
   nil, `Trim` is nil, and `Compaction.Summarizer` and
   `Compaction.Calibrated` are both set; then
   `deriveReasoningEffort`.
6. Conclude transforms: `resolveConcludeNotice`, then
   `computeDeadlineAt(ext.StartTime, ext.Conclude.Deadline)`.
7. Loop construction from the prepared locals.

Derivation moves after `compileSchemas` against today's order. That
is behavior-neutral: derivation reads only `Options` fields. Moving
it after the schema compile keeps one canonical pipeline: validate,
offer, compile, default, adopt, transform, build.

### The zero-Bounds defaulting rule

- Only the fully zero struct gets defaults: `opts.Bounds ==
  Bounds{}`. A partially set `Bounds`, any member non-zero, stays
  exactly as given.
- Zero members inside a partial `Bounds` keep their zero meanings.
  Zero `MaxIterations` is unbounded. Zero `MaxTotalTokens` is
  unbounded. Zero `MaxConsecutiveToolFailures` sets no tripwire.
- A caller wanting an unbounded `MaxIterations` sets
  `math.MaxInt32` explicitly.
- `unboundedOrSet` stays. Its comment drops the legacy-loop wording
  and states the rule above.
- `ErrMaxIterations`'s doc comment at `options.go` drops the
  zero-equals-unbounded promise. It states: negative fails
  `Validate`; zero inside a fully zero `Bounds` receives
  `DefaultBounds` at `New`; zero inside a partial `Bounds` stays
  unbounded.
- `bounds.go`'s member comments that say zero means unbounded gain
  the same pointer to the defaulting rule.
- Sixteen `Options` literals in the test tree omit `Bounds` today:
  `loop_wiring_test.go:20`, `streaming_partial_test.go:66`,
  `enable_compaction_test.go:60`, eight in `work_budget_test.go`, and
  five in `tool_budget_test.go`. None runs past the defaults of 24
  turns, 8 calls per turn, a 4-way pool, 3 failing turns, or 200k
  tokens. The builder runs the full suite to confirm.

### EnableCompaction's new contract

The signature is unchanged:

```go
func EnableCompaction(o *Options, completer provider.Completer,
	window contextplan.Window, alpha float64) error
```

- The fallible steps run first and unchanged: the
  `provider.TokenEstimator` assertion fails with
  `ErrNoTokenEstimator`, and `contextsummary.NewSummarizer` may fail.
  `Options` stay untouched on either failure.
- New Window rule: when `window.MaxTokens > 0`, `EnableCompaction`
  copies the window into `o.Compaction.Window`. Otherwise it leaves
  `o.Compaction.Window` nil. A negative `MaxTokens` takes the same
  derive path as zero, not an error.
- Rationale: `contextplan.Window.Validate` rejects `MaxTokens <= 0`,
  so such a value can never be an explicit window. It can only mean
  "derive".
- Accepted consequence: today `New` fails a negative `MaxTokens` with
  a wrap of `contextplan.ErrMaxTokensNotPositive`. This change loses
  that failure for the `EnableCompaction` path, on purpose. There is
  no valid configuration to protect: `MaxTokens <= 0` is never a
  usable explicit window, so derivation is the only sensible reading.
- `EnableCompaction` then writes `o.Compaction.Summarizer` and
  `o.Compaction.Calibrated` as today.
- `New`'s derivation then supplies a window at 80 percent trigger
  and 50 percent target of the Completer's `ContextWindow`, with one
  fifth held back as reserve, when `Trim` is unset.
- Corner: a caller setting only `TriggerPercent` gets the derived
  default 80/50, not its own percent. A caller wanting custom
  percents sets `MaxTokens`.
- Zero Window with `Trim` set: `Validate` stays silent, because the
  Window block guards on a non-nil `Compaction.Window`, and
  derivation stands down on `Trim`. The wired pair goes unused, the
  same outcome today's shape produces.
- The `docs/examples/_agentloop_minimal` example becomes the
  positive control for the derivation path.

### Definitions' new error contract

- One new sentinel:

```go
// ErrNoSchema is Definitions's error when a registered tool
// publishes no parameter schema. Wrapped with the tool's registry
// name. Test with errors.Is.
ErrNoSchema = errors.New("agentloop: registered tool publishes no parameter schema")
```

- The loop checks `tools.SchemaOf` first, scope second. A not-ok
  `SchemaOf` returns immediately:

```go
schema, ok := tools.SchemaOf(t)
if !ok {
	return nil, fmt.Errorf("agentloop: tool %q: %w", t.Name(), ErrNoSchema)
}
```

- A scope-denied tool keeps the silent skip. Denial is policy
  filtering, not a mistake.
- `ErrNoSchemas` stays, with narrowed wording: a non-empty registry
  whose offered set ends empty now means the scope denied every
  tool. Every schema-less cause is unreachable past `ErrNoSchema`.
- `New` inherits the failure. `Definitions` runs before
  `compileSchemas`, so the loud error precedes schema compilation.
- `api/agentloop.txt` gains the `ErrNoSchema` const line.

### The spool resolution

Chosen: spool's contract and docs change to match the loud failure.
`SpoolTool` stays permissive over a schema-less inner.

- `spool` imports `tools` only and stays agentloop-blind. A
  schema-less tool is valid registry surface elsewhere: the e2e
  spool test drives a schema-less `SpoolTool` through `agentrun`
  today.
- The spool parity suite treats the schema-less inner as a
  first-class case. Construction-time rejection would delete
  behavior and weaken tests.
- The loud failure names the wrapper at `New`, the layer that owns
  the policy. Diagnosis quality is equal.
- `memory/tool.go`'s `SpoolTool` doc comment changes its closing
  sentences: `tools.SchemaOf` fails closed, a schema-less inner
  reports nil and false, and `agentloop.New` then fails with
  `ErrNoSchema` naming the wrapper. Wrap schema-bearing inners for
  model-facing registries.
- `memory/memory_test/tool_parity_test.go`'s header comment states the
  same consequence.
- `docs/history/spool.md` (spool's plan, now the memory package's SpoolTool section)'s Schema forwarding section drops its
  silent-skip sentences and states the loud contract.
- Tests asserting `tools.SchemaOf(wrapper)` reports nil and false
  stay unchanged. They pin `tools.SchemaOf`'s fail-closed property,
  which survives this change.
- No spool code change. No `policy/layers.json` change.

### Migration list

Package code:

- `agentloop/options.go`, `agentloop/extensions.go`,
  `agentloop/compaction.go`, `agentloop/loop.go`,
  `agentloop/bounds.go`, `agentloop/definitions.go`.
- Comment sweeps in `agentloop/stop.go` (`Options.ContinueOnStop`)
  and `agentloop/surface.go` (`Options.Surface`) to the `Extensions`
  paths. A grep over the twelve moved names sweeps any other comment
  site in the package.

Test files, mechanical `Extensions` literal moves:

- `on_tool_call_error_test.go` and `tool_call_context_test.go`
  (`OnToolCallError`).
- `surface_rotation_test.go` (`Surface`).
- `streaming_partial_test.go` (`StreamingWriter`).
- `conclude_test.go`, `conclude_terms_test.go`,
  `continue_on_stop_test.go`, `continue_on_stop_bounds_test.go`
  (`Conclude`, `StartTime`, `ContinueOnStop`).
- `dedup_test.go`, `dedup_canonicalize_test.go`,
  `batch_order_test.go`, `parallel_tools_test.go`,
  `steer_injector_review_test.go` (`DedupWithinTurn`).
- `work_budget_test.go` (`WorkBudget`).
- `tool_budget_test.go` (`ToolBudget`).

Test files, mechanical `Compaction` literal moves:

- `compaction_test.go`, `compaction_budget_test.go`,
  `compaction_estimator_test.go`, `compaction_recovery_test.go`,
  `compaction_reentry_test.go`, `enable_compaction_test.go`,
  `events_bridge_test.go`, `heartbeat_iteration_test.go`,
  `observe_pairing_test.go`, `steer_recovery_test.go`,
  `work_budget_test.go`, and the internal
  `capability_derivation_test.go`.

Test files, contract changes:

- `definitions_test.go`, `loop_wiring_test.go`, `options_test.go`,
  and `unbounded_max_iterations_test.go`; see the tests section.

Method: the Bounds-and-Conclude addendum's rename method, reused.
A one-off script rewrites the nine moved literal keys and their
assignment forms into `Extensions: agentloop.Extensions{...}` and
the three triple keys into `Compaction: agentloop.Compaction{...}`,
merging same-destination members into one key per literal. The
builder deletes the script; it is not committed. Two closing greps
cover both straggler classes, assignment and composite-literal key:

```sh
grep -rnE '\b(Window|Summarizer|Calibrated)([[:space:]]*=[^=]|:)' \
  agentloop/agentloop_test/ docs/examples/ e2e/ --include='*.go'
grep -rnE '\b(OnToolCallError|Surface|StreamingWriter|StartTime|DedupWithinTurn|WorkBudget|ToolBudget|ContinueOnStop)([[:space:]]*=[^=]|:)' \
  agentloop/agentloop_test/ docs/examples/ e2e/ --include='*.go'
```

These greps keep hits after the migration: every member key inside
the new `Extensions{...}` and `Compaction{...}` literals matches.
The builder reads each hit and confirms it sits inside one of those
literals. A hit naming a key on an `Options` literal is a straggler.
Compile is the final arbiter: a straggler key fails to build.

Examples and e2e:

- `docs/examples/_agentloop/main.go`: drop `shoutTool` and the
  `shout` allowlist entry. Every registered tool must publish a
  schema now, and the example says so in a comment. Move
  `DedupWithinTurn`, `StartTime`, `Conclude`, `WorkBudget`, and
  `ToolBudget` into one `Extensions` literal. Move the planning
  triple into `Compaction`. Output stays `final: HELLO`. Update
  `docs/examples/agentloop.md`'s fence byte-identically;
  `check_examples_sync.py` enforces the pair.
- `docs/examples/agentloop.md`: rewrite the post-fence prose. Drop
  the `shout` "implements `tools.Tool` only, so the offered set skips
  it" explanation; state the loud `ErrNoSchema` failure instead. The
  sync gate byte-compares only the fence, so this prose has no
  standing check and needs this explicit edit.
- `docs/examples/_agentloop_adoption/main.go`: move `DedupWithinTurn`,
  `Conclude`, and `StartTime` into `Extensions`; the triple into
  `Compaction`. Update the header's row list and the per-row
  comments. This stays the adopted-row positive control. Output
  stays `final: HELLO`.
- `docs/examples/_agentloop_minimal/main.go`: pass
  `contextplan.Window{}` to `EnableCompaction` and give the canned
  completer a `ContextWindow() int` returning 2048. The example
  becomes the derivation positive control: `New` derives 2048 with a
  409 reserve at 80/50. Output stays `final: HELLO`.
- `internal/e2e/e2e_test/anthropic_compaction_test.go`: regroup the triple
  into one `Compaction` literal. No behavior change.

Docs:

- `docs/packages/agentloop.md`: sweep the moved field names to their
  new paths; add `Extensions` and `Compaction` to the Types section;
  restate `EnableCompaction`'s contract, the zero-Bounds defaulting,
  and the loud `Definitions` failure. The failure-modes section
  gains `ErrNoSchema`.
- `docs/architecture.md`: grep the twelve moved names, the nine
  Extensions fields plus `Window`, `Summarizer`, and `Calibrated`;
  update every hit, including line 367's "A non-nil `Options.Window`
  plans every iteration". The package count is unchanged, so the
  opening paragraph stands.
- `docs/history/spool.md` (spool's plan, now the memory package's SpoolTool section): the Schema forwarding section, per the
  spool resolution. Historical sections already marked superseded
  stay untouched.

Policy:

- `policy/pending_wiring.json`: rewrite the agentloop row's reason
  text to describe the `Extensions` split and the consumer's
  outstanding migration. Keep `permanent: true` and the target line.

### Addendum tests

New tests, exact names:

- `TestNewDefaultsFullyZeroBounds` in
  `agentloop_test/loop_bounds_test.go`. `Options` without `Bounds`;
  a scripted completer that always requests one tool call. `Run`
  stops with `StopMaxIterations` at exactly 24 iterations, proving
  `DefaultBounds` landed.
- `TestNewKeepsPartialBoundsAsGiven` in the same file. Three rows.
  `Bounds{MaxIterations: 2}` stops at two, not 24.
  `Bounds{MaxTotalTokens: 10}` with usage over the cap fails with
  `ErrTokenBudgetExceeded`, proving the 200k default did not land.
  `Bounds{MaxIterations: 0, MaxCallsPerTurn: 1}` with 25 scripted
  tool-calling turns runs past 24 iterations, proving a zero member
  inside a partial `Bounds` stays unbounded.
- `TestEnableCompactionZeroWindowLeavesWindowNil` in
  `agentloop_test/enable_compaction_test.go`. Two rows. A zero
  `contextplan.Window` leaves `Compaction.Window` nil while
  `Summarizer` and `Calibrated` land, and the `Options` pass
  `Validate`. A negative `MaxTokens` row proves the same derive path.
- `TestEnableCompactionExplicitWindowStillWins` in the same file. An
  explicit 512-token window lands as given; derivation does not
  replace it.
- `TestEnableCompactionZeroWindowAdoptsDerivedWindow` in the internal
  `capability_derivation_test.go`. `EnableCompaction` with a zero
  window, then `New` over a `ContextAccountant` completer reporting
  10000: the loop's window is 10000 with a 2000 reserve and a 6400
  trigger.
- `TestDefinitionsRejectsSchemaFreeToolNames` in
  `agentloop_test/definitions_test.go`. A mixed registry fails with
  `errors.Is(err, ErrNoSchema)`, a message naming the schema-free
  tool, and a nil definition set.
- `TestExtensionsNilBehavesAsZero` in `agentloop_test/options_test.go`.
  A valid `Options` without `Extensions` passes `Validate` and builds
  through `New`. The same `Options` with
  `Extensions{Conclude: {Margin: -1}}` fails with
  `ErrConcludeMargin`, proving the walk reads the pointer.

Changed tests, names kept, bodies flipped; the name-kept precedent is
`TestRepeatedToolFailuresExcludesArgValidationAndToolError`:

- `TestDefinitionsSkipsSchemaFreeTools` now proves the mixed registry
  fails with `ErrNoSchema`.
- `TestDefinitionsSkipsNilSchemaSchemaTool` now expects
  `ErrNoSchema` and requires the tool's name in the message. The
  old not-named assertion flips.
- `TestDefinitionsErrNoSchemasEveryToolMissingSchema` now expects
  `ErrNoSchema` naming the first tool in sorted order.
- `TestDefinitionsScopeDenial`,
  `TestDefinitionsErrNoSchemasScopeDeniesEveryTool`, and
  `TestDefinitionsEmptyRegistry` stay unchanged: scope denial keeps
  the silent skip, and a fully denied registry keeps `ErrNoSchemas`.
- `TestNewPropagatesDefinitionsError` in `loop_wiring_test.go` flips
  to `errors.Is(err, agentloop.ErrNoSchema)`.
- `TestRunHallucinatedSchemaFreeToolName` keeps its name and now
  proves `New` fails with `ErrNoSchema` naming `no-schema` before
  `Run`. The runtime hallucinated-name case stays covered by
  `unknown_tool_error_test.go` for unregistered names.
- `TestSDKValidateAllowsUnboundedMaxIterations` stays green, but the
  pin changes. Its `Bounds{MaxIterations: 0}` literal is the fully
  zero struct, so `New` now applies `DefaultBounds` there; the test
  pins the defaulting path, not uncapped zero. Its legacy-contract
  comment is reworded to say `New` accepts a fully zero `Bounds` by
  applying defaults. Partial-`Bounds` uncapped coverage lives in
  `TestNewKeepsPartialBoundsAsGiven` row three.
- `options_test.go`'s Conclude and ToolBudget rows move under
  `Extensions`; every sentinel stays.
- Every mechanical move keeps its test name and assertion count.
  No test is deleted or skipped. Predicted
  `check_test_tampering.py` findings: none. If a TT finding fires
  anyway, the builder adds one `Allow-Test-Change: TTxx <reason>`
  trailer per finding.

### Statements this addendum supersedes

- The base API section's `Definitions` bullet and sentinel list:
  rewritten in place, this change.
- The Scope section's skip bullet: rewritten in place, this change.
- The ErrMaxIterations zero-equals-unbounded sentence in the base
  API section and in `options.go`: superseded by the defaulting
  rule. Historical plan sentences stay as records.
- The Conclude-and-Bounds addendum's "New's transforms" wording
  about the legacy contract: superseded by the defaulting rule.
- The minimal-entry-surface addendum's "sets all three Options
  fields": superseded by the zero-Window contract.
- The loop-extensions addendum's `Options` field list for
  `OnToolCallError`, `Surface`, `StreamingWriter`, `StartTime`,
  `WorkBudget`, and `ToolBudget`: the fields now live on
  `Extensions`.
- The stop-decision hook addendum's `Options.ContinueOnStop`, the
  conclude addendum's `Options.Conclude`, the dedup addendum's
  `Options.DedupWithinTurn`, and the context-planning addendum's
  flat triple: all regrouped per the field map.
- `memory/tool.go`'s skip-reliance sentences and `docs/history/spool.md` (spool's plan, now the memory package's SpoolTool section)'s
  Schema forwarding text: superseded by the spool resolution.
- The capability-derivation addendum stays accurate. Its derivation
  condition is this addendum's step 5, restated with member paths.

### Addendum verification

- `make verify` passes: gofmt, vet, tests, the doc, plan, API, deps,
  orphan, symbol-wiring, mutation, thirdparty, and test-tampering
  gates, the Semgrep scan and probes, and the 85 percent coverage
  floor for every package.
- `go test -race -count=1 ./agentloop/...` passes. The agentloop
  mutation floor of 98 holds; every new branch above has a covering
  test.
- `go vet` and `go run` pass for all three `docs/examples/_agentloop*`
  programs through the `verify-fast` lines, each printing
  `final: HELLO`.
- Exactly one `make api-update` runs at the end. The
  `api/agentloop.txt` diff lands in the same change: `Options`
  shrinks to 17 fields, `Extensions` and `Compaction` gain type
  blocks, and `ErrNoSchema` joins the const list.
- `python3 scripts/check_plan.py`, `scripts/check_prose.py`,
  `scripts/check_labels.py`, and `scripts/check_names.py` pass.
- No conformance vector applies; `agentloop` carries no wire format
  of its own.

## Addendum: reject a typed-nil Summarizer in Validate

Status: shipped.

Note: this addendum was written and landed against the pre-split
field path `Options.Summarizer`; the Options and Extensions split
addendum above renamed it to `Options.Compaction.Summarizer` in the
same merge. Every bullet below uses the post-split path.

### Addendum goal

Close the typed-nil gap the summarizer-interface addendum documented
as a warning. `Options.Validate` now rejects `(*plan.Summarizer)(nil)`
the same way it rejects an untyped nil `Summarizer`.

### Addendum bug

A caller who assigns a typed nil, `var s *plan.Summarizer; opts.Summarizer
= s`, produces a non-nil `Summarizer` interface value: the interface
carries a type and a nil pointer, so `opts.Summarizer == nil` is
false. Before this addendum, `Validate` passed such an `Options`
through, and the first `Summarize` call inside `compactHistory` ran a
method on a nil receiver and panicked. `EnableCompaction` and
`plan.NewSummarizer`, the two sanctioned constructors, never produce
this value; only a caller who hand-builds the field can hit it. AGENTS.md
requires an invariant a comment states to live in `Validate`, not the
comment alone; the prior text stated the panic as a documented warning
with no enforcement, which is exactly that gap.

### Addendum decision: a type assertion, not reflection or a documentation-only warning

Three options were weighed, mirroring the shape of the nil-schema-lookup
addendum decision above.

- Option A: reflect over the interface value inside `Validate` and
  reject any nil pointer, whatever the concrete type.
- Option B (chosen): assert the field against the one sanctioned
  concrete type, `*plan.Summarizer`, and reject a nil pointer of that
  type.
- Option C: keep the warning as prose only, and leave the panic
  reachable from a hand-built `Options`.

Option A was tried first and reverted: `semgrep/sdk-standards.yml`'s
`sdk.go.no-reflection-in-packages` rule blocks every `reflect.*` call
outside a `_test.go` file, unconditionally, and `make verify` failed
on it. `agentloop`'s own context-planning addendum already states the
module's convention this rule enforces: "reflection in `agentloop` is
forbidden by this module's no-third-party, no-reflection-mapping
convention." Option C repeats the gap AGENTS.md flags: a comment
stating a rule with no `Validate` enforcement.

Option B is chosen. `EnableCompaction` and `plan.NewSummarizer` are
the interface's only two sanctioned constructors, and both return
`*plan.Summarizer`; the typed-nil footgun is reachable only by
hand-assigning that one concrete type instead of using a constructor.
A direct type assertion catches exactly that footgun with no
reflection, at the cost of not catching a nil pointer of some other,
unsanctioned `Summarizer` implementation the module cannot name. The
doc comment states this narrower scope explicitly, so the check's
limit is not overclaimed.

### Addendum scope

Inside:

- `Options.Validate`'s `Compaction.Window` branch: after the
  existing `o.Compaction.Summarizer == nil` check, a new check
  asserts `p, ok := o.Compaction.Summarizer.(*plan.Summarizer); ok &&
  p == nil`, still returning `ErrSummarizerRequired`.
- The `Summarizer` interface doc comment (`agentloop/options.go`)
  states the type-assertion check and its narrower scope: it catches
  a nil `*plan.Summarizer`, not a nil pointer of an arbitrary custom
  `Summarizer` implementation.
- `TestValidateSummarizerInterfaceNilChecks`
  (`agentloop/agentloop_test/compaction_skip_test.go`) flips its typed-nil
  case from asserting `Validate() == nil` to asserting
  `errors.Is(err, agentloop.ErrSummarizerRequired)`.
- Every prior reference to the typed-nil case "passing" `Validate", in
  this file and in `docs/packages/agentloop.md`, is superseded by this
  section: a nil `*plan.Summarizer` now fails `Validate` with
  `ErrSummarizerRequired`.

Outside:

- `ErrSummarizerRequired` itself: unchanged sentinel, unchanged text.
- No change to `EnableCompaction` or `plan.NewSummarizer`; neither
  constructor could produce a typed nil before, and neither can now.
- No API surface change: `Validate`'s signature and documented
  first-failure ordering are unchanged (the `Compaction.Window`
  branch's internal check order is not part of the locked API).
- A nil pointer of a custom `Summarizer` implementation outside
  `*plan.Summarizer`: not caught, and not claimed to be. See the
  decision above.

### Addendum tests

In `agentloop/agentloop_test/compaction_skip_test.go`:

- `TestValidateSummarizerInterfaceNilChecks` — updated: a typed nil
  `(*plan.Summarizer)(nil)` now fails `Validate` with
  `errors.Is(err, agentloop.ErrSummarizerRequired)`, the same
  assertion already used for the untyped-nil case just above it.
- `TestCompactionSkipWrappedSentinelStillSkips` — a summarizer that
  wraps `plan.ErrSummarySkipped` with a reason
  (`fmt.Errorf("%w: %s", ...)`) still takes the skip path: `Run`
  proceeds with no surfaced error and the dropped message stays
  dropped, pinning the wrapped-sentinel contract the sentinel's own
  doc comment now states.
- `TestHostStyleWindowMapping` — `TriggerPercent: 100` with
  `TargetTokens: MaxTokens/2` reaches an exact 80%/50% trigger and
  target of `MaxTokens`, the parity anchor for a host wiring its own
  window to a literal percent of its context ceiling, not the
  effective 64%/40% `deriveWindow` reaches with its own 80/50 pair.

In `agentloop/capability_derivation_test.go`:

- `TestDeriveWindowEffectiveThresholds` — for `MaxTokens` 1000 and
  1003, `deriveWindow`'s `CompactTrigger()` and `CompactTarget()`
  equal the floored effective 64% and 40% of `MaxTokens`, pinning the
  percent math the doc and comment fixes above state.

### Addendum verification

- `make verify` passes.
- `go test -race ./agentloop/...` passes.
- `python3 scripts/check_prose.py` passes.
- No `api/agentloop.txt` diff: the new check is inline inside
  `Validate`, and `Validate`'s exported signature is unchanged.

## Addendum: collapse validation sentinels, fix Definitions order

Status: shipped

### Goal

`Options.Validate`, `Bounds.Validate`, and `Conclude.Validate`
returned 14 distinct sentinels for a shape error: an unset required
field, or a numeric field outside its range. A caller cannot branch on
these usefully; each one means "fix the Options value." `Definitions`
also checked a tool's schema before its scope, so a schema-less tool
the scope excluded still failed `New`, contradicting `New`'s own doc
comment.

### Sentinel table

One new sentinel, `ErrInvalidOptions`, replaces the 14 deleted ones:
`ErrNoCompleter`, `ErrNoTools`, `ErrMaxIterations`, `ErrMaxTotalTokens`,
`ErrMaxCallsPerTurn`, `ErrMaxConcurrentTools`,
`ErrMaxConsecutiveToolFailures`, `ErrSessionIDRequired`,
`ErrSummarizerRequired`, `ErrEstimatorRequired`, `ErrTrimExcluded`,
`ErrConcludeMargin`, `ErrConcludeDeadline`, `ErrHeartbeatRequiresBus`.
The wrapped message names the field and the rule, for example
`"Completer: required"` or `"HeartbeatInterval: requires a non-nil
Bus"`. Every remaining sentinel keeps a return site outside these
three `Validate` methods and stays unchanged. This supersedes the
typed-nil-Summarizer addendum above: its `ErrSummarizerRequired`
return site now returns `ErrInvalidOptions` naming `Summarizer`.

### Definitions order

`Definitions` checked `tools.SchemaOf` before `scope.Allowed`.
Reordered: `scope.Allowed` now runs first, so a scope-denied tool
never reaches the schema check. `ErrNoSchema`'s doc comment now says
"scope-allowed registered tool," matching the code.

### Tests

Every test asserting a deleted sentinel now asserts
`errors.Is(err, ErrInvalidOptions)` plus `strings.Contains(err.Error(),
"<Field>")`, in `options_test.go`, `compaction_test.go`,
`compaction_skip_test.go`, `repeated_tool_failures_test.go`, and
`loop_wiring_test.go`. No test function was deleted. Two new tests in
`definitions_test.go`, `TestDefinitionsSkipsScopeDeniedSchemaFreeTool`
and `TestNewSucceedsWithScopeDeniedSchemaFreeTool`, cover a
schema-less tool a scope excludes.

### Verification

- `go build ./...` and `go vet ./...` pass.
- `go test -race ./agentloop/... ./internal/e2e/...` passes.
- `make api-update` ran once; the `api/agentloop.txt` diff drops 14
  sentinel lines and adds `ErrInvalidOptions`.
- `python3 scripts/check_symbol_wiring.py` passes with no new entry
  needed in `policy/pending_symbols.json`.

## Addendum: typed-nil Summarizer check runs unconditionally

Status: shipped

### Goal

Close a gap the typed-nil-Summarizer addendum above left open: its
check ran only inside `Validate`'s `Compaction.Window != nil` branch,
so a caller who left `Window` nil could still panic.

### Bug

`New` derives a `Window` from the Completer's `ContextAccountant`
capability whenever `Window` is nil, `Trim` is nil, and `Summarizer`
and `Calibrated` are both non-nil interfaces. A typed nil
`(*plan.Summarizer)(nil)` reads as a non-nil interface, so this
derivation condition holds even though the underlying pointer is nil.
`Validate` passed such an `Options` through, since its typed-nil
assertion lived inside the `Window != nil` branch and `Window` was
still nil at that point. `New` then derived a working `Window`, and
the first compaction called `Summarize` on the nil `*plan.Summarizer`
receiver, panicking inside `(*plan.Summarizer).Summarize`'s unguarded
`s.completer.Chat` call.

### Fix

Moved the type assertion `p, ok := o.Compaction.Summarizer.(*plan.Summarizer); ok
&& p == nil` out of the `Window != nil` branch in `Options.Validate`,
so it runs whenever `Summarizer` is set, before New's derivation can
ever see it. Reworded the returned message to `"Summarizer: typed nil
*plan.Summarizer is invalid"`, since the check no longer depends on
`Window`.

### Tests

`TestNewRejectsTypedNilSummarizerBeforeDerivation`
(`agentloop/capability_derivation_test.go`) builds an `Options` with a
`ContextAccountant`-capable Completer, `Window` left nil, and a typed
nil `Summarizer`; `New` must fail instead of building a `Loop` that
panics on first compaction. Written and confirmed failing before the
fix, per this repo's TDD convention.

### Verification

- `go test -race ./agentloop/... ./internal/e2e/...` passes.
- No `api/agentloop.txt` diff: `Validate`'s exported signature and
  the `ErrInvalidOptions` sentinel are unchanged.

## Addendum: compaction trigger parity, recovery mirror, option sentinels

Status: shipped.

Three defects, found by a read-only audit of the last twenty commits.

### Trigger parity between planHistory and Compact

`planHistory` estimates the whole history against `Window.CompactTrigger`.
`compactHistory` then calls `splitSummary` and passes only the rest to
`plan.Compact`. The two calls measure different message sets.

A prior summary sits in the gap. When the whole history reaches the
trigger and the history without the summary does not, `plan.Compact`
returns `Compacted` false and an empty `Dropped`. The old code still
entered the summarize branch on `prior != nil` alone. The summarizer
then ran with the prior summary as its only input.

Two consequences follow. The rebuilt history keeps its original size,
so `checkCompactedBudget` can return `ErrCompactionFailed`. The default
`TriggerPercent` of 100 makes the trigger equal the budget, so the
default window reaches that hard failure. Each crossing also spends one
summarizer call and rewrites the summary with no new content.

Fix: pass the whole history to `plan.Compact`. `preserveSummaryName`
already adds the summary name to `PreserveNames`, so the summary stays
in the mandatory retention set and lands in `Kept`, never in `Dropped`.
Split the summary out of `res.Kept` after the call. Enter the
summarize branch on `len(res.Dropped) > 0` alone. Re-inject an
unchanged prior when nothing was dropped.

The alternative, re-injecting the prior with no summarizer call when
`Compacted` is false, removes the wasted call but leaves the
default-trigger failure. It is rejected as incomplete.

The fix does not make every default-trigger history succeed. A history
whose units are all mandatory still fails closed, because
`plan.Compact` returns `ErrRetentionOverflow` when the mandatory set
alone exceeds the budget. The fix moves that failure to its correct
cause and removes it for a history that holds a droppable unit.

The recovery path keeps its present unrecoverable semantics. An
uncompacted result under `notice` still returns a nil history.

Two side effects follow from passing the whole history, and neither is
reachable through the loop today. `hasUserMessage` can now be satisfied
by the summary itself, which carries the user role, so a history whose
only user-role message is the summary compacts instead of returning
`ErrNoObjective`. `res.Key` now fingerprints a `Kept` that includes the
summary. `compactHistory` ignores `res.Key`, and mandatory retention
always keeps a real user unit, so both are recorded rather than fixed.

### Recovery retry loses the streaming mirror

`runChat` sets `StreamingWriter` from `streamMirror` on the primary
request. It calls `recoverPromptTooLong` without the capture buffer,
and the retry request in `recoverPromptTooLong` carries no writer.
`Extensions.StreamingWriter` promises a mirror of what the Completer
writes, for the whole `runChat` call.

Fix: pass the capture buffer into `recoverPromptTooLong` and set
`StreamingWriter` on the retry request. The retry must not reset the
buffer a second time. `runChat` already reset it for this call, and the
failed primary attempt's partial bytes belong to the same call.

The retry must also leave `StreamingWriter` nil when the buffer is nil.
`newStreamBuffer` returns nil exactly when the caller set no
`Extensions.StreamingWriter`. A multi-writer over two nil values is
still a non-nil writer, which would put the retry into streaming mode
for a caller who asked for none. So the retry sets the writer only when
the buffer is non-nil, and pairs it with the sink with no reset.

### Options.Validate sentinel parity

`Options.Validate` documents one sentinel for every check. Four checks
return a different one: the `Budget` check wraps `budget.ErrInvalidOptions`,
the `Window` check wraps `plan.ErrMaxTokensNotPositive`, and the two
`Extensions` budget checks return their own incomplete-budget sentinels
unwrapped.

Fix: wrap all four with the package sentinel through a second `%w`
verb. `errors.Is` then matches the inner sentinel and
`ErrInvalidOptions` both. The module targets a Go release that supports
a second `%w`.

The constraint on the fix is the four existing assertions. They match
the inner sentinels, and they must keep matching. No test compares an
exact error string, so the message text is free to gain a prefix.

### Addendum tests

Each test below is written first and confirmed failing against the
present code.

Both compaction tests share one history shape, pinned here so the
builder does not have to search for a working band.

The history holds four messages with a one-byte-per-token estimator: a
one-byte system message, a twenty-byte prior summary, a four-hundred
byte user message, and a five-byte final user message. The window sets
`MaxTokens` to 420, `Reserve` to zero, `TriggerPercent` to 100, and
`TargetTokens` to 100.

Those numbers put the fixture in the band. The whole history estimates
426 and the history without the summary estimates 406, against a
trigger of 420. The mandatory set is the system message, the preserved
summary, and the last user message, which totals 26 and fits the
budget. The four-hundred byte message is the droppable unit, and the
target of 100 keeps it dropped.

Both failures follow from one fixture. Before the fix, `plan.Compact`
sees 406, passes through, and the summarizer runs on the prior alone.
After the fix, `plan.Compact` sees 426, drops the large unit, and the
summarizer runs on the prior plus that unit.

The budget check separates the two by a wide margin. The test
summarizer's reply renders to a 172 byte summary message. The
uncompacted rebuild reaches 578 against a budget of 420 and fails. The
compacted rebuild reaches 178 and passes. Neither outcome sits near the
threshold, so the test does not turn on a byte.

TestCompactionPriorSummaryKeepsTriggerParity builds a history whose
whole-history estimate reaches the trigger while the estimate without
the summary stays under it. The summarizer records the messages of
every call. The test asserts no recorded call holds the prior summary
as its only message.

The assertion is on the recorded input, not on a call count. A call
count cannot discriminate here: after the fix a droppable unit produces
one legitimate call, and the defect also produces exactly one call. The
input is what separates them.

The gap between the two estimates is the prior summary's own length,
which the shared fixture above sets to twenty bytes.

The recording point needs care. The existing summarizer script records
requests at the `Chat` boundary, one level below `Summarize`, so it
captures rendered excerpt text rather than the raw message list. Assert
on that excerpt text, or record at a wrapping Summarizer. Either
choice is acceptable; guessing is not.

TestCompactionPriorSummaryAtDefaultTriggerSucceeds runs the same
history and window. It asserts `Run` returns no error and the sent
history no longer holds the large message.

The droppable unit is what makes the assertion meaningful. Without it
the mandatory set alone exceeds the budget and `plan.Compact` fails
closed both before and after the fix. Before the fix the run returns
`ErrCompactionFailed` wrapping `ErrRetentionOverflow`.

TestRecoveryRetryMirrorsStreaming scripts a Completer that writes to
`req.StreamingWriter`, fails the first call with `ErrPromptTooLong`,
and answers the second. It asserts the caller's sink holds the second
call's bytes. Before the fix the sink holds only the first call's.

The fixture must reach the retry. It sets a full `Compaction` group and
a history the recovery window actually compacts. An uncompacted
recovery returns the original error with no second call, so the
assertion would pass vacuously against an unfixed loop.

A second case asserts the nil-sink path. With no
`Extensions.StreamingWriter`, the retry request's `StreamingWriter`
stays nil, so a caller who asked for no streaming gets none.

TestOptionsValidateWrapsNestedSentinels is a table over the four
checks: a negative `Budget.MaxBytes`, a zero `Window.MaxTokens`, a
half-wired `WorkBudget`, and a `ToolBudget` with no `Reserve`. Each
case asserts `errors.Is` against `ErrInvalidOptions` and against the
inner sentinel. All four cases fail before the fix.

### Addendum verification

- `go test -race ./agentloop/... ./context/...` passes.
- `make verify` passes. `agentloop` holds the 85 coverage floor.
- No `api/agentloop.txt` diff. `recoverPromptTooLong` is unexported and
  the four wrapped errors keep their exported sentinel.
- No `policy/layers.json` row changes. No new import edge.
