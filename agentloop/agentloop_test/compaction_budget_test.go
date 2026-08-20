package agentloop_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// failOnSummaryEstimator prices one token per content byte, like
// scaleEstimator, except it fails with err whenever req carries a
// message named contextsummary.SummaryMessageName. Only
// checkCompactedBudget's post-injection re-check ever estimates a
// history carrying that message: contextplan.Compact's own internal
// estimates (before, mandatory, tail-fill, after) all run over the
// summary-stripped rest, and planHistory's own first estimate runs
// before any injection. This isolates checkCompactedBudget's own
// EstimateTokens failure branch from every other estimator call in
// one compaction pass.
type failOnSummaryEstimator struct{ err error }

func (e failOnSummaryEstimator) EstimateTokens(req provider.Request) (int, error) {
	total := 0
	for _, m := range req.Messages {
		if m.Name == contextsummary.SummaryMessageName {
			return 0, e.err
		}
		total += len(m.Content)
	}
	return total, nil
}

// TestRunCompactedHistoryStillOverBudgetFailsClosed proves
// checkCompactedBudget's documented fail-closed invariant: when the
// rebuilt history, summary included, still estimates over the
// window's budget after a successful compaction, Run fails with
// ErrCompactionFailed wrapping contextplan.ErrRetentionOverflow,
// distinct from Compact's own overflow on the retained set alone
// (TestRunRetentionOverflowFailsBeforeRequest). The oversized
// summarizer reply is what pushes the rebuilt total over budget: the
// Kept set alone fits under TargetTokens, but Compact never accounts
// for the summary message agentloop injects afterward.
func TestRunCompactedHistoryStillOverBudgetFailsClosed(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 200, Compaction: contextplan.Compaction{TriggerPercent: 40, TargetTokens: 20}}
	hugeField := strings.Repeat("x", 1024)
	hugeReply := fmt.Sprintf(`{"Objective":%q,"State":%q,"Decisions":[],"OpenWork":[],"Risks":[]}`, hugeField, hugeField)
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`)})
	sc := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	sum := &summaryScript{reply: hugeReply}
	summarizer, err := contextsummary.NewSummarizer(sum)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     sc,
		Tools:         reg,
		MaxIterations: 4,
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, contextplan.ErrRetentionOverflow) {
		t.Fatalf("Run() error = %v, want errors.Is ErrRetentionOverflow: the post-injection re-check must fail closed", err)
	}
	if got := sc.callCount(); got != 0 {
		t.Fatalf("completer calls = %d, want 0: nothing is sent once the rebuilt history still overflows", got)
	}
	// A summarizer call already happened: this proves checkCompactedBudget's
	// own post-injection re-check failed, not Compact's earlier
	// mandatory-retention check, which returns before any summarizer call
	// (see TestRunRetentionOverflowFailsBeforeRequest for that contrast).
	if sumCalls, _ := sum.stats(); sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: the failure must come from the post-injection budget check, not Compact's own mandatory-overflow check", sumCalls)
	}
}

// TestRunCompactedHistoryExactlyAtBudgetPasses pins
// checkCompactedBudget's boundary: a rebuilt history whose estimate
// lands exactly on the window's Budget() passes, since the check
// fails only strictly above it (est > w.Budget()), not at or under.
// The empty system message, the fixed-content summary, and the
// single-byte mandatory user message sum to exactly 51 bytes; Budget
// is set to 51 to land the estimate exactly on the boundary.
func TestRunCompactedHistoryExactlyAtBudgetPasses(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: ""},
		{Role: provider.RoleUser, Content: strings.Repeat("d", 100)},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 51, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 1}}
	reg := tools.New()
	sc := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	sum := &summaryScript{reply: `{"Objective":"o","State":"s","Decisions":[],"OpenWork":[],"Risks":[]}`}
	summarizer, err := contextsummary.NewSummarizer(sum)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     sc,
		Tools:         reg,
		MaxIterations: 4,
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil: an estimate exactly at Budget() must pass, not fail closed", err)
	}
	_, reqs := completerRequests(sc)
	total := 0
	for _, m := range reqs[0].Messages {
		total += len(m.Content)
	}
	if total != w.Budget() {
		t.Fatalf("sent history bytes = %d, want exactly Budget() %d: the boundary fixture drifted", total, w.Budget())
	}
}

// TestRunCheckCompactedBudgetEstimatorErrorFailsClosed proves
// checkCompactedBudget's own EstimateTokens failure, distinct from
// planHistory's first-call failure or Compact's internal estimates,
// fails the run with ErrCompactionFailed wrapping the estimator's own
// error. failOnSummaryEstimator only fails once the rebuilt history
// carries the injected summary message, which happens only inside
// checkCompactedBudget's re-check.
func TestRunCheckCompactedBudgetEstimatorErrorFailsClosed(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	estErr := errors.New("post-injection estimate boom")
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	reg := tools.New()
	sc := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     sc,
		Tools:         reg,
		MaxIterations: 4,
		Window:        &w,
		Summarizer:    sum,
		Calibrated:    contextplan.Calibrate(failOnSummaryEstimator{err: estErr}, 1.0),
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
	if got := sc.callCount(); got != 0 {
		t.Fatalf("completer calls = %d, want 0: a failed post-injection re-check must never reach Chat", got)
	}
}

func TestRunRetentionOverflowFailsBeforeRequest(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("u", 100)},
	}
	w := contextplan.Window{MaxTokens: 50, Compaction: contextplan.Compaction{TriggerPercent: 100}}
	loop, f := newPlanningFixture(t, w, nil, nil)
	_, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, contextplan.ErrRetentionOverflow) {
		t.Fatalf("Run() error = %v, want errors.Is ErrRetentionOverflow", err)
	}
	if got := f.completer.callCount(); got != 0 {
		t.Fatalf("completer calls = %d, want 0", got)
	}
	// No summarizer call: this proves Compact's own mandatory-retention
	// check failed before any summarization, distinct from
	// TestRunCompactedHistoryStillOverBudgetFailsClosed's post-injection
	// re-check, which only fails after one summarizer call.
	if sumCalls, _ := f.summary.stats(); sumCalls != 0 {
		t.Fatalf("summarizer calls = %d, want 0: the failure must come from Compact's own mandatory-overflow check, before any summarization", sumCalls)
	}
}
