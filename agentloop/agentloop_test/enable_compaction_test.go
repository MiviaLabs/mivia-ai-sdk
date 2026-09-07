package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// minimalCompleter implements provider.Completer alone, with no
// TokenEstimator capability.
type minimalCompleter struct{}

func (minimalCompleter) Name() string { return "minimal" }

func (minimalCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{}, errors.New("not scripted")
}

func (minimalCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("not supported")
}

// estimatingCompleter wraps minimalCompleter with the TokenEstimator
// capability.
type estimatingCompleter struct {
	minimalCompleter
}

func (estimatingCompleter) EstimateTokens(req provider.Request) (int, error) {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	return n / 4, nil
}

// TestEnableCompactionRequiresEstimator proves a Completer without
// the TokenEstimator capability fails with ErrNoTokenEstimator and
// leaves the Options untouched.
func TestEnableCompactionRequiresEstimator(t *testing.T) {
	var opts agentloop.Options
	window := plan.Window{MaxTokens: 512, Reserve: 128}
	err := agentloop.EnableCompaction(&opts, minimalCompleter{}, window, 0.25)
	if !errors.Is(err, agentloop.ErrNoTokenEstimator) {
		t.Fatalf("EnableCompaction err = %v, want ErrNoTokenEstimator", err)
	}
	if opts.Compaction.Window != nil || opts.Compaction.Summarizer != nil || opts.Compaction.Calibrated != nil {
		t.Fatal("EnableCompaction mutated Options on failure")
	}
}

// TestEnableCompactionSetsTriple proves the Compaction group lands
// populated and the Options pass Validate.
func TestEnableCompactionSetsTriple(t *testing.T) {
	opts := agentloop.Options{}
	window := plan.Window{MaxTokens: 512, Reserve: 128}
	if err := agentloop.EnableCompaction(&opts, estimatingCompleter{}, window, 0.25); err != nil {
		t.Fatalf("EnableCompaction: %v", err)
	}
	if opts.Compaction.Window == nil || opts.Compaction.Window.MaxTokens != window.MaxTokens || opts.Compaction.Window.Reserve != window.Reserve {
		t.Fatalf("Compaction.Window = %+v, want the configured window", opts.Compaction.Window)
	}
	if opts.Compaction.Summarizer == nil {
		t.Fatal("Compaction.Summarizer is nil")
	}
	if opts.Compaction.Calibrated == nil {
		t.Fatal("Compaction.Calibrated is nil")
	}
	opts.Completer = estimatingCompleter{}
	opts.Tools = tools.New()
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestEnableCompactionZeroWindowLeavesWindowNil proves a zero or
// negative MaxTokens leaves Compaction.Window nil for New's
// derivation, while Summarizer and Calibrated land and the Options
// pass Validate. plan.Window.Validate rejects MaxTokens <= 0,
// so such a value can never be an explicit window; derive is the only
// sensible reading.
func TestEnableCompactionZeroWindowLeavesWindowNil(t *testing.T) {
	cases := []struct {
		name      string
		maxTokens int
	}{
		{"zero MaxTokens derives", 0},
		{"negative MaxTokens derives", -512},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := agentloop.Options{}
			window := plan.Window{MaxTokens: tc.maxTokens, Reserve: 128}
			if err := agentloop.EnableCompaction(&opts, estimatingCompleter{}, window, 0.25); err != nil {
				t.Fatalf("EnableCompaction: %v", err)
			}
			if opts.Compaction.Window != nil {
				t.Fatalf("Compaction.Window = %+v, want nil for derivation", opts.Compaction.Window)
			}
			if opts.Compaction.Summarizer == nil {
				t.Fatal("Compaction.Summarizer is nil")
			}
			if opts.Compaction.Calibrated == nil {
				t.Fatal("Compaction.Calibrated is nil")
			}
			opts.Completer = estimatingCompleter{}
			opts.Tools = tools.New()
			if err := opts.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestEnableCompactionExplicitWindowStillWins proves a positive
// MaxTokens lands the window as given; derivation does not replace
// it.
func TestEnableCompactionExplicitWindowStillWins(t *testing.T) {
	opts := agentloop.Options{}
	window := plan.Window{MaxTokens: 512, Reserve: 128}
	if err := agentloop.EnableCompaction(&opts, estimatingCompleter{}, window, 0.25); err != nil {
		t.Fatalf("EnableCompaction: %v", err)
	}
	if opts.Compaction.Window == nil {
		t.Fatal("Compaction.Window is nil, want the explicit window")
	}
	if opts.Compaction.Window.MaxTokens != 512 || opts.Compaction.Window.Reserve != 128 {
		t.Fatalf("Compaction.Window = %+v, want MaxTokens 512 Reserve 128 as given", opts.Compaction.Window)
	}
}

// TestDefaultBoundsPassesValidate pins DefaultBounds against the
// Validate contract and against per-cap zero-suppression drift.
func TestDefaultBoundsPassesValidate(t *testing.T) {
	b := agentloop.DefaultBounds()
	if err := b.Validate(); err != nil {
		t.Fatalf("DefaultBounds().Validate: %v", err)
	}
	if b.MaxIterations <= 0 || b.MaxTotalTokens <= 0 || b.MaxConsecutiveToolFailures <= 0 {
		t.Fatalf("DefaultBounds lost a positive cap: %+v", b)
	}
	if b.MaxConcurrentTools < 1 {
		t.Fatalf("DefaultBounds MaxConcurrentTools = %d, want serial-or-parallel positive", b.MaxConcurrentTools)
	}
}
