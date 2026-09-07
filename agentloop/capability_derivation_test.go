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

// TestDeriveWindowEffectiveThresholds pins the percent math
// deriveWindow's Reserve and hysteresis constants produce: Reserve is
// MaxTokens/5, so Budget is 4/5 of MaxTokens, and the 80/50 Compaction
// percents price against Budget, not MaxTokens. The derived Window
// therefore triggers at an effective 64% of MaxTokens and targets an
// effective 40%, floored. Checked at MaxTokens 1000 (a multiple of
// five, no floor rounding) and 1003 (not a multiple, so Reserve and
// both thresholds floor). See docs/plans/agentloop.md, "Effective
// thresholds for host-style configs".
func TestDeriveWindowEffectiveThresholds(t *testing.T) {
	cases := []struct {
		name        string
		maxTokens   int
		wantReserve int
		wantTrigger int
		wantTarget  int
	}{
		{name: "multiple of five", maxTokens: 1000, wantReserve: 200, wantTrigger: 640, wantTarget: 400},
		{name: "not a multiple of five", maxTokens: 1003, wantReserve: 200, wantTrigger: 642, wantTarget: 401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := deriveWindow(&capabilityCompleter{window: tc.maxTokens})
			if w == nil {
				t.Fatal("deriveWindow = nil")
			}
			if w.Reserve != tc.wantReserve {
				t.Fatalf("Reserve = %d, want %d", w.Reserve, tc.wantReserve)
			}
			budget := tc.maxTokens - tc.wantReserve
			if got := w.CompactTrigger(); got != tc.wantTrigger {
				t.Fatalf("CompactTrigger() = %d, want %d (floor(%d*80/100), an effective 64%% of MaxTokens %d)",
					got, tc.wantTrigger, budget, tc.maxTokens)
			}
			if got := w.CompactTarget(); got != tc.wantTarget {
				t.Fatalf("CompactTarget() = %d, want %d (floor(%d*50/100), an effective 40%% of MaxTokens %d)",
					got, tc.wantTarget, budget, tc.maxTokens)
			}
		})
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
		Completer: completer,
		Tools:     reg,
		SessionID: "cap",
		Bounds:    Bounds{MaxIterations: 2},

		Compaction: Compaction{
			Summarizer: summarizer,
			Calibrated: plan.Calibrate(completer, 0.25),
		}}
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
	// would otherwise reach the Trim/Window forbidden combination
	// (ErrInvalidOptions) without ever going through Validate.
	trimmed := Options{
		Completer: completer,
		Tools:     reg,
		SessionID: "cap",
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			return msgs, nil
		},

		Compaction: Compaction{
			Summarizer: summarizer,
			Calibrated: plan.Calibrate(completer, 0.25),
		}}
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

// TestEnableCompactionZeroWindowAdoptsDerivedWindow proves the chain
// EnableCompaction -> New composes with derivation: a window with a
// zero MaxTokens stays nil through EnableCompaction, and New derives
// the window from the Completer's ContextAccountant capability.
func TestEnableCompactionZeroWindowAdoptsDerivedWindow(t *testing.T) {
	completer := &capabilityCompleter{window: 10000}
	reg := tools.New()
	if err := reg.Add(&capabilityTool{}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	opts := Options{Completer: completer, Tools: reg, SessionID: "cap", Bounds: Bounds{MaxIterations: 2}}
	if err := EnableCompaction(&opts, completer, plan.Window{}, 0.25); err != nil {
		t.Fatalf("EnableCompaction: %v", err)
	}
	if opts.Compaction.Window != nil {
		t.Fatalf("Compaction.Window = %+v, want nil before New", opts.Compaction.Window)
	}
	loop, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if loop.window == nil || loop.window.MaxTokens != 10000 || loop.window.Reserve != 2000 {
		t.Fatalf("window = %+v, want MaxTokens 10000 Reserve 2000", loop.window)
	}
	if loop.window.CompactTrigger() != 6400 {
		t.Fatalf("trigger = %d, want 6400", loop.window.CompactTrigger())
	}
}
