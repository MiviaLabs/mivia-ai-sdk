# Plan: provider/anthropic

Status: shipped. Builds on the shipped `provider` package (`docs/plans/provider.md`).

## Goal

Provide a concrete `provider.Completer` adapter for the Anthropic Messages API. The adapter uses Go standard library packages `net/http` and `encoding/json` alone.

## Scope

Inside this package:

- The `Client` type implementing `provider.Completer`, `provider.ContextAccountant`, and `provider.ReasoningPolicy`.
- Non-streaming completion via `Chat` posting to Anthropic Messages endpoint.
- Streaming completion via `ChatStream` decoding Server-Sent Events.
- Request mapping: model, max tokens, system prompts, messages, tools, tool choice, temperature, reasoning effort, and prompt caching.
- Response mapping: text content, tool calls, thinking content, cache usage, usage totals, finish reasons, and stop details.
- Error handling: HTTP status mapping, retry loop with exponential backoff and `Retry-After` header support, and refusal mapping.
- Configuration options via `Options` and constructor `New(opts Options) (*Client, error)`.

Outside this package:

- Any third-party dependency. No external Anthropic Go SDK.
- Server tools, files, batches, compaction beta, or Managed Agents.
- Conformance kit like `durablefence` with adapters kept in application code.
- Pruned provider vocabulary for `contextplan` or `agentloop`.
- Any modification to `provider` package contracts or core types.

## Decisions

- Decision: Add one concrete `provider.Completer` to the module as a new package `provider/anthropic`.
- Rejected option B: A conformance kit like `durablefence` with adapters kept in application code. The SDK needs an out-of-the-box working adapter for integration tests and agent loop composition.
- Rejected option C: Prune the provider vocabulary to what `contextplan` and `agentloop` read. That would fragment the provider contract and require continuous churn.
- Rejected option: The official Go SDK (`github.com/anthropics/anthropic-sdk-go`) was considered and rejected for this batch. AGENTS.md forbids a third-party import without its own plan review and a `policy/thirdparty.json` row. A later plan may swap the transport for the SDK behind the same exported surface.

## API

The surface below is the lock target. It will land in `api/provider/anthropic.txt` via `make api-update`.

```go
package anthropic

import (
	"context"
	"net/http"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// DefaultModel is the model Client uses when Request.Model is empty.
const DefaultModel = "claude-opus-5"

// DefaultMaxTokensNonStreaming is the max_tokens limit used when Request.MaxTokens is nil in Chat.
const DefaultMaxTokensNonStreaming = 16000

// DefaultMaxTokensStreaming is the max_tokens limit used when Request.MaxTokens is nil in ChatStream.
const DefaultMaxTokensStreaming = 64000

// DefaultMaxRetries is the maximum number of retry attempts for retryable HTTP errors.
const DefaultMaxRetries = 2

// Sentinel errors for client construction, request execution, and HTTP response translation.
var (
	// ErrAPIKeyRequired is New's error when Options.APIKey is empty.
	ErrAPIKeyRequired error
	// ErrInvalidOptions is New's error when an option field is negative or invalid.
	ErrInvalidOptions error
	// ErrAuth reports 401 Unauthorized or 403 Forbidden responses.
	ErrAuth error
	// ErrRateLimited reports 429 Too Many Requests responses.
	ErrRateLimited error
	// ErrBadRequest reports 400 Bad Request responses.
	ErrBadRequest error
	// ErrServer reports 5xx server responses.
	ErrServer error
	// ErrRefused reports turn termination caused by model refusal.
	ErrRefused error
)

// Options configures a Client.
type Options struct {
	// APIKey is the Anthropic API key. Required.
	APIKey string
	// BaseURL overrides the default Anthropic API endpoint URL.
	BaseURL string
	// Model overrides DefaultModel when Request.Model is empty.
	Model string
	// MaxTokensNonStreaming overrides DefaultMaxTokensNonStreaming when Request.MaxTokens is nil in Chat.
	MaxTokensNonStreaming int
	// MaxTokensStreaming overrides DefaultMaxTokensStreaming when Request.MaxTokens is nil in ChatStream.
	MaxTokensStreaming int
	// MaxRetries bounds the number of retry attempts for retryable status codes.
	MaxRetries int
	// HTTPClient overrides http.DefaultClient.
	HTTPClient *http.Client
	// ContextWindow sets the model context window size reported by ContextWindow().
	ContextWindow int
	// DefaultEffort sets the fallback reasoning effort level.
	DefaultEffort provider.ReasoningEffort
	// ExposeReasoning controls whether thinking blocks populate Message.ReasoningContent.
	ExposeReasoning bool
	// OnReasoning receives every thinking block emitted by the model.
	OnReasoning func(provider.ReasoningBlock)
}

// Validate checks Options for required fields and value bounds.
func (o Options) Validate() error

// Client implements provider.Completer against the Anthropic Messages API.
type Client struct {
	// unexported fields
}

// New validates opts and returns a new Client.
func New(opts Options) (*Client, error)

// Name returns the provider identifier "anthropic".
func (c *Client) Name() string

// ContextWindow returns the configured model context window size in tokens.
func (c *Client) ContextWindow() int

// ReasoningEffort returns the configured default reasoning effort level string.
func (c *Client) ReasoningEffort() string

// Chat executes a synchronous completion turn against the Anthropic Messages API.
func (c *Client) Chat(ctx context.Context, req provider.Request) (provider.Response, error)

// ChatStream executes a streaming completion turn emitting Chunk values over a channel.
func (c *Client) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error)
```

Compile-time assertions verify interface satisfaction:

```go
var (
	_ provider.Completer         = (*Client)(nil)
	_ provider.ContextAccountant = (*Client)(nil)
	_ provider.ReasoningPolicy   = (*Client)(nil)
)
```

### Request Mapping Rules

- Wire Endpoint: `POST https://api.anthropic.com/v1/messages`.
- Headers: `x-api-key: <APIKey>`, `anthropic-version: 2023-06-01`, `content-type: application/json`.
- Model: `Request.Model` if non-empty, otherwise `Options.Model` if set, otherwise `DefaultModel`.
- Max Tokens: `*Request.MaxTokens` if non-nil. Otherwise `Options.MaxTokensNonStreaming` (or default 16000) for `Chat`, and `Options.MaxTokensStreaming` (or default 64000) for `ChatStream`.
- Prompt caching: `Request.ReasoningDialect` carries the cache instruction. When `provider.CacheStyle(req.ReasoningDialect) != provider.CacheStyleNone` (or `req.ReasoningDialect == provider.ReasoningDialect(provider.CacheStyleExplicit)`), prompt caching is active.
- System: Concatenate system message contents or map into system blocks. When prompt caching is active, place `cache_control: {"type": "ephemeral"}` on the last system block.
- Messages: Map `provider.RoleUser` to `user` and `provider.RoleAssistant` to `assistant`. Map `provider.RoleTool` messages to user role turns containing `tool_result` blocks with `tool_use_id`, `content`, and optional `is_error`.
- Tool calls: Map `provider.ToolCall` slices on assistant messages into `tool_use` blocks with `id`, `name`, and parsed JSON `input`.
- Tools: Map `provider.ToolDefinition` slices to Anthropic tool definitions with `name`, `description`, and `input_schema`. When prompt caching is active, place `cache_control: {"type": "ephemeral"}` on the last tool definition.
- Tool Choice: Map `provider.ToolChoiceAuto` to `{"type": "auto"}` and `provider.ToolChoiceNone` to `{"type": "none"}`. Never send `any` or forced tool selection.
- Temperature: Send `temperature` only when `Request.Temperature` is non-nil and allowed by the request dialect.
- Reasoning Effort: Map `provider.ReasoningEffort` when non-empty to `thinking: {"type": "adaptive"}` and `output_config: {"effort": <level>}` where level is `low`, `medium`, `high`, `xhigh`, or `max`. `ReasoningEffortNone` sends no thinking field. Never send `budget_tokens`.

### Response and Stream Mapping Rules

- Non-streaming: Concatenate text blocks into `Message.Content`. Extract `tool_use` blocks into `Response.ToolCalls`. Extract thinking blocks into `Message.ReasoningContent` only when `Options.ExposeReasoning` is true. If `Options.ExposeReasoning` is false, drop thinking content and invoke `Options.OnReasoning` with `provider.RedactBlock(block)` when `Options.OnReasoning` is non-nil.
- Cache accounting: Extract `usage.cache_creation_input_tokens` and `usage.cache_read_input_tokens` into `provider.CacheUsage` and `Usage.CachedTokens`. Set `CacheUsage.Reported` to true when present.
- Stop reason: Map `end_turn`, `max_tokens`, `tool_use`, `pause_turn`, and `refusal` directly to `FinishReason`. On `refusal`, inspect `stop_details.category` and `stop_details.explanation`, returning sentinel `ErrRefused` wrapping category.
- Streaming: Parse SSE events (`message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`, `ping`).
- SSE delta mapping: Map `text_delta` to `Chunk.Delta`. Map `thinking_delta` to `Chunk.ReasoningDelta`. Accumulate `input_json_delta` fragments per block index, emitting one `Chunk.ToolCallDelta` on `content_block_stop`.
- Terminal chunk: Combine `message_delta` and `message_stop` into a terminal chunk with `Done = true`, `Usage`, `FinishReason`, and `CacheUsage`.
- Ignored stream events: Silently ignore `signature_delta` and `ping`.

### Error Handling and Retry Rules

- Status 400 maps to `ErrBadRequest` wrapped with error message (`%w: %s`). Not retryable.
- Status 404 is not retryable.
- Status 401 and 403 map to `ErrAuth` wrapped with error message. Not retryable.
- Status 429 maps to `ErrRateLimited`. Retryable.
- Status 408, 409, 429, and 5xx map to `ErrServer` (or `ErrRateLimited` for 429) and are retryable.
- Retry loop executes at most `Options.MaxRetries` times with exponential backoff.
- Honor `Retry-After` header when received on 429 responses.
- Respect context cancellation during backoff pauses and request execution.

## Tests

Test files will live in `provider/anthropic/anthropic_test/` using an `httptest.Server` fixture.

Unit tests in `provider/anthropic/anthropic_test/`:

- `TestOptionsValidate`: Table-driven tests asserting validation of APIKey, base URL, limits, and retry counts.
- `TestChatNonStreaming`: Verifies request JSON serialization, header propagation, and response aggregation.
- `TestChatStreamingText`: Verifies SSE parsing of `message_start`, `text_delta`, `message_delta`, and `message_stop`.
- `TestChatStreamingToolCall`: Verifies tool call accumulation across three split `input_json_delta` chunks and emission on `content_block_stop`.
- `TestChatThinkingBlock`: Verifies handling of thinking blocks, `Options.ExposeReasoning` flag, and `Options.OnReasoning` redaction callbacks.
- `TestChatPromptCaching`: Verifies `cache_control` placement on final system block and tool definition, and response cache token accounting.
- `TestChatRefusal`: Verifies mapping of `stop_reason: refusal` and extraction of `stop_details.category` wrapped in `ErrRefused`.
- `TestChatRateLimitRetry`: Verifies retry loop on 429 honoring `Retry-After` header up to `MaxRetries`.
- `TestChatServerRetrySuccess`: Verifies recovering after two initial 500 responses followed by a 200 response.
- `TestChatBadRequestNoRetry`: Verifies immediate failure on 400 without retry.
- `TestChatStreamDisconnect`: Verifies error propagation when HTTP stream closes mid-turn.
- `TestChatStreamContextCancel`: Verifies background goroutine cleanly exits when context is canceled mid-stream. Verifies termination deterministically using a settle loop with `runtime.NumGoroutine` or an explicit server done notification channel.
- `TestRunTurnAdapter`: Verifies `provider.RunTurn` executes smoothly over the Anthropic adapter for both streaming and non-streaming modes.

Positive control end-to-end test in `e2e/e2e_test/`:

- `TestAnthropicAgentLoopCompactionControl`: A positive control scenario wiring `agentloop.New` with `provider/anthropic.New` against an `httptest.Server` fixture. The fixture simulates multi-turn tool interaction. Window size is configured small with `Window.MaxTokens = 400`, `Reserve = 100`, `Budget = 300`, and `TriggerPercent = 80` (trigger threshold: 240 tokens). The test passes a deterministic `scaleEstimator{div: 1}` (one token per byte) to `contextplan.Calibrate(est, 1.0)`. Turn 1 carries initial history with 200 estimated tokens (below trigger 240 tokens, so turn 1 history does not compact). The Anthropic adapter fixture reports actual usage of 280 tokens on turn 1. `Calibrated.Observe(200, 280)` updates the factor to `1.0 * (0.7 + 0.3 * (280 / 200)) = 1.12`. On turn 2, tool result expands history to 220 raw tokens, and the calibrated estimator yields `220 * 1.12 = 246` tokens. Estimated tokens now exceed the trigger threshold (240 tokens), triggering compaction. The test asserts `res.Compacted` was true, history was compacted, summary message was injected, and the run completed successfully.

## Verification

- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_deps.py` passes with `"provider/anthropic": ["provider"]` declared in `policy/layers.json`.
- `python3 scripts/check_orphan_packages.py` passes with permanent entry in `policy/pending_wiring.json`.
- `python3 scripts/check_prose.py` passes on all plan documentation.
- `python3 scripts/check_labels.py` passes with zero audit labels.
- `make verify` passes once the builder implements code, tests, and API lock.
- Package files must stay under 500 lines and functions under 80 lines per `scripts/check_structure.py`.
- Package coverage reaches or exceeds 85% floor.
