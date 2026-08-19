# Package reference: agentloop

The agentloop package runs a model until it stops asking for tools.
`Run` offers a `tools.Registry`'s tools to a `provider.Completer`,
runs the tool calls the model requests, appends the results as
`RoleTool` messages, and repeats until the model returns no tool call
or a bound trips. The exported surface below mirrors
`api/agentloop.txt`.

## Types

- `Loop` — a bound, ready-to-run tool-calling loop. The zero value is
  not usable; create one with `New`.
- `Options` — the config struct `New` validates and wires:
  `Completer`, `Tools`, `Scope`, `Model`, `MaxIterations`,
  `MaxCallsPerTurn`, `MaxTotalTokens`, `OnToolError`, `Hooks`,
  `Tracer`, `Usage`, `SessionID`, `Bus`, `Budget`, `Trim`, `Audit`.
  `Completer` and `Tools` are required; the rest are optional. `Bus`
  is reserved for the loop's own events, pending a future event
  vocabulary; `Run` does not yet emit anything through it.
- `Result` — one `Run` call's outcome: `Final`, `History`,
  `Iterations`, `Usage`, `Stop`. See "Result shape" below for how
  each field behaves on a graceful stop versus a hard-fail error
  return.
- `StopReason` — a string enum naming why `Run` stopped gracefully:
  `StopNoToolCalls`, `StopMaxIterations`, `StopHookVeto`. No
  `StopToolError` constant exists: a tool error under
  `ErrorPolicyFail` is a hard failure, not a graceful stop.
- `ErrorPolicy` — a string enum naming what `Run` does with a
  tool-run error: `ErrorPolicyReport` (the zero value; sends the
  error text back as the tool's `RoleTool` result and continues) or
  `ErrorPolicyFail` (turns the error into `Run`'s own hard-fail
  return).
- `AuditKind` — a string enum naming what an `AuditRecord` describes:
  `AuditKindCompletion` (one `Completer.Chat` turn) or
  `AuditKindToolCall` (one tool call whose result reached history).
- `AuditRecord` — one audited event: `Kind`, `Iteration`, and, set
  only for the matching `Kind`, `Request`/`Response`
  (`AuditKindCompletion`) or `ToolCall`/`ToolResult`/`Err`
  (`AuditKindToolCall`). `Err` carries the tool-run error `Run`
  reported for that call, or nil on success.
- `AuditFunc` — `func(ctx, rec AuditRecord) error`, the type of
  `Options.Audit`. A non-nil return is a hard failure, following the
  same Result-shape rule as a `Trim` error.

## Functions and methods

- `New(opts)` — validates `opts`, calls
  `Definitions(opts.Tools, opts.Scope)` once, and binds the result
  onto a `Loop`. `Run` reuses that same `[]provider.ToolDefinition`
  slice for `provider.Request.Tools` on every iteration.
- `(*Loop) Run(ctx, msgs)` — calls `Registry.RunScoped`, never
  `Registry.Run`, so a model-chosen call always passes through the
  `Loop`'s `Scope`. Returns the final `Result` and the first error.
- `Options.Validate()` — checks `Completer` and `Tools` are set,
  `MaxIterations` is positive, `Usage` requires a non-blank
  `SessionID`, a non-nil `Budget` passes `contextbudget.Limits.
  Validate`, and `MaxTotalTokens` is not negative.
- `Definitions(reg, scope)` — builds `[]provider.ToolDefinition` from
  `reg`, skipping a tool with no published schema and one `scope`
  denies. The second return holds the names skipped for a missing
  schema. Fails closed with `ErrNoSchemas` whenever `reg` is
  non-empty and the offered set ends up empty, whatever the cause. An
  empty `reg` returns an empty set and no error.

## Failure modes

Use `errors.Is` to test these.

- `ErrNoCompleter` ("agentloop: completer is required") —
  `Options.Validate` returns it when `Completer` is nil.
- `ErrNoTools` ("agentloop: tools registry is required") —
  `Options.Validate` returns it when `Tools` is nil.
- `ErrMaxIterations` ("agentloop: MaxIterations must be positive") —
  `Options.Validate` returns it for a non-positive `MaxIterations`.
  `Run` never returns it; hitting `MaxIterations` at run time is a
  graceful `StopMaxIterations` stop, not an error.
- `ErrNoSchemas` ("agentloop: registry offers no schema-bearing tool
  the scope allows") — `Definitions` returns it when the registry is
  non-empty and the offered tool set ends up empty.
- `ErrUnrenderableResult` ("agentloop: tool result cannot be
  rendered") — the render path's error when a tool result's
  `Out.Value` cannot be marshaled to JSON after failing the string
  and UTF-8-bytes cases.
- `ErrCallsPerTurnExceeded` ("agentloop: turn requested more calls
  than MaxCallsPerTurn allows") — `Run`'s error when one turn's
  response requests more calls than a positive `MaxCallsPerTurn`
  allows. Always fails the run, before any call in the turn runs,
  regardless of `OnToolError`.
- `ErrOverBudget` ("agentloop: message history exceeds Budget") —
  `Run`'s error when the message history fails a non-nil `Budget`'s
  `Fits` check ahead of a `Completer` call.
- `ErrTokenBudgetExceeded` ("agentloop: cumulative tokens exceed
  MaxTotalTokens") — `Run`'s error when the run's cumulative billed
  tokens exceed a positive `MaxTotalTokens` after a `Completer` call
  returns.
- `ErrInvalidSchema` — `New`'s error when a `Scope`-offered
  `SchemaTool`'s `ParameterSchema()` fails `schema.Compile`, wrapped
  with the tool name and the underlying `schema` error.
- `ErrArgumentValidation` — `Run`'s error when a model-chosen call's
  `Arguments` fail `schema.Compiled.Validate` against the resolved
  tool's compiled schema, wrapped with the call ID and the underlying
  `schema` error. Never reaches `DecodeArguments`.

## Result shape

On every graceful stop (`StopNoToolCalls`, `StopMaxIterations`,
`StopHookVeto`), `Run` returns a fully populated `Result` and a nil
error: `History` carries every message appended so far, `Iterations`
carries the completed iteration count, and `Usage` carries the tokens
summed so far. `Final` carries the last message appended, or the zero
value when the stop happened before a new response arrived.

On every hard-fail error return — a canceled ctx, a `Completer.Chat`
error, `ErrOverBudget`, `ErrTokenBudgetExceeded`,
`ErrCallsPerTurnExceeded`, a `Trim` error, a post-`Trim`
`provider.Message.Validate` error, a tool error under
`ErrorPolicyFail`, a non-veto `hooks.Fire` error, or a non-nil
`Options.Audit` return — `Run` also
returns the partial `Result` alongside the error, not the zero value,
once at least one iteration has completed. `Final` and `Stop` stay
the zero value in this case, since the run failed a stop condition
instead of reaching one. When no iteration has completed yet, the
rule degrades to the zero-value `Result` on its own, with no special
case for ctx cancellation or any other cause.

## Argument validation

`New` compiles every `Scope`-offered `SchemaTool`'s
`ParameterSchema()` once, via `schema.Compile`, and caches the result
on `Loop`. A model-chosen call's `Arguments` run through
`schema.Compiled.Validate` against that cached schema before
`DecodeArguments`, so a payload that fails validation never reaches
the tool's own decoder. A validation failure wraps
`ErrArgumentValidation` and renders through `schema.Corrective`
instead of a raw Go error string, giving the model a bounded,
schema-derived corrective message.

## Audit

A wired `Options.Audit` receives one `AuditRecord` per audited event,
in the order `Run` produces them: `AuditKindCompletion` once per
iteration, right after that iteration's `Completer.Chat` response is
appended to history and before any of that iteration's bound checks
run; `AuditKindToolCall` once per tool call whose result reaches
history. A `PointPreTool` veto and an `ErrorPolicyFail` tool error
produce no `AuditKindToolCall` record, since neither appends a
`RoleTool` message to history. `Options.Audit` stays optional and
`agentloop` stays envelope-agnostic: a caller wanting a signed audit
trail builds and signs its own `envelope.Message` chain from the
`AuditRecord` values, the same way `agent.confirmStep` signs steps
outside the `flow` block it wraps.

## Error marker

Under `ErrorPolicyReport`, a tool-run error's rendered `RoleTool`
content starts with `ToolErrorPrefix` ("[tool-error] "), marking it as
untrusted, error-path content rather than a normal tool result.

## Render path

`render` turns a tool's `Out.Value` into the `RoleTool` message
content, in a fixed order: a string value passes through unchanged; a
`[]byte` value valid as UTF-8 becomes its string form; anything else
falls back to `json.Marshal`. A marshal failure wraps
`ErrUnrenderableResult`.

When the tool implements `tools.ResultBudgetTool` with a positive bound
smaller than the rendered content, `render` truncates the content to
fit the bound. A bound at least as long as the truncation marker keeps
`bound - len(marker)` content bytes, then appends the marker. The
result lands at exactly `bound` bytes. A bound equal to the marker's
own length keeps zero content bytes and returns the marker alone. A
bound shorter than the marker hard-cuts the content to `bound` bytes
with no marker, since the marker itself would not fit.

## Invariants

- `New` calls `Definitions` once; a tool registered after `New` but
  before `Run` is never offered to the model.
- A `PointPreTool` veto (`errors.Is(err, hooks.ErrVetoed)`) stops the
  run with `StopHookVeto` and does not run the tool. Any other
  `PointPreTool` handler error is a hard failure.
- A wired `Hooks` registry fires `PointPreTool` before each tool
  call, `PointPostTool` after it, and `PointStop` exactly once, on
  every return path out of `Run`, with the returned `Result` as
  payload. `PointPostTool` and `PointStop` are informational: a
  handler's veto or error at either point never changes what `Run`
  already decided to return.
- A `DecodeArguments` failure on malformed model-supplied JSON
  arguments is a tool-run error and goes through the same
  `OnToolError` policy as a failed `Run`.
- A zero `MaxCallsPerTurn` means unbounded. A zero `MaxTotalTokens`
  means unbounded.
- A nil `Options.Trim` passes history through unchanged and skips
  `provider.Message.Validate` on it.

## Why this shape

`agentloop` sits beside `flow` and `agentrun` as a second composition
path for tool calling, not a change to the step-graph runner: a
caller with a single model loop and no branching workflow does not
need `machine.Definition` or `flow.Definition` to drive one. `Run`
calls `Registry.RunScoped`, never `Registry.Run`, so a model-chosen
call always passes through the caller's `Scope`, matching
`agentrun.Runner.chain`'s precedent of never letting a model bypass
scoping. `Options.Trim`'s signature stays type-compatible with a
closure over `contextplan.Planner.Plan`, so a caller can bind context
trimming without `agentloop` importing `contextplan` itself. See
[../plans/agentloop.md](../plans/agentloop.md).

## Cross-references

- [tools.md](tools.md) — `SchemaTool`, `SchemaOf`, and
  `Registry.Tools()` let `Definitions` build the offered tool set;
  `DecodeArguments` sits on the tool, not in `agentloop`.
  `ResultBudgetTool` publishes the bound the render path truncates
  against; `tools` never enforces it itself. See "Render path" above.
- [hooks.md](hooks.md) — `PointPreTool`, `PointPostTool`, and
  `PointStop` fire the same way `agentrun.Runner.Run` fires them.
- [trace.md](trace.md) — a wired `Tracer` opens one span per
  iteration and one per tool call.
- [contextbudget.md](contextbudget.md) — a wired `Budget` caps one
  `Completer` call's message history.

## Usage

```go
reg := tools.New()
_ = reg.Add(echoTool{})

loop, err := agentloop.New(agentloop.Options{
    Completer:     myCompleter,
    Tools:         reg,
    MaxIterations: 10,
})
if err != nil {
    // Completer or Tools missing, or MaxIterations not positive
}

res, err := loop.Run(context.Background(), []provider.Message{
    {Role: provider.RoleUser, Content: "hi"},
})
if err != nil {
    // a hard-fail cause; res still carries partial History,
    // Iterations, and Usage once an iteration has completed
}
// res.Stop == agentloop.StopNoToolCalls on a normal finish
```
