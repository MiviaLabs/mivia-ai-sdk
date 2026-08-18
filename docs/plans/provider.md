# Plan: provider

Status: shipped. A new leaf package with no internal imports and no
third-party import. `provider` defines the contract only; no concrete
client ships in this package.

## Goal

Define one interface a caller uses to complete a chat turn against a
language model, plus the request and response shapes the interface
carries. The interface has no implementation in this SDK. A caller
supplies a concrete type; the SDK only names the contract.

## Scope

Inside: the `Completer` interface, the `RunTurn` dispatch function,
the `Request` and `Response` types, the `Message`, `ToolDefinition`,
`ToolCall`, `Usage`, and `Chunk` types, and two optional extension
interfaces for context accounting and reasoning policy.

Outside: any concrete client. No HTTP call, no vendor SDK, no
streaming transport. AGENTS.md forbids a third-party import outside
`a2aclient`; a concrete OpenAI, Anthropic, or local-model client is a
separate module or a separate SDK, never this package. Outside: the
`agent` composition wiring. A future phase adds a `Model` field or a
`Complete` method to `agent.Agent` and threads a `Completer` through
`Run`, the same way phase 13 threaded `AckWait`. That wiring needs its
own plan review. Outside: retry, backoff, rate limiting, and cost
tracking. Those are caller or future-phase concerns layered on top of
one `Chat` call, not part of the contract itself.

`provider` imports nothing internal. No third-party import. Stdlib
only: `context`, `errors`, `fmt`. A leaf package stays easiest to test
and depend on: any future package that needs a model binding imports
`provider` alone, with no transitive pull-in. This matches AGENTS.md's
dependency-inward design: leaf blocks first, composition last.

`Completer` has zero implementations in this SDK and two or more
expected outside it: at minimum a hosted-API client and a local or
test double. A caller swaps providers without touching `agent` or any
other block, the reasoning `tools.Tool` already sets. `Chat` always
waits for the complete response before it returns; a caller ignores
`Request.Stream` when it calls `Chat` directly. `ChatStream` always
returns a channel of `Chunk` values immediately; the channel closes
after the final chunk, which carries the completed `Usage` totals or a
non-nil `Err` on a mid-stream failure.

## API

The surface below is the lock target. It lands in `api/provider.txt`
via `make api-update`.

- `type Completer interface { Name() string; Chat(ctx context.Context, req Request) (Response, error); ChatStream(ctx context.Context, req Request) (<-chan Chunk, error) }`
  is the required contract. `Name` returns the provider's own label,
  for logs and error messages.
- `func RunTurn(ctx context.Context, c Completer, req Request) (Response, error)`
  dispatches on `req.Stream`: calls `c.Chat` when false; calls
  `c.ChatStream`, drains, and aggregates when true. `RunTurn` is a
  package function, not a `Completer` method, since its logic depends
  only on `Chat` and `ChatStream`; every implementation would
  otherwise hand-write the same dispatch and aggregation code.

  `RunTurn` validates every entry of `req.Messages` with
  `Message.Validate`, in order, before it calls either method. The
  first invalid entry stops validation; `RunTurn` returns the zero
  `Response` and that error, unwrapped, and calls neither `Completer`
  method.

  Aggregation merges `Chunk` values into one `Response`: `Delta`
  values concatenate into `Response.Message.Content`; the final
  chunk's `Usage` becomes `Response.Usage`; the final chunk's
  `FinishReason` becomes `Response.FinishReason`; `Response.Message.
  Role` is set to `RoleAssistant` unconditionally, since `Chunk`
  carries no `Role` field and every streamed chunk answers the
  caller's turn. `RunTurn` calls `Chunk.Validate` on every chunk it
  reads and returns a `Validate` error unwrapped, in place of the zero
  `Response`, the same way it returns a `Chat` or `ChatStream` error.

  Tool-call deltas merge by this rule: group `ToolCallDelta` fragments
  by `Index`; within a group, `Arguments` bytes concatenate in arrival
  order; `ID` and `Name` take the first non-empty value seen for that
  `Index`, since later fragments in the same group carry an empty `ID`
  and `Name`. The merged `[]ToolCall` slice is ordered by ascending
  `Index`, not arrival order.

  `RunTurn` returns the first error either method returns, unwrapped,
  alongside the zero `Response`. A stream can also fail after chunks
  have already flowed: when a drained `Chunk` carries a non-nil `Err`,
  `RunTurn` stops draining at that chunk and returns the zero
  `Response` alongside that `Err`, discarding any partial aggregation.
  A channel that closes without ever sending a `Chunk` with
  `Done == true` or a non-nil `Err` is a stream failure too: `RunTurn`
  returns `ErrStreamClosedEarly` alongside the zero `Response`. A
  mid-stream failure never returns a partial `Response`.

  `RunTurn` selects on `ctx.Done()` on every iteration of the drain
  loop, against the next `Chunk` and against the channel closing;
  whichever is ready first wins. When `ctx` is done first, `RunTurn`
  stops draining immediately, discards any partial aggregation, and
  returns the zero `Response` alongside `ctx.Err()`. `RunTurn` does
  not check `ctx.Done()` before starting the drain of a non-nil
  channel `ChatStream` already returned; a `Completer` that wants a
  pre-call cancellation check performs it inside `Chat` or
  `ChatStream` before it returns.
- `type Role string` with constants `RoleSystem`, `RoleUser`,
  `RoleAssistant`, and `RoleTool` names a message's role.
- `type Message struct { Role Role; Content string; ToolCallID string }`
  is one turn in the conversation `Request.Messages` carries.
- `func (m Message) Validate() error` enforces the `ToolCallID`/`Role`
  pairing rule and the closed set of `Role` constants, each with its
  own sentinel error: `ErrToolCallIDUnexpected`,
  `ErrToolCallIDRequired`, and `ErrUnknownRole`. `Validate` checks
  `Role` legality first: an unknown `Role` always returns
  `ErrUnknownRole`, regardless of `ToolCallID`.
- `type ToolDefinition struct { Name string; Description string; Schema []byte }`
  names one tool a model may call.
- `type ToolCall struct { Index int; ID string; Name string; Arguments []byte }`
  is one call the model requests, or one fragment of a call while it
  streams.
- `type Usage struct { PromptTokens int; CompletionTokens int; TotalTokens int; CachedTokens int }`
  reports token accounting for one completed turn.
- `type Request struct { Model string; Messages []Message; Tools []ToolDefinition; Stream bool }`
  is the input to every `Completer` method.
- `type Response struct { Model string; Message Message; ToolCalls []ToolCall; Usage Usage; FinishReason string }`
  is the aggregated result of one turn.
- `type Chunk struct { Delta string; ToolCallDelta *ToolCall; Done bool; Usage Usage; FinishReason string; Err error }`
  is one increment of a streamed response. `FinishReason` is the zero
  value until the terminal chunk, the same "zero until Done" pattern
  `Usage` follows.
- `func (c Chunk) Validate() error` enforces `Err` and `Done == true`
  are mutually exclusive on one `Chunk`, returning
  `ErrChunkErrDoneConflict` when they are not.
- `ErrStreamClosedEarly` is `RunTurn`'s error when a `Completer`'s
  `ChatStream` channel closes before any `Chunk` carries
  `Done == true` or a non-nil `Err`. `RunTurn` returns the zero
  `Response` alongside this error; a premature channel closure never
  returns a partial `Response`.
- `type ContextAccountant interface { ContextWindow() int }` is the
  optional context-size capability.
- `type ReasoningPolicy interface { ReasoningEffort() string }` is
  the optional reasoning-policy capability.
- `type TokenEstimator interface { EstimateTokens(req Request) (int, error) }`
  is the optional token-estimation capability, added in phase 44
  (`docs/plans/agents/phase44_provider_token_estimation.md`). It
  exposes a best-effort token count for a given `Request`, ahead of a
  `Chat` or `ChatStream` call. `EstimateTokens` returns a non-nil
  error when it cannot produce an estimate; it returns `(0, nil)` only
  for a `Request` the implementation judges to cost zero tokens.

No constructor. `provider` defines no concrete type to construct; a
caller builds its own `Completer` implementation and passes it where
one is needed.

An implementation may additionally satisfy `ContextAccountant`,
`ReasoningPolicy`, or both. A caller type-asserts:
`if ca, ok := c.(provider.ContextAccountant); ok { ... }`. Neither
interface is required on `Completer`; this mirrors the optional
capability pattern `tools.ProfiledTool` already sets: a small required
interface, plus narrow opt-in interfaces a concrete type adds only
when it has the data.

## Tests

Test files live in `provider/provider_test/`:

- `types_test.go` — red-green cases for the value types. `Request` and
  `Response` zero values behave as documented. `Role` constants
  compare equal to their string form. `Message.Validate` cases:
  valid for each role with a correctly paired `ToolCallID`; invalid
  for a non-tool role carrying a `ToolCallID`
  (`ErrToolCallIDUnexpected`), a `RoleTool` message with an empty
  `ToolCallID` (`ErrToolCallIDRequired`), and an unknown `Role`
  (`ErrUnknownRole`, checked first regardless of `ToolCallID`).
  `Chunk.Validate` cases: valid for every `Err`/`Done` combination
  except both set; invalid, asserting `ErrChunkErrDoneConflict`, when
  both are set.
- `completer_test.go` — a fake `Completer` in the test package.
  `Chat` and `ChatStream` cases assert the fake's recorded request,
  its returned value, and its unwrapped error on a configured
  failure. `RunTurn` cases: dispatches to `Chat` when
  `Request.Stream` is false and to `ChatStream` when true; aggregates
  plain-text `Delta` chunks into `Response.Message.Content`; calls
  `Chunk.Validate` on a chunk read from the fake and returns
  `ErrChunkErrDoneConflict` unwrapped for a real, not hand-built,
  invalid chunk; merges two concurrent tool-call streams by `Index`
  regardless of fragment arrival order, asserting
  `Response.Message.Role == RoleAssistant`; returns the fake's error
  unwrapped and the zero `Response` on a `Chat` or `ChatStream`
  failure; discards partial output and returns the terminal `Err`
  unwrapped on a mid-stream failure after chunks already flowed;
  returns `ErrStreamClosedEarly` on a channel that closes with no
  terminal chunk; returns a `Message.Validate` error unwrapped and
  calls neither `Completer` method for an invalid `req.Messages`
  entry; returns `ctx.Err()` promptly, under a test-level timeout,
  when the caller cancels `ctx` while the drain is blocked on a
  channel that never yields another chunk. A second fake implements
  `ContextAccountant` and `ReasoningPolicy`; a third implements
  neither; the type assertion succeeds on the second and fails
  cleanly on the third.
- `completer_integration_test.go` — a fake `Completer` simulates one
  full turn with a tool call: `RunTurn` returns a `Response` naming
  one tool, the test builds a `RoleTool` reply `Message` with the
  matching `ToolCallID`, appends it to `Request.Messages`, and calls
  `RunTurn` again for a final text `Response` with `FinishReason` set
  and empty `ToolCalls`. This proves the types compose across two
  turns with no concrete network client.
- `completer_bench_test.go` — benchmark `RunTurn` against the fake in
  non-streaming mode, one hundred sequential calls. Target under one
  microsecond per call, since the fake does no I/O. `AllocsPerRun`
  states the measured allocation budget.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `provider` and for the total.
- `policy/layers.json` carries a `provider` row with an empty import
  list, landed with this plan before the code.
- `api/provider.txt` lands via `make api-update` in the same change
  as the code, holding `Completer`, `RunTurn`, `Role` and its
  constants, `Message`, `Message.Validate`, `ErrToolCallIDRequired`,
  `ErrToolCallIDUnexpected`, `ErrUnknownRole`, `ToolDefinition`,
  `ToolCall`, `Usage`, `Request`, `Response`, `Chunk`,
  `Chunk.Validate`, `ErrChunkErrDoneConflict`, `ErrStreamClosedEarly`,
  `ContextAccountant`, and `ReasoningPolicy`.
- `docs/architecture.md` gains a `provider/` bullet describing the
  leaf interface and its zero internal imports, in the same change as
  the code.
- `AGENTS.md`'s Layout section gains a `provider/` line, in the same
  change as the code.
- `docs/packages/provider.md` documents the exported surface, matching
  the docs-maintenance convention already used for `envelope` and
  `room`.
- This phase adds no conformance vector. `provider` defines no wire
  format; it carries in-process values only.
- No `agent` change lands in this phase. `agent`'s row in
  `policy/layers.json` stays unchanged; wiring `provider` into `agent`
  is a later phase's plan.
