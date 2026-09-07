// Command agentloop_minimal shows the smallest useful agentloop
// entry: one completer, one tool, DefaultBounds, and EnableCompaction
// build the whole Options. The canned provider.Completer reports its
// context window through the ContextAccountant capability, and the
// zero Window passed to EnableCompaction asks New to derive the
// planning window from it: 2048 tokens with a 409 reserve at an 80/50
// trigger and target. A canned provider.Completer stands in for
// a model, so the run is offline and deterministic. Compare
// docs/examples/_agentloop, which wires every Options group.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// cannedCompleter implements provider.Completer,
// provider.TokenEstimator, and provider.ContextAccountant over a
// fixed script. Turn one requests one upper call; turn two returns
// the final answer. EstimateTokens is a bytes-over-four count.
// ContextWindow reports the model's window size New derives from.
type cannedCompleter struct {
	responses []provider.Response
	calls     int
}

// Name returns the completer's own label.
func (c *cannedCompleter) Name() string { return "canned" }

// Chat returns one scripted response per call, in order.
func (c *cannedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	if c.calls >= len(c.responses) {
		return provider.Response{}, errors.New("cannedCompleter: no response scripted for this call")
	}
	resp := c.responses[c.calls]
	c.calls++
	return resp, nil
}

// ChatStream is unsupported; the scripted exchange is non-streaming.
func (c *cannedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("cannedCompleter: ChatStream not supported")
}

// ContextWindow reports the canned model's 2048-token window.
func (c *cannedCompleter) ContextWindow() int { return 2048 }

// EstimateTokens sums the request's message content bytes over four.
func (c *cannedCompleter) EstimateTokens(req provider.Request) (int, error) {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	return n / 4, nil
}

// upperTool implements tools.Tool and tools.SchemaTool; Run
// uppercases the input string.
type upperTool struct{}

// Name returns the tool's registry name.
func (upperTool) Name() string { return "upper" }

// ParameterSchema returns a JSON object requiring the string property text.
func (upperTool) ParameterSchema() []byte {
	return []byte(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`)
}

// DecodeArguments unmarshals the raw arguments into the tool's shape.
func (upperTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args.Text}, nil
}

// Run returns the input string uppercased.
func (upperTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: strings.ToUpper(s)}, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	completer := &cannedCompleter{responses: []provider.Response{
		{
			Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{Index: 0, ID: "call-1", Name: "upper", Arguments: []byte(`{"text":"hello"}`)},
			}},
			ToolCalls: []provider.ToolCall{
				{Index: 0, ID: "call-1", Name: "upper", Arguments: []byte(`{"text":"hello"}`)},
			},
			FinishReason: "tool_calls",
		},
		{
			Message:      provider.Message{Role: provider.RoleAssistant, Content: "HELLO"},
			FinishReason: "stop",
		},
	}}

	reg := tools.New()
	reg.Add(upperTool{})

	opts := agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.DefaultBounds(),
	}
	// A zero Window asks New to derive the planning window from the
	// completer's ContextAccountant capability.
	if err := agentloop.EnableCompaction(&opts, completer, contextplan.Window{}, 0.25); err != nil {
		fmt.Println("EnableCompaction:", err)
		return
	}

	loop, err := agentloop.New(opts)
	if err != nil {
		fmt.Println("agentloop.New:", err)
		return
	}
	res, err := loop.Run(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "please upper the word hello"},
	})
	if err != nil {
		fmt.Println("run:", err)
		return
	}
	fmt.Println("final:", res.Final.Content)
}
