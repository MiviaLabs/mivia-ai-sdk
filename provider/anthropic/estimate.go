package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

const countTokensPath = "/v1/messages/count_tokens"

// countTokensTimeout bounds one count_tokens round trip. The
// TokenEstimator interface carries no ctx, so the estimate bounds
// itself; on timeout the caller falls back to the ratio estimate.
const countTokensTimeout = 30 * time.Second

// fallbackCharsPerToken is the character-to-token ratio of the
// fallback estimate. Four is the conventional rule of thumb for
// English prose and code.
const fallbackCharsPerToken = 4

var _ provider.TokenEstimator = (*Client)(nil)

// countTokensResponse is the count_tokens endpoint's reply.
type countTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// EstimateTokens implements provider.TokenEstimator. It asks the
// Messages API count_tokens endpoint for the exact prompt token
// count of the request. Any endpoint failure falls back to a local
// character-ratio estimate, so a rate limit or an outage degrades the
// accuracy, not the availability, of the estimate. See
// provider.TokenEstimator for the contract.
func (c *Client) EstimateTokens(req provider.Request) (int, error) {
	if len(req.Messages) == 0 && len(req.Tools) == 0 {
		return 0, nil
	}
	n, err := c.countTokens(req)
	if err == nil {
		return n, nil
	}
	return estimateByRatio(req), nil
}

// countTokens posts the request to count_tokens and returns
// input_tokens. It makes one attempt with no retry schedule: the
// caller's fallback covers transient failures. The payload carries
// only the fields count_tokens accepts: model, messages, system, and
// tools. A full Messages body would add max_tokens and friends,
// which the endpoint may reject. Replay follows the decision Chat
// makes, so replayed thinking blocks are counted; a request whose
// messages all convert to nothing falls back instead of posting an
// empty messages array.
func (c *Client) countTokens(req provider.Request) (int, error) {
	model := req.Model
	if model == "" {
		model = c.opts.Model
	}
	if model == "" {
		model = DefaultModel
	}
	effort := req.ReasoningEffort
	if effort == "" {
		effort = c.opts.DefaultEffort
	}
	replay := effort != "" && effort != provider.ReasoningEffortNone && !req.DisableProviderReplay
	systemBlocks, msgs := convertMessages(req.Messages, replay)
	if len(msgs) == 0 {
		return 0, fmt.Errorf("anthropic: no countable messages for this request")
	}
	payload, err := encodeCountTokensBody(countTokensBody{
		Model:    model,
		Messages: msgs,
		System:   systemBlocks,
		Tools:    convertTools(req.Tools, false),
	})
	if err != nil {
		return 0, fmt.Errorf("anthropic: marshal count_tokens request: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), countTokensTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.countTokensEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("x-api-key", c.opts.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersionHdr)
	httpReq.Header.Set("content-type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, c.mapHTTPError(resp.StatusCode, nil)
	}
	var parsed countTokensResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("anthropic: decode count_tokens response: %w", err)
	}
	return parsed.InputTokens, nil
}

func (c *Client) countTokensEndpoint() string {
	baseURL := c.opts.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return baseURL + countTokensPath
}

// estimateByRatio estimates prompt tokens as total prompt characters
// divided by fallbackCharsPerToken. It counts message content,
// replayed reasoning-block content, and tool names plus schemas.
func estimateByRatio(req provider.Request) int {
	chars := 0
	for _, msg := range req.Messages {
		chars += len(msg.Content)
		for _, block := range msg.ReasoningBlocks {
			chars += len(block.Content) + len(block.Data)
		}
		for _, tc := range msg.ToolCalls {
			chars += len(tc.Arguments)
		}
	}
	for _, tool := range req.Tools {
		chars += len(tool.Name) + len(tool.Description) + len(tool.Schema)
	}
	if chars == 0 {
		return 0
	}
	return chars/fallbackCharsPerToken + 1
}
