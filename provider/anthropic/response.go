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
	Data      string          `json:"data,omitempty"`
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

// decodedContent aggregates one response's content blocks in wire
// order.
type decodedContent struct {
	text      string
	blocks    []provider.ReasoningBlock
	toolCalls []provider.ToolCall
}

// decodeContent walks one response's content blocks. Text
// concatenates. Every thinking block lands in blocks, in arrival
// order, with its own signature: the slice is the replay carrier, so
// capture never depends on ExposeReasoning. When OnReasoning is set,
// it additionally receives every readable block in redacted form,
// whether or not ExposeReasoning is on. A redacted_thinking block
// fires no OnReasoning call: it carries no readable text.
func (c *Client) decodeContent(parts []anthropicRespContent) decodedContent {
	var out decodedContent
	var textBuilder strings.Builder

	for i, part := range parts {
		switch part.Type {
		case "text":
			textBuilder.WriteString(part.Text)
		case "thinking":
			out.blocks = append(out.blocks, provider.ReasoningBlock{
				Content:   part.Thinking,
				Signature: part.Signature,
			})
			if c.opts.OnReasoning != nil {
				c.opts.OnReasoning(provider.RedactBlock(provider.ReasoningBlock{
					Content: part.Thinking,
				}))
			}
		case "redacted_thinking":
			// A redacted block carries no readable text, so it fires
			// no OnReasoning call; it lands in the carrier for replay.
			out.blocks = append(out.blocks, provider.ReasoningBlock{
				Redacted: true,
				Data:     part.Data,
			})
		case "tool_use":
			argBytes := []byte(part.Input)
			if len(argBytes) == 0 {
				argBytes = []byte("{}")
			}
			out.toolCalls = append(out.toolCalls, provider.ToolCall{
				Index:     i,
				ID:        part.ID,
				Name:      part.Name,
				Arguments: argBytes,
			})
		}
	}

	out.text = textBuilder.String()
	return out
}

func parseResponse(c *Client, data []byte) (provider.Response, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return provider.Response{}, fmt.Errorf("anthropic: parse response: %w", err)
	}

	if resp.StopReason == "refusal" {
		return provider.Response{}, refusalError(resp.StopDetails)
	}

	decoded := c.decodeContent(resp.Content)

	totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
	usage := provider.Usage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      totalTokens,
		CachedTokens:     resp.Usage.CacheReadInputTokens,
	}

	cacheUsage := cacheUsageFrom(resp.Usage.InputTokens, resp.Usage.CacheReadInputTokens, resp.Usage.CacheCreationInputTokens)

	return provider.Response{
		Model: resp.Model,
		Message: provider.Message{
			Role:            provider.RoleAssistant,
			Content:         decoded.text,
			ReasoningBlocks: decoded.blocks,
			ToolCalls:       decoded.toolCalls,
		},
		ToolCalls:    decoded.toolCalls,
		Usage:        usage,
		FinishReason: resp.StopReason,
		CacheUsage:   cacheUsage,
	}, nil
}

// refusalError builds ErrRefused wrapped with the stop category,
// "unknown" when details carries none. Shared by the non-streamed
// decode and the streamed terminal chunk, so the two paths classify a
// refusal identically.
func refusalError(details *anthropicStopDetails) error {
	cat := "unknown"
	if details != nil && details.Category != "" {
		cat = details.Category
	}
	return fmt.Errorf("%w: %s", ErrRefused, cat)
}

// cacheUsageFrom builds a provider.CacheUsage from the three raw
// token counts the Messages API reports, explicit-style. Reported
// stays false when neither cache field is set, the same "nothing to
// report" convention CacheUsage documents. Shared by the non-streamed
// decode and the streamed terminal chunk.
func cacheUsageFrom(inputTokens, cacheReadTokens, cacheCreationTokens int) provider.CacheUsage {
	if cacheCreationTokens <= 0 && cacheReadTokens <= 0 {
		return provider.CacheUsage{}
	}
	return provider.CacheUsage{
		Reported:          true,
		Style:             provider.CacheStyleExplicit,
		InputTokens:       inputTokens,
		CachedInputTokens: cacheReadTokens,
		CacheWriteTokens:  cacheCreationTokens,
	}
}
