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
