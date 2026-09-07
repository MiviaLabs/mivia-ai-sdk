// Command quickstart is the README Quick Start program: one schema
// tool, one agentloop.Run call. It uses the live Anthropic completer
// when ANTHROPIC_API_KEY is set, and a canned offline completer
// otherwise, so go run always prints a fixed final line.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// upperTool implements tools.Tool and tools.SchemaTool.
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

// cannedCompleter scripts one tool call and one final answer, so the
// Quick Start runs offline when ANTHROPIC_API_KEY is unset.
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

// EstimateTokens sums the request's message content bytes over four.
func (c *cannedCompleter) EstimateTokens(req provider.Request) (int, error) {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	return n / 4, nil
}

// newCompleter returns the live Anthropic completer when
// ANTHROPIC_API_KEY is set, and an offline canned completer otherwise.
func newCompleter() (provider.Completer, error) {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return anthropic.New(anthropic.Options{APIKey: key})
	}
	return &cannedCompleter{responses: []provider.Response{
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
	}}, nil
}

func main() {
	completer, err := newCompleter()
	if err != nil {
		fmt.Println("newCompleter:", err)
		return
	}

	reg := tools.New()
	_ = reg.Add(upperTool{})

	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.DefaultBounds(),
	})
	if err != nil {
		fmt.Println("agentloop.New:", err)
		return
	}

	res, err := loop.Run(context.Background(), []provider.Message{
		{Role: provider.RoleUser, Content: "Uppercase the word hello."},
	})
	if err != nil {
		fmt.Println("run:", err)
		return
	}
	fmt.Println("final:", res.Final.Content)
}
