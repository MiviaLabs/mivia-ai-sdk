package agentloop_test

// Trigger-parity tests: planHistory and plan.Compact must measure the
// same history. A prior summary sits in the gap between the two
// measurements, so these fixtures put the estimate inside that gap.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// droppedMarker leads the droppable unit's content. buildExcerpts caps
// an excerpt by cutting its tail, so a leading marker survives.
const droppedMarker = "DROPPEDUNIT"

// parityHistory returns the shared fixture: a one-byte system message,
// a twenty-byte prior summary, a four-hundred byte droppable unit, and
// a five-byte final user message. The whole history estimates 426 and
// the history without the summary estimates 406.
func parityHistory() []provider.Message {
	big := droppedMarker + strings.Repeat("a", 400-len(droppedMarker))
	return []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		priorSummaryMessage("prior summary bytes!"),
		{Role: provider.RoleUser, Content: big},
		{Role: provider.RoleUser, Content: "final"},
	}
}

// parityWindow returns the window whose trigger equals its budget, so
// the default trigger percent is under test.
func parityWindow() plan.Window {
	return plan.Window{
		MaxTokens:  420,
		Compaction: plan.Compaction{TriggerPercent: 100, TargetTokens: 100},
	}
}

// TestCompactionPriorSummaryKeepsTriggerParity proves the summarizer
// never runs with the prior summary as its only input. Before the fix
// plan.Compact saw 406, passed through, and summarizeDropped ran on the
// prior alone. The assertion reads the recorded input, not a call
// count: after the fix one legitimate call exists, so a count cannot
// separate the two outcomes.
func TestCompactionPriorSummaryKeepsTriggerParity(t *testing.T) {
	msgs := parityHistory()
	loop, f := newPlanningFixture(t, parityWindow(), []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}, nil)
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	calls, reqs := f.summary.stats()
	if calls == 0 {
		t.Fatalf("summarizer never ran; the fixture must reach compaction")
	}
	for i, req := range reqs {
		if !strings.Contains(excerptText(req), droppedMarker) {
			t.Fatalf("summarizer call %d ran without the dropped unit: "+
				"its only input was the prior summary", i)
		}
	}
}

// TestCompactionPriorSummaryAtDefaultTriggerSucceeds proves the run
// completes when the trigger equals the budget. Before the fix the
// rebuilt history kept every message and gained a fresh summary, so it
// reached 578 against a budget of 420 and checkCompactedBudget failed.
func TestCompactionPriorSummaryAtDefaultTriggerSucceeds(t *testing.T) {
	msgs := parityHistory()
	loop, f := newPlanningFixture(t, parityWindow(), []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}, nil)
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil: the droppable unit must fit the budget", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	_, reqs := completerRequests(f.completer)
	for _, m := range reqs[0].Messages {
		if strings.Contains(m.Content, droppedMarker) {
			t.Fatalf("the droppable unit survived compaction: %q", m.Content)
		}
	}
}

// newPlanningFixtureWithResult is newPlanningFixture with a tool whose
// result carries content. A second compaction needs a droppable unit,
// and an empty tool result gives it nothing to drop.
func newPlanningFixtureWithResult(t *testing.T, w plan.Window, responses []provider.Response, result string) (*agentloop.Loop, *planningFixture) {
	t.Helper()
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`), result: result})
	sc := &scriptedCompleter{responses: responses}
	sum := &summaryScript{}
	summarizer, err := plan.NewSummarizer(sum)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: sc,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: 4},
		Compaction: agentloop.Compaction{
			Window:     &w,
			Summarizer: summarizer,
			Calibrated: plan.Calibrate(scaleEstimator{div: 1}, 1.0),
		}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop, &planningFixture{completer: sc, summary: sum, window: w}
}

// excerptText joins one summarizer request's message contents.
func excerptText(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
	}
	return b.String()
}

// recordingCompleter writes one tagged chunk to the request's
// StreamingWriter, fails the first call with ErrPromptTooLong, and
// answers later calls. It records each call's writer.
type recordingCompleter struct {
	mu       sync.Mutex
	calls    int
	hadWrite []bool
}

func (c *recordingCompleter) Name() string { return "recording" }

func (c *recordingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.hadWrite = append(c.hadWrite, req.StreamingWriter != nil)
	c.mu.Unlock()
	if req.StreamingWriter != nil {
		_, _ = req.StreamingWriter.Write([]byte(streamTag(n)))
	}
	if n == 1 {
		return provider.Response{}, provider.ErrPromptTooLong
	}
	return provider.Response{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "recovered"},
		FinishReason: "stop",
	}, nil
}

func (c *recordingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("recordingCompleter: ChatStream not supported")
}

func (c *recordingCompleter) writers() []bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bool(nil), c.hadWrite...)
}

// streamTag names one call's streamed bytes.
func streamTag(n int) string {
	if n == 1 {
		return "first-call "
	}
	return "second-call "
}

// newMirrorFixture wires one Loop over recordingCompleter with the
// recovery window set, and sink as Extensions.StreamingWriter.
func newMirrorFixture(t *testing.T, sink *bytes.Buffer) (*agentloop.Loop, *recordingCompleter) {
	t.Helper()
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`)})
	rc := &recordingCompleter{}
	summarizer, err := plan.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	w := parityWindow()
	opts := agentloop.Options{
		Completer: rc,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: 4},
		Compaction: agentloop.Compaction{
			Window:     &w,
			Summarizer: summarizer,
			Calibrated: plan.Calibrate(scaleEstimator{div: 1}, 1.0),
		},
	}
	if sink != nil {
		opts.Extensions = &agentloop.Extensions{StreamingWriter: sink}
	}
	loop, err := agentloop.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop, rc
}

// TestRecoveryRetryMirrorsStreaming proves the prompt-too-long retry
// keeps the caller's streaming mirror. Before the fix the retry request
// carried no StreamingWriter, so the recovered turn streamed nothing.
func TestRecoveryRetryMirrorsStreaming(t *testing.T) {
	var sink bytes.Buffer
	loop, rc := newMirrorFixture(t, &sink)
	if _, err := loop.Run(context.Background(), parityHistory()); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if rc.calls < 2 {
		t.Fatalf("completer calls = %d, want the recovery retry to run", rc.calls)
	}
	if !strings.Contains(sink.String(), streamTag(2)) {
		t.Fatalf("sink = %q, want the recovered turn's bytes", sink.String())
	}
}

// TestRecoveryRetryNoSinkLeavesWriterNil proves the retry does not
// invent a writer for a caller who set no StreamingWriter. A
// multi-writer over nil values is still non-nil, which would put the
// retry into streaming mode unasked.
func TestRecoveryRetryNoSinkLeavesWriterNil(t *testing.T) {
	loop, rc := newMirrorFixture(t, nil)
	if _, err := loop.Run(context.Background(), parityHistory()); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	for i, had := range rc.writers() {
		if had {
			t.Fatalf("call %d carried a StreamingWriter with no sink set", i)
		}
	}
}
