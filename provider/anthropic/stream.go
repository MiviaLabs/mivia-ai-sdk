package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

type sseEvent struct {
	Event string
	Data  []byte
	Err   error
}

// maxSSELineSize caps one SSE line, well above any real Messages API
// event; the default bufio.Scanner cap (64 KiB) is too small for a
// large content_block_start or input_json_delta line.
const maxSSELineSize = 32 << 20

type sseMessageStart struct {
	Message struct {
		ID    string         `json:"id"`
		Type  string         `json:"type"`
		Role  string         `json:"role"`
		Model string         `json:"model"`
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

type sseContentBlockStart struct {
	Index        int                  `json:"index"`
	ContentBlock anthropicRespContent `json:"content_block"`
}

type sseContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		Signature   string `json:"signature"`
	} `json:"delta"`
}

type sseContentBlockStop struct {
	Index int `json:"index"`
}

type sseMessageDelta struct {
	Delta struct {
		StopReason  string                `json:"stop_reason"`
		StopDetails *anthropicStopDetails `json:"stop_details"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type toolCallAccumulator struct {
	id   string
	name string
	args strings.Builder
}

type streamState struct {
	inputTokens         int
	cacheCreationTokens int
	cacheReadTokens     int
	outputTokens        int
	stopReason          string
	stopDetails         *anthropicStopDetails
	toolCalls           map[int]*toolCallAccumulator
	reasoning           map[int]*reasoningAccumulator
}

// reasoningAccumulator assembles one streamed thinking or
// redacted_thinking block. The finished block carries the replay
// signature, so a streamed turn replays like a non-streamed one.
type reasoningAccumulator struct {
	content   strings.Builder
	signature string
	redacted  bool
	data      string
}

func readSSEEvents(r io.Reader, done <-chan struct{}) <-chan sseEvent {
	ch := make(chan sseEvent)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
		var eventName string
		var dataBuf bytes.Buffer

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				if dataBuf.Len() > 0 || eventName != "" {
					dataCopy := make([]byte, dataBuf.Len())
					copy(dataCopy, dataBuf.Bytes())
					select {
					case ch <- sseEvent{Event: eventName, Data: dataCopy}:
					case <-done:
						return
					}
					eventName = ""
					dataBuf.Reset()
				}
				continue
			}

			if bytes.HasPrefix(line, []byte(":")) {
				continue
			}

			if bytes.HasPrefix(line, []byte("event:")) {
				eventName = strings.TrimSpace(string(line[6:]))
			} else if bytes.HasPrefix(line, []byte("data:")) {
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.Write(bytes.TrimSpace(line[5:]))
			}
		}

		if dataBuf.Len() > 0 || eventName != "" {
			select {
			case ch <- sseEvent{Event: eventName, Data: dataBuf.Bytes()}:
			case <-done:
				return
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case ch <- sseEvent{Err: err}:
			case <-done:
			}
		}
	}()
	return ch
}

func (c *Client) handleStream(ctx context.Context, body io.ReadCloser) <-chan provider.Chunk {
	out := make(chan provider.Chunk)

	go func() {
		defer close(out)
		defer body.Close()

		done := make(chan struct{})
		defer close(done)
		eventsCh := readSSEEvents(body, done)
		state := &streamState{
			toolCalls: make(map[int]*toolCallAccumulator),
			reasoning: make(map[int]*reasoningAccumulator),
		}

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-eventsCh:
				if !ok {
					return
				}
				if ev.Err != nil {
					sendChunkOrDone(ctx, out, provider.Chunk{Err: fmt.Errorf("anthropic: stream read: %w", ev.Err)})
					return
				}
				if stop := c.processSSEEvent(ctx, ev, state, out); stop {
					return
				}
			}
		}
	}()

	return out
}

func (c *Client) processSSEEvent(ctx context.Context, ev sseEvent, s *streamState, out chan<- provider.Chunk) bool {
	switch ev.Event {
	case "message_start":
		var start sseMessageStart
		if err := json.Unmarshal(ev.Data, &start); err == nil {
			s.inputTokens = start.Message.Usage.InputTokens
			s.cacheCreationTokens = start.Message.Usage.CacheCreationInputTokens
			s.cacheReadTokens = start.Message.Usage.CacheReadInputTokens
		}
	case "content_block_start":
		var bs sseContentBlockStart
		if err := json.Unmarshal(ev.Data, &bs); err == nil {
			switch bs.ContentBlock.Type {
			case "tool_use":
				s.toolCalls[bs.Index] = &toolCallAccumulator{id: bs.ContentBlock.ID, name: bs.ContentBlock.Name}
			case "thinking":
				s.reasoning[bs.Index] = &reasoningAccumulator{}
			case "redacted_thinking":
				s.reasoning[bs.Index] = &reasoningAccumulator{redacted: true, data: bs.ContentBlock.Data}
			}
		}
	case "content_block_delta":
		return c.handleContentBlockDelta(ctx, ev.Data, s, out)
	case "content_block_stop":
		return c.handleContentBlockStop(ctx, ev.Data, s, out)
	case "message_delta":
		var md sseMessageDelta
		if err := json.Unmarshal(ev.Data, &md); err == nil {
			if md.Delta.StopReason != "" {
				s.stopReason = md.Delta.StopReason
			}
			if md.Delta.StopDetails != nil {
				s.stopDetails = md.Delta.StopDetails
			}
			// usage.output_tokens is a cumulative running total;
			// the last message_delta carries the turn's final count.
			s.outputTokens = md.Usage.OutputTokens
		}
	case "message_stop":
		return c.handleMessageStop(ctx, s, out)
	case "error":
		return c.handleErrorEvent(ctx, ev.Data, out)
	}
	return false
}

func (c *Client) handleContentBlockDelta(ctx context.Context, data []byte, s *streamState, out chan<- provider.Chunk) bool {
	var bd sseContentBlockDelta
	if err := json.Unmarshal(data, &bd); err != nil {
		return false
	}
	switch bd.Delta.Type {
	case "text_delta":
		return !sendChunkOrDone(ctx, out, provider.Chunk{Delta: bd.Delta.Text})
	case "thinking_delta":
		acc := s.reasoningBlock(bd.Index)
		acc.content.WriteString(bd.Delta.Thinking)
		if c.opts.ExposeReasoning {
			return !sendChunkOrDone(ctx, out, provider.Chunk{ReasoningDelta: bd.Delta.Thinking})
		}
	case "signature_delta":
		if acc, exists := s.reasoning[bd.Index]; exists {
			acc.signature = bd.Delta.Signature
		}
	case "input_json_delta":
		if acc, exists := s.toolCalls[bd.Index]; exists {
			acc.args.WriteString(bd.Delta.PartialJSON)
		}
	}
	return false
}

// reasoningBlock returns the accumulator for one content-block index,
// creating an implicit one when a delta arrived without a matching
// content_block_start.
func (s *streamState) reasoningBlock(index int) *reasoningAccumulator {
	acc, exists := s.reasoning[index]
	if !exists {
		acc = &reasoningAccumulator{}
		s.reasoning[index] = acc
	}
	return acc
}

func (c *Client) handleContentBlockStop(ctx context.Context, data []byte, s *streamState, out chan<- provider.Chunk) bool {
	var bstop sseContentBlockStop
	if err := json.Unmarshal(data, &bstop); err != nil {
		return false
	}
	if acc, exists := s.toolCalls[bstop.Index]; exists {
		argsStr := acc.args.String()
		if argsStr == "" {
			argsStr = "{}"
		}
		tc := &provider.ToolCall{
			Index:     bstop.Index,
			ID:        acc.id,
			Name:      acc.name,
			Arguments: []byte(argsStr),
		}
		return !sendChunkOrDone(ctx, out, provider.Chunk{ToolCallDelta: tc})
	}
	if racc, exists := s.reasoning[bstop.Index]; exists {
		delete(s.reasoning, bstop.Index)
		block := provider.ReasoningBlock{
			Content:   racc.content.String(),
			Signature: racc.signature,
			Redacted:  racc.redacted,
			Data:      racc.data,
		}
		// The callback mirrors the non-streamed decode: every readable
		// block fires once, in redacted form, when OnReasoning is set.
		if c.opts.OnReasoning != nil && !block.Redacted {
			c.opts.OnReasoning(provider.RedactBlock(provider.ReasoningBlock{Content: block.Content}))
		}
		return !sendChunkOrDone(ctx, out, provider.Chunk{ReasoningBlock: &block})
	}
	return false
}

func (c *Client) handleMessageStop(ctx context.Context, s *streamState, out chan<- provider.Chunk) bool {
	if s.stopReason == "refusal" {
		sendChunkOrDone(ctx, out, provider.Chunk{Err: refusalError(s.stopDetails)})
		return true
	}

	usage := provider.Usage{
		PromptTokens:     s.inputTokens,
		CompletionTokens: s.outputTokens,
		TotalTokens:      s.inputTokens + s.outputTokens,
		CachedTokens:     s.cacheReadTokens,
	}

	cacheUsage := cacheUsageFrom(s.inputTokens, s.cacheReadTokens, s.cacheCreationTokens)

	sendChunkOrDone(ctx, out, provider.Chunk{
		Done:         true,
		Usage:        usage,
		FinishReason: s.stopReason,
		CacheUsage:   cacheUsage,
	})
	return true
}

func (c *Client) handleErrorEvent(ctx context.Context, data []byte, out chan<- provider.Chunk) bool {
	var errResp anthropicResponse
	if err := json.Unmarshal(data, &errResp); err == nil && errResp.Error != nil {
		sendChunkOrDone(ctx, out, provider.Chunk{Err: mapAPIErrorType(errResp.Error.Type, errResp.Error.Message)})
	} else {
		sendChunkOrDone(ctx, out, provider.Chunk{Err: fmt.Errorf("anthropic: stream error: %s", string(data))})
	}
	return true
}

func sendChunkOrDone(ctx context.Context, out chan<- provider.Chunk, chunk provider.Chunk) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- chunk:
		return true
	}
}
