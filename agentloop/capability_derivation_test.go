package agentloop

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// capabilityCompleter implements Completer, ContextAccountant,
// ReasoningPolicy, and TokenEstimator over a scripted pair of
// responses, so New's capability derivation can be observed through
// the loop's own fields.
type capabilityCompleter struct {
	window int
	effort string
}

func (c *capabilityCompleter) Name() string { return "capability" }

func (c *capabilityCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "done"},
		FinishReason: "stop",
	}, nil
}

func (c *capabilityCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, context.Canceled
}

func (c *capabilityCompleter) ContextWindow() int { return c.window }

func (c *capabilityCompleter) ReasoningEffort() string { return c.effort }

func (c *capabilityCompleter) EstimateTokens(req provider.Request) (int, error) {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	return n / 4, nil
}

// TestDeriveWindowFromContextAccountant pins the default window: it
// sizes from the capability with the 80/50 hysteresis and one fifth
// held back as reserve.
func TestDeriveWindowFromContextAccountant(t *testing.T) {
	w := deriveWindow(&capabilityCompleter{window: 10000})
	if w == nil {
		t.Fatal("deriveWindow = nil for a ContextAccountant completer")
	}
	if w.MaxTokens != 10000 || w.Reserve != 2000 {
		t.Fatalf("window = %+v, want MaxTokens 10000 Reserve 2000", w)
	}
	// The trigger applies to the budget after reserve: 80% of 8000.
	if w.CompactTrigger() != 6400 {
		t.Fatalf("trigger = %d, want 6400", w.CompactTrigger())
	}
	if deriveWindow(&capabilityCompleter{window: 0}) != nil {
		t.Fatal("deriveWindow must return nil for a non-positive window")
	}
	if deriveWindow(nil) != nil {
		t.Fatal("deriveWindow must return nil for a nil completer")
	}
}

// TestDeriveReasoningEffort pins the effort default: the capability's
// value passes through, its absence yields empty.
func TestDeriveReasoningEffort(t *testing.T) {
	if got := deriveReasoningEffort(&capabilityCompleter{effort: "high"}); got != "high" {
		t.Fatalf("effort = %q, want high", got)
	}
	if got := deriveReasoningEffort(&capabilityCompleter{}); got != "" {
		t.Fatalf("effort = %q, want empty without the capability", got)
	}
}

// TestNewAdoptsDerivedWindow pins the New wiring: with the summarizer
// and estimator wired and no explicit Window, the loop's window is
// the derived one; the effort default rides the loop too. With the
// summarizer missing, derivation stays off and New still succeeds.
func TestNewAdoptsDerivedWindow(t *testing.T) {
	completer := &capabilityCompleter{window: 10000, effort: "medium"}
	reg := tools.New()
	if err := reg.Add(&capabilityTool{}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	summarizer, err := plan.NewSummarizer(completer)
	if err != nil {
		t.Fatalf("summarizer: %v", err)
	}
	opts := Options{
		Completer:  completer,
		Tools:      reg,
		Summarizer: summarizer,
		Calibrated: plan.Calibrate(completer, 0.25),
		SessionID:  "cap",
		Bounds:     Bounds{MaxIterations: 2},
	}
	loop, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if loop.window == nil || loop.window.MaxTokens != 10000 {
		t.Fatalf("window = %+v, want the derived window", loop.window)
	}
	if loop.defaultEffort != "medium" {
		t.Fatalf("defaultEffort = %q, want medium", loop.defaultEffort)
	}

	// No summarizer: derivation stays off, no window, New succeeds.
	plain := Options{Completer: completer, Tools: reg, SessionID: "cap"}
	plainLoop, err := New(plain)
	if err != nil {
		t.Fatalf("New without summarizer: %v", err)
	}
	if plainLoop.window != nil {
		t.Fatal("window derived without a summarizer; Validate would reject the triple")
	}

	// Trim set: derivation must stand down, since a derived Window
	// would otherwise reach ErrTrimExcluded's forbidden combination
	// without ever going through Validate.
	trimmed := Options{
		Completer:  completer,
		Tools:      reg,
		Summarizer: summarizer,
		Calibrated: plan.Calibrate(completer, 0.25),
		SessionID:  "cap",
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			return msgs, nil
		},
	}
	trimmedLoop, err := New(trimmed)
	if err != nil {
		t.Fatalf("New with Trim set: %v", err)
	}
	if trimmedLoop.window != nil {
		t.Fatal("window derived with Trim set; Validate would reject Window and Trim together")
	}
}

// capabilityTool is a minimal schema tool for the registry the
// derivation tests build.
type capabilityTool struct{}

// Name returns the tool's registry name.
func (capabilityTool) Name() string { return "noop" }

// ParameterSchema publishes an empty object schema.
func (capabilityTool) ParameterSchema() []byte {
	return []byte(`{"type":"object","properties":{}}`)
}

// DecodeArguments returns the raw payload unchanged.
func (capabilityTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

// Run reports done.
func (capabilityTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "done"}, nil
}
