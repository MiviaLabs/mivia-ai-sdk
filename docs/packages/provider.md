# Package reference: provider

The provider package defines one interface a caller uses to complete
a chat turn against a language model, plus the request and response
shapes the interface carries. The interface has no implementation in
this SDK; a caller supplies a concrete type. The exported surface
below mirrors `api/provider.txt`.

## Types

- `Completer` — the required contract: `Name`, `Chat`, `ChatStream`.
- `Role` — a message's role. Constants: `RoleSystem`, `RoleUser`,
  `RoleAssistant`, `RoleTool`.
- `Message` — one turn in a conversation. `Role`, `Content`, `Name`,
  `ToolCallID`, `ToolCalls`, `ReasoningContent`, `CreatedAt`. `Name` is
  legal only on `RoleUser` and `RoleTool`; an empty `Name` is legal on
  every role. `ReasoningContent` is legal only on `RoleAssistant`.
  `CreatedAt` is wall-clock time for when the message entered the
  caller's own history; its zero value means unknown, on every role.
- `ToolDefinition` — one tool a model may call.
- `ToolCall` — one call the model requests, or one fragment while it
  streams.
- `Usage` — token accounting for one completed turn.
- `Request` — the input to every `Completer` method. Adds `Temperature`
  and `MaxTokens` (`*float64`/`*int`; nil means "use the completer's
  own default"), `ToolChoice`, `Timeout`, `SessionID`,
  `DisableProviderReplay`, `ReasoningEffort`, `ReasoningDialect`, and
  `StreamingWriter` (`io.Writer`; nil changes no behavior; no code in
  this SDK writes to it yet).
- `ToolChoice` — controls whether and how a completion may call a
  tool. Constants: `ToolChoiceAuto`, `ToolChoiceNone`. The empty value
  means unspecified.
- `ReasoningDialect` — names the wire dialect a `Completer` uses to
  carry `ReasoningEffort` to its provider. Opaque: `provider` defines
  no closed set of dialect names; a concrete client owns its own
  vocabulary.
- `Response` — the aggregated result of one turn. Adds `CacheUsage` and
  `WebSearch`. Carries no separate reasoning-content field;
  `Response.Message.ReasoningContent` already holds it.
- `Chunk` — one increment of a streamed response. Adds
  `ReasoningDelta`, `CacheUsage`, and `WebSearch`, zero until `Done`,
  the same convention `Usage` and `FinishReason` follow.
- `CacheStyle` — names how a provider's wire format expresses
  prompt-cache reuse for one turn. Constants: `CacheStyleNone`,
  `CacheStyleImplicit`, `CacheStyleExplicit`.
- `CacheUsage` — provider-side prompt-cache accounting for one turn.
  `Reported == false` means the provider's response carried none of
  the recognized cache-usage fields; every other field is meaningless
  then.
- `WebSearchResult` — one provider-supplied search result attached to
  a completion. Every field is a raw transport-level string; `provider`
  does not interpret or render it.
- `ContextAccountant` — an optional capability exposing the bound
  model's maximum token count.
- `ReasoningPolicy` — an optional capability exposing the configured
  reasoning-effort level.
- `TokenEstimator` — an optional capability exposing a best-effort
  token count for a given `Request`, ahead of a `Chat` or `ChatStream`
  call.
- `ReasoningEffort` — the provider-neutral reasoning-effort
  vocabulary. Constants: `ReasoningEffortNone`, `ReasoningEffortLow`,
  `ReasoningEffortMedium`, `ReasoningEffortHigh`. `ReasoningPolicy`
  reports one of these as a string.
- `ReasoningBlock` — one reasoning segment a model produced. `Content`
  is empty whenever `Redacted` is true. Never appears on `Message` or
  `Response`; a caller carries it alongside its own session state.
- `ReasoningEventKind` — the `contextstate.SourceEvent.Kind` value
  that marks a reasoning trace. The one place the literal appears;
  `contextplan.IsReasoningEvent` compares against this constant.
- `MaxNameBytes` — 128, the byte bound `Message.Validate` applies to a
  non-empty `Name`.

## Reasoning fold

`provider` carries the reasoning vocabulary alongside the completer
interface, so a caller and `contextplan` share one set of types
without either importing the other.

- `RedactBlock(b ReasoningBlock) ReasoningBlock` — returns `b` with
  `Content` cleared and `Redacted` set true. Idempotent: a second call
  on an already-redacted block returns it unchanged.

## Functions and methods

- `RunTurn(ctx, c, req)` — calls `req.Validate()` once, then validates
  every `req.Messages` entry with `Message.Validate`, before it
  dispatches on `req.Stream`: calls `c.Chat` when false; calls
  `c.ChatStream`, drains, and aggregates when true. Selects on
  `ctx.Done()` while it drains a stream.
- `Message.Validate()` — enforces the `ToolCallID`/`Role` pairing rule,
  the closed set of `Role` constants, the `Name` rule, the
  `ToolCalls`/`Role` rule, and the `ReasoningContent`/`Role` rule.
- `Chunk.Validate()` — enforces `Err` and `Done == true` are mutually
  exclusive on one `Chunk`.
- `Request.Validate()` — enforces the closed `ToolChoice` vocabulary:
  `""`, `ToolChoiceAuto`, or `ToolChoiceNone`.

## Failure modes

Use `errors.Is` to test these.

- `ErrToolCallIDUnexpected` ("provider: tool call id unexpected outside
  RoleTool") — `Message.Validate` returns it when `ToolCallID` is
  non-empty on a message whose `Role` is not `RoleTool`. Pinned by
  `provider/provider_test/types_test.go`.
- `ErrToolCallIDRequired` ("provider: tool call id required for
  RoleTool") — `Message.Validate` returns it when `ToolCallID` is empty
  on a `RoleTool` message; `RunTurn` surfaces the same error unwrapped.
  Pinned by `provider/provider_test/types_test.go` and
  `provider/provider_test/runturn_test.go`.
- `ErrUnknownRole` ("provider: unknown role") — `Message.Validate`
  returns it for a `Role` outside the four declared constants. Pinned
  by `provider/provider_test/types_test.go` and
  `provider/provider_test/runturn_test.go`.
- `ErrToolCallsUnexpected` ("provider: tool calls unexpected outside
  RoleAssistant") — `Message.Validate` returns it when `ToolCalls` is
  non-empty on a `Message` whose `Role` is not `RoleAssistant`. Pinned
  by `provider/provider_test/types_test.go` and
  `provider/provider_test/validate_fuzz_test.go`.
- `ErrChunkErrDoneConflict` ("provider: chunk carries both Err and
  Done") — `Chunk.Validate` returns it when a `Chunk` carries both a
  non-nil `Err` and `Done == true`. Pinned by
  `provider/provider_test/runturn_test.go`.
- `ErrStreamClosedEarly` ("provider: stream closed before a terminal
  chunk") — `RunTurn` returns it when a `ChatStream` channel closes
  before any `Chunk` carries `Done == true` or a non-nil `Err`. Pinned
  by `provider/provider_test/runturn_test.go`.
- `ErrNameUnexpected` ("provider: name unexpected outside RoleUser and
  RoleTool") — `Message.Validate` returns it when `Name` is non-empty
  on a message whose `Role` is `RoleSystem` or `RoleAssistant`. Pinned
  by `provider/provider_test/types_test.go`.
- `ErrNameInvalid` ("provider: name is invalid or too long") —
  `Message.Validate` returns it when a non-empty `Name` exceeds
  `MaxNameBytes`, is not valid UTF-8, or carries a control character.
  Pinned by `provider/provider_test/types_test.go`.
- `ErrPromptTooLong` ("provider: prompt exceeds the model context
  window") — a caller's `Completer` returns or wraps it when the
  provider rejects an over-long prompt. `provider` ships no
  implementation itself; `agentloop` tests for it with `errors.Is` on
  its recovery path.
- `ErrReasoningContentUnexpected` ("provider: reasoning content
  unexpected outside RoleAssistant") — `Message.Validate` returns it
  when `ReasoningContent` is non-empty on a message whose `Role` is
  not `RoleAssistant`. Pinned by
  `provider/provider_test/types_test.go` and
  `provider/provider_test/validate_fuzz_test.go`.
- `ErrToolChoiceInvalid` ("provider: tool choice is not auto, none, or
  empty") — `Request.Validate` returns it when `ToolChoice` holds any
  value other than `""`, `ToolChoiceAuto`, or `ToolChoiceNone`. Pinned
  by `provider/provider_test/types_test.go`.

## Invariants

`Message.Validate`, `Chunk.Validate`, and `RunTurn` enforce the rules
below.

- `Message.Validate` checks `Role` legality first: an unknown `Role`
  always returns `ErrUnknownRole`, regardless of `Name`, `ToolCallID`,
  or `ToolCalls`.
- `Message.Validate` checks the `Name` rule after the `Role` check and
  before the `ToolCallID` pairing check: a non-empty `Name` outside
  `RoleUser` and `RoleTool` returns `ErrNameUnexpected`; a non-empty
  `Name` over `MaxNameBytes`, not valid UTF-8, or carrying a control
  character returns `ErrNameInvalid`. An empty `Name` is always legal.
- `Message.Validate` then checks the `ToolCallID` pairing rule only
  for one of the four known roles.
- `Message.Validate` rejects a non-empty `ToolCalls` on any known role
  other than `RoleAssistant`; the `ToolCallID` check runs first.
- `Message.Validate` checks the `ReasoningContent` rule last, after
  the `ToolCalls` check: a non-empty `ReasoningContent` outside
  `RoleAssistant` returns `ErrReasoningContentUnexpected`.
- `RunTurn` calls `req.Validate()` before it validates any
  `req.Messages` entry and before it dispatches; a `Validate` failure
  returns the zero `Response` and that error, unwrapped.
- `RunTurn` sets `Response.Message.ToolCalls` to the same merged calls
  it sets on `Response.ToolCalls`, and sets
  `Response.Message.ReasoningContent` to the concatenated
  `ReasoningDelta` text, on the streamed path.
- `RunTurn` copies the terminal `Chunk`'s `CacheUsage` and `WebSearch`
  onto `Response.CacheUsage` and `Response.WebSearch`, on the streamed
  path.
- `Chat` always waits for the complete response before it returns; a
  caller ignores `Request.Stream` when it calls `Chat` directly.
- `ChatStream` always returns a channel immediately; the channel
  closes after the final `Chunk`.
- `RunTurn` validates every `req.Messages` entry before it calls
  either `Completer` method. The first invalid entry stops validation
  and returns that error, unwrapped; no `Completer` method runs.
- `RunTurn`'s drain loop calls `Chunk.Validate` on every `Chunk` it
  reads, before it applies the chunk's `Err` or `Done` value, and
  returns a `Validate` error unwrapped in place of the zero `Response`.
- `RunTurn` merges `ToolCallDelta` fragments by `Index`: `Arguments`
  concatenate in arrival order; `ID` and `Name` take the first
  non-empty value seen for that `Index`. The merged `[]ToolCall` slice
  is ordered by ascending `Index`.
- `RunTurn` sets `Response.Message.Role = RoleAssistant`
  unconditionally on the streamed path.
- On a drained `Chunk` with a non-nil `Err`, `RunTurn` discards any
  partial aggregation and returns that `Err` unwrapped alongside the
  zero `Response`.
- When the `ChatStream` channel closes before any `Chunk` carries
  `Done == true` or a non-nil `Err`, `RunTurn` discards any partial
  aggregation and returns the zero `Response` alongside
  `ErrStreamClosedEarly`.
- `RunTurn` selects on `ctx.Done()` on every iteration of the drain
  loop. When `ctx` finishes first, `RunTurn` returns the zero
  `Response` alongside `ctx.Err()`.

## Wire contract

`provider` defines no wire format. It carries in-process values only;
no conformance vector applies. A future concrete client package owns
its own wire conformance against the vendor API it calls.

## Usage

```go
package main

import (
    "context"
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// echoCompleter is a minimal Completer for demonstration: it never
// calls a network.
type echoCompleter struct{}

func (echoCompleter) Name() string { return "echo" }

func (echoCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
    last := req.Messages[len(req.Messages)-1]
    return provider.Response{
        Model:        req.Model,
        Message:      provider.Message{Role: provider.RoleAssistant, Content: "echo: " + last.Content},
        FinishReason: "stop",
    }, nil
}

func (echoCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
    ch := make(chan provider.Chunk, 1)
    ch <- provider.Chunk{Done: true, FinishReason: "stop"}
    close(ch)
    return ch, nil
}

func main() {
    c := echoCompleter{}
    req := provider.Request{
        Model:    "demo",
        Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
    }
    resp, err := provider.RunTurn(context.Background(), c, req)
    if err != nil {
        panic(err)
    }
    fmt.Println(resp.Message.Content)
}
```

### What the program shows

`RunTurn` dispatches to `Chat` because `req.Stream` is false. The
`echoCompleter` never calls a network; it builds its `Response`
in-process. The program prints `echo: hello`.
