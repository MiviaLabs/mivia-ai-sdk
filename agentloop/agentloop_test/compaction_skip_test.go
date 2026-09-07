package agentloop_test

// Summarizer-skip tests: a summarizer that returns
// plan.ErrSummarySkipped declines summary generation, and
// compactHistory reuses the prior summary or proceeds without one.
// Covers the planning path and the recovery path, each with and
// without a prior summary held aside, plus the interface nil checks
// in Options.Validate.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// skipSummarizer implements agentloop.Summarizer directly, with no
// concrete adapter, and returns plan.ErrSummarySkipped
// from every Summarize call.
type skipSummarizer struct {
	mu    sync.Mutex
	calls int
}

func (s *skipSummarizer) Summarize(ctx context.Context, msgs []provider.Message) (plan.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return plan.Summary{}, plan.ErrSummarySkipped
}

func (s *skipSummarizer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// newSkipFixture wires one Loop whose Summarizer is a skipSummarizer.
func newSkipFixture(t *testing.T, w plan.Window, errs []error, responses []provider.Response) (*agentloop.Loop, *scriptedCompleter, *skipSummarizer) {
	t.Helper()
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`)})
	sc := &scriptedCompleter{errs: errs, responses: responses}
	skip := &skipSummarizer{}
	loop, err := agentloop.New(agentloop.Options{
		Completer: sc,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: 4},

		Compaction: agentloop.Compaction{
			Window:     &w,
			Summarizer: skip,
			Calibrated: plan.Calibrate(scaleEstimator{div: 1}, 1.0),
		}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop, sc, skip
}

// priorSummaryMessage builds the summary-named message a prior
// compaction left in history.
func priorSummaryMessage(content string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Name: plan.SummaryMessageName, Content: content}
}

// TestCompactionSkipWithPriorReinjectsPrior proves the planning path
// re-injects the held-aside prior summary unchanged after the system
// message when the summarizer skips, with no summarizer error, and
// the run proceeds.
func TestCompactionSkipWithPriorReinjectsPrior(t *testing.T) {
	big := strings.Repeat("a", 200)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		priorSummaryMessage("prior summary"),
		{Role: provider.RoleUser, Content: big},
		{Role: provider.RoleUser, Content: "final"},
	}
	w := plan.Window{MaxTokens: 400, Compaction: plan.Compaction{TriggerPercent: 40, TargetTokens: 20}}
	loop, sc, skip := newSkipFixture(t, w, nil, []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	})
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil: a skip must not surface a summarizer error", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := skip.callCount(); got != 1 {
		t.Fatalf("summarizer calls = %d, want 1: the skip route must have run", got)
	}
	_, reqs := completerRequests(sc)
	sent := reqs[0].Messages
	if len(sent) != 3 {
		t.Fatalf("sent history = %d messages, want 3: [system, re-injected prior, kept final]", len(sent))
	}
	if sent[0].Role != provider.RoleSystem {
		t.Fatalf("system message not first: %+v", sent[0])
	}
	if sent[1].Name != plan.SummaryMessageName || sent[1].Content != "prior summary" {
		t.Fatalf("prior not re-injected unchanged after the system message: %+v", sent[1])
	}
	for _, m := range sent {
		if strings.Contains(m.Content, big) {
			t.Fatalf("dropped message still in the sent history: %+v", m)
		}
	}
	if summaryNamed(sent) != 1 {
		t.Fatalf("summary messages = %d, want 1", summaryNamed(sent))
	}
}

// TestCompactionSkipWithoutPriorDropsQuietly proves the planning path
// with a skip and no prior summary injects nothing: the dropped
// messages stay dropped and the run proceeds with the kept history.
func TestCompactionSkipWithoutPriorDropsQuietly(t *testing.T) {
	big := strings.Repeat("a", 200)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: big},
		{Role: provider.RoleUser, Content: "final"},
	}
	w := plan.Window{MaxTokens: 400, Compaction: plan.Compaction{TriggerPercent: 40, TargetTokens: 20}}
	loop, sc, skip := newSkipFixture(t, w, nil, []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	})
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil: a skip with no prior must proceed quietly", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := skip.callCount(); got != 1 {
		t.Fatalf("summarizer calls = %d, want 1", got)
	}
	_, reqs := completerRequests(sc)
	sent := reqs[0].Messages
	if len(sent) != len(msgs)-1 {
		t.Fatalf("sent history = %d messages, want %d: the dropped message must stay dropped", len(sent), len(msgs)-1)
	}
	if got := summaryNamed(sent); got != 0 {
		t.Fatalf("summary messages = %d, want 0: no summary message may be injected", got)
	}
	for _, m := range sent {
		if strings.Contains(m.Content, big) {
			t.Fatalf("dropped message still in the sent history: %+v", m)
		}
	}
}

// TestRecoverySkipWithPriorRetriesWithNotice proves the recovery path
// with a skip and a prior proceeds: the retry fires, and the notice
// sits directly after the re-injected prior summary.
func TestRecoverySkipWithPriorRetriesWithNotice(t *testing.T) {
	big := strings.Repeat("o", 2000)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		priorSummaryMessage("prior summary"),
		{Role: provider.RoleUser, Content: big},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := plan.Window{MaxTokens: 4000, Compaction: plan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	loop, sc, skip := newSkipFixture(t, w, []error{provider.ErrPromptTooLong}, []provider.Response{
		provider.Response{},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	})
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil: a skip with a prior must let the retry proceed", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := sc.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want 2: one rejection, one retry", got)
	}
	if got := skip.callCount(); got != 1 {
		t.Fatalf("summarizer calls = %d, want 1", got)
	}
	_, reqs := completerRequests(sc)
	retried := reqs[1].Messages
	if retried[0].Role != provider.RoleSystem {
		t.Fatalf("system message not first: %+v", retried[0])
	}
	if retried[1].Name != plan.SummaryMessageName || retried[1].Content != "prior summary" {
		t.Fatalf("prior not re-injected unchanged after the system message: %+v", retried[1])
	}
	if retried[2].Content != agentloop.CompactionNotice {
		t.Fatalf("notice does not sit directly after the re-injected prior: %+v", retried[2])
	}
	if got := summaryNamed(retried); got != 1 {
		t.Fatalf("retried request summary count = %d, want 1", got)
	}
}

// TestRecoverySkipWithoutPriorReturnsOriginalErr proves the recovery
// path with a skip and no prior is unrecoverable: Run returns the
// original ErrPromptTooLong, with no retry and no notice.
func TestRecoverySkipWithoutPriorReturnsOriginalErr(t *testing.T) {
	big := strings.Repeat("o", 2000)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: big},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := plan.Window{MaxTokens: 4000, Compaction: plan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	rejection := fmt.Errorf("vendor: %w", provider.ErrPromptTooLong)
	loop, sc, skip := newSkipFixture(t, w, []error{rejection}, []provider.Response{provider.Response{}})
	res, err := loop.Run(context.Background(), msgs)
	if !errors.Is(err, provider.ErrPromptTooLong) {
		t.Fatalf("Run() error = %v, want errors.Is ErrPromptTooLong", err)
	}
	if err != rejection {
		t.Fatalf("Run() error = %v, want the original rejection unchanged", err)
	}
	if got := sc.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1: no retry after a skip with no prior", got)
	}
	if got := skip.callCount(); got != 1 {
		t.Fatalf("summarizer calls = %d, want 1", got)
	}
	if len(res.History) != len(msgs) || res.Iterations != 0 {
		t.Fatalf("Result = {History: %d msgs, Iterations: %d}, want {%d, 0}: the fromRecovery route carries the pre-failure Result", len(res.History), res.Iterations, len(msgs))
	}
}

// TestValidateSummarizerInterfaceNilChecks proves the interface nil
// check in Options.Validate: an untyped nil Summarizer with Window set
// fails ErrSummarizerRequired, and a typed nil
// (*plan.Summarizer)(nil) passes, documenting the typed-nil
// warning on the Summarizer interface.
func TestValidateSummarizerInterfaceNilChecks(t *testing.T) {
	w := plan.Window{MaxTokens: 100, Compaction: plan.Compaction{TriggerPercent: 50}}
	opts := agentloop.Options{
		Completer: &scriptedCompleter{},
		Tools:     tools.New(),

		Compaction: agentloop.Compaction{
			Window:     &w,
			Calibrated: plan.Calibrate(scaleEstimator{div: 1}, 1.0),
		}}
	if err := opts.Validate(); !errors.Is(err, agentloop.ErrSummarizerRequired) {
		t.Fatalf("Validate() = %v, want ErrSummarizerRequired for an untyped nil Summarizer", err)
	}
	var typedNil *plan.Summarizer
	opts.Compaction.Summarizer = typedNil
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil: a typed nil passes the nil check, which is the documented warning", err)
	}
}
