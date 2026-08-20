package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// countingEstimator counts bytes like scaleEstimator through its
// failAfter'th call, then fails every call after that with
// errEstimateBoom: it isolates a specific EstimateTokens call site
// from the ones ahead of it in the same iteration.
type countingEstimator struct {
	mu        sync.Mutex
	n         int
	failAfter int
}

func (e *countingEstimator) EstimateTokens(req provider.Request) (int, error) {
	e.mu.Lock()
	e.n++
	n := e.n
	e.mu.Unlock()
	if n > e.failAfter {
		return 0, errEstimateBoom
	}
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}

// errEstimateBoom is a sentinel a fixture estimator returns to prove
// an EstimateTokens failure propagates unchanged.
var errEstimateBoom = errors.New("agentloop_test: estimate boom")

// erroringEstimator fails every EstimateTokens call with
// errEstimateBoom.
type erroringEstimator struct{}

func (erroringEstimator) EstimateTokens(provider.Request) (int, error) {
	return 0, errEstimateBoom
}

// TestRunPlanEstimateFailureFailsBeforeRequest proves an EstimateTokens
// failure on planHistory's own pre-trigger estimate, before any
// compaction runs, fails the iteration with ErrPlanFailed and the
// wrapped estimator error, and never reaches the Completer.
func TestRunPlanEstimateFailureFailsBeforeRequest(t *testing.T) {
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	w := &contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{TriggerPercent: 50}}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         tools.New(),
		MaxIterations: 3,
		Window:        w,
		Summarizer:    sum,
		Calibrated:    contextplan.Calibrate(erroringEstimator{}, 1.0),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if !errors.Is(err, agentloop.ErrPlanFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrPlanFailed", err)
	}
	if !errors.Is(err, errEstimateBoom) {
		t.Fatalf("Run() error = %v, want the wrapped estimator error", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("completer calls = %d, want 0: a planning estimate failure must never reach the Completer", completer.callCount())
	}
	if len(res.History) != 1 || res.History[0].Content != "hi" {
		t.Fatalf("Result.History = %+v, want the pre-planning history unchanged", res.History)
	}
}

// TestRunCompactedBudgetExceededAfterSummaryInjection proves the
// post-compaction budget re-check fails closed when the mandatory kept
// messages fit the window on their own, but the injected summary
// message pushes the rebuilt history over Window.Budget(): a case
// Compact itself cannot see, since the summary is injected after
// Compact returns.
func TestRunCompactedBudgetExceededAfterSummaryInjection(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 40)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "final"},
	}
	w := contextplan.Window{MaxTokens: 50, Compaction: contextplan.Compaction{TriggerPercent: 10, TargetTokens: 5}}
	loop, f := newPlanningFixture(t, w, nil, nil)
	_, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, contextplan.ErrRetentionOverflow) {
		t.Fatalf("Run() error = %v, want errors.Is ErrRetentionOverflow", err)
	}
	sumCalls, _ := f.summary.stats()
	if sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: Compact must succeed and inject a summary before the re-check fails", sumCalls)
	}
	if got := f.completer.callCount(); got != 0 {
		t.Fatalf("completer calls = %d, want 0: the budget re-check must fail before any request is sent", got)
	}
}

// TestRunCompactedBudgetEstimateFailure proves an EstimateTokens
// failure on checkCompactedBudget's own post-compaction re-estimate,
// the last of six calls this scenario makes when every call succeeds,
// fails the iteration with ErrCompactionFailed and the wrapped
// estimator error, distinct from every earlier call along the same
// planning and compaction path.
func TestRunCompactedBudgetEstimateFailure(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 40)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "final"},
	}
	w := contextplan.Window{MaxTokens: 200, Compaction: contextplan.Compaction{TriggerPercent: 10, TargetTokens: 5}}
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	est := &countingEstimator{failAfter: 5}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 3,
		Window: &w, Summarizer: sum, Calibrated: contextplan.Calibrate(est, 1.0),
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, errEstimateBoom) {
		t.Fatalf("Run() error = %v, want the wrapped estimator error", err)
	}
	if got := completer.callCount(); got != 0 {
		t.Fatalf("completer calls = %d, want 0: the budget re-check must fail before any request is sent", got)
	}
}

// TestRunRecoveryWindowValidateFailureFailsIteration proves a recovery
// window whose Validate fails, because the clamped recovery target
// tokens land at or above a one-token Budget, fails the iteration with
// ErrCompactionFailed instead of retrying the Completer.
func TestRunRecoveryWindowValidateFailureFailsIteration(t *testing.T) {
	msgs := []provider.Message{{Role: provider.RoleUser, Content: ""}}
	w := contextplan.Window{MaxTokens: 1, Compaction: contextplan.Compaction{TriggerPercent: 100}}
	loop, f := newRecoveryFixture(t, w, 1, []error{provider.ErrPromptTooLong}, []provider.Response{{}}, nil)
	_, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if got := f.completer.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1: an invalid recovery window must never retry", got)
	}
}
