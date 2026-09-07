# Package reference: provider/anthropic

Package `anthropic` provides a concrete `provider.Completer` adapter
for the Anthropic Messages API. The adapter uses Go standard library
packages `net/http` and `encoding/json` alone.

## Types

- `Client` — implements `provider.Completer`, `provider.ContextAccountant`,
  and `provider.ReasoningPolicy`.
- `Options` — configuration for `Client`:
  - `APIKey string` — required Anthropic API key.
  - `BaseURL string` — optional override for the Anthropic API endpoint.
  - `Model string` — model override when `Request.Model` is empty.
  - `MaxTokensNonStreaming int` — fallback token bound for `Chat`.
  - `MaxTokensStreaming int` — fallback token bound for `ChatStream`.
  - `MaxRetries int` — retry limit for retryable status codes. Zero
    means `DefaultMaxRetries`; use `DisableRetries` to send none.
  - `DisableRetries bool` — turns off all retries regardless of `MaxRetries`.
  - `HTTPClient *http.Client` — HTTP client override; the default
    client carries a 10-minute timeout.
  - `ContextWindow int` — token limit reported by `ContextWindow()`.
    Zero derives a default from `Model` through a small built-in table.
  - `DefaultEffort provider.ReasoningEffort` — fallback reasoning effort.
  - `ExposeReasoning bool` — controls whether thinking text streams to
    the caller as `Chunk.ReasoningDelta` chunks. The replay carrier,
    `Message.ReasoningBlocks`, is always populated regardless of this
    field.
  - `OnReasoning func(provider.ReasoningBlock)` — receives every
    readable thinking block, in redacted form, on both `Chat` and
    `ChatStream`. Fires whether or not `ExposeReasoning` is on. A
    redacted block fires no call, since it carries no readable text.

## Constants

- `DefaultModel` — `"claude-opus-5"`.
- `DefaultMaxTokensNonStreaming` — `16000`.
- `DefaultMaxTokensStreaming` — `64000`.
- `DefaultMaxRetries` — `2`.

## Functions and methods

- `New(opts Options) (*Client, error)` — validates options and returns a `Client`.
- `(Options) Validate() error` — verifies required fields and bounds.
- `(*Client) Name() string` — returns `"anthropic"`.
- `(*Client) ContextWindow() int` — returns the configured context window.
- `(*Client) ReasoningEffort() string` — returns the default reasoning effort.
- `(*Client) Chat(ctx context.Context, req provider.Request) (provider.Response, error)` —
  executes a non-streaming turn.
- `(*Client) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error)` —
  executes a streaming turn over SSE.

## Sentinels

- `ErrAPIKeyRequired` — returned by `New` when `APIKey` is empty.
- `ErrInvalidOptions` — returned by `New` when options fail bounds.
- `ErrAuth` — wraps 401 and 403 HTTP responses.
- `ErrRateLimited` — wraps 429 HTTP responses.
- `ErrBadRequest` — wraps 400, 413, and 422 HTTP responses.
- `ErrServer` — wraps 408, 409, and 5xx HTTP responses, and a
  mid-stream `error` event whose `type` is not one of the recognized
  auth, rate-limit, or bad-request classes.
- `ErrRefused` — wraps model refusal responses with stop category.

## Mapping rules

- Decode, `Chat` and `ChatStream` both: every `thinking` and
  `redacted_thinking` content block in a response decodes into its own
  `provider.ReasoningBlock` on `Message.ReasoningBlocks`, in arrival
  order. A readable block carries `Content` and `Signature`; a
  redacted block carries `Redacted: true` and its opaque payload in
  `Data`, with `Content` empty. Capture does not depend on
  `ExposeReasoning`: that field controls only whether readable text
  also streams to the caller as `Chunk.ReasoningDelta`, and whether
  `OnReasoning` receives a redacted copy of each readable block. On
  `ChatStream`, a block's signature arrives on its own `signature_delta`
  event and is attached to the block at `content_block_stop`.
- Replay: when the outgoing request enables reasoning and
  `Request.DisableProviderReplay` is false, the assistant turn
  prepends one content part per entry in `Message.ReasoningBlocks`, in
  the same order, before text and `tool_use` parts. A readable block
  replays as a `thinking` part with its content and signature; a
  redacted block replays as a `redacted_thinking` part with its `Data`
  payload. Setting `Request.DisableProviderReplay` suppresses replay
  for that request; the adapter does not otherwise guard the pairing,
  so a caller that changes `Request.Model` between turns must set it
  itself, since a block is valid only beside the model that minted it.
- An assistant turn whose only content is entries in
  `ReasoningBlocks` is not dropped as empty. A turn with no text, no
  tool calls, and no reasoning blocks still drops.
