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

// scaleEstimator prices one token per content byte, divided by div.
// div one is exact; a larger div under-reports.
type scaleEstimator struct{ div int }

func (e scaleEstimator) EstimateTokens(req provider.Request) (int, error) {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total / e.div, nil
}

// summaryScript backs one Summarizer: it counts calls, records every
// request, and answers with one fixed valid summary reply, or one
// configured error.
type summaryScript struct {
	mu    sync.Mutex
	calls int
	reqs  []provider.Request
	err   error
}

func (s *summaryScript) Name() string { return "summary-script" }

func (s *summaryScript) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return provider.Response{}, s.err
	}
	return provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: summaryReplyJSON},
	}, nil
}

func (s *summaryScript) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("summaryScript: ChatStream not supported")
}

func (s *summaryScript) stats() (int, []provider.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]provider.Request(nil), s.reqs...)
}

// summaryReplyJSON is one strict-schema reply every field set.
const summaryReplyJSON = `{"Objective":"Ship","State":"Two tests fail","Decisions":["d1"],"OpenWork":["w1"],"Risks":["r1"]}`

// planningFixture wires one Loop with Window, Summarizer, and
// Calibrated for the compaction tests.
type planningFixture struct {
	completer *scriptedCompleter
	summary   *summaryScript
	window    contextplan.Window
}

func newPlanningFixture(t *testing.T, w contextplan.Window, responses []provider.Response, summaryErr error) (*agentloop.Loop, *planningFixture) {
	t.Helper()
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`)})
	sc := &scriptedCompleter{responses: responses}
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
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop, &planningFixture{completer: sc, summary: sum, window: w}
}

// summaryNamed counts messages named contextsummary.SummaryMessageName.
func summaryNamed(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Name == contextsummary.SummaryMessageName {
			n++
		}
	}
	return n
}

// contentBytes sums one request's message content lengths.
func contentBytes(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content)
	}
	return total
}

func TestOptionsValidateWindowRules(t *testing.T) {
	validWindow := &contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{TriggerPercent: 50}}
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	cal := contextplan.Calibrate(scaleEstimator{div: 1}, 1.0)
	base := agentloop.Options{
		Completer:     &scriptedCompleter{},
		Tools:         tools.New(),
		MaxIterations: 2,
	}
	cases := []struct {
		name    string
		mutate  func(o *agentloop.Options)
		wantErr error
	}{
		{
			name:    "window without summarizer",
			mutate:  func(o *agentloop.Options) { o.Window = validWindow; o.Calibrated = cal },
			wantErr: agentloop.ErrSummarizerRequired,
		},
		{
			name:    "window without calibrated",
			mutate:  func(o *agentloop.Options) { o.Window = validWindow; o.Summarizer = sum },
			wantErr: agentloop.ErrEstimatorRequired,
		},
		{
			name: "window with both and no trim passes",
			mutate: func(o *agentloop.Options) {
				o.Window = validWindow
				o.Summarizer = sum
				o.Calibrated = cal
			},
		},
		{
			name: "window and trim together fail",
			mutate: func(o *agentloop.Options) {
				o.Window = validWindow
				o.Summarizer = sum
				o.Calibrated = cal
				o.Trim = func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) { return msgs, nil }
			},
			wantErr: agentloop.ErrTrimExcluded,
		},
		{
			name: "invalid window fails wrapping contextplan",
			mutate: func(o *agentloop.Options) {
				o.Window = &contextplan.Window{MaxTokens: 0}
				o.Summarizer = sum
				o.Calibrated = cal
			},
			wantErr: contextplan.ErrMaxTokensNotPositive,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := base
			c.mutate(&opts)
			err := opts.Validate()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, want errors.Is %v", err, c.wantErr)
			}
		})
	}
}

func TestRunUnderTriggerNoCompaction(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: strings.Repeat("o", 40)},
		{Role: provider.RoleUser, Content: strings.Repeat("u", 59)},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 40, TargetPercent: 5}}
	responses := []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "c1", Name: "search", Arguments: []byte("{}")}),
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}, Usage: provider.Usage{TotalTokens: 396}},
	}
	responses[0].Usage = provider.Usage{TotalTokens: 396}
	loop, f := newPlanningFixture(t, w, responses, nil)
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := f.completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want 2", got)
	}
	_, reqs := completerRequests(f.completer)
	if len(reqs) != 2 {
		t.Fatalf("recorded requests = %d, want 2", len(reqs))
	}
	if len(reqs[0].Messages) != len(msgs) {
		t.Fatalf("under-trigger request changed the history: %d messages", len(reqs[0].Messages))
	}
	sumCalls, _ := f.summary.stats()
	if sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: Observe must move the factor and trip the trigger", sumCalls)
	}
	if summaryNamed(reqs[1].Messages) != 1 {
		t.Fatalf("second request lacks the injected summary: %+v", reqs[1].Messages)
	}
}

// completerRequests reads the scripted completer's recorded requests.
func completerRequests(s *scriptedCompleter) (int, []provider.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]provider.Request(nil), s.reqs...)
}

func TestRunAtExactTriggerCompacts(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("x", 38)},
		{Role: provider.RoleUser, Content: "y"},
	}
	w := contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{TriggerPercent: 40, TargetTokens: 20}}
	loop, f := newPlanningFixture(t, w, []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}, nil)
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	sumCalls, _ := f.summary.stats()
	if sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: an estimate at the trigger compacts", sumCalls)
	}
	_, reqs := completerRequests(f.completer)
	if strings.Contains(reqs[0].Messages[len(reqs[0].Messages)-1].Content, strings.Repeat("x", 38)) {
		t.Fatal("dropped message still in the sent history at the exact trigger")
	}
}

func TestRunOverTriggerCompactsThroughSummarizer(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	loop, f := newPlanningFixture(t, w, []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}, nil)
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	_, reqs := completerRequests(f.completer)
	sent := reqs[0].Messages
	if sent[0].Role != provider.RoleSystem {
		t.Fatalf("system message not first: %+v", sent[0])
	}
	if sent[1].Name != contextsummary.SummaryMessageName || sent[1].Role != provider.RoleUser {
		t.Fatalf("summary not injected after the system message: %+v", sent[1])
	}
	if strings.Contains(sent[3].Content, strings.Repeat("o", 100)) {
		t.Fatalf("dropped message still in the sent history: %+v", sent)
	}
	sumCalls, sumReqs := f.summary.stats()
	if sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1", sumCalls)
	}
	if len(sumReqs[0].Messages) == 0 || !strings.Contains(sumReqs[0].Messages[len(sumReqs[0].Messages)-1].Content, strings.Repeat("o", 100)) {
		t.Fatalf("summarizer input missing the dropped message")
	}
}

func TestRunAtTriggerNothingDroppableSkipsSummarizer(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: "u"},
	}
	w := contextplan.Window{MaxTokens: 100, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 5}}
	loop, f := newPlanningFixture(t, w, []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}, nil)
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	sumCalls, _ := f.summary.stats()
	if sumCalls != 0 {
		t.Fatalf("summarizer calls = %d, want 0 for an all-mandatory history", sumCalls)
	}
	_, reqs := completerRequests(f.completer)
	if summaryNamed(reqs[0].Messages) != 0 {
		t.Fatal("summary injected although nothing was droppable")
	}
	if len(reqs[0].Messages) != len(msgs) {
		t.Fatalf("sent history = %d messages, want %d", len(reqs[0].Messages), len(msgs))
	}
}

func TestRunSummarizerFailureFailsBeforeRequest(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 100)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 400, Compaction: contextplan.Compaction{TriggerPercent: 1, TargetTokens: 20}}
	loop, f := newPlanningFixture(t, w, nil, errors.New("summary boom"))
	res, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrCompactionFailed) {
		t.Fatalf("Run() error = %v, want errors.Is ErrCompactionFailed", err)
	}
	if !errors.Is(err, contextsummary.ErrCallFailed) {
		t.Fatalf("Run() error = %v, want the contextsummary sentinel wrapped", err)
	}
	if got := f.completer.callCount(); got != 0 {
		t.Fatalf("completer calls = %d, want 0: nothing is sent on a failed compaction", got)
	}
	if len(res.History) != len(msgs) {
		t.Fatalf("Result.History = %d messages, want the pre-compaction %d", len(res.History), len(msgs))
	}
}

// TestRunBudgetChecksAfterWindowCompaction proves checkBudget runs on
// the post-compaction history, after planHistory: a Budget too tight
// for the caller's four raw starting messages still succeeds, because
// the configured Window compacts that history down to a size the
// Budget allows before checkBudget ever inspects it.
func TestRunBudgetChecksAfterWindowCompaction(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 5000)},
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
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
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
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil: compaction must bring the history under Budget", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := completer.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1", got)
	}
	sumCalls, _ := sum.stats()
	if sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: compaction must run before the budget check", sumCalls)
	}
	_, reqs := completerRequests(completer)
	for _, m := range reqs[0].Messages {
		if strings.Contains(m.Content, strings.Repeat("o", 5000)) {
			t.Fatalf("dropped message still in the sent history: %+v", m)
		}
	}
}

// TestRunBudgetTripsAfterCompactionStillOverBudget proves compaction is
// not a bypass: when the post-compaction history still exceeds Budget,
// checkBudget still trips with ErrOverBudget, after the summarizer ran
// but before any Completer.Chat call for the turn.
func TestRunBudgetTripsAfterCompactionStillOverBudget(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 5000)},
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
	completer := &scriptedCompleter{}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 3,
		Budget:        &contextbudget.Limits{MaxBytes: 50},
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, agentloop.ErrOverBudget) {
		t.Fatalf("Run() error = %v, want errors.Is ErrOverBudget", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("completer calls = %d, want 0: the budget check must trip before any Completer call", completer.callCount())
	}
	sumCalls, _ := sum.stats()
	if sumCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1: compaction must run before the budget check trips", sumCalls)
	}
	if !isZeroResult(res) {
		t.Fatalf("Result = %+v, want the zero Result: no iteration completed before the budget check tripped", res)
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
}
