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
  `MaxCallsPerTurn`, `MaxTotalTokens`, `OnToolError`, `OnToolCallError`,
  `Hooks`, `Tracer`, `Usage`, `SessionID`, `Bus`, `Budget`, `Trim`,
  `Surface`, `StreamingWriter`, `Audit`, `Window`, `Summarizer`,
  `Calibrated`, `ConcludeMargin`, `StartTime`, `ConcludeDeadline`,
  `ConcludeToolCallsLeft`, `ConcludeStepsLeft`, `ConcludeNotice`,
  `DedupWithinTurn`, `MaxConcurrentTools`, `HeartbeatInterval`,
  `TurnResultBudget`, `WorkBudget`, `ToolBudget`. `Completer` and `Tools` are required;
  the rest are optional. `Bus` receives lifecycle and heartbeat events.
  See "Events" below.
- `Surface` — one iteration's tool surface from `Options.Surface`:
  `Advertised`, `Registry`, `Scope`.
- `WorkBudget` — host token-reservation hooks: `Reserve` and `Refund`.
- `ToolBudget` — host cumulative tool-call budget hook: `Reserve`.
- `ErrorFunc` — `func(ctx context.Context, call provider.ToolCall, err error) (provider.Message, error)`,
  custom tool-error message constructor for `Options.OnToolCallError`.
- `Result` — one `Run` or `RunSteerable` call's outcome: `Final`,
  `History`, `Iterations`, `Usage`, `Stop`. See "Result shape" below
  for how each field behaves on a graceful stop versus a hard-fail
  error return.
- `StopReason` — a string enum naming why `Run` or `RunSteerable`
  stopped gracefully: `StopNoToolCalls`, `StopEmptyResponse`,
  `StopMaxIterations`, `StopHookVeto`, `StopConcluded`, `StopSteered`.
  No `StopToolError` constant exists: a tool error under `ErrorPolicyFail`
  is a hard failure, not a graceful stop.
- `Steer` — a caller-held handle that requests a soft-cancel of one
  `RunSteerable` call's in-flight `Completer.Chat` call. Create one
  with `NewSteer` and call `Trigger` from another goroutine. One
  `Steer` must not be passed to two concurrent `RunSteerable` calls.
  See "Steering and interruption" below.
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
  slice for `provider.Request.Tools` on every iteration unless rotated
  by `Surface`.
- `(*Loop) Run(ctx, msgs)` — calls `Registry.RunScoped`, never
  `Registry.Run`, so a model-chosen call always passes through the
  `Loop`'s `Scope`. Returns the final `Result` and the first error.
  Equivalent to `RunSteerable(ctx, msgs, nil)`.
- `(*Loop) RunSteerable(ctx, msgs, steer)` — `Run` with one addition:
  a non-nil `steer` lets the caller request a soft-cancel of the
  current iteration's in-flight `Completer.Chat` call from another
  goroutine, through `steer.Trigger`. See "Steering and interruption"
  below.
- `NewSteer()` — returns a ready `Steer`, unbound to any `RunSteerable`
  call until passed to one.
- `(*Steer) Trigger()` — requests the soft-cancel `Steer` is bound to
  for its current `RunSteerable` call, if any. Safe to call from
  another goroutine, any number of times.
- `Options.Validate()` — checks, in order: `Completer` and `Tools` are
  set, `MaxIterations` is non-negative, `Usage` requires a non-blank
  `SessionID`, a non-nil `Budget` passes `contextbudget.Limits.
  Validate`, `MaxTotalTokens` is not negative, a non-nil `Window`
  passes `Window.Validate` and requires `Summarizer`, requires
  `Calibrated`, and excludes `Trim`, `ConcludeMargin`, `ConcludeDeadline`,
  `ConcludeToolCallsLeft`, `ConcludeStepsLeft`, `TurnResultBudget`, and
  `MaxConcurrentTools` are not negative, and `HeartbeatInterval` requires `Bus`.
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
- `ErrMaxIterations` ("agentloop: MaxIterations must be non-negative") —
  `Options.Validate` returns it for a negative `MaxIterations`.
  `Run` never returns it; hitting `MaxIterations` at run time is a
  graceful `StopMaxIterations` stop, not an error.
- `ErrIncompleteWorkBudget` ("agentloop: WorkBudget requires both Reserve and Refund") —
  `Options.Validate` returns it when `WorkBudget` is set but either
  `Reserve` or `Refund` is nil.
- `ErrIncompleteToolBudget` ("agentloop: ToolBudget requires Reserve") —
  `Options.Validate` returns it when `ToolBudget` is set but
  `Reserve` is nil.
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
- `ErrToolNotOffered` ("agentloop: tool call names a tool not offered
  when New ran") — `Run`'s error when a model-chosen call names a
  tool with no entry in the schema set `New` compiled once at
  construction. This happens when a caller registers a schema-bearing,
  scope-allowed tool on the shared `*tools.Registry` after `New` ran.
  Routed through `OnToolError` exactly like `ErrArgumentValidation`.
- `ErrPlanFailed` ("agentloop: context planning failed") — `Run`'s
  error when the per-iteration estimate fails.
- `ErrCompactionFailed` ("agentloop: compaction failed") — `Run`'s
  error when a required compaction cannot complete: a
  `contextplan.Compact` failure (wrapping its sentinel), a summarizer
  failure (wrapping the `contextsummary` sentinel), or a rebuilt
  history still over `Window.Budget` (wrapping
  `contextplan.ErrRetentionOverflow`). Nothing is sent for that
  iteration.
- `ErrSummarizerRequired` ("agentloop: Window requires Summarizer") —
  `Options.Validate` returns it when `Window` is set and `Summarizer`
  is nil.
- `ErrEstimatorRequired` ("agentloop: Window requires Calibrated") —
  `Options.Validate` returns it when `Window` is set and `Calibrated`
  is nil.
- `ErrTrimExcluded` ("agentloop: Window and Trim are mutually
  exclusive") — `Options.Validate` returns it when both `Window` and
  `Trim` are set.
- `ErrConcludeMargin` ("agentloop: ConcludeMargin must not be
  negative") — `Options.Validate` returns it for a negative
  `ConcludeMargin`.
- `ErrTurnResultBudget` — `Options.Validate` returns it for a negative
  `TurnResultBudget`.
- `ErrHeartbeatRequiresBus` ("agentloop: HeartbeatInterval requires a
  non-nil Bus") — `Options.Validate` returns it when
  `HeartbeatInterval` is positive and `Bus` is nil.

## Context planning and prompt-too-long recovery

A non-nil `Options.Window` plans every iteration against a token
budget. `Window` requires `Summarizer` and `Calibrated`, and excludes
`Trim`; a nil `Window` keeps the loop exactly as it was.

Before each `Completer.Chat`, `Run` estimates the history through
`Calibrated` and passes through under `Window.CompactTrigger`. At or
above the trigger it runs the compaction sequence: `contextplan.
Compact` under a copy of the caller's `Window` whose
`Compaction.PreserveNames` gained `contextsummary.
SummaryMessageName` when absent, the prior summary message held aside
as summarizer input, the dropped messages summarized through
`contextsummary`, the fresh summary injected after the leading system
message, and the rebuilt history re-estimated against
`Window.Budget`. The compacted history replaces the old one only
after the whole sequence succeeds; a failed compaction returns the
pre-compaction history in `Result.History`.

Before every `Chat` call, when `Calibrated` is set, `Run` calls
`Calibrated.EstimateTokens` over that call's own request and holds the
result. After `Chat` returns a response, `Run` calls
`Calibrated.Observe(estimated, resp.Usage.TotalTokens)`, pairing that
same call's estimate with its own actual usage, before the
`MaxTotalTokens` check. A non-positive estimate or `TotalTokens` is a
no-op. The recovery path holds its own estimate over its own retried
request, never the pre-recovery estimate.

On a `provider.ErrPromptTooLong` rejection with a non-nil `Window`,
`Run` builds a recovery window with `Compaction.TriggerPercent` of 1
and `Compaction.TargetTokens` of `max(1, min(RecoveryTargetTokens,
Budget over four))`, runs the compaction sequence with one
`CompactionNotice` message appended after the summary injection, and
retries the same iteration exactly once. A second rejection
propagates. An uncompacted result means the history sits under one
percent of the budget: `Run` returns the original error unchanged,
with no retry and no notice. Without a `Window`, the rejection
propagates unchanged. Compaction is LLM-only: no structural fallback
path exists anywhere in `Run`.

## Graceful conclude near MaxIterations

A positive `Options.ConcludeMargin` nudges the model toward a final
answer as `MaxIterations` approaches, instead of hard-stopping with
whatever partial state the transcript holds. Zero disables nudging.

Number each `Completer` call with a 1-based index `k`, from 1 to
`MaxIterations`. `Run` appends `ConcludeNotice` to history once,
immediately before the call at the first `k` for which
`MaxIterations - k < ConcludeMargin` holds. A `ConcludeMargin` at or
above `MaxIterations` satisfies this at `k = 1`, so the nudge fires on
`Run`'s first iteration. An empty `ConcludeNotice` uses
`DefaultConcludeNotice`.

The append lands at the tail of history, as the last message in the
nudged iteration's `Request.Messages`, after that iteration's `Trim`,
`Budget`, and `Window` steps — not spliced near the system message the
way `CompactionNotice` is. `Run` stops at `StopConcluded`, not
`StopNoToolCalls`, when the model returns no tool call on an iteration
whose actual sent request still carried the notice. A model that keeps
requesting tool calls through the nudge is not blocked; `Run` still
ends at `StopMaxIterations`, unchanged, if the limit is hit.

`StopConcluded` never fires from notice text that reached history some
other way — a caller re-feeding a prior `Run` call's leftover
`History`, or coincidental content matching `ConcludeNotice`. The
check requires both that this run's own `ConcludeMargin` logic queued
the notice and that the notice still sits in the request the model
actually answered; a `Trim` or `Window` step that strips the notice
before the model sees it falls back to `StopNoToolCalls`.

`Options.Window` may also drop, reorder, or summarize away the notice
before a nudged call; no test covers `ConcludeMargin` combined with
`Window`, and the two have no current caller pairing them.

## Duplicate-call dedup within a turn

`Options.DedupWithinTurn` detects a duplicate `(tool name, canonical
arguments)` call already served earlier in the same turn, and serves
`DuplicateCallNotice` instead of running the tool a second time. False,
the zero value, runs every call, unchanged from the base behavior.

`runToolCalls` starts an empty dedup set on every call, one call per
turn, so no dedup carries across iterations. Canonicalization decodes
`call.Arguments` with `json.Decoder.UseNumber()`, so numbers keep
their source digit string instead of collapsing into `float64`, then
re-marshals; `encoding/json` sorts object keys on marshal, so two
calls with the same arguments in a different key order compare equal.
A canonicalization error (malformed JSON, or a valid JSON value
followed by trailing bytes) excludes that call from the dedup set: it
always runs and is never recorded or matched against the set.

The dedup check runs before `PointPreTool` and short-circuits: a call
identified as a duplicate never reaches `PointPreTool` or
`PointPostTool`, and the underlying tool never runs for it. The
synthesized `RoleTool` message for a deduped call carries the
duplicate call's own `ToolCallID`. Any call that does run, success or
an `ErrorPolicyReport`-rendered error, seeds the dedup set once its
`RoleTool` message reaches history, so a later byte-identical retry in
the same turn is deduped either way.

## Result shape

On every graceful stop (`StopNoToolCalls`, `StopMaxIterations`,
`StopHookVeto`, `StopConcluded`, `StopSteered`), `Run` or
`RunSteerable` returns a fully populated `Result` and a nil error:
`History` carries every message appended so far, `Iterations` carries
the completed iteration count, and `Usage` carries the tokens summed
so far. `Final` carries the last message appended, or the zero value
when the stop happened before a new response arrived.

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

## Steering and interruption

`RunSteerable` lets a caller request a soft-cancel of the current
iteration's in-flight `Completer.Chat` call without a hard `ctx`
cancellation. A hard `ctx` cancellation still ends the run at its
hard-fail path, unchanged from `Run`. A triggered `Steer` ends the run
gracefully instead, at the next iteration boundary, with `Stop ==
StopSteered` and `Final` at the zero value, following the same
Result-shape rule as every other pre-response graceful stop.

`Trigger` fired mid-tool-call-batch has no effect on calls already
dispatched in that batch; it takes effect at the start of the next
iteration's `Completer.Chat` call instead. A `Trigger` call fired
during a prompt-too-long recovery retry has no effect until the
following iteration boundary, for the same reason: the retry call runs
on the plain outer `ctx`, not on a steer-derived context. Interrupting
a running tool call is out of scope: today's tool calls run
sequentially, strictly after the iteration's `Completer` call returns,
so no tool call is ever in flight when a steer request can fire.

`Steer` is a per-`RunSteerable`-call value, not a per-`Loop` value.
`RunSteerable` resets it at the call's own start, so a `Trigger` call
before `RunSteerable` starts, or left over from a prior call on a
reused `Steer`, is a no-op. One `Steer` must not be passed to two
concurrent `RunSteerable` calls: both calls would arm and disarm the
same triggered flag, and one caller's `Trigger` could stop the other
caller's unrelated run. See
[../plans/agentloop.md](../plans/agentloop.md)'s "Addendum: steering
and interruption" for the full mechanics.

## Steer injector and soft-continue

`Steer.SetInjector(f func() []provider.Message)` installs a pull-based
message source the loop drains at the iteration-top boundary. A
non-empty drain grows `history` by those messages; a nil or empty
drain is a no-op. The injector runs on the loop goroutine, so `f`
must not assume concurrency or call back into the loop. `Steer` is
the place to install the injector: `SetInjector` is called once
before the first `RunSteerable` and `reset()` preserves the
injector across calls, so a caller can reuse one `Steer` across
multiple `RunSteerable` calls.

A `Steer` with an installed injector soft-continues every steer. A
`Trigger` fired mid-`Completer.Chat` cancels only that one call; the
run continues, the iteration-top boundary drains the next payload,
and the next iteration's `Chat` call arms un-triggered. The split
on `hasInjector()` is load-bearing: a host that installs an injector
opts into the soft-continue shape; a host that does not keeps the
original single-shot `StopSteered` shape every pre-injector `Steer`
test pins.

`Steer.HasActiveCall() bool` reports whether a `Completer.Chat` is
currently in flight. Continuous-bridge triggers fire on every poll
tick; a trigger fired when no call is in flight sets the trigger
flag for the next arm to observe, which then immediately cancels
that chat, which the bridge fires again, and the run never makes
progress. A bridge that guards each `Trigger` on `HasActiveCall`
closes that loop.

See [../plans/agentloop.md](../plans/agentloop.md)'s "Addendum:
pull-based steer injector (commit d914611)" for the full mechanics
and the four follow-up items.

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
history, including a deduped call. A `PointPreTool` veto and an
`ErrorPolicyFail` tool error produce no `AuditKindToolCall` record,
since neither appends a `RoleTool` message to history. A deduped
call's `AuditKindToolCall` record carries a nil `Err`, since a served
duplicate is not a tool-run error. `Options.Audit` stays optional and
`agentloop` stays envelope-agnostic: a caller wanting a signed audit
trail builds and signs its own `envelope.Message` chain from the
`AuditRecord` values, the same way `agent.confirmStep` signs steps
outside the `flow` block it wraps.

## Events

A non-nil `Options.Bus` receives lifecycle and progress events emitted by
`Run`. Heartbeat events require a positive `Options.HeartbeatInterval`.

`Run` emits the following `events.Name` constants:

- `EventIterationStart` — fires once at the start of every iteration.
- `EventCompletionHeartbeat` — fires every `HeartbeatInterval` while
  one `Completer` call is in flight.
- `EventToolCallStart` — fires once at the start of every tool call,
  before the `PointPreTool` hook fires.
- `EventToolCallHeartbeat` — fires every `HeartbeatInterval` while one
  tool call is in flight.
- `EventToolCallEnd` — fires once at the end of every tool call.
- `EventIterationEnd` — fires once at the end of every iteration.
- `EventAssistant` — fires once per completed assistant turn.
- `EventThinkingStart` — fires before assistant reasoning content.
- `EventThinkingDelta` — carries assistant reasoning content.
- `EventThinkingEnd` — fires after assistant reasoning content.
- `EventCacheUsage` — fires with prompt cache usage JSON payload.
- `EventCalibrationDelta` — fires with token calibration JSON payload.
- `EventToolParallel` — fires when multiple tools dispatch in parallel.

`Run` swallows every `Bus.Emit` error, including "no subscriber for
name", matching the `PointStop`-fire swallow precedent elsewhere in
`Run`.

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

## Turn result budget

A positive `Options.TurnResultBudget` caps the summed byte size of one
turn's rendered tool results, across every call in that turn. It
shapes each call's content after that call's own `tools.ResultBudgetOf`
bound already applied, not instead of it.

`runToolCalls` runs calls in `ToolCall.Index` order and tracks a
running byte total for the turn, reset to zero at the start of each
turn. A call's content stays whole only when the running total plus
its byte length does not exceed `TurnResultBudget`; otherwise the
content is replaced with `BatchTruncationNotice`
("[batch-truncated] Turn tool-result budget exhausted; this result
was omitted."), and the running total does not grow for that call. A
zero `TurnResultBudget` skips the check entirely and every call's
content passes through whole.

Shaping applies to every appended `RoleTool` message's content,
including an `ErrorPolicyReport` error report marked with
`ToolErrorPrefix`. `AuditRecord.Err` always carries the call's true
outcome, independent of whether `ToolResult.Content` was replaced by
shaping. A `PointPreTool` veto stops the turn before shaping considers
any later call, unchanged from a run with `TurnResultBudget` at zero.

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
  means unbounded. A zero `TurnResultBudget` means unbounded.
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
- [events.md](events.md) — `Bus.Subscribe` and `Bus.Emit` back the
  progress events. See "Events" above.
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
    // Completer or Tools missing, or MaxIterations negative
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
