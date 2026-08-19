package contextplan_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/spool"
)

// fakeContentStore is a spool.ContentStore backed by an in-memory map,
// keyed deterministically by content hash so byte-identical Data
// across two calls resolves to one ref, matching
// spool/spool_test/spool_test.go's fakeStore. putErr, when set, fails
// every Put.
type fakeContentStore struct {
	mu     sync.Mutex
	blobs  map[string][]byte
	putErr error
}

func newFakeContentStore() *fakeContentStore {
	return &fakeContentStore{blobs: make(map[string][]byte)}
}

func refForContent(content []byte) string {
	sum := sha256.Sum256(content)
	return "ref-" + hex.EncodeToString(sum[:8])
}

func (f *fakeContentStore) Put(content []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return "", f.putErr
	}
	ref := refForContent(content)
	cp := make([]byte, len(content))
	copy(cp, content)
	f.blobs[ref] = cp
	return ref, nil
}

func (f *fakeContentStore) Get(ref string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blobs[ref]
	if !ok {
		return nil, fmt.Errorf("fakeContentStore: no blob for %s", ref)
	}
	return b, nil
}

// putFailingStore is a spool.ContentStore whose Put fails the test if
// called. Used to prove ElisionReasonReasoningRedacted and
// ElisionReasonRevoked never reach Spool.Spool.
type putFailingStore struct{ t *testing.T }

func (f putFailingStore) Put(content []byte) (string, error) {
	f.t.Fatal("Put called for content that must never spool")
	return "", nil
}

func (f putFailingStore) Get(ref string) ([]byte, error) {
	return nil, fmt.Errorf("putFailingStore: unused")
}

func TestPlanNilSpoolerLeavesSpoolRefEmpty(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache, nil)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte(strings.Repeat("x", 50))
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 10}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	if result.Elisions[0].SpoolRef != "" {
		t.Fatalf("SpoolRef = %q, want empty with a nil spooler", result.Elisions[0].SpoolRef)
	}
}

func TestPlanSpoolsWindowOverflow(t *testing.T) {
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
	data := []byte(strings.Repeat("x", 50))
	ref := putPayloadForSubject(t, store, "sess-a", "subject-overflow", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 10}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonWindowOverflow {
		t.Fatalf("Reason = %v, want window overflow", e.Reason)
	}
	if e.SpoolRef == "" {
		t.Fatal("SpoolRef is empty, want a live spool reference")
	}
	got, err := sp.Load(context.Background(), "subject-overflow", e.SpoolRef)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("Load = %q, want %q", got, data)
	}
}

func TestPlanSpoolsRetentionExpired(t *testing.T) {
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
	newest := []byte("0123456789")
	oldestData := []byte(strings.Repeat("x", 500))
	refNewest := putPayloadForSubject(t, store, "sess-a", "subject-newest", contextstate.RetentionSession, newest)
	refOldest := putPayloadForSubject(t, store, "sess-a", "subject-oldest", contextstate.RetentionCompliance, oldestData)
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
	if e.SpoolRef == "" {
		t.Fatal("SpoolRef is empty, want a live spool reference")
	}
	got, err := sp.Load(context.Background(), "subject-oldest", e.SpoolRef)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != string(oldestData) {
		t.Fatal("spooled content is the stub, want the full un-truncated payload")
	}
}

func TestPlanReasoningRedactedNeverSpools(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	sp, err := spool.NewSpool(putFailingStore{t: t}, 4096)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	reasoning := []byte("reasoning trace content")
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, reasoning)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, provider.ReasoningEventKind, string(provider.RoleAssistant), ref, len(reasoning)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonReasoningRedacted {
		t.Fatalf("Reason = %v, want reasoning redacted", e.Reason)
	}
	if e.SpoolRef != "" {
		t.Fatalf("SpoolRef = %q, want empty: reasoning content must never spool", e.SpoolRef)
	}
}

func TestPlanRevokedNeverSpools(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	sp, err := spool.NewSpool(putFailingStore{t: t}, 4096)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte("will be revoked")
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	if err := store.Revoke(ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 1000}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonRevoked {
		t.Fatalf("Reason = %v, want revoked", e.Reason)
	}
	if e.SpoolRef != "" {
		t.Fatalf("SpoolRef = %q, want empty: revoked content must never spool", e.SpoolRef)
	}
}

func TestPlanSpoolWriteFailureDoesNotFailPlan(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	contentStore := newFakeContentStore()
	contentStore.putErr = errors.New("store unavailable")
	sp, err := spool.NewSpool(contentStore, 4096)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte(strings.Repeat("x", 50))
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 10}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v, want nil error even when the spool write fails", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	if result.Elisions[0].SpoolRef != "" {
		t.Fatalf("SpoolRef = %q, want empty after a failed write", result.Elisions[0].SpoolRef)
	}
}

func TestPlanSpoolBudgetDoesNotFailPlan(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	contentStore := newFakeContentStore()
	sp, err := spool.NewSpool(contentStore, 10)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache, sp)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte(strings.Repeat("x", 50))
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 10}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v, want nil error on a grant-too-large spool write", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	if result.Elisions[0].SpoolRef != "" {
		t.Fatalf("SpoolRef = %q, want empty when data exceeds the grant budget", result.Elisions[0].SpoolRef)
	}
}

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
	var firstRef string
	for _, e := range result.Elisions {
		if e.Ref.Ref == refFirst.Ref {
			firstRef = e.SpoolRef
		}
	}
	if firstRef == "" {
		t.Fatal("did not find a spooled ref for the first payload")
	}
	if _, err := sp.Load(context.Background(), "subject-second", firstRef); !errors.Is(err, spool.ErrWrongPrincipal) {
		t.Fatalf("Load with the second payload's subject: err = %v, want ErrWrongPrincipal", err)
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
