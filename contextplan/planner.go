package contextplan

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/spool"
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
// EstimatedTokens stays at or under Window.Budget() for a deterministic estimator whose empty-list total fits.
// A larger fixed overhead exceeds it; an estimator that errors on the final call reports zero.
type PlanResult struct {
	Request         provider.Request
	Elisions        []Elision
	EstimatedTokens int
}

// Planner fits one session's source events into a bounded provider
// request. Built only through NewPlanner. Safe for concurrent use:
// its three dependencies guard their own state, and Plan holds no
// other mutable state of its own between calls.
type Planner struct {
	store   *contextstate.MemStore
	cache   *memory.Store
	spooler *spool.Spool
}

// NewPlanner builds a Planner over store, the durable payload source,
// cache, a same-process decode cache, and spooler, an optional durable
// overflow target. A nil store wraps ErrNilStore; a nil cache wraps
// ErrNilCache. A nil spooler is valid: Plan never calls Spool.Spool,
// and behaves exactly as it does with a wired spooler that never gets
// used, byte for byte.
func NewPlanner(store *contextstate.MemStore, cache *memory.Store, spooler *spool.Spool) (*Planner, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if cache == nil {
		return nil, ErrNilCache
	}
	return &Planner{store: store, cache: cache, spooler: spooler}, nil
}

// Plan walks sess.Source newest to oldest. For every event it
// resolves the full contextstate.PayloadRecord through one store.Get
// call before it decides anything, including a reasoning event's and
// a payload it ends up fully dropping. A revoked payload never enters
// Request.Messages and always produces an ElisionReasonRevoked entry,
// checked before the reasoning check. A reasoning event never enters
// Request.Messages and always produces an ElisionReasonReasoningRedacted
// entry. For every other event, Plan adds the decoded provider.Message
// while the running estimate stays at or under w.Budget(); once the
// next-oldest message would exceed the budget, a RetentionCompliance
// payload gets a stub instead, unless the stub itself would exceed the
// budget, in which case it drops too. A wired Spool receives the full
// payload behind every ElisionReasonWindowOverflow and
// ElisionReasonRetentionExpired entry, keyed to record.Ref.SubjectID,
// best-effort, never failing Plan.
// EstimatedTokens stays at or under w.Budget() for a deterministic estimator whose empty-list total fits.
// A larger fixed overhead exceeds it; an estimator that errors on the final call reports zero.
// Plan returns a non-nil error only on a malformed
// Window, a nil sess, or a payload-resolution failure other than a
// revocation; it never returns a partial PlanResult.
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
			if errors.Is(err, contextstate.ErrPayloadRevoked) {
				elisions = append(elisions, Elision{Ref: record.Ref, Reason: ElisionReasonRevoked})
				continue
			}
			return PlanResult{}, err
		}
		if IsReasoningEvent(event) {
			elisions = append(elisions, Elision{Ref: record.Ref, Reason: ElisionReasonReasoningRedacted})
			continue
		}
		messages, overBudget, elisions = admit(ctx, e, p.spooler, budget, messages, elisions, overBudget, event, record)
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
// failure; only a payload-resolution failure fails Plan. A non-nil
// spooler receives the full record.Data for the two budget-driven
// drop paths, best-effort: a spool.Spool error leaves the returned
// Elision's SpoolRef empty and never fails admit.
func admit(ctx context.Context, e provider.TokenEstimator, spooler *spool.Spool, budget int, messages []provider.Message, elisions []Elision, overBudget bool, event contextstate.SourceEvent, record contextstate.PayloadRecord) ([]provider.Message, bool, []Elision) {
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
			elisions = append(elisions, Elision{
				Ref:      record.Ref,
				Reason:   ElisionReasonRetentionExpired,
				Kept:     len(stub),
				SpoolRef: spoolRecord(ctx, spooler, record),
			})
			return candidate, true, elisions
		}
	}
	elisions = append(elisions, Elision{
		Ref:      record.Ref,
		Reason:   ElisionReasonWindowOverflow,
		SpoolRef: spoolRecord(ctx, spooler, record),
	})
	return messages, true, elisions
}

// spoolRecord writes record.Data to spooler under record.Ref.SubjectID
// and returns the reference. A nil spooler or a Spool.Spool error
// returns an empty string; the caller never fails on either.
func spoolRecord(ctx context.Context, spooler *spool.Spool, record contextstate.PayloadRecord) string {
	if spooler == nil {
		return ""
	}
	_, ref, err := spooler.Spool(ctx, record.Ref.SubjectID, record.Data)
	if err != nil {
		return ""
	}
	return ref
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

// resolvePayload resolves event's full PayloadRecord through one
// store.Get call, on every call: no cache-hit skip, so a Revoke
// issued between two Plan calls is visible on the very next call. On
// success it backfills cache, preserving today's write-side
// population. On contextstate.ErrPayloadRevoked, it makes one more
// call, store.Status, to recover the denied record's metadata for the
// caller's Elision, and returns that record alongside the original
// error. Any other resolution failure, including one from Status,
// propagates unwrapped.
func (p *Planner) resolvePayload(event contextstate.SourceEvent) (contextstate.PayloadRecord, error) {
	ref := contextstate.ContentRef{Ref: event.PayloadRef}
	record, err := p.store.Get(ref)
	if err != nil {
		if errors.Is(err, contextstate.ErrPayloadRevoked) {
			status, statusErr := p.store.Status(ref)
			if statusErr != nil {
				return contextstate.PayloadRecord{}, statusErr
			}
			return status, err
		}
		return contextstate.PayloadRecord{}, err
	}
	_, _ = p.cache.Put(record.Data)
	return record, nil
}
