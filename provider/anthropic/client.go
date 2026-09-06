package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

const (
	defaultBaseURL      = "https://api.anthropic.com"
	messagesPath        = "/v1/messages"
	anthropicVersionHdr = "2023-06-01"
)

var (
	_ provider.Completer         = (*Client)(nil)
	_ provider.ContextAccountant = (*Client)(nil)
	_ provider.ReasoningPolicy   = (*Client)(nil)
)

// Client implements provider.Completer against the Anthropic Messages API.
type Client struct {
	opts       Options
	httpClient *http.Client
}

// New validates opts and returns a new Client.
func New(opts Options) (*Client, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = DefaultMaxRetries
	}
	return &Client{
		opts:       opts,
		httpClient: httpClient,
	}, nil
}

// Name returns the provider identifier "anthropic".
func (c *Client) Name() string {
	return "anthropic"
}

// ContextWindow returns the configured model context window size in tokens.
func (c *Client) ContextWindow() int {
	return c.opts.ContextWindow
}

// ReasoningEffort returns the configured default reasoning effort level string.
func (c *Client) ReasoningEffort() string {
	return string(c.opts.DefaultEffort)
}

// Chat executes a synchronous completion turn against the Anthropic Messages API.
func (c *Client) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	body, err := buildRequestBody(c, req, false)
	if err != nil {
		return provider.Response{}, err
	}

	payload, err := encodeRequestBody(body)
	if err != nil {
		return provider.Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	respBytes, err := c.postWithRetry(ctx, payload)
	if err != nil {
		return provider.Response{}, err
	}

	return parseResponse(c, respBytes)
}

// ChatStream executes a streaming completion turn emitting Chunk values over a channel.
func (c *Client) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	body, err := buildRequestBody(c, req, true)
	if err != nil {
		return nil, err
	}

	payload, err := encodeRequestBody(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	respBody, err := c.postStreamWithRetry(ctx, payload)
	if err != nil {
		return nil, err
	}

	return c.handleStream(ctx, respBody), nil
}

func (c *Client) endpoint() string {
	baseURL := c.opts.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return baseURL + messagesPath
}

func (c *Client) newHTTPRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", c.opts.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersionHdr)
	httpReq.Header.Set("content-type", "application/json")
	return httpReq, nil
}

func (c *Client) postWithRetry(ctx context.Context, payload []byte) ([]byte, error) {
	maxRetries := c.opts.MaxRetries
	var attempt int

	for {
		httpReq, err := c.newHTTPRequest(ctx, payload)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < maxRetries {
				attempt++
				if backoffErr := sleepBackoff(ctx, attempt, 0); backoffErr != nil {
					return nil, backoffErr
				}
				continue
			}
			return nil, err
		}

		respBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBytes, nil
		}

		apiErr := c.mapHTTPError(resp.StatusCode, respBytes)
		if !isRetryable(resp.StatusCode) || attempt >= maxRetries {
			return nil, apiErr
		}

		var retryAfter time.Duration
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}

		attempt++
		if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
			return nil, err
		}
	}
}

func (c *Client) postStreamWithRetry(ctx context.Context, payload []byte) (io.ReadCloser, error) {
	maxRetries := c.opts.MaxRetries
	var attempt int

	for {
		httpReq, err := c.newHTTPRequest(ctx, payload)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < maxRetries {
				attempt++
				if backoffErr := sleepBackoff(ctx, attempt, 0); backoffErr != nil {
					return nil, backoffErr
				}
				continue
			}
			return nil, err
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp.Body, nil
		}

		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		apiErr := c.mapHTTPError(resp.StatusCode, respBytes)
		if !isRetryable(resp.StatusCode) || attempt >= maxRetries {
			return nil, apiErr
		}

		var retryAfter time.Duration
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}

		attempt++
		if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
			return nil, err
		}
	}
}

func isRetryable(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusConflict || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}

func (c *Client) mapHTTPError(statusCode int, body []byte) error {
	var resp anthropicResponse
	errMsg := ""
	if err := json.Unmarshal(body, &resp); err == nil && resp.Error != nil {
		errMsg = resp.Error.Message
	}
	if errMsg == "" {
		errMsg = fmt.Sprintf("status code %d", statusCode)
	}

	switch {
	case statusCode == http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, errMsg)
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrAuth, errMsg)
	case statusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrRateLimited, errMsg)
	case statusCode >= 500 && statusCode <= 599:
		return fmt.Errorf("%w: %s", ErrServer, errMsg)
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrServer, errMsg)
	default:
		return fmt.Errorf("anthropic: unexpected HTTP status %d: %s", statusCode, errMsg)
	}
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if d, err := time.ParseDuration(header + "s"); err == nil && d > 0 {
		return d
	}
	return 0
}

func sleepBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := retryAfter
	if delay <= 0 {
		delay = time.Duration(1<<(attempt-1)) * 5 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}
