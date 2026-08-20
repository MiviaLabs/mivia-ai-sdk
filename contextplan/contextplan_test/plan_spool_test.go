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

// TestPlanSpoolsRetentionCompliantStubOverBudget covers admit's other
// path into ElisionReasonWindowOverflow: a RetentionCompliance payload
// whose stub still exceeds budget falls through past the
// retention-expired branch into the same window-overflow spool write
// TestPlanSpoolsWindowOverflow already proves for a plain
// RetentionSession payload.
func TestPlanSpoolsRetentionCompliantStubOverBudget(t *testing.T) {
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
	data := []byte(strings.Repeat("x", 500))
	ref := putPayloadForSubject(t, store, "sess-a", "subject-stub-over-budget", contextstate.RetentionCompliance, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 5}, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Elisions) != 1 {
		t.Fatalf("Elisions = %v, want 1", result.Elisions)
	}
	e := result.Elisions[0]
	if e.Reason != contextplan.ElisionReasonWindowOverflow {
		t.Fatalf("Reason = %v, want window overflow even though Retention is Compliance", e.Reason)
	}
	if e.SpoolRef == "" {
		t.Fatal("SpoolRef is empty, want a live spool reference")
	}
	got, err := sp.Load(context.Background(), "subject-stub-over-budget", e.SpoolRef)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("Load = %q, want %q", got, data)
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
