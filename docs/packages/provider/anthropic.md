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
  - `MaxRetries int` — retry limit for retryable status codes.
  - `HTTPClient *http.Client` — HTTP client override.
  - `ContextWindow int` — token limit reported by `ContextWindow()`.
  - `DefaultEffort provider.ReasoningEffort` — fallback reasoning effort.
  - `ExposeReasoning bool` — expose thinking text on `Message.ReasoningContent`.
  - `OnReasoning func(provider.ReasoningBlock)` — callback for thinking blocks.

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
- `ErrBadRequest` — wraps 400 HTTP responses.
- `ErrServer` — wraps 5xx HTTP responses.
- `ErrRefused` — wraps model refusal responses with stop category.

## Mapping rules

- Decode, `Chat` only: when `ExposeReasoning` is on, each thinking
  block's `signature` lands in `Message.ReasoningSignature` beside the
  thinking text; the last non-empty signature wins. `ExposeReasoning`
  off keeps decode unchanged and captures no signature.
- Replay: when both `Message.ReasoningContent` and
  `Message.ReasoningSignature` are non-empty and the request enables
  reasoning, the assistant turn prepends one thinking part carrying
  both values, before text and `tool_use` parts. When reasoning is
  disabled, no thinking part is sent.
- A thinking-only assistant turn that carries the replay carrier is no
  longer dropped as empty. The same turn without a signature still
  drops.
