package agentloop_test

// Recovery-boundary steer cases live in this sibling file, split by
// concern from steer_test.go, which pushed past the 500-line
// structure gate once this file's cases were added.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestSteerTriggerMidPromptTooLongRecovery proves a Trigger fired
// while the prompt-too-long recovery retry is in flight has no effect
// on that retry: recoverPromptTooLong calls Chat on the plain ctx, not
// a steer-derived context. The retry still completes and its response
// reaches History; the steer takes effect only at the following
// iteration boundary.
func TestSteerTriggerMidPromptTooLongRecovery(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`), result: "ok"})
	recovered := toolCallResponse(provider.ToolCall{ID: "c1", Name: "search", Arguments: []byte("{}")})
	c := &recoveryBlockCompleter{recovered: recovered, entered: make(chan struct{}), release: make(chan struct{})}
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     c,
		Tools:         reg,
		MaxIterations: 5,
		Window:        &w,
		Summarizer:    sum,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	steer := agentloop.NewSteer()

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-c.entered
	steer.Trigger()
	close(c.release)

	var res agentloop.Result
	var runErr error
	select {
	case res = <-resCh:
		runErr = <-errCh
	case <-time.After(5 * time.Second):
		t.Fatalf("RunSteerable did not return within 5s: a triggered Steer failed to carry over to the next iteration boundary")
	}

	if runErr != nil {
		t.Fatalf("RunSteerable() error = %v, want nil", runErr)
	}
	if res.Stop != agentloop.StopSteered {
		t.Fatalf("Stop = %q, want StopSteered", res.Stop)
	}
	foundAssistantToolCall := false
	foundToolResult := false
	for _, m := range res.History {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "c1" {
			foundAssistantToolCall = true
		}
		if m.Role == provider.RoleTool && m.ToolCallID == "c1" {
			foundToolResult = true
		}
	}
	if !foundAssistantToolCall {
		t.Fatalf("History missing the retry's assistant tool-call message: %+v", res.History)
	}
	if !foundToolResult {
		t.Fatalf("History missing the retry's completed tool call's RoleTool message: %+v", res.History)
	}
}

// TestSteerTriggeredDuringFailingRecoveryRetry proves isSteerStop's
// fromRecovery guard is load-bearing: a triggered Steer must not
// reclassify a failing prompt-too-long recovery retry as StopSteered,
// even when that retry's own error wraps context.Canceled for a
// reason unrelated to the Steer (a vendor-side reset). Without the
// fromRecovery short-circuit, the four remaining isSteerStop
// conditions alone would misclassify this failure as a graceful stop.
func TestSteerTriggeredDuringFailingRecoveryRetry(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: strings.Repeat("o", 2000)},
		{Role: provider.RoleAssistant, Content: "a"},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := contextplan.Window{MaxTokens: 4000, Compaction: contextplan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	reg := tools.New()
	c := &recoveryFailCompleter{entered: make(chan struct{}), release: make(chan struct{})}
	sum, err := contextsummary.NewSummarizer(&summaryScript{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer:     c,
		Tools:         reg,
		MaxIterations: 5,
		Window:        &w,
		Summarizer:    sum,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	steer := agentloop.NewSteer()

	resCh := make(chan agentloop.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := loop.RunSteerable(context.Background(), msgs, steer)
		resCh <- res
		errCh <- err
	}()

	<-c.entered
	steer.Trigger()
	close(c.release)

	var res agentloop.Result
	var runErr error
	select {
	case res = <-resCh:
		runErr = <-errCh
	case <-time.After(5 * time.Second):
		t.Fatalf("RunSteerable did not return within 5s")
	}

	if runErr == nil {
		t.Fatalf("RunSteerable() error = nil, want a wrapped errRecoveryVendorReset error")
	}
	if !errors.Is(runErr, errRecoveryVendorReset) {
		t.Fatalf("RunSteerable() error = %v, want it to wrap errRecoveryVendorReset", runErr)
	}
	if res.Stop == agentloop.StopSteered {
		t.Fatalf("Stop = StopSteered, want a hard-fail result: a triggered Steer must not mask a failing recovery retry")
	}
}
