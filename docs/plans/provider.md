# Plan: provider

Status: ready to build. Phase contract at
docs/plans/agents/phase29_provider.md. This file is the package plan
docs/plans/TEMPLATE.md and scripts/check_plan.py require.

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
only: `context`, `errors`, `fmt`.

## API

The surface below is the lock target. It lands in `api/provider.txt`
via `make api-update`.

- `type Completer interface { Name() string; Chat(ctx context.Context, req Request) (Response, error); ChatStream(ctx context.Context, req Request) (<-chan Chunk, error) }`
  is the required contract. `Name` returns the provider's own label,
  for logs and error messages.
- `func RunTurn(ctx context.Context, c Completer, req Request) (Response, error)`
  dispatches on `req.Stream`: calls `c.Chat` when false; calls
  `c.ChatStream` and drains and aggregates when true. See the phase
  doc for the full aggregation, validation, and cancellation
  contract.
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

No constructor. `provider` defines no concrete type to construct; a
caller builds its own `Completer` implementation and passes it where
one is needed.

The full field-by-field rationale, the merge rule for streamed tool
calls, the `RunTurn` validation and cancellation contract, and the
aggregated `Response.Message.Role` rule live in
docs/plans/agents/phase29_provider.md. This file states the locked
surface; that file states the design reasoning.

## Tests

Test files live in `provider/provider_test/`. The case list —
`types_test.go`, `completer_test.go`, `completer_integration_test.go`,
and `completer_bench_test.go` — is defined in full in
docs/plans/agents/phase29_provider.md under Tests. That list is
binding; this file does not repeat it to avoid drift between the two
documents.

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
