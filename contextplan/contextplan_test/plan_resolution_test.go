package contextplan_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// resolvePayload's cache-hit branch still confirms the record against
// the store; a cache hit over a payload never committed to the store
// must still fail. Seeding the cache directly (never calling
// store.Put) reaches that branch, distinct from every other
// resolution-failure case in plan_test.go, which never populates the
// cache and so only exercises the cache-miss error path.
func TestPlanResolutionCacheHitStoreMiss(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte("cached but never committed")
	if _, err := cache.Put(data); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	ref := unstoredRef(t, "sess-a", data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	_, err = planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 100}, byteEstimator{})
	if !errors.Is(err, contextstate.ErrPayloadNotFound) {
		t.Fatalf("err = %v, want ErrPayloadNotFound", err)
	}
}

// TestPlanResolutionCacheHitSkipsStore pins that a full cache hit
// (both the decoded bytes and the retention metadata) never
// round-trips through the store: after one Plan call resolves and
// caches an event, overwriting the store's record under the same ref
// must not change a second Plan call's outcome, since the second
// call reads the cached metadata instead of the mutated store entry.
func TestPlanResolutionCacheHitSkipsStore(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	// Longer than StubContentBytes so the stub (truncated) estimates
	// smaller than the full content, distinguishing a stub decision
	// from a full-fit decision under byteEstimator's byte-per-token
	// count.
	data := make([]byte, contextplan.StubContentBytes+50)
	for i := range data {
		data[i] = 'a'
	}
	ref := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	est := byteEstimator{}
	win := contextplan.Window{MaxTokens: len(data)}

	first, err := planner.Plan(context.Background(), sess, win, est)
	if err != nil {
		t.Fatalf("first Plan: %v", err)
	}
	if len(first.Request.Messages) != 1 {
		t.Fatalf("first Messages = %d, want 1", len(first.Request.Messages))
	}

	// Overwrite the stored record under the same ref with
	// RetentionSession. A resolve that still hits the store would
	// see this and stop protecting the payload with a stub once it
	// no longer fits; a resolve served from the meta cache would not.
	if err := store.Put(contextstate.PayloadRecord{Ref: ref, Retention: contextstate.RetentionSession, Data: data}); err != nil {
		t.Fatalf("overwrite Put: %v", err)
	}

	// Between the stub's estimate (StubContentBytes) and the full
	// content's estimate (len(data)): the full insert overflows, so
	// only a still-protected payload earns a stub instead of a drop.
	stubWin := contextplan.Window{MaxTokens: contextplan.StubContentBytes}
	second, err := planner.Plan(context.Background(), sess, stubWin, est)
	if err != nil {
		t.Fatalf("second Plan: %v", err)
	}
	if len(second.Elisions) != 1 || second.Elisions[0].Reason != contextplan.ElisionReasonRetentionExpired {
		t.Fatalf("second Elisions = %+v, want one RetentionExpired stub: the cached RetentionCompliance metadata must still protect it", second.Elisions)
	}
}

// TestPlanResolutionMetaSurvivesCacheEviction pins that a meta-cache
// hit on a ref whose decode-cache entry was evicted for budget still
// resolves correctly: resolvePayload falls through to store.Get on
// the decode-cache miss, and re-backfills the decode cache, rather
// than returning a record built from a stale or missing Data.
func TestPlanResolutionMetaSurvivesCacheEviction(t *testing.T) {
	store := newStore(t)
	// A budget that holds both payloads together but not one after
	// the other, so putting the second payload evicts the first's
	// decode-cache entry while its meta-cache entry (unbounded, keyed
	// separately) survives.
	cache, err := memory.New(40)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	dataA := []byte("payload-a-content")
	refA := putPayload(t, store, "sess-a", contextstate.RetentionSession, dataA)
	sessA := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), refA, len(dataA)),
	}}
	est := byteEstimator{}
	win := contextplan.Window{MaxTokens: 100}

	first, err := planner.Plan(context.Background(), sessA, win, est)
	if err != nil {
		t.Fatalf("first Plan (A): %v", err)
	}
	if len(first.Request.Messages) != 1 || first.Request.Messages[0].Content != string(dataA) {
		t.Fatalf("first Plan (A) Messages = %+v, want one message with content %q", first.Request.Messages, dataA)
	}

	// Resolve a second, larger payload: dataA's decode-cache entry
	// evicts under the 20-byte budget, but refA's meta stays cached.
	dataB := []byte("payload-b-content-longer-than-budget")
	refB := putPayload(t, store, "sess-b", contextstate.RetentionSession, dataB)
	sessB := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-b", 1, "message", string(provider.RoleUser), refB, len(dataB)),
	}}
	if _, err := planner.Plan(context.Background(), sessB, win, est); err != nil {
		t.Fatalf("second Plan (B): %v", err)
	}
	if _, err := cache.Get(refA.Ref); !errors.Is(err, memory.ErrUnknownRef) {
		t.Fatalf("cache.Get(refA) err = %v, want ErrUnknownRef: the eviction this test relies on did not happen", err)
	}

	// Resolving A again must still return A's real content: a meta
	// hit with a decode-cache miss must fall through to store.Get,
	// not return a record with stale or missing Data.
	third, err := planner.Plan(context.Background(), sessA, win, est)
	if err != nil {
		t.Fatalf("third Plan (A again): %v", err)
	}
	if len(third.Request.Messages) != 1 || third.Request.Messages[0].Content != string(dataA) {
		t.Fatalf("third Plan (A again) Messages = %+v, want one message with content %q", third.Request.Messages, dataA)
	}
	if _, err := cache.Get(refA.Ref); err != nil {
		t.Fatalf("cache.Get(refA) after third Plan: %v, want the decode cache re-backfilled", err)
	}
}

// TestPlanResolutionConcurrentSameRef pins that two goroutines
// resolving the same new ref at the same time never corrupt the meta
// cache or the decode cache: both calls must see the real content,
// and neither may observe a torn or partially written cache entry.
// go test -race exercises the mutex and the two dependencies' own
// concurrency guarantees together.
func TestPlanResolutionConcurrentSameRef(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	data := []byte("shared-ref-content")
	ref := putPayload(t, store, "sess-shared", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-shared", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	est := byteEstimator{}
	win := contextplan.Window{MaxTokens: 100}

	const n = 8
	results := make([]contextplan.PlanResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = planner.Plan(context.Background(), sess, win, est)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: Plan: %v", i, errs[i])
		}
		if len(results[i].Request.Messages) != 1 || results[i].Request.Messages[0].Content != string(data) {
			t.Fatalf("goroutine %d: Messages = %+v, want one message with content %q", i, results[i].Request.Messages, data)
		}
	}
}

// countingEstimator fails from the (failAt+1)th call onward, letting
// a test target one specific EstimateTokens call in Plan's sequence:
// every per-event admission trial succeeds, but the final aggregate
// estimate over the built Request fails.
type countingEstimator struct {
	calls  *int
	failAt int
}

// EstimateTokens sums message content bytes until the call budget
// runs out, then fails every call after.
func (c countingEstimator) EstimateTokens(req provider.Request) (int, error) {
	*c.calls++
	if *c.calls > c.failAt {
		return 0, errors.New("countingEstimator: exhausted")
	}
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}

// TestPlanFinalEstimateFailureYieldsZero pins Plan's behavior when
// the aggregate EstimateTokens call after the walk fails: Plan does
// not fail the whole call, and EstimatedTokens reports 0, matching
// the admit-trial fallback ("does not fit," never a Plan failure)
// applied to the one estimator call outside admit.
func TestPlanFinalEstimateFailureYieldsZero(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte("0123456789")
	ref := putPayload(t, store, "sess-a", contextstate.RetentionSession, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}

	calls := 0
	// One event admits with exactly one EstimateTokens call inside
	// admit; failAt=1 lets that call succeed and fails only the
	// second, final call Plan makes after the walk.
	est := countingEstimator{calls: &calls, failAt: 1}
	result, err := planner.Plan(context.Background(), sess, contextplan.Window{MaxTokens: 100}, est)
	if err != nil {
		t.Fatalf("Plan: %v, want no error even though the final estimate failed", err)
	}
	if len(result.Request.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1: the walk's own estimate succeeded", len(result.Request.Messages))
	}
	if result.EstimatedTokens != 0 {
		t.Fatalf("EstimatedTokens = %d, want 0 when the final estimate call fails", result.EstimatedTokens)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want at least 2: one admit trial plus the final estimate", calls)
	}
}

// TestPlanStubStaysValidUTF8 checks the rune-safe stub through Plan: a
// RetentionCompliance payload of multi-byte runes, too large to fit in
// full, enters Request.Messages as a stub whose Content is valid
// UTF-8.
func TestPlanStubStaysValidUTF8(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	data := []byte(strings.Repeat("é", contextplan.StubContentBytes))
	ref := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, data)
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{
		sourceEvent("sess-a", 1, "message", string(provider.RoleUser), ref, len(data)),
	}}
	// The budget sits between the stub's byte count and the full
	// payload's, so the payload earns a stub rather than a full fit.
	win := contextplan.Window{MaxTokens: contextplan.StubContentBytes}
	result, err := planner.Plan(context.Background(), sess, win, byteEstimator{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(result.Request.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1 stub message", len(result.Request.Messages))
	}
	content := result.Request.Messages[0].Content
	if content == string(data) {
		t.Fatal("message holds the full payload, want the stub")
	}
	if !utf8.ValidString(content) {
		t.Fatalf("stub content = %q, want valid UTF-8", content)
	}
	if len(result.Elisions) != 1 || result.Elisions[0].Reason != contextplan.ElisionReasonRetentionExpired {
		t.Fatalf("Elisions = %+v, want one retention_expired entry", result.Elisions)
	}
}

// overheadEstimator charges a fixed per-request overhead on top of the
// message bytes, so its total for an empty message list is overhead.
type overheadEstimator struct{ overhead int }

// EstimateTokens returns overhead plus the byte length of every
// message's Content.
func (o overheadEstimator) EstimateTokens(req provider.Request) (int, error) {
	total := o.overhead
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}

// TestPlanOverheadEstimatorExceedsBudget pins the documented limit of
// the EstimatedTokens bound: an estimator whose empty-list total
// already exceeds the budget drops every event and still reports that
// overhead.
func TestPlanOverheadEstimatorExceedsBudget(t *testing.T) {
	store, cache := newStore(t), newCache(t)
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	var events []contextstate.SourceEvent
	for i := 1; i <= 3; i++ {
		data := []byte(fmt.Sprintf("payload %d", i))
		ref := putPayload(t, store, "sess-a", contextstate.RetentionCompliance, data)
		events = append(events, sourceEvent("sess-a", uint64(i), "message", string(provider.RoleUser), ref, len(data)))
	}
	sess := &contextstate.Session{Source: events}
	win := contextplan.Window{MaxTokens: 100}
	result, err := planner.Plan(context.Background(), sess, win, overheadEstimator{overhead: win.Budget() + 1})
	if err != nil {
		t.Fatalf("Plan: %v, want no error", err)
	}
	if len(result.Request.Messages) != 0 {
		t.Fatalf("Messages = %d, want none: no candidate fits", len(result.Request.Messages))
	}
	if len(result.Elisions) != len(events) {
		t.Fatalf("Elisions = %d, want one per source event", len(result.Elisions))
	}
	for _, e := range result.Elisions {
		if e.Reason != contextplan.ElisionReasonWindowOverflow {
			t.Fatalf("Elision reason = %q, want window_overflow", e.Reason)
		}
	}
	if result.EstimatedTokens <= win.Budget() {
		t.Fatalf("EstimatedTokens = %d, want above the budget %d", result.EstimatedTokens, win.Budget())
	}
}
