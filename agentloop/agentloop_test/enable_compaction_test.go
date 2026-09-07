package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
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
// the TokenEstimator capability fails with ErrEstimatorRequired and
// leaves the Options untouched.
func TestEnableCompactionRequiresEstimator(t *testing.T) {
	var opts agentloop.Options
	window := contextplan.Window{MaxTokens: 512, Reserve: 128}
	err := agentloop.EnableCompaction(&opts, minimalCompleter{}, window, 0.25)
	if !errors.Is(err, agentloop.ErrEstimatorRequired) {
		t.Fatalf("EnableCompaction err = %v, want ErrEstimatorRequired", err)
	}
	if opts.Window != nil || opts.Summarizer != nil || opts.Calibrated != nil {
		t.Fatal("EnableCompaction mutated Options on failure")
	}
}

// TestEnableCompactionSetsTriple proves the Window, Summarizer, and
// Calibrated fields land populated and the Options pass Validate.
func TestEnableCompactionSetsTriple(t *testing.T) {
	opts := agentloop.Options{}
	window := contextplan.Window{MaxTokens: 512, Reserve: 128}
	if err := agentloop.EnableCompaction(&opts, estimatingCompleter{}, window, 0.25); err != nil {
		t.Fatalf("EnableCompaction: %v", err)
	}
	if opts.Window == nil || opts.Window.MaxTokens != window.MaxTokens || opts.Window.Reserve != window.Reserve {
		t.Fatalf("Window = %+v, want the configured window", opts.Window)
	}
	if opts.Summarizer == nil {
		t.Fatal("Summarizer is nil")
	}
	if opts.Calibrated == nil {
		t.Fatal("Calibrated is nil")
	}
	opts.Completer = estimatingCompleter{}
	opts.Tools = tools.New()
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
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
