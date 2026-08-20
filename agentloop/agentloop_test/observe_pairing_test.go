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

// referenceObserve computes the same formula contextplan.Calibrated.Observe
// applies: sample := actual/estimated, then next := factor *
// ((1-alpha) + alpha*sample), clamped to [MinCorrectionFactor,
// MaxCorrectionFactor]. Tests in this file check agentloop's call
// sites against this reference, not against a hand-picked literal.
func referenceObserve(factor, alpha float64, estimated, actual int) float64 {
	if estimated <= 0 || actual <= 0 {
		return factor
	}
	sample := float64(actual) / float64(estimated)
	next := factor * ((1 - alpha) + alpha*sample)
	if next < contextplan.MinCorrectionFactor {
		next = contextplan.MinCorrectionFactor
	}
	if next > contextplan.MaxCorrectionFactor {
		next = contextplan.MaxCorrectionFactor
	}
	return next
}

// TestRunRecoveryObservePairsWithRecoveryEstimate proves the
// recovered iteration's Observe call scores the recovery path's own
// estimate, not the pre-recovery request's estimate: runChat's second
// estimateTokens call, over retryReq, must be the one Observe pairs
// with. The pre-recovery and retry requests differ sharply in size
// after compaction, so a mispairing produces a measurably different
// factor from the correct one.
func TestRunRecoveryObservePairsWithRecoveryEstimate(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	const actual = 500
	final := provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: "done"},
		Usage:   provider.Usage{TotalTokens: actual},
	}
	sc := &scriptedCompleter{responses: []provider.Response{{}, final}, errs: []error{provider.ErrPromptTooLong}}
	summarizer, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	cal := contextplan.Calibrate(scaleEstimator{div: 1}, 1.0)
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`)})
	loop, err := agentloop.New(agentloop.Options{
		Completer:     sc,
		Tools:         reg,
		MaxIterations: 4,
		Window:        &w,
		Summarizer:    summarizer,
		Calibrated:    cal,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	calls, reqs := completerRequests(sc)
	if calls != 2 {
		t.Fatalf("completer calls = %d, want 2", calls)
	}
	preEstimate := contentBytes(reqs[0].Messages)
	retryEstimate := contentBytes(reqs[1].Messages)
	if preEstimate == retryEstimate {
		t.Fatalf("pre-recovery (%d) and retry (%d) estimates coincide: the test cannot distinguish pairing", preEstimate, retryEstimate)
	}

	// Exactly one Observe call ran, on a fresh Calibrated at factor
	// 1.0, so this is the reference formula's first application.
	wantFactor := referenceObserve(1.0, 1.0, retryEstimate, actual)
	wrongFactor := referenceObserve(1.0, 1.0, preEstimate, actual)

	const probeBytes = 1000
	probe := provider.Request{Messages: []provider.Message{{Content: strings.Repeat("p", probeBytes)}}}
	got, err := cal.EstimateTokens(probe)
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	wantTokens := int(probeBytes * wantFactor)
	if diff := got - wantTokens; diff < -2 || diff > 2 {
		t.Fatalf("probe estimate = %d, want %d: Observe must pair with the recovery estimate (%d bytes), not the pre-recovery one (%d bytes)",
			got, wantTokens, retryEstimate, preEstimate)
	}
	if wrongTokens := int(probeBytes * wrongFactor); wantTokens == wrongTokens {
		t.Fatalf("recovery and pre-recovery pairings produce the same probe result (%d): the test cannot distinguish them", wantTokens)
	}
}

// TestRunCalibratedWithoutWindowEstimates proves runChat calls
// EstimateTokens even when l.window is nil, as long as l.calibrated is
// set: this configuration made no EstimateTokens call before this
// fix, so the correction factor never moved off its 1.0 starting
// value.
func TestRunCalibratedWithoutWindowEstimates(t *testing.T) {
	cal := contextplan.Calibrate(scaleEstimator{div: 1}, 1.0)
	reg := tools.New()
	final := provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: "done"},
		Usage:   provider.Usage{TotalTokens: 50},
	}
	sc := &scriptedCompleter{responses: []provider.Response{final}}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     sc,
		Tools:         reg,
		MaxIterations: 1,
		Calibrated:    cal,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 100)}}
	if _, err := loop.Run(context.Background(), msgs); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	probe := provider.Request{Messages: []provider.Message{{Content: strings.Repeat("y", 100)}}}
	got, err := cal.EstimateTokens(probe)
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got == 100 {
		t.Fatalf("probe estimate = %d, want != 100: runChat never called EstimateTokens for a Calibrated-without-Window configuration", got)
	}
}

// TestRunEstimatorFailureNonFatal proves an EstimateTokens error never
// fails the run: Chat still runs, estimatedTokens stays zero, and the
// later Observe call sees a non-positive estimated value and no-ops.
func TestRunEstimatorFailureNonFatal(t *testing.T) {
	cal := contextplan.Calibrate(errEstimator{}, 0.5)
	reg := tools.New()
	final := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}
	sc := &scriptedCompleter{responses: []provider.Response{final}}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     sc,
		Tools:         reg,
		MaxIterations: 1,
		Calibrated:    cal,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil: an estimate failure must not fail the run", err)
	}
	if got := sc.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1", got)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
}

// checkpointGate coordinates one goroutine's Chat call in
// TestRunConcurrentSharedLoopWithPlanning. Chat closes reached on
// entry, proving that goroutine's own EstimateTokens call has already
// returned, then blocks until the test closes release.
type checkpointGate struct {
	reached chan struct{}
	release chan struct{}
	resp    provider.Response
}

// checkpointCompleter gates each goroutine's Chat call behind its own
// checkpointGate, keyed by the calling goroutine's distinct message
// content. See TestRunConcurrentSharedLoopWithPlanning.
type checkpointCompleter struct {
	mu    sync.Mutex
	gates map[string]*checkpointGate
}

func (c *checkpointCompleter) Name() string { return "checkpoint" }

func (c *checkpointCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	if len(req.Messages) == 0 {
		return provider.Response{}, fmt.Errorf("checkpointCompleter: request carries no messages")
	}
	c.mu.Lock()
	gate, ok := c.gates[req.Messages[0].Content]
	c.mu.Unlock()
	if !ok {
		return provider.Response{}, fmt.Errorf("checkpointCompleter: no gate for content of length %d", len(req.Messages[0].Content))
	}
	close(gate.reached)
	<-gate.release
	return gate.resp, nil
}

func (c *checkpointCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("checkpointCompleter: ChatStream not supported")
}

// TestRunConcurrentSharedLoopWithPlanning proves Observe pairs
// correctly under real concurrency on one shared *Calibrated. Four
// goroutines each capture their own EstimateTokens result while
// factor is still exactly 1.0 (proven by the reached barrier below),
// then the test releases their Chat calls one at a time, fixing the
// Observe call order to exactly 1, 2, 3, 4. The Loop is built with
// Window: nil, so planHistory and Compact never run: no Budget() or
// retention-overflow risk exists for the message sizes this test
// picks.
func TestRunConcurrentSharedLoopWithPlanning(t *testing.T) {
	cal := contextplan.Calibrate(scaleEstimator{div: 1}, 1.0)
	reg := tools.New()

	sizes := [4]int{100, 200, 300, 400}
	runes := [4]rune{'a', 'b', 'c', 'd'}
	actuals := [4]int{110, 180, 360, 320}

	msgs := make([][]provider.Message, 4)
	gates := make([]*checkpointGate, 4)
	completer := &checkpointCompleter{gates: map[string]*checkpointGate{}}
	for g := 0; g < 4; g++ {
		content := strings.Repeat(string(runes[g]), sizes[g])
		msgs[g] = []provider.Message{{Role: provider.RoleUser, Content: content}}
		gate := &checkpointGate{
			reached: make(chan struct{}),
			release: make(chan struct{}),
			resp: provider.Response{
				Message: provider.Message{Role: provider.RoleAssistant, Content: "done"},
				Usage:   provider.Usage{TotalTokens: actuals[g]},
			},
		}
		completer.gates[content] = gate
		gates[g] = gate
	}

	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		MaxIterations: 1,
		Calibrated:    cal,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make([]chan struct{}, 4)
	for g := range done {
		done[g] = make(chan struct{})
	}
	for g := 0; g < 4; g++ {
		go func(idx int) {
			if _, err := loop.Run(context.Background(), msgs[idx]); err != nil {
				t.Errorf("Run: %v", err)
			}
			close(done[idx])
		}(g)
	}

	// Wait for every goroutine's own EstimateTokens capture to
	// complete before releasing any Chat call: factor stays exactly
	// 1.0 for all four captures, proven by this barrier.
	for g := 0; g < 4; g++ {
		<-gates[g].reached
	}

	// Release one goroutine at a time, waiting for each Run to
	// return before releasing the next. This fixes the Observe order
	// to exactly 1, 2, 3, 4.
	for g := 0; g < 4; g++ {
		close(gates[g].release)
		<-done[g]
	}

	probe := provider.Request{Messages: []provider.Message{{Content: strings.Repeat("z", 10000)}}}
	got, err := cal.EstimateTokens(probe)
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if got < 9502 || got > 9506 {
		t.Fatalf("EstimateTokens = %d, want 9504 within a tolerance of 2", got)
	}
}
