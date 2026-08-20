package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// erroringEstimator always fails with err, so a caller sees the
// estimate error path without needing a real token count.
type erroringEstimator struct{ err error }

func (e erroringEstimator) EstimateTokens(req provider.Request) (int, error) {
	return 0, e.err
}

// summaryAwareEstimator prices one token per content byte, like
// scaleEstimator, for a request carrying no message named
// contextsummary.SummaryMessageName. For a request that does carry
// one, it instead applies failErr or overflow, so a test can isolate
// checkCompactedBudget's own post-injection re-estimate from every
// earlier estimate in the same Run call: contextplan.Compact's own
// internal estimates never see the injected summary message, since
// injection happens after Compact returns.
type summaryAwareEstimator struct {
	failErr  error
	overflow bool
}

func (e summaryAwareEstimator) EstimateTokens(req provider.Request) (int, error) {
	hasSummary := false
	for _, m := range req.Messages {
		if m.Name == contextsummary.SummaryMessageName {
			hasSummary = true
			break
		}
	}
	if hasSummary {
		if e.failErr != nil {
			return 0, e.failErr
		}
		if e.overflow {
			return 1 << 30, nil
		}
	}
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}

// TestRunPlanHistoryEstimatorErrorFailsWithErrPlanFailed proves
// planHistory's own EstimateTokens call, the first estimate any Run
// call with a Window makes, wraps a failing estimator's error in
// ErrPlanFailed and fails before any Completer call.
func TestRunPlanHistoryEstimatorErrorFailsWithErrPlanFailed(t *testing.T) {
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	w := contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{TriggerPercent: 50}}
	reg := tools.New()
	summarizer, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	estErr := errors.New("estimator boom")
	completer := &scriptedCompleter{}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 3,
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(erroringEstimator{err: estErr}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrPlanFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrPlanFailed", err)
	}
	if !errors.Is(err, estErr) {
		t.Fatalf("Run() error = %v, want the estimator's own error wrapped", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("completer calls = %d, want 0: the estimate must fail before any Completer call", completer.callCount())
	}
	if !isZeroResult(res) {
		t.Fatalf("Result = %+v, want the zero Result: no iteration completed before the estimate failed", res)
	}
}

// TestRunCheckCompactedBudgetEstimatorErrorFailsWithErrCompactionFailed
// proves checkCompactedBudget's own re-estimate, made after the summary
// message is injected, wraps a failing estimator's error in
// ErrCompactionFailed. summaryAwareEstimator only fails once the
// summary message is present, isolating this call from every earlier
// estimate contextplan.Compact makes on the pre-injection message set.
func TestRunCheckCompactedBudgetEstimatorErrorFailsWithErrCompactionFailed(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	reg := tools.New()
	summarizer, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	estErr := errors.New("post-compaction estimate boom")
	completer := &scriptedCompleter{}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 3,
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(summaryAwareEstimator{failErr: estErr}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, estErr) {
		t.Fatalf("Run() error = %v, want the estimator's own error wrapped", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("completer calls = %d, want 0: the re-estimate must fail before any Completer call", completer.callCount())
	}
}

// TestRunCheckCompactedBudgetOverflowAfterSummaryInjection proves
// checkCompactedBudget's own fail-closed check: a compaction that
// contextplan.Compact reports as fitting the target can still land
// over Budget once the summary message is injected afterward.
// checkCompactedBudget's re-estimate catches that case and fails with
// ErrCompactionFailed wrapping contextplan.ErrRetentionOverflow, before
// any Completer call.
func TestRunCheckCompactedBudgetOverflowAfterSummaryInjection(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	reg := tools.New()
	summarizer, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	completer := &scriptedCompleter{}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 3,
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(summaryAwareEstimator{overflow: true}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, contextplan.ErrRetentionOverflow) {
		t.Fatalf("Run() error = %v, want errors.Is contextplan.ErrRetentionOverflow", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("completer calls = %d, want 0: the overflow check must fail before any Completer call", completer.callCount())
	}
}
