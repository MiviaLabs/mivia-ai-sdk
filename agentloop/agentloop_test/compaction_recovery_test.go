package agentloop_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

func TestRunPriorSummaryReplacedOnSecondCompaction(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("a", 100)},
		{Role: provider.RoleUser, Content: strings.Repeat("b", 100)},
		{Role: provider.RoleAssistant, Content: "x"},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 40, TargetTokens: 20}}
	responses := []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "c1", Name: "search", Arguments: []byte("{}")}),
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}
	loop, f := newPlanningFixture(t, w, responses, nil)
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	_, reqs := completerRequests(f.completer)
	final := reqs[len(reqs)-1].Messages
	if got := summaryNamed(final); got != 1 {
		t.Fatalf("summary messages in the final request = %d, want 1", got)
	}
	sumCalls, sumReqs := f.summary.stats()
	if sumCalls != 2 {
		t.Fatalf("summarizer calls = %d, want 2", sumCalls)
	}
	excerpts := sumReqs[1].Messages[1].Content
	if !strings.HasPrefix(excerpts, "[user] Objective:") {
		t.Fatalf("second summarizer input missing the prior summary excerpt first:\n%s", excerpts)
	}
	if summaryNamed(res.History) != 1 {
		t.Fatalf("Result.History summary count = %d, want 1", summaryNamed(res.History))
	}
}

func TestRunPreserveNameDuplicateSafe(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("a", 100)},
		{Role: provider.RoleUser, Content: strings.Repeat("b", 100)},
		{Role: provider.RoleAssistant, Content: "x"},
	}
	names := []string{contextsummary.SummaryMessageName}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{
		TriggerPercent: 40, TargetTokens: 20, PreserveNames: names}}
	loop, f := newPlanningFixture(t, w, []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}, nil)
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil: the preserve-name append must stay duplicate-safe", err)
	}
	_, reqs := completerRequests(f.completer)
	if got := summaryNamed(reqs[0].Messages); got != 1 {
		t.Fatalf("summary messages = %d, want 1", got)
	}
	if len(w.Compaction.PreserveNames) != 1 {
		t.Fatalf("caller PreserveNames mutated: %+v", w.Compaction.PreserveNames)
	}
	if w.Compaction.PreserveNames[0] != contextsummary.SummaryMessageName {
		t.Fatalf("caller PreserveNames changed: %+v", w.Compaction.PreserveNames)
	}
}

// recoveryFixture wires one Loop whose completer fails the first Chat
// with provider.ErrPromptTooLong.
func newRecoveryFixture(t *testing.T, w contextplan.Window, div int, errs []error, responses []provider.Response, summaryErr error) (*agentloop.Loop, *planningFixture) {
	t.Helper()
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`)})
	sc := &scriptedCompleter{responses: responses, errs: errs}
	sum := &summaryScript{err: summaryErr}
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
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: div}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop, &planningFixture{completer: sc, summary: sum, window: w}
}

func TestRunRecoveryRetriesOnceWithNotice(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	final := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}
	loop, f := newRecoveryFixture(t, w, 1, []error{provider.ErrPromptTooLong}, []provider.Response{provider.Response{}, final}, nil)
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := f.completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want exactly 2: one rejection, one retry", got)
	}
	_, reqs := completerRequests(f.completer)
	retried := reqs[1].Messages
	foundNotice := false
	for _, m := range retried {
		if m.Role == provider.RoleUser && m.Content == agentloop.CompactionNotice {
			foundNotice = true
		}
	}
	if !foundNotice {
		t.Fatalf("retried request missing the compaction notice: %+v", retried)
	}
	if got := summaryNamed(retried); got != 1 {
		t.Fatalf("retried request summary count = %d, want 1", got)
	}
	afterSummary := false
	for i, m := range retried {
		if m.Name != contextsummary.SummaryMessageName {
			continue
		}
		if i+1 >= len(retried) || retried[i+1].Content != agentloop.CompactionNotice {
			t.Fatalf("notice does not sit directly after the summary: %+v", retried)
		}
		afterSummary = true
	}
	if !afterSummary {
		t.Fatalf("retried request carries no summary to anchor the notice: %+v", retried)
	}
	if est := contentBytes(retried); est > 4000/4 {
		t.Fatalf("retried request estimated at %d bytes, want at most the recovery target %d", est, 4000/4)
	}
}

func TestRunRecoveryLowEstimatorStillCompacts(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 1000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	final := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}
	loop, f := newRecoveryFixture(t, w, 10, []error{provider.ErrPromptTooLong}, []provider.Response{provider.Response{}, final}, nil)
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := f.completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want 2: the trigger override must compact below the configured trigger", got)
	}
}

func TestRunRecoveryTinyBudgetClampsTargetToOne(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleUser, Content: strings.Repeat("u", 100)},
	}
	w := contextplan.Window{MaxTokens: 4, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	final := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}
	loop, f := newRecoveryFixture(t, w, 100, []error{provider.ErrPromptTooLong}, []provider.Response{provider.Response{}, final}, nil)
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil: a budget of four clamps the target to one and proceeds", err)
	}
	if got := f.completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want 2", got)
	}
	_, reqs := completerRequests(f.completer)
	for _, m := range reqs[1].Messages {
		if m.Content == agentloop.CompactionNotice {
			return
		}
	}
	t.Fatalf("retried request missing the notice: %+v", reqs[1].Messages)
}

func TestRunRecoveryTinyHistoryReturnsOriginalError(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("u", 10)},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	rejection := fmt.Errorf("vendor: %w", provider.ErrPromptTooLong)
	loop, f := newRecoveryFixture(t, w, 1, []error{rejection}, []provider.Response{provider.Response{}}, nil)
	_, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, provider.ErrPromptTooLong) {
		t.Fatalf("Run() error = %v, want errors.Is ErrPromptTooLong", err)
	}
	if err != rejection {
		t.Fatalf("Run() error = %v, want the original rejection unchanged", err)
	}
	if got := f.completer.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1: no retry below one percent of the budget", got)
	}
}

func TestRunRecoverySecondRejectionPropagates(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	loop, f := newRecoveryFixture(t, w, 1,
		[]error{provider.ErrPromptTooLong, provider.ErrPromptTooLong},
		[]provider.Response{provider.Response{}, provider.Response{}}, nil)
	_, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, provider.ErrPromptTooLong) {
		t.Fatalf("Run() error = %v, want the second rejection propagated", err)
	}
	if got := f.completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want 2: no third call after a second rejection", got)
	}
}

func TestRunRecoverySummarizerFailureNoRetry(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	final := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}
	loop, f := newRecoveryFixture(t, w, 1, []error{provider.ErrPromptTooLong},
		[]provider.Response{provider.Response{}, final}, errors.New("summary boom"))
	_, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if got := f.completer.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1: no retry after a recovery summarizer failure", got)
	}
}

func TestRunPromptTooLongWithoutWindowPropagates(t *testing.T) {
	reg := tools.New()
	sc := &scriptedCompleter{
		responses: []provider.Response{},
		errs:      []error{provider.ErrPromptTooLong},
	}
	loop, err := agentloop.New(agentloop.Options{Completer: sc, Tools: reg, MaxIterations: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if !errors.Is(err, provider.ErrPromptTooLong) {
		t.Fatalf("Run() error = %v, want the rejection unchanged without a Window", err)
	}
	if got := sc.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1", got)
	}
}

// TestRunRecoveryWindowValidateFailurePropagatesErrCompactionFailed
// proves a caller Window whose Budget is small enough that
// recoveryWindow's clamped TargetTokens lands at or above that same
// Budget fails validation: a MaxTokens of one clamps the recovery
// TargetTokens to one, which sits at, not under, a Budget of one.
// recoverPromptTooLong's own rw.Validate() call catches this before
// ever calling compactHistory; contextplan.Compact's own internal
// Validate call is a second, redundant backstop reached only if the
// first one is ever removed, so this test's ErrCompactionFailed
// assertion holds either way. The starting message carries empty
// Content so planHistory's own pre-call estimate (zero) stays under the
// original window's TriggerPercent-100 trigger (one) and reaches
// runChat unchanged; only the recovery path's own window computation
// trips. The run fails with ErrCompactionFailed after the original
// rejection's one call, with no retry.
func TestRunRecoveryWindowValidateFailurePropagatesErrCompactionFailed(t *testing.T) {
	msgs := []provider.Message{{Role: provider.RoleUser, Content: ""}}
	w := contextplan.Window{MaxTokens: 1, Compaction: contextplan.Compaction{TriggerPercent: 100, TargetPercent: 5}}
	loop, f := newRecoveryFixture(t, w, 1, []error{provider.ErrPromptTooLong}, []provider.Response{provider.Response{}}, nil)
	_, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !strings.Contains(err.Error(), "at or above budget") {
		t.Fatalf("Run() error = %v, want it to carry the recovery window's own Validate reason", err)
	}
	if got := f.completer.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1: no retry once the recovery window itself fails to validate", got)
	}
}

func TestRunConcurrentSharedLoopWithPlanning(t *testing.T) {
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	final := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}
	loop, f := newRecoveryFixture(t, w, 1, nil, []provider.Response{final, final, final, final}, nil)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := loop.Run(context.Background(), msgs); err != nil {
				t.Errorf("Run: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := f.completer.callCount(); got != 4 {
		t.Fatalf("completer calls = %d, want 4", got)
	}
}
