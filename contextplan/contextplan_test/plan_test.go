package contextplan_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestPlanFitsWhole(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	data1, data2, data3 := []byte("0123456789"), []byte("abcdefghij"), []byte("klmnopqrst")
	ref1 := putPayload(t, store, "sess-a", contextstate.RetentionSession, data1)
	ref2 := putPayload(t, store, "sess-a", contextstate.RetentionSession, data2)
	ref3 := putPayload(t, store, "sess-a", contextstate.RetentionSession, data3)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref1, len(data1)),
		sourceEvent("sess-a", 2, "message", string(provider.RoleAssistant), ref2, len(data2)),
		sourceEvent("sess-a", 3, "message", string(provider.RoleUser), ref3, len(data3)),
	}}

	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 100}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 0 {
		t.Fatalf("Elisions = %v, want none", result.Elisions)
	}
	if len(result.Request.Messages) != 3 {
		t.Fatalf("Messages = %d, want 3", len(result.Request.Messages))
	}
	if result.EstimatedTokens != 30 {
		t.Fatalf("EstimatedTokens = %d, want 30", result.EstimatedTokens)
	}
	if result.Request.Messages[0].Content != string(data1) || result.Request.Messages[2].Content != string(data3) {
		t.Fatalf("Messages out of chronological order: %+v", result.Request.Messages)
	}
}

func TestPlanElidesOldestFirst(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	data := []byte("0123456789")
	ref1 := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	ref2 := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	ref3 := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref1, len(data)),
		sourceEvent("sess-a", 2, "message", string(provider.RoleUser), ref2, len(data)),
		sourceEvent("sess-a", 3, "message", string(provider.RoleUser), ref3, len(data)),
	}}

	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 15}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Request.Messages) != 1 || result.Request.Messages[0].Content != string(data) {
		t.Fatalf("Messages = %+v, want the newest content only", result.Request.Messages)
	}
	if len(result.Elisions) != 2 {
		t.Fatalf("Elisions = %v, want 2", result.Elisions)
	}
	elided := map[string]bool{}
	for _, e := range result.Elisions {
		if e.Reason != contextplan.ElisionReasonWindowOverflow || e.Kept != 0 {
			t.Fatalf("Elision = %+v, want window overflow, kept 0", e)
		}
		elided[e.Ref.Ref] = true
	}
	if !elided[ref1.Ref] || !elided[ref2.Ref] {
		t.Fatalf("Elisions %v do not cover the two oldest refs", result.Elisions)
	}
	if result.EstimatedTokens > 15 {
		t.Fatalf("EstimatedTokens = %d, over budget 15", result.EstimatedTokens)
	}
}

func TestPlanRespectsRetention(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	newest := []byte("0123456789")
	oldestData := []byte(strings.Repeat("x", 500))
	refNewest := putPayload(t, store, "sess-a", contextstate.RetentionSession, newest)
	refOldest := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, oldestData)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refOldest, len(oldestData)),
		sourceEvent("sess-a", 2, "message", string(provider.RoleUser), refNewest, len(newest)),
	}}

	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 270}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	stub := contextplan.StubContent(oldestData)
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonRetentionExpired || e.Kept != len(stub) {
		t.Fatalf("Elision = %+v, want retention expired kept %d", e, len(stub))
	}
	if len(result.Request.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2 (stub plus newest)", len(result.Request.Messages))
	}
	if result.Request.Messages[0].Content != string(stub) {
		t.Fatalf("first message = %q, want the stub", result.Request.Messages[0].Content)
	}
	if result.EstimatedTokens < len(stub)+len(newest) || result.EstimatedTokens > 270 {
		t.Fatalf("EstimatedTokens = %d, want the stub counted and at most 270", result.EstimatedTokens)
	}
}

func TestPlanUnrecognizedRetention(t *testing.T) {
	cases := []struct {
		name      string
		retention contextstate.RetentionClass
	}{
		{"session retention", contextstate.RetentionSession},
		{"unrecognized non-empty retention", contextstate.RetentionClass("archived")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(t)
			cache := newCache(t)
			planner, err := contextplan.NewPlanner(store, cache)
			if err != nil {
				t.Fatalf("NewPlanner: %v", err)
			}
			newest := []byte("0123456789")
			oldestData := []byte(strings.Repeat("x", 500))
			refNewest := putPayload(t, store, "sess-a", contextstate.RetentionSession, newest)
			refOldest := putPayload(t, store, "sess-a", tc.retention, oldestData)
			sess := &contextstate.Session{Source: []contextstate.SourceEvent{
				sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refOldest, len(oldestData)),
				sourceEvent("sess-a", 2, "message", string(provider.RoleUser), refNewest, len(newest)),
			}}

			result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 270}, byteEstimator{})
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if len(result.Elisions) != 1 {
				t.Fatalf("Elisions = %v, want 1", result.Elisions)
			}
			e := result.Elisions[0]
			if e.Reason != contextplan.ElisionReasonWindowOverflow || e.Kept != 0 || e.Ref.Ref != refOldest.Ref {
				t.Fatalf("Elision = %+v, want window overflow, kept 0, over the oldest ref", e)
			}
			if len(result.Request.Messages) != 1 {
				t.Fatalf("Messages = %d, want 1 (newest only)", len(result.Request.Messages))
			}
		})
	}
}

func TestPlanReservesHeadroom(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte(strings.Repeat("y", 40))
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	w := contextplan.Window{MaxTokens: 50, Reserve: 20}
	result, err := planner.Plan(context.Background(), sess, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if result.EstimatedTokens > w.Budget() {
		t.Fatalf("EstimatedTokens = %d, over budget %d", result.EstimatedTokens, w.Budget())
	}
	if len(result.Request.Messages) != 0 {
		t.Fatalf("Messages = %d, want 0: 40 bytes exceeds the 30-token budget", len(result.Request.Messages))
	}
	if len(result.Elisions) != 1 || result.Elisions[0].Reason != contextplan.ElisionReasonWindowOverflow {
		t.Fatalf("Elisions = %v, want one window overflow", result.Elisions)
	}
}

func TestPlanStubDoesNotFit(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	newest := []byte(strings.Repeat("n", 250))
	oldestData := []byte(strings.Repeat("o", 500))
	refNewest := putPayload(t, store, "sess-a", contextstate.RetentionSession, newest)
	refOldest := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, oldestData)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refOldest, len(oldestData)),
		sourceEvent("sess-a", 2, "message", string(provider.RoleUser), refNewest, len(newest)),
	}}
	w := contextplan.Window{MaxTokens: 260}
	result, err := planner.Plan(context.Background(), sess, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonWindowOverflow || e.Kept != 0 {
		t.Fatalf("Elision = %+v, want window overflow despite retention, kept 0", e)
	}
	if len(result.Request.Messages) != 1 || result.Request.Messages[0].Content != string(newest) {
		t.Fatalf("Messages = %+v, want the newest only", result.Request.Messages)
	}
	if result.EstimatedTokens > w.Budget() {
		t.Fatalf("EstimatedTokens = %d, over budget %d", result.EstimatedTokens, w.Budget())
	}
}

func TestPlanStubBoundaryStacked(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	newest := []byte(strings.Repeat("n", 50))
	middleData := []byte(strings.Repeat("m", 500))
	oldestData := []byte(strings.Repeat("o", 500))
	refNewest := putPayload(t, store, "sess-a", contextstate.RetentionSession, newest)
	refMiddle := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, middleData)
	refOldest := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, oldestData)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refOldest, len(oldestData)),
		sourceEvent("sess-a", 2, "message", string(provider.RoleUser), refMiddle, len(middleData)),
		sourceEvent("sess-a", 3, "message", string(provider.RoleUser), refNewest, len(newest)),
	}}
	w := contextplan.Window{MaxTokens: 316}
	result, err := planner.Plan(context.Background(), sess, w, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 2 {
		t.Fatalf("Elisions = %v, want 2", result.Elisions)
	}
	byRef := map[string]contextplan.Elision{}
	for _, e := range result.Elisions {
		byRef[e.Ref.Ref] = e
	}
	middle, ok := byRef[refMiddle.Ref]
	if !ok || middle.Reason != contextplan.ElisionReasonRetentionExpired || middle.Kept == 0 {
		t.Fatalf("middle elision = %+v, want a kept stub", middle)
	}
	oldest, ok := byRef[refOldest.Ref]
	if !ok || oldest.Reason != contextplan.ElisionReasonWindowOverflow || oldest.Kept != 0 {
		t.Fatalf("oldest elision = %+v, want a full drop", oldest)
	}
	if result.EstimatedTokens > w.Budget() {
		t.Fatalf("EstimatedTokens = %d, over budget %d", result.EstimatedTokens, w.Budget())
	}
}

func TestPlanReasoningEvents(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	ordinary := []byte("ordinary content")
	reasoning := []byte("reasoning trace content")
	refOrdinary := putPayload(t, store, "sess-a", contextstate.RetentionSession, ordinary)
	refReasoning := putPayload(t, store, "sess-a", contextstate.RetentionSession, reasoning)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refOrdinary, len(ordinary)),
		sourceEvent("sess-a", 2, provider.ReasoningEventKind, string(provider.RoleAssistant), refReasoning, len(reasoning)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Request.Messages) != 1 || result.Request.Messages[0].Content != string(ordinary) {
		t.Fatalf("Messages = %+v, want only the ordinary content", result.Request.Messages)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonReasoningRedacted {
		t.Fatalf("Elision reason = %v, want reasoning redacted", e.Reason)
	}
	if e.Ref.Ref != refReasoning.Ref {
		t.Fatalf("Elision.Ref = %+v, want the resolved reasoning payload's ref", e.Ref)
	}
}

func TestPlanErrorCases(t *testing.T) {
	validWindow := contextplan.Window{MaxTokens: 100}

	t.Run("nil session", func(t *testing.T) {
		store, cache := newStore(t), newCache(t)
		planner, err := contextplan.NewPlanner(store, cache)
		if err != nil {
			t.Fatalf("NewPlanner: %v", err)
		}
		_, err = planner.Plan(context.Background(), nil, validWindow, byteEstimator{})
		if !errors.Is(err, contextplan.ErrNilSession) {
			t.Fatalf("err = %v, want ErrNilSession", err)
		}
	})

	t.Run("invalid window", func(t *testing.T) {
		store, cache := newStore(t), newCache(t)
		planner, err := contextplan.NewPlanner(store, cache)
		if err != nil {
			t.Fatalf("NewPlanner: %v", err)
		}
		sess := &contextstate.Session{}
		_, err = planner.Plan(context.Background(), sess, contextplan.Window{}, byteEstimator{})
		if err == nil {
			t.Fatal("Plan accepted an invalid Window")
		}
	})

	t.Run("resolution failure for a payload that would be kept", func(t *testing.T) {
		store, cache := newStore(t), newCache(t)
		planner, err := contextplan.NewPlanner(store, cache)
		if err != nil {
			t.Fatalf("NewPlanner: %v", err)
		}
		data := []byte("missing")
		ref := unstoredRef(t, "sess-a", data)
		sess := &contextstate.Session{Source: []contextstate.SourceEvent{
			sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
		}}
		_, err = planner.Plan(context.Background(), sess, validWindow, byteEstimator{})
		if !errors.Is(err, contextstate.ErrPayloadNotFound) {
			t.Fatalf("err = %v, want ErrPayloadNotFound", err)
		}
	})

}

// TestPlanErrorCasesResolution covers the resolution-failure branches
// split out of TestPlanErrorCases to keep each test function short.
func TestPlanErrorCasesResolution(t *testing.T) {
	validWindow := contextplan.Window{MaxTokens: 100}

	t.Run("resolution failure for a payload that would be fully dropped", func(t *testing.T) {
		store, cache := newStore(t), newCache(t)
		planner, err := contextplan.NewPlanner(store, cache)
		if err != nil {
			t.Fatalf("NewPlanner: %v", err)
		}
		newest := []byte(strings.Repeat("n", 100))
		refNewest := putPayload(t, store, "sess-a", contextstate.RetentionSession, newest)
		missingData := []byte("missing")
		refMissing := unstoredRef(t, "sess-a", missingData)
		sess := &contextstate.Session{Source: []contextstate.SourceEvent{
			sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refMissing, len(missingData)),
			sourceEvent("sess-a", 2, "message", string(provider.RoleUser), refNewest, len(newest)),
		}}
		_, err = planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 100}, byteEstimator{})
		if !errors.Is(err, contextstate.ErrPayloadNotFound) {
			t.Fatalf("err = %v, want ErrPayloadNotFound", err)
		}
	})

	t.Run("resolution failure for a reasoning event", func(t *testing.T) {
		store, cache := newStore(t), newCache(t)
		planner, err := contextplan.NewPlanner(store, cache)
		if err != nil {
			t.Fatalf("NewPlanner: %v", err)
		}
		data := []byte("missing reasoning")
		ref := unstoredRef(t, "sess-a", data)
		sess := &contextstate.Session{Source: []contextstate.SourceEvent{
			sourceEvent("sess-a", 1, provider.ReasoningEventKind, string(provider.RoleAssistant), ref, len(data)),
		}}
		_, err = planner.Plan(context.Background(), sess, validWindow, byteEstimator{})
		if !errors.Is(err, contextstate.ErrPayloadNotFound) {
			t.Fatalf("err = %v, want ErrPayloadNotFound", err)
		}
	})
}

func TestPlanConcurrentUse(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	const n = 8
	sessions := make([]*contextstate.Session, n)
	refs := make([]contextstate.ContentRef, n)
	for i := 0; i < n; i++ {
		data := []byte(strings.Repeat(string(rune('a'+i)), 10))
		sid := "sess-" + string(rune('a'+i))
		ref := putPayload(t, store, sid, contextstate.RetentionSession, data)
		refs[i] = ref
		sessions[i] = &contextstate.Session{Source: []contextstate.SourceEvent{
			sourceEvent(sid, 1, "message", string(provider.RoleUser), ref, len(data)),
		}}
	}

	results := make([]contextplan.PlanResult, n)
	errs := make([]error, n)
	done := make(chan int, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			results[idx], errs[idx] = planner.Plan(context.Background(), sessions[idx], contextplan.Window{MaxTokens: 100}, byteEstimator{})
			done <- idx
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: Plan: %v", i, errs[i])
		}
		if len(results[i].Request.Messages) != 1 {
			t.Fatalf("goroutine %d: Messages = %d, want 1", i, len(results[i].Request.Messages))
		}
		want := strings.Repeat(string(rune('a'+i)), 10)
		if results[i].Request.Messages[0].Content != want {
			t.Fatalf("goroutine %d: result mixed with another goroutine's session: got %q, want %q", i, results[i].Request.Messages[0].Content, want)
		}
	}
}
