package contextplan_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestPlanRevokedMiddleEvent kills a mutation that propagates
// ErrPayloadRevoked as a Plan-level failure.
func TestPlanRevokedMiddleEvent(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	older := []byte("older content")
	revoked := []byte("revoked content")
	newer := []byte("newer content")
	refOlder := putPayload(t, store, "sess-a", contextstate.RetentionSession, older)
	refRevoked := putPayload(t, store, "sess-a", contextstate.RetentionSession, revoked)
	refNewer := putPayload(t, store, "sess-a", contextstate.RetentionSession, newer)
	if err := store.Revoke(refRevoked); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refOlder, len(older)),
		sourceEvent("sess-a", 2, "message", string(provider.RoleUser), refRevoked, len(revoked)),
		sourceEvent("sess-a", 3, "message", string(provider.RoleUser), refNewer, len(newer)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v, want nil error", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %+v, want 1", result.Elisions)
	}
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonRevoked || e.Kept != 0 {
		t.Fatalf("Elision = %+v, want revoked, kept 0", e)
	}
	if e.Ref.Ref != refRevoked.Ref {
		t.Fatalf("Elision.Ref = %+v, want the revoked ref", e.Ref)
	}
	if len(result.Request.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2 (older and newer, not revoked)", len(result.Request.Messages))
	}
	for _, m := range result.Request.Messages {
		if m.Content == string(revoked) {
			t.Fatal("revoked content reached Request.Messages")
		}
	}
}

// TestPlanRevokeAfterWarmCache is the adversarial case the feature
// exists for. It fails against the shipped cache-hit fast path and
// must pass against this change. Kills a mutation that reintroduces a
// cache-hit skip of store.Get.
func TestPlanRevokeAfterWarmCache(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte("will be revoked")
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	win := contextplan.Window{MaxTokens: 1000}

	first, err := planner.Plan(context.Background(), sess, win, byteEstimator{})
	if err != nil {
		t.Fatalf("first Plan: %v", err)
	}
	if len(first.Request.Messages) != 1 || first.Request.Messages[0].Content != string(data) {
		t.Fatalf("first Messages = %+v, want the live content", first.Request.Messages)
	}
	if len(first.Elisions) != 0 {
		t.Fatalf("first Elisions = %+v, want none", first.Elisions)
	}

	if err := store.Revoke(ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	second, err := planner.Plan(context.Background(), sess, win, byteEstimator{})
	if err != nil {
		t.Fatalf("second Plan: %v, want nil error", err)
	}
	if len(second.Request.Messages) != 0 {
		t.Fatalf("second Messages = %+v, want none: the ref is revoked", second.Request.Messages)
	}
	if len(second.Elisions) != 1 || second.Elisions[0].Reason != contextplan.ElisionReasonRevoked {
		t.Fatalf("second Elisions = %+v, want one revoked entry", second.Elisions)
	}
	if second.Elisions[0].Ref.Ref != ref.Ref {
		t.Fatalf("second Elision.Ref = %+v, want the revoked ref", second.Elisions[0].Ref)
	}
}

// TestPlanRevokedRetentionCompliance kills a mutation that runs the
// retention-stub path before the revoked check.
func TestPlanRevokedRetentionCompliance(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte("compliance content, revoked")
	ref := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, data)
	if err := store.Revoke(ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 5}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Request.Messages) != 0 {
		t.Fatalf("Messages = %+v, want none: revoked content never gets a stub", result.Request.Messages)
	}
	if len(result.Elisions) != 1 || result.Elisions[0].Reason != contextplan.ElisionReasonRevoked || result.Elisions[0].Kept != 0 {
		t.Fatalf("Elisions = %+v, want one revoked entry with Kept 0", result.Elisions)
	}
}

// TestPlanRevokedReasoningEvent kills a mutation that reorders the
// revoked check and the reasoning check.
func TestPlanRevokedReasoningEvent(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte("revoked reasoning trace")
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	if err := store.Revoke(ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, provider.ReasoningEventKind, string(provider.RoleAssistant), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 || result.Elisions[0].Reason != contextplan.ElisionReasonRevoked {
		t.Fatalf("Elisions = %+v, want one revoked entry, not reasoning_redacted", result.Elisions)
	}
}

// TestPlanEveryEventRevoked kills a mutation that fails Plan when
// Request.Messages ends up empty.
func TestPlanEveryEventRevoked(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	var events []contextstate.SourceEvent
	for i := 1; i <= 3; i++ {
		data := []byte{byte('a' + i)}
		ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
		if err := store.Revoke(ref); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		events = append(events, sourceEvent("sess-a", uint64(i), "message", string(provider.RoleUser), ref, len(data)))
	}
	sess := &contextstate.Session{Source: events}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v, want nil error", err)
	}
	if len(result.Request.Messages) != 0 {
		t.Fatalf("Messages = %d, want 0", len(result.Request.Messages))
	}
	if len(result.Elisions) != len(events) {
		t.Fatalf("Elisions = %d, want %d", len(result.Elisions), len(events))
	}
	for _, e := range result.Elisions {
		if e.Reason != contextplan.ElisionReasonRevoked {
			t.Fatalf("Elision reason = %q, want revoked", e.Reason)
		}
	}
}

// TestPlanNonRevokedStaysGreen proves the new revoked branch does not
// affect the common path: a session with no revoked payload plans
// exactly as before.
func TestPlanNonRevokedStaysGreen(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte("ordinary, never revoked")
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 0 {
		t.Fatalf("Elisions = %+v, want none", result.Elisions)
	}
	if len(result.Request.Messages) != 1 || result.Request.Messages[0].Content != string(data) {
		t.Fatalf("Messages = %+v, want the live content", result.Request.Messages)
	}
}
