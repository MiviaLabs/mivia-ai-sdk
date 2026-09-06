package anthropic

import (
	"errors"
	"net/http"
	"net/url"

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

// fallbackContextWindow is the context window New assumes for a Model
// string absent from knownContextWindows: a conservative floor, not a
// guess at any specific model's real window.
const fallbackContextWindow = 200000

// knownContextWindows names the context window, in tokens, for every
// model DefaultModel or a caller's Options.Model is likely to name.
// This table is a cache of the GET /v1/models/{id} endpoint's
// max_input_tokens field, the live source of truth for an exact
// value; a model absent here falls back to fallbackContextWindow.
var knownContextWindows = map[string]int{
	"claude-fable-5-1":  1000000,
	"claude-mythos-5-1": 1000000,
	"claude-fable-5":    1000000,
	"claude-opus-5":     1000000,
	"claude-opus-4-8":   1000000,
	"claude-opus-4-7":   1000000,
	"claude-opus-4-6":   1000000,
	"claude-sonnet-5":   1000000,
	"claude-sonnet-4-6": 1000000,
	"claude-haiku-4-5":  200000,
}

func defaultContextWindow(model string) int {
	if model == "" {
		model = DefaultModel
	}
	if w, ok := knownContextWindows[model]; ok {
		return w
	}
	return fallbackContextWindow
}

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
	// Zero means "use DefaultMaxRetries"; to send no retries at all,
	// set DisableRetries instead, since a zero MaxRetries alone cannot
	// be told apart from an unset field.
	MaxRetries int
	// DisableRetries turns off all retry attempts regardless of MaxRetries.
	DisableRetries bool
	// HTTPClient overrides the client's default 10-minute-timeout http.Client.
	HTTPClient *http.Client
	// ContextWindow sets the model context window size reported by
	// ContextWindow(). Zero means New derives a default from Model
	// using a small built-in table; the GET /v1/models/{id} endpoint's
	// max_input_tokens field is the live source for an exact value.
	ContextWindow int
	// DefaultEffort sets the fallback reasoning effort level.
	DefaultEffort provider.ReasoningEffort
	// ExposeReasoning controls whether thinking text streams to the
	// caller as ReasoningDelta chunks. Message.ReasoningBlocks, the
	// replay carrier, is always populated.
	ExposeReasoning bool
	// OnReasoning receives every readable thinking block the model
	// emits, in redacted form, on both the Chat and ChatStream paths.
	// It fires whether or not ExposeReasoning is on. A redacted
	// thinking block fires no call: it carries no readable text.
	OnReasoning func(provider.ReasoningBlock)
}

// Validate checks Options for required fields and value bounds.
func (o Options) Validate() error {
	if o.APIKey == "" {
		return ErrAPIKeyRequired
	}
	if o.BaseURL != "" {
		u, err := url.Parse(o.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return errors.Join(ErrInvalidOptions, errors.New("anthropic: BaseURL must be a valid absolute URL"))
		}
	}
	if o.MaxTokensNonStreaming < 0 {
		return errors.Join(ErrInvalidOptions, errors.New("anthropic: MaxTokensNonStreaming must not be negative"))
	}
	if o.MaxTokensStreaming < 0 {
		return errors.Join(ErrInvalidOptions, errors.New("anthropic: MaxTokensStreaming must not be negative"))
	}
	if o.MaxRetries < 0 {
		return errors.Join(ErrInvalidOptions, errors.New("anthropic: MaxRetries must not be negative"))
	}
	if o.ContextWindow < 0 {
		return errors.Join(ErrInvalidOptions, errors.New("anthropic: ContextWindow must not be negative"))
	}
	return nil
}
