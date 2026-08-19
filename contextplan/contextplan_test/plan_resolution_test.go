package contextplan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
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
