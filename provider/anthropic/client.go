package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

const (
	defaultBaseURL      = "https://api.anthropic.com"
	messagesPath        = "/v1/messages"
	anthropicVersionHdr = "2023-06-01"
)

// defaultHTTPTimeout bounds a request made through a caller-unset
// HTTPClient. A hung read on http.DefaultClient (no timeout) would
// otherwise block until ctx cancels, which may be never.
const defaultHTTPTimeout = 10 * time.Minute

// backoffBaseDelay and backoffCapDelay bound the exponential backoff
// between retries. A retryable status without a Retry-After header
// uses this schedule with full jitter, matching the Messages API's
// own documented retry guidance.
const (
	backoffBaseDelay = 500 * time.Millisecond
	backoffCapDelay  = 8 * time.Second
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
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = DefaultMaxRetries
	}
	if opts.ContextWindow == 0 {
		opts.ContextWindow = defaultContextWindow(opts.Model)
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

// effectiveMaxRetries returns the retry budget for one call: 0 when
// DisableRetries asks for a real off switch, else the configured or
// default MaxRetries. Options.MaxRetries alone cannot express "no
// retries", since 0 already means "use the default".
func (c *Client) effectiveMaxRetries() int {
	if c.opts.DisableRetries {
		return 0
	}
	return c.opts.MaxRetries
}

// doWithRetry sends payload and retries a transport error or a
// retryable status, following the same schedule for both the plain
// and the streaming call. On a 2xx response it returns the *http.Response
// unread and unclosed; the caller reads and closes the body, since
// postWithRetry and postStreamWithRetry need different things from it.
func (c *Client) doWithRetry(ctx context.Context, payload []byte) (*http.Response, error) {
	maxRetries := c.effectiveMaxRetries()
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
			return resp, nil
		}

		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		apiErr := c.mapHTTPError(resp.StatusCode, respBytes)
		if !isRetryable(resp.StatusCode) || attempt >= maxRetries {
			return nil, apiErr
		}

		retryAfter := retryAfterFor(resp.Header)
		attempt++
		if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
			return nil, err
		}
	}
}

func (c *Client) postWithRetry(ctx context.Context, payload []byte) ([]byte, error) {
	resp, err := c.doWithRetry(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return respBytes, nil
}

func (c *Client) postStreamWithRetry(ctx context.Context, payload []byte) (io.ReadCloser, error) {
	resp, err := c.doWithRetry(ctx, payload)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
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
	case statusCode == http.StatusRequestEntityTooLarge || statusCode == http.StatusUnprocessableEntity:
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

// retryAfterFor reads a Retry-After header, which the Messages API
// sends on 429 and may send on other retryable statuses. The value is
// either an integer count of seconds or an HTTP-date; a value in
// neither form yields no caller-side hint, and the caller falls back
// to the computed backoff schedule.
func retryAfterFor(header http.Header) time.Duration {
	return parseRetryAfter(header.Get("Retry-After"))
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if d, err := time.ParseDuration(value + "s"); err == nil && d > 0 {
		return d
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// backoffDelay returns a full-jitter exponential backoff delay for
// the given attempt: a uniform random duration in [0, min(cap, base *
// 2^(attempt-1))). Full jitter avoids every concurrent caller retrying
// in lockstep after a shared failure (a "thundering herd").
func backoffDelay(attempt int) time.Duration {
	d := backoffBaseDelay * time.Duration(1<<uint(attempt-1))
	if d <= 0 || d > backoffCapDelay {
		d = backoffCapDelay
	}
	return time.Duration(rand.Int63n(int64(d)) + 1)
}

func sleepBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := retryAfter
	if delay <= 0 {
		delay = backoffDelay(attempt)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}
