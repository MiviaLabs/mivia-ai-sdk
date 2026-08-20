package contextplan_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/spool"
)

// TestPlanPrincipalConflictDoesNotFailPlan builds two separate
// MemStores, each holding one byte-identical, over-budget payload
// under a different SubjectID, and runs Plan against each store with
// one shared *spool.Spool. contentStore computes ref deterministically
// from data, so the second store's write collides with the first
// store's grant even though the two MemStores never share state.
func TestPlanPrincipalConflictDoesNotFailPlan(t *testing.T) {
	contentStore := newFakeContentStore()
	sp, err := spool.NewSpool(contentStore, 4096)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}

	data := []byte(strings.Repeat("x", 50))

	storeA, cacheA := newStore(t), newCache(t)
	plannerA, err := contextplan.NewPlanner(storeA, cacheA, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	refA := putPayloadForSubject(t, storeA, "sess-a", "subject-a", contextstate.RetentionSession, data)
	sessA := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refA, len(data)),
	}}
	resultA, err := plannerA.Plan(context.Background(), sessA, contextplan.Window{MaxTokens: 10}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan (store A): %v", err)
	}
	if len(resultA.Elisions) != 1 || resultA.Elisions[0].SpoolRef == "" {
		t.Fatalf("Elisions (store A) = %+v, want one spooled elision", resultA.Elisions)
	}

	storeB, cacheB := newStore(t), newCache(t)
	plannerB, err := contextplan.NewPlanner(storeB, cacheB, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	refB := putPayloadForSubject(t, storeB, "sess-b", "subject-b", contextstate.RetentionSession, data)
	sessB := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-b", 1, "message", string(provider.RoleUser), refB, len(data)),
	}}
	resultB, err := plannerB.Plan(context.Background(), sessB, contextplan.Window{MaxTokens: 10}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan (store B): %v, want nil error on a principal conflict", err)
	}
	if len(resultB.Elisions) != 1 {
		t.Fatalf("Elisions (store B) = %v, want 1", resultB.Elisions)
	}
	if resultB.Elisions[0].SpoolRef != "" {
		t.Fatalf("SpoolRef (store B) = %q, want empty: the ref already belongs to subject-a's grant", resultB.Elisions[0].SpoolRef)
	}
}

// TestPlanSpoolPrincipalIsContentSubject proves Plan never reuses one
// caller-level principal across payloads: each spooled ref is granted
// to its own record's SubjectID, not a Planner-wide principal.
func TestPlanSpoolPrincipalIsContentSubject(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	contentStore := newFakeContentStore()
	sp, err := spool.NewSpool(contentStore, 4096)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	dataFirst := []byte(strings.Repeat("f", 50))
	dataSecond := []byte(strings.Repeat("s", 50))
	refFirst := putPayloadForSubject(t, store, "sess-a", "subject-first", contextstate.RetentionSession, dataFirst)
	refSecond := putPayloadForSubject(t, store, "sess-a", "subject-second", contextstate.RetentionSession, dataSecond)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refFirst, len(dataFirst)),
		sourceEvent("sess-a", 2, "message", string(provider.RoleUser), refSecond, len(dataSecond)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 10}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 2 {
		t.Fatalf("Elisions = %v, want 2", result.Elisions)
	}
	var firstRef, secondRef string
	for _, e := range result.Elisions {
		switch e.Ref.Ref {
		case refFirst.Ref:
			firstRef = e.SpoolRef
		case refSecond.Ref:
			secondRef = e.SpoolRef
		}
	}
	if firstRef == "" {
		t.Fatal("did not find a spooled ref for the first payload")
	}
	if secondRef == "" {
		t.Fatal("did not find a spooled ref for the second payload")
	}

	// Each payload's own subject must be able to load its own ref: a
	// hardcoded or shared principal across payloads would still fail
	// the wrong-principal checks below for the wrong reason.
	gotFirst, err := sp.Load(context.Background(), "subject-first", firstRef)
	if err != nil {
		t.Fatalf("Load(subject-first, firstRef): %v", err)
	}
	if string(gotFirst) != string(dataFirst) {
		t.Fatalf("Load(subject-first, firstRef) = %q, want %q", gotFirst, dataFirst)
	}
	gotSecond, err := sp.Load(context.Background(), "subject-second", secondRef)
	if err != nil {
		t.Fatalf("Load(subject-second, secondRef): %v", err)
	}
	if string(gotSecond) != string(dataSecond) {
		t.Fatalf("Load(subject-second, secondRef) = %q, want %q", gotSecond, dataSecond)
	}

	if _, err := sp.Load(context.Background(), "subject-second", firstRef); !errors.Is(err, spool.ErrWrongPrincipal) {
		t.Fatalf("Load with the second payload's subject: err = %v, want ErrWrongPrincipal", err)
	}
	if _, err := sp.Load(context.Background(), "subject-first", secondRef); !errors.Is(err, spool.ErrWrongPrincipal) {
		t.Fatalf("Load with the first payload's subject: err = %v, want ErrWrongPrincipal", err)
	}
}

// TestPlanConcurrentUseWithSpool extends TestPlanConcurrentUse with a
// shared *spool.Spool: proves contextplan's new call site adds no
// race of its own. Run under go test -race.
func TestPlanConcurrentUseWithSpool(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	contentStore := newFakeContentStore()
	sp, err := spool.NewSpool(contentStore, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	const n = 8
	sessions := make([]*contextstate.Session, n)
	subjects := make([]string, n)
	datas := make([][]byte, n)
	for i := 0; i < n; i++ {
		data := []byte(strings.Repeat(string(rune('a'+i)), 50))
		sid := "sess-" + string(rune('a'+i))
		subject := "subject-" + string(rune('a'+i))
		ref := putPayloadForSubject(t, store, sid, subject, contextstate.RetentionSession, data)
		subjects[i] = subject
		datas[i] = data
		sessions[i] = &contextstate.Session{Source: []contextstate.SourceEvent{
			sourceEvent(sid, 1, "message", string(provider.RoleUser), ref, len(data)),
		}}
	}

	results := make([]contextplan.PlanResult, n)
	errs := make([]error, n)
	done := make(chan int, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			results[idx], errs[idx] = planner.Plan(context.Background(), sessions[idx], contextplan.Window{MaxTokens: 10}, byteEstimator{})
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
		if len(results[i].Elisions) != 1 || results[i].Elisions[0].SpoolRef == "" {
			t.Fatalf("goroutine %d: Elisions = %+v, want one spooled elision", i, results[i].Elisions)
		}
		got, err := sp.Load(context.Background(), subjects[i], results[i].Elisions[0].SpoolRef)
		if err != nil {
			t.Fatalf("goroutine %d: Load: %v", i, err)
		}
		if string(got) != string(datas[i]) {
			t.Fatalf("goroutine %d: Load = %q, want %q", i, got, datas[i])
		}
	}
}
