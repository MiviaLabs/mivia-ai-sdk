# Example: wiring a complete agentloop run offline

This walkthrough wires one field from every group of
`agentloop.Options` that fits a scripted offline example into one
literal and runs a scripted two-turn tool exchange with no network
access. A canned `provider.Completer` stands in for a model: turn one
requests one `upper` tool call, turn two returns the final assistant
text, so the run is deterministic.

The literal carries the two field groups the Conclude and Bounds
addendum defines: `Bounds` holds the loop's numeric caps, and
`Conclude` holds the graceful-conclude margin, deadline, and notice.
It also wires the context-planning block (`Window`, `Summarizer`,
`Calibrated`), tracing, hooks, usage accounting, both host-callable
budgets, and an `Audit` function. `agentloop.New` validates the whole
literal before it builds anything; a half-wired budget fails
`Validate`.

The run goes through `RunSteerable` with a fresh `agentloop.NewSteer`.
No trigger fires; the call shows the steerable entry point. The loop
ends at `StopNoToolCalls` after two iterations, and `main` prints the
`Result`'s stop reason, iteration count, usage, and final content.

## The program

```go
// Command agentloop wires a complete agentloop.Options and runs one
// scripted two-turn tool exchange offline. A canned provider.Completer
// stands in for a model: the first turn requests one tool call, the
// second returns the final answer. Every Options group this example
// carries is the grouping the Conclude and Bounds addendum defines;
// see docs/plans/agentloop.md. The run needs no network access and
// prints the Result's stop reason, iteration count, usage, and final
// content.
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

// cannedCompleter implements provider.Completer over a script. Each
// Chat call returns the next scripted response, in order; ChatStream
// is unsupported. The script is fixed, so the run is deterministic.
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

// newCannedCompleter scripts the two-turn exchange. Turn one requests
// one upper call. Turn two carries the final assistant text. Both
// usages sit in the same small scale, far under Bounds.MaxTotalTokens.
func newCannedCompleter() *cannedCompleter {
	call := provider.ToolCall{Index: 0, ID: "call-1", Name: "upper", Arguments: []byte(`{"text":"hello"}`)}
	return &cannedCompleter{responses: []provider.Response{
		{
			Message:      provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call}},
			ToolCalls:    []provider.ToolCall{call},
			Usage:        provider.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
			FinishReason: "tool_calls",
		},
		{
			Message:      provider.Message{Role: provider.RoleAssistant, Content: "HELLO"},
			Usage:        provider.Usage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12},
			FinishReason: "stop",
		},
	}}
}

// cannedEstimator implements provider.TokenEstimator as a
// bytes-over-four count over the request's message contents. It feeds
// contextplan.Calibrate for planning and calibration.
type cannedEstimator struct{}

// EstimateTokens sums the request's message content bytes over four.
func (cannedEstimator) EstimateTokens(req provider.Request) (int, error) {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	return n / 4, nil
}

// upperTool implements tools.Tool and tools.SchemaTool. The schema
// requires the string property text; Run uppercases it.
type upperTool struct{}

// Name returns the tool's registry name.
func (upperTool) Name() string { return "upper" }

// ParameterSchema returns a JSON object requiring the string property
// text.
func (upperTool) ParameterSchema() []byte {
	return []byte(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`)
}

// upperArgs is upperTool's decoded argument shape.
type upperArgs struct {
	Text string `json:"text"`
}

// DecodeArguments unmarshals the raw arguments into upperArgs.
func (upperTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	var args upperArgs
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

// shoutTool implements tools.Tool only, with no parameter schema. It
// shows the plain-tool path beside the schema tool: registered and
// allowed, but skipped by Definitions, so the model is never offered
// it.
type shoutTool struct{}

// Name returns the tool's registry name.
func (shoutTool) Name() string { return "shout" }

// Run returns the input string uppercased with an exclamation mark.
func (shoutTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: strings.ToUpper(s) + "!"}, nil
}

// buildRegistry registers both tools under one registry.
func buildRegistry() *tools.Registry {
	reg := tools.New()
	_ = reg.Add(upperTool{})
	_ = reg.Add(shoutTool{})
	return reg
}

// buildHooks returns a hooks registry with one handler at
// PointPostTool. The handler prints the call's ID and name and returns
// true, nil, so Fire continues.
func buildHooks() *events.Registry {
	hr := events.NewRegistry()
	_ = hr.Add(events.PointPostTool, "print-post-tool", func(ctx context.Context, payload any) (bool, error) {
		if call, ok := payload.(provider.ToolCall); ok {
			fmt.Printf("post-tool hook: id=%s name=%s\n", call.ID, call.Name)
		}
		return true, nil
	})
	return hr
}

// auditPrinter is the AuditFunc. It prints each record's kind and
// iteration and returns nil, so the run continues.
func auditPrinter(ctx context.Context, rec agentloop.AuditRecord) error {
	fmt.Printf("audit: kind=%s iteration=%d\n", rec.Kind, rec.Iteration)
	return nil
}

func main() {
	ctx := context.Background()
	canned := newCannedCompleter()
	summarizer, err := contextsummary.NewSummarizer(canned)
	if err != nil {
		fmt.Println("contextsummary.NewSummarizer:", err)
		return
	}

	// One Options literal wires every group: the completer and
	// registry, the scope, the Bounds and Conclude groups, the event
	// bus with its heartbeat and dedup switches, the history budget,
	// the start-time anchor, the context window with its summarizer
	// and calibrated estimator, tracing, hooks, usage, both host
	// budgets, and audit. New validates the whole literal before it
	// builds anything.
	loop, err := agentloop.New(agentloop.Options{
		Bus:               events.New(),
		HeartbeatInterval: time.Hour,
		DedupWithinTurn:   true,
		StartTime:         time.Now(),
		Budget:            &contextbudget.Limits{MaxBytes: 1 << 20, MaxEvents: 4096},
		Completer:         canned,
		Tools:             buildRegistry(),
		Scope:             tools.NewScope(tools.ScopeOptions{Allowlist: []string{"upper", "shout"}}),
		Bounds: agentloop.Bounds{
			MaxIterations:              4,
			MaxCallsPerTurn:            4,
			MaxTotalTokens:             100000,
			MaxConcurrentTools:         2,
			MaxConsecutiveToolFailures: 2,
		},
		Conclude:   agentloop.Conclude{Margin: 1, Deadline: time.Minute, Notice: "Wrap up with your best answer now."},
		Window:     &contextplan.Window{MaxTokens: 512, Compaction: contextplan.Compaction{TriggerPercent: 80, TargetPercent: 50}},
		Summarizer: summarizer,
		Calibrated: contextplan.Calibrate(cannedEstimator{}, 0.25),
		Tracer:     trace.New(),
		Hooks:      buildHooks(),
		Usage:      usage.New(),
		SessionID:  "agentloop-example",
		WorkBudget: &agentloop.WorkBudget{
			Reserve: func(ctx context.Context, req provider.Request) error { return nil },
			Refund:  func(ctx context.Context, req provider.Request, used provider.Usage) {},
		},
		ToolBudget: &agentloop.ToolBudget{
			Reserve: func(ctx context.Context, calls int) error { return nil },
		},
		Audit: auditPrinter,
	})
	if err != nil {
		fmt.Println("agentloop.New:", err)
		return
	}

	steer := agentloop.NewSteer()
	res, err := loop.RunSteerable(ctx, []provider.Message{
		{Role: provider.RoleUser, Content: "please upper the word hello"},
	}, steer)
	if err != nil {
		fmt.Println("run:", err)
		return
	}

	fmt.Println("stop:", res.Stop)
	fmt.Println("iterations:", res.Iterations)
	fmt.Printf("usage: %+v\n", res.Usage)
	fmt.Println("final:", res.Final.Content)
}
```

The `Bounds` and `Conclude` fields replace this addendum's nine flat
`Options` fields; see
[../plans/agentloop.md](../plans/agentloop.md)'s "Addendum: the
agentloop composition example and the Conclude and Bounds groups" for
the field map. The `upper` tool publishes its parameter schema through
`tools.SchemaTool`, so `Definitions` offers it; `shout` implements
`tools.Tool` only, so the offered set skips it. `verify-fast` vets
this program through the explicit `go vet` line in the `Makefile`.
