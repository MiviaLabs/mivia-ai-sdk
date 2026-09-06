# Plan: provider

Status: shipped. A new leaf package with no internal imports and no
third-party import. `provider` defines the contract only; no concrete
client ships in this package.

## Goal

Define one interface a caller uses to complete a chat turn against a
language model, plus the request and response shapes the interface
carries. `provider` itself defines the contract only; `provider/anthropic`
is this SDK's one implementation, the Anthropic Messages API adapter
(see `docs/plans/provider/anthropic.md`). A caller may also supply its
own concrete type.

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

Named callers, for the zero-caller Request controls: an external
`Completer` implementation honors `Request.Timeout` as a caller-side
deadline override, and a session-keyed client multiplexer uses
`Request.SessionID`. `provider` itself reads neither; `RunTurn`
forwards both untouched, and every in-tree adapter may ignore them.

`provider` imports nothing internal. No third-party import. Stdlib
only: `context`, `errors`, `fmt`, `time`. A leaf package stays easiest to test
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
- `type Message struct { Role Role; Content string; ToolCallID string; ToolCalls []ToolCall }`
  is one turn in the conversation Request.Messages carries. `ToolCalls`
  is non-empty only on a `RoleAssistant` message; it holds the calls
  that assistant turn made, in the same `[]ToolCall` shape
  `Response.ToolCalls` already returns.
- `func (m Message) Validate() error` enforces the `ToolCallID`/`Role`
  pairing rule, the closed set of `Role` constants, and the `ToolCalls`
  rule, each with its own sentinel error:
  `ErrToolCallIDUnexpected`, `ErrToolCallIDRequired`,
  `ErrUnknownRole`, and `ErrToolCallsUnexpected`. `Validate` checks
  `Role` legality first: an unknown `Role` always returns
  `ErrUnknownRole`, regardless of `ToolCallID`. On a known `Role` other
  than `RoleAssistant`, a non-empty `ToolCalls` returns
  `ErrToolCallsUnexpected`. The `ToolCallID` check runs before the
  `ToolCalls` check, so it wins on precedence.
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
  is the optional token-estimation capability, added in phase 44. It
  exposes a best-effort token count for a given `Request`, ahead of a
  `Chat` or `ChatStream` call. `EstimateTokens` returns a non-nil
  error when it cannot produce an estimate; it returns `(0, nil)` only
  for a `Request` the implementation judges to cost zero tokens.

- `const ReasoningEventKind = "reasoning"` names the
  `contextstate.SourceEvent.Kind` value a reasoning trace carries, in
  `reasoning.go`. `type ReasoningEffort string` with constants
  `ReasoningEffortNone`, `ReasoningEffortLow`, `ReasoningEffortMedium`,
  and `ReasoningEffortHigh` closes the reasoning-effort vocabulary
  `ReasoningPolicy.ReasoningEffort()` reports as a string.
  `type ReasoningBlock struct { Content string; Redacted bool }` is
  one reasoning segment; it never appears on `Message` or `Response`.
  `func RedactBlock(b ReasoningBlock) ReasoningBlock` clears `Content`
  and sets `Redacted`, idempotently. This fold ships as the companion
  change `docs/plans/contextplan.md` names: `contextplan` compares
  `contextstate.SourceEvent.Kind` against `ReasoningEventKind` instead
  of a literal.

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

## Change: message names and the prompt-too-long sentinel

Status: shipped. Two additions: a `Name` field on `Message` with
validation rules, and one sentinel error for prompt-too-long
rejections. No other shape changes.

### Change goal

Carry a name on a message, so a host frame and a tool result name
survive the trip through compaction and wire adapters. Name one
sentinel for a provider's context-window rejection, so a caller can
detect it with `errors.Is`.

### Change scope

Inside:

- `Name string` on `Message`.
- Two sentinels and one bound constant for `Name` validation.
- `ErrPromptTooLong`, one sentinel error. No behavior attaches to it
  here; `provider` ships no completer that returns it.

Outside:

- Any `Request` change. `Request` gains no `MaxTokens`, no
  temperature, and no session field. The summarizer bound decision in
  `docs/plans/contextsummary.md` relies on this.
- Any cross-message pairing check. `Message.Validate` still checks one
  message alone; a tool result's name matching its call stays the
  caller's concern, as the shipped plan already states for pairing.
- Any wire or transport work. The field is an in-process value.

### Change API

```go
// MaxNameBytes bounds Message.Name when set.
const MaxNameBytes = 128

// ErrNameUnexpected is Validate's error when Name is non-empty on a
// Role other than RoleUser or RoleTool.
var ErrNameUnexpected = errors.New("provider: name unexpected outside RoleUser and RoleTool")

// ErrNameInvalid is Validate's error when a non-empty Name exceeds
// MaxNameBytes, is not valid UTF-8, or carries a control character.
var ErrNameInvalid = errors.New("provider: name is invalid or too long")

// ErrPromptTooLong marks a provider's rejection of a prompt that
// exceeds the model's context window. A Completer returns or wraps
// it; provider ships no implementation itself.
var ErrPromptTooLong = errors.New("provider: prompt exceeds the model context window")

// Message gains one field:
type Message struct {
    Role       Role
    Content    string
    Name       string
    ToolCallID string
    ToolCalls  []ToolCall
}
```

Rule decision, stated: a `Name` is legal on `RoleUser` and on
`RoleTool`, and illegal on `RoleSystem` and `RoleAssistant`. The two
legal carriers are exactly the two the port needs: a host frame rides
`RoleUser` (the injected `context-summary` message), and a tool result
names its tool on `RoleTool`. The reference allowed the same two
carriers and masked user names for its shape gate; this SDK has no
cross-message gate, so the per-message rule stands alone.

`Message.Validate` inserts the name checks after the role check and
before the `ToolCallID` pairing check: a non-empty `Name` on
`RoleSystem` or `RoleAssistant` returns `ErrNameUnexpected`; a
non-empty `Name` on any role then passes the form check against
`MaxNameBytes`, UTF-8 validity, and control characters, returning
`ErrNameInvalid` on failure. An empty `Name` is always legal.

`RunTurn` changes nothing: it already calls `Message.Validate` on
every entry, so the new rules apply on both paths with no new code.

### Change tests

In `provider/provider_test/types_test.go`, table-driven additions:

- A named `RoleUser` message and a named `RoleTool` message validate.
- A named `RoleSystem` message and a named `RoleAssistant` message
  fail `ErrNameUnexpected`.
- A `Name` over `MaxNameBytes`, a `Name` with invalid UTF-8, and a
  `Name` with a control character each fail `ErrNameInvalid`.
- An empty `Name` passes on every role.
- Precedence: an unknown role with a non-empty name still returns
  `ErrUnknownRole`; a `RoleSystem` message with both a name and a
  `ToolCallID` returns `ErrNameUnexpected` before
  `ErrToolCallIDUnexpected`.
- `ErrPromptTooLong` exists and is distinct from every other sentinel,
  asserted by string inequality across the sentinel set.

`completer_test.go` gains one `RunTurn` case: a request message
carrying an illegal name fails validation before either `Completer`
method runs.

### Change verification

- `make verify` passes; `provider` holds the 85 coverage floor.
- `api/provider.txt` gains `Name`, `MaxNameBytes`,
  `ErrNameUnexpected`, `ErrNameInvalid`, and `ErrPromptTooLong`
  through `make api-update`, in the same change as the code.
- `python3 scripts/check_plan.py`, `check_deps.py`, and
  `check_prose.py` pass. The `provider` row stays empty; it remains a
  leaf with stdlib-only imports.
- `docs/packages/provider.md` gains the field, the constant, and both
  sentinels in the same change as the code.
- No conformance vector: `envelope` owns the wire; this field is
  in-process only.
- Consumers in this change window: `contextplan.Compact` reads
  `Name` for `PreserveNames`; `contextsummary.SummaryMessage` sets it;
  `agentloop` tests `ErrPromptTooLong`. Each lands its own plan.

## Change: request controls, reasoning replay, and turn accounting

Status: shipped. This change widens the value types only. The
`Completer` interface keeps its three methods: `Name`, `Chat`,
`ChatStream(ctx, req) (<-chan Chunk, error)`. No new method, no new
required capability.

### Change goal

Carry the request controls, the reasoning-replay content, and the
turn-accounting fields a real hosted-model client needs, so a caller
does not have to fork `Request`, `Message`, `Response`, or `Chunk`
into its own package to add them. `provider` still ships no
implementation; every field here is a value the interface carries,
never logic the interface runs.

### Change scope

Inside:

- Eight new `Request` fields: `Temperature`, `MaxTokens`, `ToolChoice`,
  `Timeout`, `SessionID`, `DisableProviderReplay`, `ReasoningEffort`,
  and `ReasoningDialect`. Eight fields, one bullet.
- Two new `Message` fields: `ReasoningContent` and `CreatedAt`, plus
  the role rule `ReasoningContent` follows.
- Two new `Response` fields: `CacheUsage` and `WebSearch`.
- Three new `Chunk` fields that keep the streamed path able to
  produce the same `Response` shape the non-streamed path produces:
  `ReasoningDelta`, `CacheUsage`, `WebSearch`.
- Four new types: `ToolChoice`, `ReasoningDialect`, `CacheStyle`, and
  `CacheUsage`, plus one struct: `WebSearchResult`.
- One new sentinel, `ErrReasoningContentUnexpected`, and one new
  sentinel, `ErrToolChoiceInvalid`, plus the `Request.Validate` method
  that returns the second one.
- One new stdlib import, `time`, for `Request.Timeout` and
  `Message.CreatedAt`; the shipped plan's intro line, "Stdlib only:
  `context`, `errors`, `fmt`," must change in the same change.

Outside:

- Any concrete client. This change adds no HTTP call, no vendor
  wire-decode, and no factory or registry construction function. The
  reference file's `New`, `NewForProvider`, `Options`, and the
  built-in provider factory table stay out of this SDK; a concrete
  client is a separate module.
- `ContextAccountingAware` and `ContextAccountingProfile`. The
  reference file's byte- and token-cost estimator
  (`EstimateRequestCost`, `PruneMessagesKeepTurns`,
  `MessageTokensAt`, and the rest of its `context.go`) is a large,
  separate concern: per-message cost estimation and history pruning,
  not a request or response shape. `provider` already ships
  `TokenEstimator` for the estimation half of that concern; pruning
  stays a `contextplan`-level concern in this SDK. Out of scope here;
  a future plan ports it if a caller needs it.
- `ReasoningPolicyAware`'s `RequiresReplay` and `RejectReasoningLess`
  bits. `provider` already ships `ReasoningPolicy` as an optional
  capability with a different, narrower shape
  (`ReasoningEffort() string`). Widening that capability is a
  separate decision from carrying replay content on `Message`, and
  the gap list for this change names only the value types, not a new
  capability interface.
- Range validation on `Temperature` or `MaxTokens`. Valid ranges are
  provider-specific; a caller's `Completer` implementation checks its
  own provider's bounds. `provider` validates only the vocabulary it
  owns: `Role`, `Name`, and now `ToolChoice`.
- Provider-specific reasoning dialect names. See the design decision
  below.

### Change API

The surface below is the lock target. It lands in `api/provider.txt`
via `make api-update`, in the same change as the code.

`Request` gains eight fields:

```go
type Request struct {
    Model                  string
    Messages               []Message
    Tools                  []ToolDefinition
    Stream                 bool
    Temperature            *float64
    MaxTokens              *int
    ToolChoice             ToolChoice
    Timeout                time.Duration
    SessionID              string
    DisableProviderReplay  bool
    ReasoningEffort        ReasoningEffort
    ReasoningDialect       ReasoningDialect
}
```

- `Temperature *float64` and `MaxTokens *int` are pointers: a nil
  pointer means "the caller sent no override, use the completer's own
  default"; a non-nil pointer to `0` is a caller instruction, not an
  absent field. `Request` had no numeric field before this change, so
  this fixes the pointer convention for this package's first two.
- `ToolChoice ToolChoice` stays a plain value, not a pointer, because
  its zero value (`""`) already means "unspecified" and is distinct
  from `ToolChoiceAuto` and `ToolChoiceNone`, the same way `Role`'s
  zero value differs from its four named constants.
- `Timeout time.Duration` stays a plain value: zero already means "no
  caller-side timeout override", the standard meaning of a zero
  `time.Duration` in this module (`heartbeat.Monitor` uses the same
  rule).
- `SessionID string` and `DisableProviderReplay bool` stay plain
  values: an empty string and `false` are their natural "not set"
  reading, so a pointer buys nothing here.
- `ReasoningEffort ReasoningEffort` reuses the existing type from
  `reasoning.go`, rather than adding a new `ReasoningLevel` type. Its
  zero value (`""`) means "send no reasoning field at all", matching
  this change's design decision below; `ReasoningEffortNone` stays a
  distinct, explicit value from empty.
- `ReasoningDialect ReasoningDialect` is a new, opaque string type.
  See the design decision below for why it carries no constants in
  this package.

```go
// ToolChoice controls whether and how a completion may call a tool.
// The empty value means unspecified: the completer's own default
// applies. ToolChoiceAuto and ToolChoiceNone are the two closed,
// provider-neutral overrides Request.Validate accepts.
type ToolChoice string

const (
    ToolChoiceAuto ToolChoice = "auto"
    ToolChoiceNone ToolChoice = "none"
)

// ErrToolChoiceInvalid is Request.Validate's error when ToolChoice
// holds any value other than "", ToolChoiceAuto, or ToolChoiceNone.
var ErrToolChoiceInvalid = errors.New("provider: tool choice is not auto, none, or empty")

// Validate enforces the closed ToolChoice vocabulary. RunTurn calls
// it once, before it validates any Messages entry.
func (r Request) Validate() error

// ReasoningDialect names the wire dialect a Completer should use to
// carry ReasoningEffort to its provider. The empty value means "use
// the completer's own default dialect". provider defines no closed
// set of dialect names; a concrete client package owns its own
// vocabulary and compares against its own constants, never a
// provider literal.
type ReasoningDialect string
```

`Message` gains two fields and one role rule:

```go
type Message struct {
    Role             Role
    Content          string
    Name             string
    ToolCallID       string
    ToolCalls        []ToolCall
    ReasoningContent string
    CreatedAt        time.Time
}
```

- `ReasoningContent string` carries a model's chain-of-thought for one
  assistant turn, verbatim, for a completer whose provider requires
  the caller to echo it back on a later tool-call turn. `Validate`
  treats it the same way it already treats `ToolCalls`: legal only on
  `RoleAssistant`, and `ErrReasoningContentUnexpected` on any other
  role. `Validate` checks it last, after the `ToolCalls` check.
- `CreatedAt time.Time` is wall-clock time for when the message
  entered the caller's own history. The zero value means unknown.
  `Validate` applies no rule to it; every role may carry it or omit
  it.

```go
// ErrReasoningContentUnexpected is Validate's error when
// ReasoningContent is non-empty on a Message whose Role is not
// RoleAssistant.
var ErrReasoningContentUnexpected = errors.New("provider: reasoning content unexpected outside RoleAssistant")
```

`Response` gains two fields:

```go
type Response struct {
    Model        string
    Message      Message
    ToolCalls    []ToolCall
    Usage        Usage
    FinishReason string
    CacheUsage   CacheUsage
    WebSearch    []WebSearchResult
}
```

`Response` carries no separate `ReasoningContent` field.
`Response.Message.ReasoningContent` already holds it, since `Response`
holds `Message` as a field; a second, flat field would be a second
source of truth for the same value. This is a deliberate difference
from the reference file, whose `Response` does not embed its `Message`
type.

```go
// CacheStyle names how a provider's wire format expresses
// prompt-cache reuse for one turn.
type CacheStyle string

const (
    CacheStyleNone     CacheStyle = "none"
    CacheStyleImplicit CacheStyle = "implicit"
    CacheStyleExplicit CacheStyle = "explicit"
)

// CacheUsage reports provider-side prompt-cache accounting for one
// turn. Reported false means the provider's response carried none of
// the recognized cache-usage fields; every other field is meaningless
// when Reported is false, the same "reported flag gates the rest"
// shape TokenEstimator's callers already expect from Usage-adjacent
// types.
type CacheUsage struct {
    Reported          bool
    Style             CacheStyle
    InputTokens       int
    CachedInputTokens int
    CacheWriteTokens  int
}

// WebSearchResult is one provider-supplied search result attached to
// a completion. Every field is a raw transport-level string; provider
// does not interpret or render it. No JSON tag: provider carries
// in-process values only and defines no wire format of its own.
type WebSearchResult struct {
    Title       string
    Content     string
    Link        string
    Media       string
    Icon        string
    Refer       string
    PublishDate string
}
```

`Chunk` gains three fields, so the streamed path can build the same
`Response` shape the non-streamed path builds:

```go
type Chunk struct {
    Delta          string
    ToolCallDelta  *ToolCall
    Done           bool
    Usage          Usage
    FinishReason   string
    Err            error
    ReasoningDelta string
    CacheUsage     CacheUsage
    WebSearch      []WebSearchResult
}
```

- `ReasoningDelta string` follows `Delta`'s own rule: it concatenates,
  in arrival order, into `Response.Message.ReasoningContent`, the same
  way `Delta` concatenates into `Response.Message.Content`.
- `CacheUsage CacheUsage` and `WebSearch []WebSearchResult` follow
  `Usage`'s existing rule: the zero value on every non-terminal
  `Chunk`, and the real value only on the terminal chunk
  (`Done == true`). `RunTurn` copies the terminal chunk's values onto
  `Response.CacheUsage` and `Response.WebSearch`, the same way it
  already copies `Usage` and `FinishReason`.
- `Chunk.Validate` gains no new rule. `CacheUsage` and `WebSearch`
  follow the same unenforced "zero until Done" convention
  `FinishReason` already follows; `provider` does not police a
  producer that sets them early, the same way it does not police one
  today for `FinishReason`.

`RunTurn`'s aggregation contract changes to match:

- `RunTurn` calls `req.Validate()` once, before it validates any
  `Messages` entry and before it dispatches to `Chat` or
  `ChatStream`. A `Validate` failure returns the zero `Response` and
  that error, unwrapped, the same precedence `Message.Validate`
  failures already hold.
- `drainStream` accumulates `ReasoningDelta` into a second
  `strings.Builder`, alongside the existing `Delta` builder.
- `buildResponse` sets `Response.Message.ReasoningContent` from the
  accumulated `ReasoningDelta` text, and sets `Response.CacheUsage`
  and `Response.WebSearch` from the terminal chunk's values, alongside
  its existing `Usage` and `FinishReason` assignment.

### Design decisions

**Reasoning dial location: inside `provider`, not a new leaf
package.** The reference file's `reasoning.Level` and
`reasoning.Dialect` live in their own package because `internal/config`
also needs them, and `provider` already imports `config`; a shared
package avoids a cycle. This SDK has no such second consumer today:
nothing outside `provider` needs `ReasoningEffort` or
`ReasoningDialect` on its own. `provider` already carries its
reasoning vocabulary in-package (`reasoning.go`, shipped: `Reasoning
Effort`, `ReasoningBlock`, `RedactBlock`). Extending that same file
keeps one cohesive vocabulary in one package, matches
`AGENTS.md`'s rule against splitting a package with no real second
consumer, and adds no `policy/layers.json` row. A future plan moves
the vocabulary to its own leaf package only once a second package
needs it standalone.

**`ReasoningDialect` carries no constants in this package.** The
reference file's `Dialect` enumerates seven wire dialects
(`DialectOpenAI`, `DialectThinking`, and so on), each tied to a
specific provider's JSON shape. `provider` ships no concrete client
and makes no HTTP call, so it has no caller for those seven names
today; defining them here would be a taxonomy with no reader, which
`AGENTS.md`'s orchestrator guidance calls speculative generality. The
type stays open (`type ReasoningDialect string`) so a caller-supplied
`Completer` package defines and compares against its own constants,
the same way `Request.Model` is an opaque string this package never
enumerates.

**Streaming shape: keep `<-chan Chunk`, add no `ChatTurn` method and
no `io.Writer`.** The reference file streams through a caller-supplied
`io.Writer` on `Request.StreamWriter` and adds a third method,
`ChatTurn`, so a tool-calling turn has a place to return
`*Response` outside the streaming path. This package's `Chat` and
`ChatStream` already produce the same `Response` shape on both paths,
including `ToolCalls`, through `RunTurn`'s aggregation; a `ChatTurn`
method would duplicate that shape for no new information. A channel
also composes with `context` cancellation the way this package
already relies on: `RunTurn`'s drain loop already selects on
`ctx.Done()` against the channel, a pattern an `io.Writer` cannot
express without a second control channel. Two production consumers,
`agentloop` and `subagent`, already depend on the channel shape;
switching to a writer would force both to rewrite their aggregation
logic for no gain this change's gap list asks for. Widening `Chunk`
instead of the method set keeps the streamed and non-streamed paths
at parity with one aggregation function, `RunTurn`, instead of two.

**Pointer fields: `Temperature` and `MaxTokens` only.** Every other
new field's zero value already means "not set" without ambiguity
(`""` for strings and `ToolChoice`/`ReasoningDialect`, `0` for
`time.Duration`, `false` for a bool, the zero `time.Time` for
`CreatedAt`). `Temperature` and `MaxTokens` are the one case where a
caller-meant `0` (a deterministic sample, a hard token cap) reads
identically to "unset" if the field were a plain `float64` or `int`.
A pointer disambiguates the two, matching the reference file's own
`*float64`/`*int` choice for exactly these two fields, and matching
no other field in this package, because no other field has this
ambiguity.

### Change tests

Test files stay in `provider/provider_test/`. Additions, not new
files, since existing files already hold each type's tests.

- `types_test.go` — table-driven additions:
  - `Request.Validate` cases: `""`, `ToolChoiceAuto`, and
    `ToolChoiceNone` all pass; any other `ToolChoice` value fails
    `ErrToolChoiceInvalid`.
  - `Message.Validate` cases: a `RoleAssistant` message with
    `ReasoningContent` set passes; the same content on `RoleSystem`,
    `RoleUser`, and `RoleTool` each fail
    `ErrReasoningContentUnexpected`. Precedence: an unknown role with
    `ReasoningContent` set still returns `ErrUnknownRole` first, and a
    `RoleTool` message missing `ToolCallID` with `ReasoningContent`
    set returns `ErrToolCallIDRequired` before
    `ErrReasoningContentUnexpected`.
  - A zero-value `CreatedAt` and a set `CreatedAt` both pass
    `Message.Validate`, on every role.
  - `CacheUsage{}` and `WebSearchResult{}` zero values behave as
    documented: `Reported == false` on the zero `CacheUsage`, an empty
    `[]WebSearchResult` on the zero `Response`.
- `completer_test.go` — `RunTurn` cases:
  - A `Request` with an invalid `ToolChoice` fails
    `req.Validate()` and calls neither `Completer` method, before any
    `Messages` entry is checked.
  - A `Request` with a valid `ToolChoice` and one invalid `Messages`
    entry still fails on the message, unwrapped, the existing
    precedence.
  - A fake `ChatStream` that sends two `Chunk` values carrying
    `ReasoningDelta` fragments produces a `Response.Message
    .ReasoningContent` that concatenates them in order, alongside the
    existing `Delta`-to-`Content` case.
  - A fake `ChatStream`'s terminal `Chunk` carrying a non-zero
    `CacheUsage` and a non-empty `WebSearch` slice produces a
    `Response` whose `CacheUsage` and `WebSearch` match; a
    non-terminal chunk's `CacheUsage`/`WebSearch` values are
    discarded, matching the terminal-chunk-only rule.
- `completer_bench_test.go` — the existing benchmark's fake `Response`
  gains a `CacheUsage` and one `WebSearchResult` entry, so the
  benchmark measures the widened `Response`'s real allocation cost,
  not the old, narrower shape.
- `validate_fuzz_test.go` — `FuzzMessageValidate` gains a fourth
  fuzzed input, a `ReasoningContent` presence flag, alongside the
  existing `Role`, `ToolCallID`, and `ToolCalls`-length inputs. The
  oracle adds the new precedence rule: a non-empty `ReasoningContent`
  on a known, non-`RoleAssistant` role returns
  `ErrReasoningContentUnexpected` only after the existing `Role`,
  `ToolCallID`, and `ToolCalls` checks pass; `ErrUnknownRole` and the
  `ToolCallID`/`ToolCalls` errors still win over it on the same
  `Message`, matching the field's documented last-checked position.

### Change verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `provider` and for the total.
- `policy/layers.json` needs no edit. `provider`'s row stays `[]`; this
  change adds fields and types, not an import.
- `api/provider.txt` gains, via `make api-update` in the same change
  as the code: the eight new `Request` fields, `ToolChoice` and its
  two constants, `Request.Validate`, `ErrToolChoiceInvalid`,
  `ReasoningDialect`, the two new `Message` fields,
  `ErrReasoningContentUnexpected`, the two new `Response` fields,
  `CacheStyle` and its three constants, `CacheUsage`,
  `WebSearchResult`, and the three new `Chunk` fields.
- `docs/architecture.md`'s `provider/` bullet gains the widened field
  and type list, in the same change as the code.
- `docs/packages/provider.md` gains every new type, field, and
  sentinel, matching the docs-maintenance convention this file already
  follows for the `Name` field change above.
- `provider/doc.go`'s file map gains the new types' home files.
- No conformance vector: `provider` still defines no wire format; it
  carries in-process values only, the same statement the shipped plan
  already makes.
- `python3 scripts/check_structure.py` must pass on `provider/types.go`
  after the build. The file holds 219 lines before this change; the
  builder checks the file stays at or below the 500-line limit after
  the nine new types and fields land, and splits the file if it does
  not.
- Consumer check, done ahead of this change landing, against
  `docs/architecture.md`'s dependency graph: `agentloop`,
  `subagent`, `contextplan`, `contextsummary`, `providerregistry`,
  `usage`, and `e2e` all import `provider`, and each constructs or
  consumes `provider.Message`, `provider.Request`, `provider.Response`,
  or `provider.Chunk` values with keyed struct literals only,
  confirmed by a repository-wide search (`agentloop/definitions.go`,
  `toolcall.go`, `options.go`, `loop.go`, `compaction.go`, `run.go`;
  `subagent/providertool.go`, `providerregistrytool.go`;
  `contextplan/planner.go`, `compact.go`; `contextsummary/summarizer.go`,
  `summary.go`; `providerregistry/route.go`; `usage/wrap.go`,
  `accumulator.go`; `e2e/fault.go`; and the doc example
  `docs/examples/_agentcomposition/main.go`). Adding a field breaks no
  keyed literal. `agentrun` and `runconfig` hold no direct `provider`
  reference today; they reach `provider` values only through
  `agentloop`'s own types, so neither needs a source change for this
  change to compile. The `Completer` interface's method set is
  unchanged, so no implementer or caller of `Completer` needs a change
  either.

## Change: `StreamingWriter` pass-through field

Status: shipped. This change supersedes one sentence of the streaming
decision above. The `<-chan Chunk` shape stays. This change adds an
optional `io.Writer` beside it, not a replacement.

### Change goal

Let a caller attach a writer to a `Request` so a completer can mirror
its stream during the call. A later change will use this field for
partial-reply capture on a steered stop in `agentloop`.

### Change scope

Inside: one field, `Request.StreamingWriter io.Writer`, in
`provider/types.go`. Nothing else. This SDK writes nothing to the
field in this change. `RunTurn`, `Chat`, and `ChatStream` behave the
same for a nil and a non-nil writer.

The earlier decision "no `io.Writer`" rejected a writer as the
streaming transport in place of the channel. This field is not a
transport. It is an optional mirror a completer may write to beside
the channel it already returns.

### Change API

- `api/provider.txt` gains the `StreamingWriter` field on `Request`,
  via `make api-update` in the same change as the code.
- No new type, method, constant, or sentinel.

### Change tests

- `TestRequestZeroValue` gains an assertion: a zero `Request` holds a
  nil `StreamingWriter`.
- `TestRunTurnIgnoresStreamingWriter` pins the current truth: `RunTurn`
  writes nothing to a non-nil writer and returns the same response it
  returns for a nil writer.

### Change verification

- `make verify` passes.
- `docs/packages/provider.md` names the field in the `Request` bullet,
  in the same change as the code.
- No conformance vector: the field carries an in-process writer, not
  wire data.

## Addendum: maintenance batch — pass-through controls pinned by execution
Status: shipped.


### Goal

- Close the residual gap between the lock-file contract and a
  proven pass-through for the request controls. One new test.

### Scope

- Verified: `Temperature`, `MaxTokens`, `Timeout`, `SessionID`,
  `DisableProviderReplay`, `ReasoningEffort`, and `ReasoningDialect`
  are already execution-pinned by
  `TestRunTurnForwardsWidenedRequestFieldsNonStream` and its stream
  counterpart in `provider/provider_test/request_forwarding_test.go`.
  The uncovered surface is the response side: `CacheUsage` with its
  `CacheStyle` constants, and `WebSearch []WebSearchResult`, which a
  `Completer` produces and `RunTurn` aggregates. No in-tree code
  constructs them; the contract is pass-through for external
  clients.
- Exact change: add one table-driven test in
  `provider/provider_test/runturn_test.go`, reusing the existing
  `fakeCompleter`. The table covers the streamed and non-streamed
  paths. Each row proves a fake-reported `CacheUsage` (one row per
  `CacheStyle` constant) and a `WebSearchResult` set reach
  `RunTurn`'s `Response` unmodified.

### Addendum tests

- New test `TestRunTurnPreservesCacheUsageAndWebSearch` in
  `provider/provider_test/runturn_test.go`. It fails against a
  `RunTurn` that drops or rebuilds the terminal chunk's response-side
  fields.

### Addendum verification

- `go test ./provider/...` passes with the new test red against a
  deliberately broken aggregation.
- `make verify` passes; `provider` holds the 85 coverage floor.
- No `api/` diff; no `policy/layers.json` change.

## Addendum: first concrete adapter
Status: shipped.


### Goal

Name `provider/anthropic` as the first concrete implementer of the
`ContextAccountant` and `ReasoningPolicy` optional capability interfaces.

### Scope

- Documents that `provider/anthropic.Client` is the first concrete
  implementation of `provider.Completer`, `provider.ContextAccountant`,
  and `provider.ReasoningPolicy`.
- One test pinning interface satisfaction.

### Addendum tests

- `TestAnthropicAdapterCapabilities` in `provider/provider_test/completer_test.go`.

### Addendum verification

- `go test ./provider/...` passes.
- `make verify` passes.

## Change: reasoning-signature replay carrier

Status: shipped. One field lands on `Message`. The Anthropic adapter
writes it on decode and reads it on replay. Code, tests, and this
section land in one change.

### Change goal

The Messages API requires an assistant turn to echo its own thinking
block first on the next request. The echo needs the original
signature. Today the adapter decodes the signature and never
reads it (`provider/anthropic/response.go:27`). The replay path
builds assistant turns from text and tool calls only
(`provider/anthropic/request.go:129`). A tool-calling loop with
extended thinking omits the required block on every replay. This
change carries the signature beside the thinking text and replays
both under one guard.

### Change scope

Inside:

- `ReasoningSignature string` on `provider.Message`, beside
  `ReasoningContent`.
- Decode: `provider/anthropic` captures the thinking block's
  signature into the new field when `Options.ExposeReasoning` is on.
- Replay: `provider/anthropic` prepends a thinking content part from
  the two carrier fields when the outgoing request enables reasoning.
- `anthropicContentPart` gains `Thinking` and `Signature`, both
  `omitempty`.
- A split of `parseResponse` in `provider/anthropic/response.go`.
  The function spans lines 50 to 127, which is 78 lines under
  `scripts/check_structure.py`. The decode edits add about five
  lines, and 83 crosses the 80-line function cap. The sanctioned
  shape extracts the content-block switch into one helper. The
  precedent is `agentloop`'s `runIteration`/`afterChat` split, made
  for the same cap (`agentloop/run.go:193`). The builder must not
  compress code into one-line ifs to duck the counter.

Outside:

- `Message.Validate`. The new field is opaque to `provider`. It is
  never validated and never interpreted. The zero value changes no
  behavior.
- `Request.DisableProviderReplay`. The replay guard does not consult
  it. The field governs provider-side stateful session replay, a
  caller-to-completer hint. The request-forwarding tests pin it as
  pass-through only
  (`provider/provider_test/request_forwarding_test.go`). It governs
  no caller-side history echo. Honoring it would reintroduce the
  400 this change fixes.
- `agentloop`. No source change there; the reader table below shows
  why none is needed.
- The streamed path. The `provider/anthropic` addendum records that
  gap and its grep evidence.

Design decision, stated: the carrier is a provider-neutral sibling
field (Option A). The two rejected shapes:

- Adapter-owned history (Option B). Rejected. `RunTurn` is stateless
  validation plus dispatch over caller-supplied `Messages`
  (`provider/runturn.go:31-48`). The `Completer` interface holds
  `Name`, `Chat`, and `ChatStream` only; no statefulness hook exists.
  A history-owning `Client` would also break shared use through
  `providerregistry`, which stores named `provider.Completer` values
  in one map (`providerregistry/registry.go:45`).
- Signature encoded inside `ReasoningContent` (Option C). Rejected.
  `ReasoningContent` is provider-neutral surface other code reads as
  plain text. An encoded signature would corrupt those readers.

Reader evidence, gathered by grep over the whole tree, not by
reasoning. Every non-test reader of `ReasoningContent`, and why it
survives:

| Site | What the reader does | Why it survives |
| --- | --- | --- |
| `provider/types.go:81,171` | `Validate` role rule | Untouched; the new field is a sibling the rule never reads |
| `provider/runturn.go:130` | Sets it from streamed reasoning text | New field stays zero there; that is the recorded streaming gap |
| `provider/anthropic/response.go:119` | Decode writes it | This change makes the same decode write the sibling field |
| `agentloop/run.go:212` | Reads it as plain text for thinking events | Option A leaves the text alone; Option C would corrupt this payload |
| `agentloop/run.go:224` | Copies it into `AuditRecord.ThinkingContent` | Same survival argument |
| `agentloop/options.go:372` | Documents it as plain text | Same |
| `agentloop/heartbeat.go:126` | Emptiness gate on the same string | Same |
| `agentloop/run.go:203` | Appends the whole `Message` into the next request | A sibling field reaches the wire with zero `agentloop` changes |

Every non-test reader of `ReasoningBlock`:
`provider/reasoning.go:30,38`, `provider/anthropic/response.go:76`,
`provider/anthropic/stream.go:225`, and
`provider/anthropic/options.go:87`. `ReasoningBlock` never appears
on `Message` or `Response`; the new field does not touch it.

Literal safety, by grep: no positional `provider.Message{`
composite literal exists in the tree, tests included. Adding a field
breaks no literal. `ReasoningSignature` names no existing symbol;
the grep returns nothing before this change.

### Change API

`api/provider.txt` gains one field through `make api-update`, in the
same change as the code:

```go
type Message struct {
    Role               Role
    Content            string
    Name               string
    ToolCallID         string
    ToolCalls          []ToolCall
    ReasoningContent   string
    ReasoningSignature string
    CreatedAt          time.Time
}
```

- `ReasoningSignature string` is an opaque adapter replay token.
  `provider` never validates or interprets it. It is meaningful only
  beside `ReasoningContent` on a `RoleAssistant` message. An adapter
  that needs signed-reasoning replay writes it on decode and reads
  it on replay. The field states no invariant, so `Validate`
  enforces none.
- No new method, sentinel, or type. The `Completer` interface is
  unchanged.

Decode rules, `provider/anthropic`:

- With `ExposeReasoning` on, `parseResponse` captures each thinking
  block's signature into `Message.ReasoningSignature`. The last
  non-empty signature wins.
- The adapter never enables interleaved thinking, so one thinking
  block per assistant turn is the wire reality. If a response ever
  carries several, text still concatenates and the last signature
  wins. This sentence records that limitation.
- With `ExposeReasoning` off, decode is unchanged:
  `ReasoningContent` stays empty, `OnReasoning` still receives
  redacted blocks, and no signature is captured.
- Replay therefore requires `ExposeReasoning`. `ReasoningContent` is
  the only content carrier; a signature without text cannot form a
  valid thinking block.

Replay rules, `provider/anthropic`:

- `buildRequestBody` resolves the reasoning effort before it calls
  `convertMessages`. The resolved value is `Request.ReasoningEffort`,
  else `Options.DefaultEffort`; it is the same value that sets the
  wire `thinking` field (`provider/anthropic/request.go:218`).
  Reordering is safe: resolution is a pure read.
- `buildRequestBody` threads one bool, reasoning enabled, into
  `convertMessages` and `appendTurnMessage`. The bool is true when
  the resolved effort is non-empty, not `ReasoningEffortNone`, and
  valid.
- `appendTurnMessage`, `RoleAssistant` case, prepends one content
  part when the bool is true and both carrier fields are non-empty:
  `{"type": "thinking", "thinking": <ReasoningContent>,
  "signature": <ReasoningSignature>}`. The part goes before every
  text and tool_use part.
- The reasoning-enabled guard is load-bearing. The API requires
  thinking enabled on any request that passes thinking blocks back.
  A caller who turns thinking off mid-conversation must not get a
  400 from stale history.
- Empty-turn rule: the `len(parts) == 0` drop
  (`provider/anthropic/request.go:146`) keeps its behavior. The
  thinking part is prepended before the emptiness check, so a turn
  carrying a replayable thinking block is no longer empty and must
  not be dropped. The branch comment and its logic change together.
- `TestChatEmptyAssistantMessageDropped` uses a zero-value assistant
  message with no reasoning. Its fixture and assertions stay valid
  unchanged; the builder does not rewrite it.
- `encodeRequestBody` stays a plain marshal. No signature goes out
  unless the replay rule fires.

### Addendum tests

In `provider/anthropic/anthropic_test/`, on the `newTestServer` plus
`writeJSON` fixture. Requests are captured through `json.Unmarshal`
of the body. A request counter serves different content per call,
the `TestChatRateLimitRetry` pattern. The five adapter names below
stay backticked here as references; the `provider/anthropic` plan
carries them as plain-prose commitments:

- `TestChatReplaysThinkingSignatureBeforeToolUse` — turn one answers
  with a thinking block plus signature, text, and one tool_use
  block. The test replays the assistant message plus a `RoleTool`
  tool_result in a second `Chat` call, with the reasoning effort set
  and `ExposeReasoning` on. It asserts the wire carries a thinking
  content part with that exact signature, first in the assistant
  turn, before text and tool_use.
- `TestChatReplayKeepsThinkingOnlyAssistantTurn` — a thinking-only
  response with no text and no tool call stays in replayed history
  once it carries reasoning content plus signature. The same
  response without a signature is still dropped. Pins both the
  changed empty-turn rule and the signature gate.
- `TestChatReplayPlainHistoryHasNoThinkingPart` — a plain text-only
  response replayed as history carries no thinking part.
- `TestChatReplaySkippedWhenReasoningDisabled` — the reasoning
  effort is unset and `Options.DefaultEffort` is empty, so the
  outgoing request disables thinking. History carries both carrier
  fields; the wire carries no thinking part.
- `TestChatReplaysBothThinkingBlocksInOrder` — the slice-carrier
  decode and in-order replay rule; the `provider/anthropic` addendum
  states the fixture.

In `provider/provider_test/`, on the `fakeCompleter` pattern,
TestMessageReasoningBlocksRoundTrip proves the block carrier
round-trips through `RunTurn` unchanged: the fake records it from
`Request.Messages` and echoes it back on `Response.Message`. A
`Message` that never sets the field behaves as before: the zero
value passes `Validate` on all four roles.

### Change verification

- `make verify` passes; `provider` and `provider/anthropic` hold the
  85 coverage floor.
- `api/provider.txt` gains `ReasoningSignature` through
  `make api-update`, committed in the same change as the code.
- `policy/layers.json` needs no edit. `provider` stays a leaf with an
  empty import list. `provider/anthropic` keeps its single
  `provider` import.
- `docs/packages/provider.md` gains one sentence on the `Message`
  bullet, naming the field and its opaque role, in the same change
  as the code.
- `docs/packages/provider/anthropic.md` gains the decode and replay
  sentences in the same change as the code.
- `python3 scripts/check_plan.py`, `check_deps.py`,
  `check_prose.py`, and `check_labels.py` pass.
- `python3 scripts/check_structure.py` passes on the split
  `parseResponse` file. The split extracts a helper; it does not
  compress conditions.
- The `### Addendum tests` names are commitments under
  `check_plan.py`. They pass once the builder lands the tests in
  this same change.
- No conformance vector: `envelope` owns the SDK's wire; the
  Anthropic wire contract stays pinned by the adapter tests above.
