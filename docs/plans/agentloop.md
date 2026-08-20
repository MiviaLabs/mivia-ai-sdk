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
tool error under `ErrorPolicyFail`, or a non-veto `hooks.Fire` error
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
zero; see the addendum's Outside bullet on `usage.Accumulator`. A
zero `MaxTotalTokens` means unbounded.

Hitting `MaxIterations` is not an error: `Run` returns
`Result{Stop: StopMaxIterations}, nil`, a normal, graceful stop,
following the `Result`-shape rule above. `ErrMaxIterations` is
reserved for `Options.Validate()` rejecting a non-positive
`MaxIterations` value — a construction-time validation error, not a
runtime stop.

`trace.Tracer` opens one span per iteration and one per tool call.
`hooks.Registry` fires `PointPreTool` and `PointPostTool` per tool
call, and `PointStop` once at the end. When a `Fire` call returns a
non-nil error, `Run` checks `errors.Is(err, hooks.ErrVetoed)`: a veto
is the graceful `StopHookVeto` stop described above — nil error, no
tool run. Any other `Fire` error, a handler-returned error that is not
a veto, is a hard failure: `Run` returns the wrapped error and the
partial `Result` per the rule above, and the tool does not run.
`usage.Accumulator` records per iteration under `SessionID`.
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
  `errors.Is(err, hooks.ErrVetoed)` is false to distinguish it from a
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

Status: plan, ready for plan review. This addendum covers three
hardening fixes found in an adversarial review of the shipped
`agentloop` code. It changes `toolcall.go`, `options.go`, `wire.go`,
and `run.go`. It adds no new package.

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
  calls finish. A `PointPreTool` veto produces no history entry and
  is not audited: `StopHookVeto` ends the run immediately, so there
  is no tool result to attest to. An `ErrorPolicyFail` tool error is
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

Status: plan, ready for plan review. This addendum fixes a correctness
bug in `run.go`'s `MaxTotalTokens` enforcement. It changes `run.go`
only. It adds no new package and no new exported symbol.

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
  `Options.Usage`/`usage.Accumulator` recording. Those three continue
  to record the raw `resp.Usage` a `Completer` reports, unchanged: they
  are a caller-facing report of what the `Completer` said, not a
  safety cap, and correcting a `Completer`'s own under-reporting there
  would silently rewrite data the caller may need to reconcile against
  a vendor invoice. Only the cap-enforcement running total changes.
- `provider.Usage` and `provider/types.go`. This addendum adds no
  `Validate` method there; see the addendum decision above. No
  `docs/plans/provider.md` change.
- `usage.Accumulator.Record` in `usage/accumulator.go`. It sums
  `TotalTokens` the same trust-assuming way `run.go` did, so
  `Accumulator.Total` under-reports for the same
  `TotalTokens`-left-zero `Completer` shape. This is a real, smaller
  gap in a different package, not fixed in this change. It needs its
  own plan review against `docs/plans/usage.md`, since `usage.Record`
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

Status: plan, ready for plan review. This addendum fixes a
nil-pointer panic an adversarial logic review found in `toolcall.go`.
It changes `toolcall.go` and `options.go`. It adds one new sentinel
error and no new package.

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
Summarizer *contextsummary.Summarizer

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
  `contextsummary.SummaryMessageName` to the copy's
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
- Injects `contextsummary.SummaryMessage(s)` directly after the
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
  `Name` is `contextsummary.SummaryMessageName`, and the request the
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
  change window adds in `docs/plans/contextplan.md` carries the
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
production caller in the separate `mivia-agent` repository, found a
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
production caller in the separate `mivia-agent` repository, found a
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

### Addendum verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change. `docs/packages/agentloop.md` gains the
`DedupWithinTurn`/`DuplicateCallNotice` surface and a "Duplicate-call
dedup within a turn" section in the same change as the code.

