// compaction_reentry_test.go continues compaction_test.go's coverage,
// split out to keep compaction_test.go under the structure gate's
// line limit: the MaxTotalTokens combination, a ctx cancellation
// mid-summarizer-call, and a second compaction across two iterations.
package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// summarizerSawPriorSummary reports whether req's excerpts carry the
// fixed summaryReplyJSON reply's Objective field, "Ship", the marker
// Summary.Render() writes for every summary this fixture produces:
// proof that a prior summary message, held aside by splitSummary and
// prepended by summarizeDropped, reached this summarizer call's
// input rather than being dropped or treated as ordinary content.
func summarizerSawPriorSummary(req provider.Request) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "Objective: Ship") {
			return true
		}
	}
	return false
}

// TestRunBudgetWindowMaxTotalTokensCombined proves the Budget-after-
// compaction reorder does not disturb MaxTotalTokens accounting: a
// Window compacts the over-trigger starting history, checkBudget
// passes on the compacted result, the completer runs, and only then
// does the running-token total trip MaxTotalTokens, on the same
// completed iteration's usage. The two checks answer different
// questions (history shape before the call, cumulative token spend
// after it) and the reorder only moved the first one.
func TestRunBudgetWindowMaxTotalTokensCombined(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	reg := tools.New()
	sum := &summaryScript{}
	summarizer, err := contextsummary.NewSummarizer(sum)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}, Usage: provider.Usage{TotalTokens: 50}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer:      completer,
		Tools:          reg,
		MaxIterations:  3,
		Budget:         &contextbudget.Limits{MaxBytes: 200},
		Window:         &w,
		Summarizer:     summarizer,
		Calibrated:     contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
		MaxTotalTokens: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want errors.Is ErrTokenBudgetExceeded", err)
	}
	if completer.callCount() != 1 {
		t.Fatalf("completer calls = %d, want 1: Budget must pass on the compacted history before the call runs", completer.callCount())
	}
	sumCalls, _ := sum.stats()
	if sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: compaction must run before either token check", sumCalls)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: MaxTotalTokens trips only after the completed iteration is recorded", res.Iterations)
	}
	if len(res.History) == 0 {
		t.Fatal("History = empty, want the one completed iteration's partial state")
	}
}

// TestRunCtxCanceledDuringCompactionSummarizer proves a ctx
// cancellation that happens during the summarizer's Chat call, inside
// planHistory, aborts the run before checkBudget or the main
// Completer.Chat call ever run: the loop's own ctx.Err() check only
// runs at the top of each iteration, so a mid-compaction cancellation
// must propagate through the summarizer's own ctx-aware Chat, not
// through a redundant check the loop adds after planHistory.
func TestRunCtxCanceledDuringCompactionSummarizer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &cancelDuringChat{}
	summarizer, err := contextsummary.NewSummarizer(fake)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	fake.cancel = cancel

	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	reg := tools.New()
	completer := &scriptedCompleter{}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 3,
		Budget:        &contextbudget.Limits{MaxBytes: 200},
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, runErr := loop.Run(ctx, msgs)
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("Run() error = %v, want errors.Is context.Canceled", runErr)
	}
	if !errors.Is(runErr, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", runErr)
	}
	if completer.callCount() != 0 {
		t.Fatalf("completer calls = %d, want 0: checkBudget and Chat must never run after a canceled compaction", completer.callCount())
	}
	if res.Iterations != 0 {
		t.Fatalf("Iterations = %d, want 0", res.Iterations)
	}
}

// cancelDuringChat is a Completer whose one Chat call cancels a
// caller-supplied context.CancelFunc, then returns the incoming ctx's
// own error. It stands in for a real Completer whose transport
// observes ctx cancellation mid-call, wired as a Summarizer's
// completer to trigger cancellation from inside planHistory's
// summarizer call.
type cancelDuringChat struct{ cancel context.CancelFunc }

func (c *cancelDuringChat) Name() string { return "cancel-during-chat" }

func (c *cancelDuringChat) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.cancel()
	return provider.Response{}, ctx.Err()
}

func (c *cancelDuringChat) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("cancelDuringChat: ChatStream not supported")
}

// TestRunBudgetWindowSecondCompactionAcrossIterations proves Budget
// and Window keep working together across a second compaction: the
// first iteration compacts and injects a summary, a tool call grows
// the history again, and the second iteration's planHistory compacts
// a second time, folding the first summary forward as the prior held
// aside by splitSummary rather than treating it as ordinary droppable
// content. Every single-shot Budget+Window test up to this point
// covers only one compaction event; this is the first to pin the
// re-compaction path a completed compaction's own output can trigger.
func TestRunBudgetWindowSecondCompactionAcrossIterations(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 80)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 500, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`), result: strings.Repeat("z", 80)})
	sum := &summaryScript{}
	summarizer, err := contextsummary.NewSummarizer(sum)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "c1", Name: "search", Arguments: []byte("{}")}),
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 4,
		Budget:        &contextbudget.Limits{MaxBytes: 350},
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil: two compactions must each keep the history under Budget", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want 2", got)
	}
	sumCalls, sumReqs := sum.stats()
	if sumCalls != 2 {
		t.Fatalf("summarizer calls = %d, want 2: one per compaction event", sumCalls)
	}
	if !summarizerSawPriorSummary(sumReqs[1]) {
		t.Fatalf("second summarizer call missing the first summary's content as prior input: %+v", sumReqs[1].Messages)
	}
	_, reqs := completerRequests(completer)
	if summaryNamed(reqs[1].Messages) != 1 {
		t.Fatalf("second Chat request must carry exactly one summary message, want the re-folded summary: %+v", reqs[1].Messages)
	}
}

// summaryFailsOnSecondCall is a Completer whose first Chat call
// succeeds with the fixed summary reply and whose second and later
// calls fail with a fixed sentinel error. It stands in for a
// Summarizer's completer to trigger a second-iteration planHistory
// failure after a first-iteration compaction already succeeded.
type summaryFailsOnSecondCall struct {
	mu    sync.Mutex
	calls int
}

func (s *summaryFailsOnSecondCall) Name() string { return "summary-fails-on-second-call" }

func (s *summaryFailsOnSecondCall) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == 1 {
		return provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: summaryReplyJSON}}, nil
	}
	return provider.Response{}, errors.New("summary boom second")
}

func (s *summaryFailsOnSecondCall) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("summaryFailsOnSecondCall: ChatStream not supported")
}

// TestRunPlanHistoryFailureLaterIterationPreservesPartialResult proves
// a planHistory failure on the second iteration, after the first
// iteration's compaction and Completer.Chat call already completed,
// carries the first iteration's partial state instead of the zero
// Result hardFail returns at iterations == 0.
func TestRunPlanHistoryFailureLaterIterationPreservesPartialResult(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 80)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 500, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`), result: strings.Repeat("z", 80)})
	summarizer, err := contextsummary.NewSummarizer(&summaryFailsOnSecondCall{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	resp := toolCallResponse(provider.ToolCall{ID: "c1", Name: "search", Arguments: []byte("{}")})
	resp.Usage = provider.Usage{TotalTokens: 30}
	completer := &scriptedCompleter{responses: []provider.Response{resp}}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 4,
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, contextsummary.ErrCallFailed) {
		t.Fatalf("Run() error = %v, want the contextsummary sentinel wrapped", err)
	}
	if got := completer.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1: the second iteration hard-fails before a second Chat call", got)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: exactly one iteration completed before the failure", res.Iterations)
	}
	if res.Usage != (provider.Usage{TotalTokens: 30}) {
		t.Fatalf("Usage = %+v, want the first iteration's usage carried forward", res.Usage)
	}
	if !isZeroMessage(res.Final) {
		t.Fatalf("Final = %+v, want the zero Message: the run did not reach a stop condition", res.Final)
	}
	if res.Stop != agentloop.StopReason("") {
		t.Fatalf("Stop = %q, want the zero StopReason", res.Stop)
	}
	if len(res.History) == 0 {
		t.Fatal("History = empty, want the first iteration's partial state")
	}
	if summaryNamed(res.History) != 1 {
		t.Fatalf("History summary count = %d, want 1: the first iteration's compaction result survives", summaryNamed(res.History))
	}
}
