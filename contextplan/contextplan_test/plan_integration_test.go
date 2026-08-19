package contextplan_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestPlanIntegrationFullSession seeds a MemStore with payloads across
// two retention classes and a reasoning-kind event, then asserts
// Plan's Request.Messages, Elisions, and EstimatedTokens together
// describe one consistent outcome.
func TestPlanIntegrationFullSession(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache, nil)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	oldCompliance := []byte(strings.Repeat("c", 500))
	oldSession := []byte(strings.Repeat("s", 200))
	reasoningData := []byte("chain of thought, never surfaced")
	recent := []byte(strings.Repeat("r", 40))

	refCompliance := putPayload(t, store, "sess-int", contextstate.RetentionCompliance, oldCompliance)
	refSession := putPayload(t, store, "sess-int", contextstate.RetentionSession, oldSession)
	refReasoning := putPayload(t, store, "sess-int", contextstate.RetentionSession, reasoningData)
	refRecent := putPayload(t, store, "sess-int", contextstate.RetentionSession, recent)

	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-int", 1, "message", string(provider.RoleUser), refCompliance, len(oldCompliance)),
		sourceEvent("sess-int", 2, "message", string(provider.RoleUser), refSession, len(oldSession)),
		sourceEvent("sess-int", 3, provider.ReasoningEventKind, string(provider.RoleAssistant), refReasoning, len(reasoningData)),
		sourceEvent("sess-int", 4, "message", string(provider.RoleUser), refRecent, len(recent)),
	}}

	w := contextplan.Window{MaxTokens: 300, Reserve: 10}
	result, err := planner.Plan(context.Background(), sess, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if result.EstimatedTokens > w.Budget() {
		t.Fatalf("EstimatedTokens = %d, over budget %d", result.EstimatedTokens, w.Budget())
	}

	byRef := map[string]contextplan.Elision{}
	for _, e := range result.Elisions {
		byRef[e.Ref.Ref] = e
	}

	reasoningElision, ok := byRef[refReasoning.Ref]
	if !ok || reasoningElision.Reason != contextplan.ElisionReasonReasoningRedacted {
		t.Fatalf("reasoning elision = %+v, want reasoning redacted", reasoningElision)
	}
	for _, m := range result.Request.Messages {
		if m.Content == string(reasoningData) {
			t.Fatal("reasoning content leaked into Request.Messages")
		}
	}

	sessionElision, sessionElided := byRef[refSession.Ref]
	if sessionElided && sessionElision.Kept != 0 {
		t.Fatalf("session-retention elision = %+v, want a full drop if elided", sessionElision)
	}

	if complianceElision, ok := byRef[refCompliance.Ref]; ok {
		if complianceElision.Reason == contextplan.ElisionReasonWindowOverflow && complianceElision.Kept != 0 {
			t.Fatalf("compliance elision = %+v, a window overflow must carry Kept == 0", complianceElision)
		}
	}

	recentPresent := false
	for _, m := range result.Request.Messages {
		if m.Content == string(recent) {
			recentPresent = true
		}
	}
	if !recentPresent {
		t.Fatal("the most recent message must survive: newest-first inclusion")
	}
}
