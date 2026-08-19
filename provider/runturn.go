package provider

import (
	"context"
	"sort"
	"strings"
)

// RunTurn dispatches on req.Stream: it calls c.Chat when false, and
// calls c.ChatStream, drains, and aggregates when true. It calls
// Message.Validate on every entry of req.Messages, in order, before
// it dispatches; the first invalid entry stops validation and RunTurn
// returns the zero Response and that error, unwrapped, without
// calling either Completer method. On the streamed path RunTurn
// selects on ctx.Done() during drain; when ctx finishes first, RunTurn
// discards any partial aggregation and returns the zero Response
// alongside ctx.Err(). RunTurn returns the first error either
// Completer method returns, unwrapped, alongside the zero Response.
// When the ChatStream channel closes before any chunk carries
// Done == true or a non-nil Err, RunTurn discards any partial
// aggregation and returns the zero Response alongside
// ErrStreamClosedEarly; a mid-stream failure never returns a partial
// Response. On the streamed path Response.Message.ToolCalls carries
// the same merged calls as Response.ToolCalls after every call.
// buildResponse assigns both fields the same toolCalls slice.
func RunTurn(ctx context.Context, c Completer, req Request) (Response, error) {
	for _, m := range req.Messages {
		if err := m.Validate(); err != nil {
			return Response{}, err
		}
	}
	if !req.Stream {
		return c.Chat(ctx, req)
	}
	ch, err := c.ChatStream(ctx, req)
	if err != nil {
		return Response{}, err
	}
	return drainStream(ctx, ch)
}

// drainStream reads ch until a terminal Chunk or ctx cancellation,
// aggregating Delta text and merged tool-call fragments into one
// Response. See RunTurn's doc comment for the cancellation and
// mid-stream failure contract.
func drainStream(ctx context.Context, ch <-chan Chunk) (Response, error) {
	var content strings.Builder
	calls := make(map[int]*ToolCall)
	var order []int
	var usage Usage
	var finishReason string
	for {
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				return Response{}, ErrStreamClosedEarly
			}
			if err := chunk.Validate(); err != nil {
				return Response{}, err
			}
			if chunk.Err != nil {
				return Response{}, chunk.Err
			}
			content.WriteString(chunk.Delta)
			if chunk.ToolCallDelta != nil {
				mergeToolCallDelta(calls, &order, chunk.ToolCallDelta)
			}
			if chunk.Done {
				usage = chunk.Usage
				finishReason = chunk.FinishReason
				return buildResponse(&content, calls, order, usage, finishReason), nil
			}
		}
	}
}

// mergeToolCallDelta folds one ToolCallDelta fragment into calls,
// grouped by Index. Arguments concatenate in arrival order; ID and
// Name take the first non-empty value seen for that Index.
func mergeToolCallDelta(calls map[int]*ToolCall, order *[]int, delta *ToolCall) {
	tc, ok := calls[delta.Index]
	if !ok {
		tc = &ToolCall{Index: delta.Index}
		calls[delta.Index] = tc
		*order = append(*order, delta.Index)
	}
	if tc.ID == "" && delta.ID != "" {
		tc.ID = delta.ID
	}
	if tc.Name == "" && delta.Name != "" {
		tc.Name = delta.Name
	}
	tc.Arguments = append(tc.Arguments, delta.Arguments...)
}

// buildResponse assembles the aggregated Response, ordering ToolCalls
// by ascending Index. Message.Role is set to RoleAssistant
// unconditionally, and Message.ToolCalls holds the same merged calls
// as Response.ToolCalls.
func buildResponse(content *strings.Builder, calls map[int]*ToolCall, order []int, usage Usage, finishReason string) Response {
	sorted := append([]int(nil), order...)
	sort.Ints(sorted)
	toolCalls := make([]ToolCall, 0, len(sorted))
	for _, idx := range sorted {
		toolCalls = append(toolCalls, *calls[idx])
	}
	return Response{
		Message:      Message{Role: RoleAssistant, Content: content.String(), ToolCalls: toolCalls},
		ToolCalls:    toolCalls,
		Usage:        usage,
		FinishReason: finishReason,
	}
}
