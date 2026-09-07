// Command agentloop_adoption is the external-adopter positive
// control: it sets every agentloop Options row an external consumer
// is expected to adopt, one commented line per row of the adoption
// table (Usage, Budget, MaxTotalTokens, MaxConsecutiveToolFailures,
// Tracer, Audit, HeartbeatInterval + Bus, the Window/Summarizer/
// Calibrated compaction triple, and the Extensions group: DedupWithinTurn,
// Conclude, and StartTime). A canned
// provider.Completer stands in for a model, so the run is offline and
// deterministic. verify-fast runs it and asserts its final output.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// cannedCompleter implements provider.Completer and
// provider.TokenEstimator over a fixed script. Turn one requests one
// upper call; turn two returns the final answer.
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

// EstimateTokens sums the request's message content and reasoning
// bytes over four. The compaction triple needs this capability;
// provider/anthropic implements it against the count_tokens endpoint.
func (c *cannedCompleter) EstimateTokens(req provider.Request) (int, error) {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
		for _, b := range m.ReasoningBlocks {
			n += len(b.Content) + len(b.Data)
		}
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

// buildAdoptionOptions sets one commented line per adoption row. The
// completer also supplies the estimator and the summarizer.
func buildAdoptionOptions(completer *cannedCompleter, reg *tools.Registry, bus *events.Bus, accum *usage.Accumulator) (agentloop.Options, error) {
	opts := agentloop.Options{
		Completer: completer,
		Tools:     reg,
		// Row: Usage (+ SessionID). The session recorder rides the run.
		Usage:     accum,
		SessionID: "agentloop-adoption",
		// Row: Budget. Per-session byte and event caps.
		Budget: &contextbudget.Limits{MaxBytes: 1 << 20, MaxEvents: 4096},
		// Rows: Bounds.MaxTotalTokens and MaxConsecutiveToolFailures.
		// The per-run billing ceiling and the failure tripwire.
		Bounds: agentloop.Bounds{
			MaxIterations:              8,
			MaxCallsPerTurn:            4,
			MaxTotalTokens:             100000,
			MaxConcurrentTools:         2,
			MaxConsecutiveToolFailures: 3,
		},
		// Row: Tracer. Spans for the host's session sink.
		Tracer: trace.New(),
		// Row: Audit. One structured record per tool call.
		Audit: func(ctx context.Context, rec agentloop.AuditRecord) error {
			return nil
		},
		// Row: HeartbeatInterval + Bus. The host subscribes to this bus.
		Bus:               bus,
		HeartbeatInterval: time.Hour,
		// Row: Extensions. The host-mirror knobs: DedupWithinTurn and
		// the Conclude group with its StartTime anchor.
		Extensions: &agentloop.Extensions{
			DedupWithinTurn: true,
			StartTime:       time.Now(),
			Conclude:        agentloop.Conclude{Margin: 1, Deadline: time.Minute, Notice: "Wrap up with your best answer now."},
		},
	}
	// Row: Compaction (Window + Summarizer + Calibrated). The
	// planning triple; the completer must implement
	// provider.TokenEstimator.
	window := contextplan.Window{
		MaxTokens:  2048,
		Reserve:    512,
		Compaction: contextplan.Compaction{TriggerPercent: 80, TargetPercent: 50},
	}
	opts.Compaction.Window = &window
	summarizer, err := contextsummary.NewSummarizer(completer)
	if err != nil {
		return opts, err
	}
	opts.Compaction.Summarizer = summarizer
	opts.Compaction.Calibrated = contextplan.Calibrate(completer, 0.25)
	return opts, nil
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
	if err := reg.Add(upperTool{}); err != nil {
		fmt.Println("register:", err)
		return
	}

	opts, err := buildAdoptionOptions(completer, reg, events.New(), usage.New())
	if err != nil {
		fmt.Println("options:", err)
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
