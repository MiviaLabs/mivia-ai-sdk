package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

type anthropicRequest struct {
	Model        string               `json:"model"`
	MaxTokens    int                  `json:"max_tokens"`
	Messages     []anthropicMessage   `json:"messages"`
	System       []anthropicSystem    `json:"system,omitempty"`
	Tools        []anthropicTool      `json:"tools,omitempty"`
	ToolChoice   *anthropicToolChoice `json:"tool_choice,omitempty"`
	Stream       bool                 `json:"stream,omitempty"`
	Temperature  *float64             `json:"temperature,omitempty"`
	Thinking     *anthropicThinking   `json:"thinking,omitempty"`
	OutputConfig *anthropicOutputCfg  `json:"output_config,omitempty"`
}

type anthropicSystem struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicMessage struct {
	Role    string                 `json:"role"`
	Content []anthropicContentPart `json:"content"`
}

type anthropicContentPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
}

type anthropicThinking struct {
	Type    string `json:"type"`
	Display string `json:"display,omitempty"`
}

type anthropicOutputCfg struct {
	Effort string `json:"effort"`
}

func isPromptCachingActive(req provider.Request) bool {
	return req.CacheStyle != provider.CacheStyleNone && req.CacheStyle != ""
}

func isValidReasoningEffort(e provider.ReasoningEffort) bool {
	switch e {
	case provider.ReasoningEffortLow, provider.ReasoningEffortMedium,
		provider.ReasoningEffortHigh, provider.ReasoningEffortXHigh, provider.ReasoningEffortMax:
		return true
	default:
		return false
	}
}

func resolveMaxTokens(c *Client, req provider.Request, isStream bool) int {
	if req.MaxTokens != nil {
		return *req.MaxTokens
	}
	if isStream {
		if c.opts.MaxTokensStreaming > 0 {
			return c.opts.MaxTokensStreaming
		}
		return DefaultMaxTokensStreaming
	}
	if c.opts.MaxTokensNonStreaming > 0 {
		return c.opts.MaxTokensNonStreaming
	}
	return DefaultMaxTokensNonStreaming
}

func convertMessages(reqMsgs []provider.Message, replayEnabled bool) ([]anthropicSystem, []anthropicMessage) {
	var systemBlocks []anthropicSystem
	var anthropicMsgs []anthropicMessage

	for _, msg := range reqMsgs {
		if msg.Role == provider.RoleSystem {
			systemBlocks = append(systemBlocks, anthropicSystem{
				Type: "text",
				Text: msg.Content,
			})
			continue
		}
		appendTurnMessage(&anthropicMsgs, msg, replayEnabled)
	}
	return systemBlocks, anthropicMsgs
}

const (
	anthropicRoleUser      = "user"
	anthropicRoleAssistant = "assistant"
)

func appendTurnMessage(anthropicMsgs *[]anthropicMessage, msg provider.Message, replayEnabled bool) {
	switch msg.Role {
	case provider.RoleUser:
		*anthropicMsgs = append(*anthropicMsgs, anthropicMessage{
			Role:    anthropicRoleUser,
			Content: []anthropicContentPart{{Type: "text", Text: msg.Content}},
		})
	case provider.RoleAssistant:
		var parts []anthropicContentPart
		// Replay every reasoning block first, in arrival order: the
		// Messages API requires an assistant turn to echo its own
		// thinking and redacted_thinking blocks, exactly as received,
		// before text and tool_use parts. A readable block replays
		// when it carries a signature, even when its text is empty. A
		// redacted block replays its opaque data payload verbatim.
		if replayEnabled {
			for _, block := range msg.ReasoningBlocks {
				if block.Redacted {
					parts = append(parts, anthropicContentPart{
						Type: "redacted_thinking",
						Data: block.Data,
					})
					continue
				}
				if block.Signature == "" {
					continue
				}
				parts = append(parts, anthropicContentPart{
					Type:      "thinking",
					Thinking:  block.Content,
					Signature: block.Signature,
				})
			}
		}
		if msg.Content != "" {
			parts = append(parts, anthropicContentPart{Type: "text", Text: msg.Content})
		}
		for _, tc := range msg.ToolCalls {
			raw := tc.Arguments
			if len(raw) == 0 {
				raw = []byte("{}")
			}
			parts = append(parts, anthropicContentPart{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Name,
				Input: json.RawMessage(raw),
			})
		}
		if len(parts) == 0 {
			// A turn with no text, no tool call, and no replayed
			// thinking block carries nothing worth replaying. Sending
			// an empty text block risks rejection by the Messages API,
			// so the turn is dropped. A thinking-only turn that
			// replayed its block above is non-empty and survives here.
			return
		}
		*anthropicMsgs = append(*anthropicMsgs, anthropicMessage{Role: anthropicRoleAssistant, Content: parts})
	case provider.RoleTool:
		part := anthropicContentPart{
			Type:      "tool_result",
			ToolUseID: msg.ToolCallID,
			Content:   msg.Content,
			IsError:   strings.HasPrefix(msg.Content, "[tool-error]"),
		}
		n := len(*anthropicMsgs)
		if n > 0 && (*anthropicMsgs)[n-1].Role == anthropicRoleUser {
			(*anthropicMsgs)[n-1].Content = append((*anthropicMsgs)[n-1].Content, part)
		} else {
			*anthropicMsgs = append(*anthropicMsgs, anthropicMessage{
				Role:    anthropicRoleUser,
				Content: []anthropicContentPart{part},
			})
		}
	}
}

func convertTools(tools []provider.ToolDefinition, promptCaching bool) []anthropicTool {
	var res []anthropicTool
	for _, t := range tools {
		schemaRaw := t.Schema
		if len(schemaRaw) == 0 {
			schemaRaw = []byte(`{"type":"object"}`)
		}
		res = append(res, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: json.RawMessage(schemaRaw),
		})
	}
	if promptCaching && len(res) > 0 {
		res[len(res)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
	return res
}

func buildRequestBody(c *Client, req provider.Request, isStream bool) (*anthropicRequest, error) {
	model := req.Model
	if model == "" {
		model = c.opts.Model
	}
	if model == "" {
		model = DefaultModel
	}

	maxTokens := resolveMaxTokens(c, req, isStream)
	promptCaching := isPromptCachingActive(req)

	// Resolve the effort before message conversion. The replay guard
	// reads the same resolved value the wire thinking field uses.
	effort := req.ReasoningEffort
	if effort == "" {
		effort = c.opts.DefaultEffort
	}
	reasoningEnabled := effort != "" && effort != provider.ReasoningEffortNone
	if reasoningEnabled && !isValidReasoningEffort(effort) {
		return nil, fmt.Errorf("%w: reasoning effort %q is not one of low, medium, high, xhigh, max", ErrInvalidOptions, effort)
	}

	// Replay is a correctness duty, but a caller may forbid it: a
	// rewritten history must not carry blocks minted against turns the
	// provider no longer sees.
	systemBlocks, anthropicMsgs := convertMessages(req.Messages, reasoningEnabled && !req.DisableProviderReplay)

	if promptCaching && len(systemBlocks) > 0 {
		systemBlocks[len(systemBlocks)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}

	tools := convertTools(req.Tools, promptCaching)

	var toolChoice *anthropicToolChoice
	if req.ToolChoice == provider.ToolChoiceAuto {
		toolChoice = &anthropicToolChoice{Type: "auto"}
	} else if req.ToolChoice == provider.ToolChoiceNone {
		toolChoice = &anthropicToolChoice{Type: "none"}
	}

	var thinking *anthropicThinking
	var outCfg *anthropicOutputCfg
	if reasoningEnabled {
		thinking = &anthropicThinking{Type: "adaptive"}
		if c.opts.ExposeReasoning || c.opts.OnReasoning != nil {
			// The API omits thinking text by default; ExposeReasoning
			// and OnReasoning both need the readable summary form.
			thinking.Display = "summarized"
		}
		outCfg = &anthropicOutputCfg{Effort: string(effort)}
	}

	return &anthropicRequest{
		Model:        model,
		MaxTokens:    maxTokens,
		Messages:     anthropicMsgs,
		System:       systemBlocks,
		Tools:        tools,
		ToolChoice:   toolChoice,
		Stream:       isStream,
		Temperature:  req.Temperature,
		Thinking:     thinking,
		OutputConfig: outCfg,
	}, nil
}
