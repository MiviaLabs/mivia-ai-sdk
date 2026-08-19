package contextplan

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// Sentinel errors for NewPlanner and Plan; test with errors.Is.
var (
	// ErrNilStore is NewPlanner's error when store is nil.
	ErrNilStore = errors.New("contextplan: store must not be nil")
	// ErrNilCache is NewPlanner's error when cache is nil.
	ErrNilCache = errors.New("contextplan: cache must not be nil")
	// ErrNilSession is Plan's error when sess is nil.
	ErrNilSession = errors.New("contextplan: session must not be nil")
)

// PlanResult is Plan's output: the built request, every elision
// decision Plan made, and the estimator's total over Request.Messages.
// EstimatedTokens is always at or under Window.Budget().
type PlanResult struct {
	Request         provider.Request
	Elisions        []Elision
	EstimatedTokens int
}

// Planner fits one session's source events into a bounded provider
// request. Built only through NewPlanner. Safe for concurrent use
// through its two dependencies' own concurrency guarantees; Plan
// holds no mutable state of its own between calls.
type Planner struct {
	store *contextstate.MemStore
	cache *memory.Store
}

// NewPlanner builds a Planner over store, the durable payload source,
// and cache, a same-process decode cache. A nil store wraps
// ErrNilStore; a nil cache wraps ErrNilCache.
func NewPlanner(store *contextstate.MemStore, cache *memory.Store) (*Planner, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if cache == nil {
		return nil, ErrNilCache
	}
	return &Planner{store: store, cache: cache}, nil
}

// Plan walks sess.Source newest to oldest. For every event it
// resolves the full contextstate.PayloadRecord before it decides
// anything, including a reasoning event's and a payload it ends up
// fully dropping. A reasoning event never enters Request.Messages and
// always produces an ElisionReasonReasoningRedacted entry. For every
// other event, Plan adds the decoded provider.Message while the
// running estimate stays at or under w.Budget(); once the next-oldest
// message would exceed the budget, a RetentionCompliance payload gets
// a stub instead, unless the stub itself would exceed the budget, in
// which case it drops too. EstimatedTokens is always at or under
// w.Budget(). Plan returns a non-nil error only on a malformed
// Window, a nil sess, or a payload-resolution failure; it never
// returns a partial PlanResult.
func (p *Planner) Plan(ctx context.Context, sess *contextstate.Session, w Window, e provider.TokenEstimator) (PlanResult, error) {
	if sess == nil {
		return PlanResult{}, ErrNilSession
	}
	if err := w.Validate(); err != nil {
		return PlanResult{}, err
	}
	budget := w.Budget()

	var messages []provider.Message
	var elisions []Elision
	overBudget := false

	for i := len(sess.Source) - 1; i >= 0; i-- {
		event := sess.Source[i]
		record, err := p.resolvePayload(event)
		if err != nil {
			return PlanResult{}, err
		}
		if IsReasoningEvent(event) {
			elisions = append(elisions, Elision{Ref: record.Ref, Reason: ElisionReasonReasoningRedacted})
			continue
		}
		messages, overBudget, elisions = admit(e, budget, messages, elisions, overBudget, event, record)
	}

	final, err := e.EstimateTokens(provider.Request{Messages: messages})
	if err != nil {
		final = 0
	}
	return PlanResult{
		Request:         provider.Request{Messages: messages},
		Elisions:        elisions,
		EstimatedTokens: final,
	}, nil
}

// admit decides whether event's payload enters messages: a full
// insertion while budget allows it, a stub for a RetentionCompliance
// payload once budget is spent, or a drop. An estimator error on a
// trial insertion is treated as "does not fit," never a Plan-level
// failure; only a payload-resolution failure fails Plan.
func admit(e provider.TokenEstimator, budget int, messages []provider.Message, elisions []Elision, overBudget bool, event contextstate.SourceEvent, record contextstate.PayloadRecord) ([]provider.Message, bool, []Elision) {
	if !overBudget {
		candidate := prepend(messages, event.Role, record.Data)
		if tokens, err := e.EstimateTokens(provider.Request{Messages: candidate}); err == nil && tokens <= budget {
			return candidate, false, elisions
		}
		overBudget = true
	}
	if record.Retention == contextstate.RetentionCompliance {
		stub := StubContent(record.Data)
		candidate := prepend(messages, event.Role, stub)
		if tokens, err := e.EstimateTokens(provider.Request{Messages: candidate}); err == nil && tokens <= budget {
			elisions = append(elisions, Elision{Ref: record.Ref, Reason: ElisionReasonRetentionExpired, Kept: len(stub)})
			return candidate, true, elisions
		}
	}
	elisions = append(elisions, Elision{Ref: record.Ref, Reason: ElisionReasonWindowOverflow})
	return messages, true, elisions
}

// prepend returns a new message slice with a role/content message
// placed ahead of messages, matching the oldest-first chronological
// order Plan builds while it walks newest to oldest.
func prepend(messages []provider.Message, role string, content []byte) []provider.Message {
	next := make([]provider.Message, 0, len(messages)+1)
	next = append(next, provider.Message{Role: provider.Role(role), Content: string(content)})
	next = append(next, messages...)
	return next
}

// resolvePayload resolves event's full PayloadRecord: the cache first,
// then store.Get on a cache miss, always confirmed against the store
// for the retention and content-reference metadata the cache does not
// carry. A resolution failure from either dependency propagates
// unwrapped.
func (p *Planner) resolvePayload(event contextstate.SourceEvent) (contextstate.PayloadRecord, error) {
	ref := contextstate.ContentRef{Ref: event.PayloadRef}
	if data, err := p.cache.Get(event.PayloadRef); err == nil {
		record, err := p.store.Get(ref)
		if err != nil {
			return contextstate.PayloadRecord{}, err
		}
		record.Data = data
		return record, nil
	}
	record, err := p.store.Get(ref)
	if err != nil {
		return contextstate.PayloadRecord{}, err
	}
	_, _ = p.cache.Put(record.Data)
	return record, nil
}
