package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

type anthropicResponse struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Role        string                 `json:"role"`
	Model       string                 `json:"model"`
	Content     []anthropicRespContent `json:"content"`
	StopReason  string                 `json:"stop_reason"`
	StopDetails *anthropicStopDetails  `json:"stop_details"`
	Usage       anthropicUsage         `json:"usage"`
	Error       *anthropicError        `json:"error"`
}

type anthropicRespContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type anthropicStopDetails struct {
	Category    string `json:"category"`
	Explanation string `json:"explanation"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func parseResponse(c *Client, data []byte) (provider.Response, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return provider.Response{}, fmt.Errorf("anthropic: parse response: %w", err)
	}

	if resp.StopReason == "refusal" {
		cat := "unknown"
		if resp.StopDetails != nil && resp.StopDetails.Category != "" {
			cat = resp.StopDetails.Category
		}
		return provider.Response{}, fmt.Errorf("%w: %s", ErrRefused, cat)
	}

	var textBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var toolCalls []provider.ToolCall

	for i, part := range resp.Content {
		switch part.Type {
		case "text":
			textBuilder.WriteString(part.Text)
		case "thinking":
			if c.opts.ExposeReasoning {
				reasoningBuilder.WriteString(part.Thinking)
			} else if c.opts.OnReasoning != nil {
				c.opts.OnReasoning(provider.RedactBlock(provider.ReasoningBlock{
					Content:  part.Thinking,
					Redacted: false,
				}))
			}
		case "tool_use":
			argBytes := []byte(part.Input)
			if len(argBytes) == 0 {
				argBytes = []byte("{}")
			}
			toolCalls = append(toolCalls, provider.ToolCall{
				Index:     i,
				ID:        part.ID,
				Name:      part.Name,
				Arguments: argBytes,
			})
		}
	}

	totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
	usage := provider.Usage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      totalTokens,
		CachedTokens:     resp.Usage.CacheReadInputTokens,
	}

	cacheUsage := provider.CacheUsage{}
	if resp.Usage.CacheCreationInputTokens > 0 || resp.Usage.CacheReadInputTokens > 0 {
		cacheUsage = provider.CacheUsage{
			Reported:          true,
			Style:             provider.CacheStyleExplicit,
			InputTokens:       resp.Usage.InputTokens,
			CachedInputTokens: resp.Usage.CacheReadInputTokens,
			CacheWriteTokens:  resp.Usage.CacheCreationInputTokens,
		}
	}

	return provider.Response{
		Model: resp.Model,
		Message: provider.Message{
			Role:             provider.RoleAssistant,
			Content:          textBuilder.String(),
			ReasoningContent: reasoningBuilder.String(),
			ToolCalls:        toolCalls,
		},
		ToolCalls:    toolCalls,
		Usage:        usage,
		FinishReason: resp.StopReason,
		CacheUsage:   cacheUsage,
	}, nil
}
