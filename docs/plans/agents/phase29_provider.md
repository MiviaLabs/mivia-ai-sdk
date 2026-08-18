# Phase 29: model provider interface

Status: ready to build. Independent of every other phase. No phase
before it ships an LLM binding; `agent` (phase 12, phase 13) composes
identity, discovery, and flow, but calls no model. This phase closes
that gap with an interface only. Wiring `provider` into `agent` is
deferred to a later phase. The package plan
`scripts/check_plan.py` requires is docs/plans/provider.md; this file
is the phase contract with the full design reasoning, the same split
docs/plans/memory.md and docs/plans/agents/phase15_memory.md use.

## Goal

Define one interface a caller uses to complete a chat turn against a
language model, plus the request and response shapes the interface
carries. The interface has no implementation in this SDK. A caller
supplies a concrete type; the SDK only names the contract.

## Scope

Inside: the `Completer` interface, the `Request` and `Response` types,
the `Message`, `ToolDefinition`, `ToolCall`, `Usage`, and `Chunk`
types, and two optional extension interfaces for context accounting
and reasoning policy.

Outside: any concrete client. No HTTP call, no vendor SDK, no
streaming transport. AGENTS.md forbids a third-party import outside
`a2aclient`; a concrete OpenAI, Anthropic, or local-model client is a
separate module or a separate SDK, never this package. Outside: the
`agent` composition wiring. A future phase adds a `Model` field or a
`Complete` method to `agent.Agent` and threads a `Completer` through
`Run`, the same way phase 13 threaded `AckWait`. That wiring needs its
own plan review and is not part of phase 29. Outside: retry, backoff,
rate limiting, and cost tracking. Those are caller or future-phase
concerns layered on top of one `Chat` call, not part of the contract
itself.

### Why a leaf package

`provider` imports nothing internal. It defines types and one
interface; it needs no envelope, no identity, no flow. A leaf package
stays easiest to test and easiest to depend on: any future package
that needs a model binding imports `provider` alone, with no
transitive pull-in. This matches the dependency-inward design in
AGENTS.md: leaf blocks first, composition last.

### Why the interface earns its place

`Completer` has zero implementations in this SDK today and two or
more expected outside it: at minimum a hosted-API client and a local
or test double. A caller swaps providers without touching `agent` or
any other block, the same reasoning phase 14 gives for `Tool`. An
interface with one implementation and no second caller in sight is
speculative; `Completer` clears that bar because every model
integration a caller writes needs the same two operations, and the
SDK's own tests need a fake `Completer` that never calls a network.

### The two required methods on `Completer`

- `Chat` always waits for the complete response before it returns. A
  caller ignores `Request.Stream` when it calls `Chat`; the
  implementation may still stream internally, but it never yields a
  partial value to the caller.
- `ChatStream` always returns a channel of `Chunk` values immediately.
  The channel closes after the final chunk. The final chunk carries
  the completed `Usage` totals, or a non-nil `Err` when the stream
  fails mid-flight. A caller drains the channel to build the full
  text.

### `RunTurn`, the package-level dispatch function

`RunTurn(ctx, c, req)` is the one call most callers use. It honors
`req.Stream`: when false, it calls `c.Chat` and returns the result;
when true, it calls `c.ChatStream`, drains the channel, and returns
one aggregated `Response`. `RunTurn` is a package function, not a
`Completer` method, because its logic depends only on `Chat` and
`ChatStream`; every implementation would otherwise hand-write the same
dispatch and aggregation code. `RunTurn` becomes `provider`'s own
first caller of `Completer`, calling `Chat` and `ChatStream` the same
way `tools.Registry.Run` calls `Tool.Run`. This shrinks `Completer`
to two required methods plus `Name`.

`RunTurn`'s aggregation step merges `Chunk` values into one `Response`
following the same rules `Chunk.ToolCallDelta` documents below:
`Delta` values concatenate into `Response.Message.Content`; tool-call
deltas merge by `Index` into `Response.ToolCalls`; the final chunk's
`Usage` becomes `Response.Usage`; the final chunk's `FinishReason`
becomes `Response.FinishReason`. Aggregation sets
`Response.Message.Role = RoleAssistant` unconditionally; `Chunk`
carries no `Role` field, since every streamed chunk answers the
caller's turn and a model never streams any role but the assistant's.
`RunTurn` returns the first error either method returns; on error it
returns the zero `Response` alongside that error, unwrapped. A stream
can also fail after chunks have already flowed: when a drained
`Chunk` carries a non-nil `Err`, `RunTurn` stops draining at that
chunk and returns the zero `Response` alongside that `Err`,
unwrapped. It discards any partial aggregation built from chunks
drained before the failing one; a mid-stream failure never returns a
partial `Response`.

### `RunTurn` validates `Request.Messages` before it dispatches

`RunTurn` calls `Message.Validate` on every entry of `req.Messages`,
in order, before it calls either `Chat` or `ChatStream`. The first
invalid entry stops validation; `RunTurn` returns the zero `Response`
and that `Validate` error, unwrapped. `RunTurn` calls neither
`Completer` method when validation fails, so a fake `Completer` in a
test records no call for an invalid request. This makes `RunTurn`
one of the callers `Message.Validate`'s doc comment above promises;
the other caller is any concrete `Completer` implementation outside
this SDK, which is free to call `Validate` again or trust `RunTurn`'s
check.

### `RunTurn` and context cancellation

`RunTurn` selects on `ctx.Done()` while it drains `ChatStream`'s
channel. On every iteration of the drain loop, `RunTurn` selects
between the next `Chunk` and `ctx.Done()`; whichever is ready first
wins. When `ctx` is done before the channel yields another `Chunk` or
closes, `RunTurn` stops draining immediately, discards any partial
aggregation, and returns the zero `Response` alongside `ctx.Err()`.
This matches `agent`'s existing cancellation-respecting pattern (see
`AckWait`): a caller-supplied `ctx` always bounds a `RunTurn` call,
even against a `Completer` whose channel never closes. `RunTurn`
does not call `ctx.Done()` before starting the drain of a non-nil
channel that `ChatStream` already returned; a `Completer` that wants
its own pre-call cancellation check performs it inside `Chat` or
`ChatStream` before it returns.

### The optional extension interfaces

An implementation may additionally satisfy one or both of:

- `ContextAccountant`, exposing `ContextWindow() int`, the maximum
  token count the bound model accepts across one request.
- `ReasoningPolicy`, exposing `ReasoningEffort() string`, the
  configured reasoning-effort level for a model that supports
  extended reasoning.

A caller type-asserts: `if ca, ok := c.(provider.ContextAccountant);
ok { ... }`. Neither interface is required on `Completer`. This
mirrors the optional-capability pattern the SDK's future
`tools.CapableTool` uses: a small required interface, plus narrow
opt-in interfaces a concrete type adds only when it has the data.

## API

The surface below lands in `api/provider.txt` via `make api-update`.

- `type Completer interface { Name() string; Chat(ctx context.Context, req Request) (Response, error); ChatStream(ctx context.Context, req Request) (<-chan Chunk, error) }`
  is the required contract. `Name` returns the provider's own label,
  for logs and error messages; it takes no argument and returns no
  error, since a bound `Completer` always has a name.
- `func RunTurn(ctx context.Context, c Completer, req Request) (Response, error)`
  dispatches on `req.Stream`, as described above, and is the one call
  most callers use instead of choosing between `Chat` and
  `ChatStream` themselves.
- `type Role string` with constants `RoleSystem`, `RoleUser`,
  `RoleAssistant`, and `RoleTool` names a message's role. AGENTS.md
  bars a string literal where a constant exists; `Message.Role` uses
  this type.
- `type Message struct { Role Role; Content string; ToolCallID string }`
  is one turn in the conversation `Request.Messages` carries.
  `ToolCallID` is set only, and always, on a `RoleTool` message; it
  names the `ToolCall.ID` the message answers. A `RoleTool` message
  requires a non-empty `ToolCallID`, since `ToolCallID`'s only purpose
  is matching a tool reply to its call.
- `func (m Message) Validate() error` enforces the three rules the
  field above documents, each with its own sentinel error, following
  the sentinel-error precedent `room` (`ErrNotMember`), `tools`
  (`ErrNilTool`), and `memory` (`ErrBudgetExceeded`) already set:
  `ErrToolCallIDUnexpected` when `ToolCallID` is non-empty on a
  message whose `Role` is not `RoleTool`; `ErrToolCallIDRequired`
  when `ToolCallID` is empty on a `RoleTool` message;
  `ErrUnknownRole` when `Role` is outside the four declared
  constants. `Validate` checks `Role` legality first: a `Role` outside
  the four constants always returns `ErrUnknownRole`, regardless of
  `ToolCallID`; only for one of the four known roles does `Validate`
  then check the `ToolCallID` pairing rule. `Encode`-style callers run `Validate` before they trust
  a `Message`; `provider` defines no `Encode` of its own, so
  `RunTurn` and any concrete `Completer` are the callers. `RunTurn`
  is a required caller: it calls `Validate` on every entry of
  `req.Messages` before it dispatches to `Chat` or `ChatStream`. See
  "`RunTurn` validates `Request.Messages` before it dispatches" above
  for the exact contract. A caller checks the specific rule with
  `errors.Is(err, provider.ErrUnknownRole)` and so on.
- `type ToolDefinition struct { Name string; Description string; Schema []byte }`
  names one tool a model may call. `Schema` holds the tool's
  parameter schema as raw bytes; `provider` does not parse it, so it
  adds no JSON-schema dependency.
- `type ToolCall struct { Index int; ID string; Name string; Arguments []byte }`
  is one call the model requests, or one fragment of a call while it
  streams. `Index` is the vendor-assigned position of this tool call
  within the turn; a non-streaming `Response.ToolCalls` entry also
  carries its `Index`, so both paths use the same identity field.
  `Arguments` holds the raw argument bytes; the caller decodes them
  against the matching `ToolDefinition.Schema`.
- `type Usage struct { PromptTokens int; CompletionTokens int; TotalTokens int; CachedTokens int }`
  reports token accounting for one completed turn. `CachedTokens`
  counts prompt tokens served from a provider-side cache, when the
  provider reports one; it is zero otherwise.
- `type Request struct { Model string; Messages []Message; Tools []ToolDefinition; Stream bool }`
  is the input to every `Completer` method. `Model` names the model
  the caller wants; an empty `Model` means the implementation's own
  default. `Tools` may be empty when the caller offers none.
- `type Response struct { Model string; Message Message; ToolCalls []ToolCall; Usage Usage; FinishReason string }`
  is the aggregated result of one turn. `Model` echoes the model that
  actually served the request, which may differ from `Request.Model`
  on a provider that redirects to a fallback. `ToolCalls` is empty
  when the model returned plain text.
- `type Chunk struct { Delta string; ToolCallDelta *ToolCall; Done bool; Usage Usage; FinishReason string; Err error }`
  is one increment of a streamed response. `Done` is true only on the
  final chunk that completes without error; `Usage` and
  `FinishReason` are the zero value until then, the same "zero until
  Done" pattern. `ToolCallDelta` is non-nil only on a chunk that
  carries a tool-call fragment. `Err` is nil on every chunk except a
  terminal chunk that reports a mid-stream failure; when a chunk
  carries a non-nil `Err`, the channel closes after it and no further
  chunk follows, whether or not `Done` was ever true for that stream.
  A chunk never carries both a non-nil `Err` and `Done == true`; a
  failure chunk reports failure instead of completion.
- `func (c Chunk) Validate() error` enforces the rule the paragraph
  above states: a `Chunk` with both `Err != nil` and `Done == true`
  is invalid, and `Validate` returns `ErrChunkErrDoneConflict` for it,
  following the same sentinel-error precedent named above. This is
  the only rule `Validate` enforces for phase 29; `RunTurn`'s drain
  loop calls `Validate` on every `Chunk` it reads before it applies
  the chunk's `Err` or `Done` value, and returns a `Validate` error
  unwrapped, in place of the zero `Response`, the same way it returns
  a `Chat` or `ChatStream` error.

  **Premature closure.** A `ChatStream` channel can close without
  ever sending a `Chunk` with `Done == true` or a non-nil `Err`, when
  the streaming goroutine on the `Completer` side exits early or
  crashes. `RunTurn`'s drain loop treats that closure as a stream
  failure, not a success: it returns the zero `Response` alongside
  `ErrStreamClosedEarly`, discarding any `Delta` or `ToolCallDelta`
  fragments already aggregated. This holds the principle stated
  above: a mid-stream failure never returns a partial `Response`, and
  a channel closing without a terminal chunk counts as a mid-stream
  failure.

  **Merge rule.** A model may stream two or more tool calls
  concurrently in one turn; a vendor API disambiguates fragments by
  position, not by ID, because `ID` and `Name` typically appear only
  on the first fragment of each call. A caller (or `RunTurn`) merges
  a sequence of `ToolCallDelta` values into a final `[]ToolCall` by
  this rule: group fragments by `Index`; within a group, `Arguments`
  bytes concatenate in arrival order; `ID` and `Name` take the first
  non-empty value seen for that `Index`, since later fragments in the
  same group carry an empty `ID` and `Name`. The merged group's
  `Index` is the group's key. The merged `[]ToolCall` slice is ordered
  by ascending `Index`.
- `type ContextAccountant interface { ContextWindow() int }` is the
  optional context-size capability described above.
- `type ReasoningPolicy interface { ReasoningEffort() string }` is
  the optional reasoning-policy capability described above.

No constructor. `provider` defines no concrete type to construct; a
caller builds its own `Completer` implementation and passes it where
one is needed.

## Tests

Test files live in `provider/provider_test/`:

- `types_test.go` — the red-green cases for the value types. Confirm
  `Request` and `Response` zero values behave as documented: empty
  `Model` on `Request`, empty `ToolCalls` on `Response`. Confirm
  `Role` constants compare equal to their string form and no case
  writes a raw role literal. Start with the assertions; confirm they
  fail on the empty phase; implement the types and watch them pass.
  `Message.Validate` cases, table-driven:
  - valid: `RoleUser` with empty `ToolCallID`; `RoleTool` with a
    non-empty `ToolCallID`; `RoleSystem` and `RoleAssistant` with
    empty `ToolCallID`; each asserts `Validate() == nil`.
  - invalid: `RoleUser` (and each non-tool role) with a non-empty
    `ToolCallID`, asserting `errors.Is(err,
    provider.ErrToolCallIDUnexpected)`; `RoleTool` with an empty
    `ToolCallID`, asserting `errors.Is(err,
    provider.ErrToolCallIDRequired)`; a `Role` value outside the four
    constants (a fabricated string) with any `ToolCallID`, asserting
    `errors.Is(err, provider.ErrUnknownRole)`.
  `Chunk.Validate` cases, table-driven:
  - valid: `Err == nil, Done == false`; `Err == nil, Done == true`;
    `Err != nil, Done == false`; each asserts `Validate() == nil`.
  - invalid: `Err != nil, Done == true`; asserts `errors.Is(err,
    provider.ErrChunkErrDoneConflict)`.
- `completer_test.go` — a fake `Completer` implemented in the test
  package. Cases:
  - `Chat` on the fake returns a fixed `Response`; assert the
    returned value matches, and that the fake recorded the exact
    `Request` it received.
  - `Chat` on a fake configured to fail returns a fixed error and the
    zero `Response`; assert the error is returned unwrapped
    (`errors.Is` against the fake's sentinel).
  - `ChatStream` on the fake returns a channel that yields three
    `Chunk` values, the last with `Done == true`; assert the drain
    order and the final `Usage`.
  - `ChatStream` on a fake configured to fail returns a fixed error
    and a nil channel; assert the error is returned unwrapped and no
    goroutine leaks (channel is nil, nothing to drain).
  - `RunTurn` with `Request.Stream == false` calls the fake's `Chat`
    path; the fake records which path ran, so the test asserts
    routing, not only the result.
  - `RunTurn` with `Request.Stream == true`, driven against a fake
    whose `ChatStream` channel yields three `Delta`-only `Chunk`
    values (no `ToolCallDelta`) terminated by a fourth `Chunk` with
    `Done == true`: assert `Response.Message.Content` equals the
    three `Delta` strings concatenated in order, and
    `Response.ToolCalls` is empty. This proves plain-text streaming
    aggregation through `RunTurn`, distinct from the tool-call merge
    case below.
  - `RunTurn` with `Request.Stream == true`, driven against a fake
    whose `ChatStream` channel yields a single invalid `Chunk`
    (`Err != nil` and `Done == true` on the same value): assert
    `RunTurn` returns an error satisfying `errors.Is(err,
    provider.ErrChunkErrDoneConflict)` and the zero `Response`. This
    proves `RunTurn`'s drain loop calls `Chunk.Validate` on a chunk
    read from a real `Completer`, not only on hand-built values in
    `types_test.go`.
  - `RunTurn` with `Request.Stream == true`, driven against a fake
    that streams two concurrent tool calls: the fake's `ChatStream`
    yields `Chunk` values over a real channel whose `ToolCallDelta`
    fragments interleave two `Index` values, first fragment of each
    group carrying `ID`/`Name`, later fragments carrying only
    `Arguments` bytes and an empty `ID`/`Name`, terminated by a
    `Chunk` with `Done == true`. The test calls `RunTurn(ctx, c,
    req)` directly — it does not drain or merge the channel itself —
    and asserts the returned `Response.ToolCalls` holds exactly two
    entries, ordered by ascending `Index`, each with concatenated
    `Arguments` and the first-seen `ID`/`Name`, and asserts
    `Response.Message.Role == RoleAssistant`. This exercises
    `RunTurn`'s own merge implementation, not a copy of the rule
    written in the test.
  - `RunTurn` with `Request.Stream == true`, driven against a fake
    whose channel yields the same two concurrent tool calls as above
    but with the `Index 1` fragments arriving before the `Index 0`
    fragments: asserts the returned `Response.ToolCalls` is still
    ordered by ascending `Index` (`[0, 1]`, not arrival order `[1,
    0]`). This exercises the merge's explicit sort, distinct from the
    in-order case above, which alone cannot tell a sorted result from
    an unsorted one that happens to arrive in order.
  - `RunTurn` with `Request.Stream == true`, driven against a fake
    whose channel yields two ordinary `Delta` chunks and then a
    terminal `Chunk` with a non-nil `Err` and no further chunks
    (mid-stream failure after partial output already flowed): assert
    `RunTurn` returns that exact `Err` unwrapped and the zero
    `Response`, discarding the two `Delta` chunks already drained.
  - `RunTurn` with `Request.Stream == true`, driven against a fake
    whose channel yields one or two ordinary `Delta` chunks and then
    closes without a terminal `Chunk` (no `Done == true`, no non-nil
    `Err`): assert `RunTurn` returns an error satisfying
    `errors.Is(err, provider.ErrStreamClosedEarly)` and the zero
    `Response`. This proves premature channel closure is a failure,
    not a silent partial success.
  - `RunTurn` propagates a `Chat` error and a `ChatStream`
    before-first-chunk error unchanged: same two failing fakes as in
    the `Chat`/`ChatStream` cases above, called through `RunTurn`
    with `Stream` false and true; assert the returned error equals
    the fake's sentinel and `Response` is the zero value.
  - `RunTurn` given a `Request.Messages` entry that fails
    `Message.Validate` (a `RoleTool` message with an empty
    `ToolCallID`) returns that `Validate` error unwrapped and the
    zero `Response`; the fake `Completer` records no call to `Chat`
    or `ChatStream`, proving `RunTurn` validates before it dispatches.
  - `RunTurn` against a fake whose `ChatStream` channel never closes
    and never yields another `Chunk` after the first: the test builds
    a `context.WithTimeout` (or a manually cancelled context) around
    the `RunTurn` call, cancels it while the drain is blocked waiting
    on the channel, and asserts `RunTurn` returns promptly with
    `ctx.Err()` and the zero `Response`. The test itself wraps the
    whole case in a short `time.After`-based timeout so a regression
    in `RunTurn`'s cancellation select fails the test instead of
    hanging the suite.
  - A second fake implements `ContextAccountant` and
    `ReasoningPolicy`; a third fake implements neither. Assert the
    type assertion succeeds on the second fake and fails cleanly
    (`ok == false`, no panic) on the third.
- `completer_integration_test.go` — build a fake `Completer` that
  simulates one full turn with a tool call: `RunTurn` returns a
  `Response` whose `ToolCalls` names one tool, the test builds a
  `RoleTool` reply `Message` with the matching `ToolCallID`, appends
  it to `Request.Messages`, and calls `RunTurn` again to get a final
  text `Response` with `FinishReason` set and empty `ToolCalls`. This
  proves the types compose across two turns without any concrete
  network client.
- `completer_bench_test.go` — benchmark `RunTurn` against the fake in
  non-streaming mode, one hundred sequential calls. Target under one
  microsecond per call, since the fake does no I/O. `AllocsPerRun`
  states the allocation budget; the builder records the measured
  baseline in this file.

## Verification

- This phase adds `docs/plans/provider.md`, the canonical package
  plan `scripts/check_plan.py` reads, in the same change as the code.
  This phase doc (`docs/plans/agents/phase29_provider.md`) is the
  phase contract with the full design reasoning; `docs/plans/memory.md`
  and `docs/plans/agents/phase15_memory.md` are the precedent for the
  split. `docs/plans/provider.md` must exist before `make api-update`
  runs, matching the memory precedent's ordering.
- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `provider` and for the total.
- `policy/layers.json` gains a `provider` row with an empty import
  list, landed with this plan before the code.
- `api/provider.txt` lands via `make api-update` in the same change
  as the code, holding `Completer`, `RunTurn`, `Role` and its
  constants, `Message`, `Message.Validate`, `ErrToolCallIDRequired`,
  `ErrToolCallIDUnexpected`, `ErrUnknownRole`, `ToolDefinition`,
  `ToolCall` (with `Index`), `Usage`, `Request`, `Response` (with
  `FinishReason`), `Chunk` (with `FinishReason`), `Chunk.Validate`,
  `ErrChunkErrDoneConflict`, `ErrStreamClosedEarly`,
  `ContextAccountant`, and `ReasoningPolicy`.
- `docs/architecture.md` gains a `provider/` bullet describing the
  leaf interface and its zero internal imports, in the same change as
  the code.
- `AGENTS.md`'s Layout section gains a `provider/` line describing the
  package in one sentence, in the same change as the code.
- `docs/packages/provider.md` documents the exported surface, matching
  the docs-maintenance convention already used for `envelope` and
  `room`.
- This phase adds no conformance vector. `provider` defines no wire
  format; it carries in-process values only. A future concrete client
  package, should one land in this module, owns its own wire
  conformance vectors against the vendor API it calls.
- No `agent` change lands in this phase. `agent`'s row in
  `policy/layers.json` stays unchanged; wiring `provider` into `agent`
  is a later phase's plan.
