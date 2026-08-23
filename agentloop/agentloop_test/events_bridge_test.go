package agentloop_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestEventsBridgeThinkingCacheCalibration proves the new
// EventThinkingStart/Delta/End, EventCacheUsage, and
// EventCalibrationDelta events fire from runChat and afterChat on a
// one-iteration run whose Response carries ReasoningContent and a
// Reported CacheUsage, when a Calibrated estimator is wired so
// Observe has both estimated and actual tokens. The events must
// carry the relevant fields in Data so a subscriber can read them
// without re-issuing a Completer call.
func TestEventsBridgeThinkingCacheCalibration(t *testing.T) {
	bus := events.New()
	rec := &eventRecorder{}
	subscribeEvents(t, bus, rec.handle,
		agentloop.EventThinkingStart,
		agentloop.EventThinkingDelta,
		agentloop.EventThinkingEnd,
		agentloop.EventCacheUsage,
		agentloop.EventCalibrationDelta,
		agentloop.EventAssistant,
	)
	est := &fixedEstimator{n: 50}
	cal := contextplan.Calibrate(est, 0.5)
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{
			Role:             provider.RoleAssistant,
			Content:          "done",
			ReasoningContent: "thinking about it",
		}, Usage: provider.Usage{TotalTokens: 200},
			CacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleImplicit,
				InputTokens: 100, CachedInputTokens: 60, CacheWriteTokens: 10}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Bus: bus, HeartbeatInterval: time.Hour,
		Calibrated: cal,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	got := rec.events()
	if len(got) == 0 {
		t.Fatalf("no events captured")
	}
	assertEventCounts(t, got)
	assertEventPayloads(t, got)
}

// assertEventCounts pins one occurrence of each of the six new names in
// a single-iteration run. The Run path also emits no ToolParallel
// because the response carries zero tool calls.
func assertEventCounts(t *testing.T, got []events.Event) {
	t.Helper()
	wantCounts := map[events.Name]int{
		agentloop.EventThinkingStart:    1,
		agentloop.EventThinkingDelta:    1,
		agentloop.EventThinkingEnd:      1,
		agentloop.EventCacheUsage:       1,
		agentloop.EventCalibrationDelta: 1,
		agentloop.EventAssistant:        1,
	}
	seen := map[events.Name]int{}
	for _, e := range got {
		seen[e.Name]++
	}
	for name, want := range wantCounts {
		if seen[name] != want {
			t.Fatalf("event %q count = %d, want %d (all seen: %v)", name, seen[name], want, seen)
		}
	}
}

// assertEventPayloads pins payload content so a build that fires the
// right name with a blank Data (or with the wrong fields) still fails.
func assertEventPayloads(t *testing.T, got []events.Event) {
	t.Helper()
	byName := map[events.Name]string{}
	for _, e := range got {
		if _, dup := byName[e.Name]; !dup {
			byName[e.Name] = e.Data
		}
	}
	if !strings.Contains(byName[agentloop.EventThinkingDelta], "thinking about it") {
		t.Fatalf("EventThinkingDelta Data = %q, want reasoning text", byName[agentloop.EventThinkingDelta])
	}
	if !strings.Contains(byName[agentloop.EventCacheUsage], "implicit") ||
		!strings.Contains(byName[agentloop.EventCacheUsage], "60") {
		t.Fatalf("EventCacheUsage Data = %q, want style and cached tokens", byName[agentloop.EventCacheUsage])
	}
	if !strings.Contains(byName[agentloop.EventCalibrationDelta], "200") ||
		!strings.Contains(byName[agentloop.EventCalibrationDelta], "50") {
		t.Fatalf("EventCalibrationDelta Data = %q, want both actual and estimated tokens", byName[agentloop.EventCalibrationDelta])
	}
	if !strings.Contains(byName[agentloop.EventAssistant], "done") {
		t.Fatalf("EventAssistant Data = %q, want assistant content", byName[agentloop.EventAssistant])
	}
}

// TestEventsBridgeToolParallelFiresOncePerTurn proves EventToolParallel
// fires exactly once on a turn with two parallel tool calls, alongside
// one EventToolCallStart per call. The Data must report the call count
// so a renderer can highlight "parallel dispatch" without re-counting.
func TestEventsBridgeToolParallelFiresOncePerTurn(t *testing.T) {
	bus := events.New()
	rec := &eventRecorder{}
	subscribeEvents(t, bus, rec.handle,
		agentloop.EventToolParallel,
		agentloop.EventToolCallStart,
	)
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "ok"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{Index: 0, ID: "call-1", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{Index: 1, ID: "call-2", Name: "echo", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "done")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		Bus: bus, HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	got := rec.events()
	parallel := 0
	startCount := 0
	var parallelData string
	for _, e := range got {
		switch e.Name {
		case agentloop.EventToolParallel:
			parallel++
			parallelData = e.Data
		case agentloop.EventToolCallStart:
			startCount++
		}
	}
	if parallel != 1 {
		t.Fatalf("EventToolParallel count = %d, want 1", parallel)
	}
	if startCount != 2 {
		t.Fatalf("EventToolCallStart count = %d, want 2", startCount)
	}
	if !strings.Contains(parallelData, "2") {
		t.Fatalf("EventToolParallel Data = %q, want call count 2", parallelData)
	}
}

// TestEventsBridgeToolParallelAbsentOnSingleCall proves the
// parallel-only emission rule: a single tool call in a turn does not
// fire EventToolParallel. The Data check stays loose - it asserts no
// parallel event at all - so a build that just omits the emission
// passes, and a build that wrongly fires it on every call fails.
func TestEventsBridgeToolParallelAbsentOnSingleCall(t *testing.T) {
	bus := events.New()
	rec := &eventRecorder{}
	subscribeEvents(t, bus, rec.handle, agentloop.EventToolParallel)
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "ok"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "done")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		Bus: bus, HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := rec.names(); len(got) != 0 {
		t.Fatalf("events = %v, want none", got)
	}
}

// TestEventsBridgeAuditCompletionCarriesThinkingAndCache proves the
// extended AuditRecord fields ThinkingContent and CacheUsage carry
// the response's reasoning text and prompt-cache accounting on a
// completion audit. This pin closes the gap between the new bus
// events and the audit pipeline so a render layer can fall back to
// the audit log if it missed the live stream.
func TestEventsBridgeAuditCompletionCarriesThinkingAndCache(t *testing.T) {
	bus := events.New()
	auditor := &recordingAuditor{}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{
			Role:             provider.RoleAssistant,
			Content:          "ok",
			ReasoningContent: "step by step",
		},
			CacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleExplicit,
				InputTokens: 30, CachedInputTokens: 10, CacheWriteTokens: 5}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Bus: bus, HeartbeatInterval: time.Hour, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	records := auditor.snapshot()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.Kind != agentloop.AuditKindCompletion {
		t.Fatalf("rec.Kind = %v, want AuditKindCompletion", rec.Kind)
	}
	if rec.ThinkingContent != "step by step" {
		t.Fatalf("rec.ThinkingContent = %q, want %q", rec.ThinkingContent, "step by step")
	}
	if !rec.CacheUsage.Reported {
		t.Fatalf("rec.CacheUsage.Reported = false, want true")
	}
	if rec.CacheUsage.Style != provider.CacheStyleExplicit {
		t.Fatalf("rec.CacheUsage.Style = %v, want CacheStyleExplicit", rec.CacheUsage.Style)
	}
	if rec.CacheUsage.CachedInputTokens != 10 {
		t.Fatalf("rec.CacheUsage.CachedInputTokens = %d, want 10", rec.CacheUsage.CachedInputTokens)
	}
}

// fixedEstimator returns n from EstimateTokens; contextplan.Calibrate
// multiplies it by the correction factor, so test cases pick n so the
// scaled estimate stays distinct from the actual Usage.TotalTokens the
// test script supplies.
type fixedEstimator struct{ n int }

func (f *fixedEstimator) EstimateTokens(req provider.Request) (int, error) { return f.n, nil }
