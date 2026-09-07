package contextsession

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/x/contextstate"
)

// Sentinel errors for NewPlanner and Plan; test with errors.Is.
var (
	// ErrNilStore is NewPlanner's error when store is nil.
	ErrNilStore = errors.New("contextsession: store must not be nil")
	// ErrNilSession is Plan's error when sess is nil.
	ErrNilSession = errors.New("contextsession: session must not be nil")
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
// its dependencies guard their own state, and Plan holds no
// mutable state of its own between calls.
type Planner struct {
	store   *contextstate.MemStore
	spooler *memory.Spool
}

// NewPlanner builds a Planner over store, the durable payload source,
// and spooler, an optional durable overflow target. A nil store returns
// ErrNilStore. A nil spooler is valid: Plan never calls Spool.Spool,
// and behaves exactly as it does with a wired spooler that never gets
// used, byte for byte.
func NewPlanner(store *contextstate.MemStore, spooler *memory.Spool) (*Planner, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	return &Planner{store: store, spooler: spooler}, nil
}

// planState holds running admission state for one Plan execution,
// preserving thread safety.
type planState struct {
	ctx        context.Context
	e          provider.TokenEstimator
	spooler    *memory.Spool
	budget     int
	messages   []provider.Message
	elisions   []Elision
	overBudget bool
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
func (p *Planner) Plan(ctx context.Context, sess *contextstate.Session, w contextplan.Window, e provider.TokenEstimator) (PlanResult, error) {
	if sess == nil {
		return PlanResult{}, ErrNilSession
	}
	if err := w.Validate(); err != nil {
		return PlanResult{}, err
	}
	budget := w.Budget()

	state := &planState{
		ctx:     ctx,
		e:       e,
		spooler: p.spooler,
		budget:  budget,
	}

	for i := len(sess.Source) - 1; i >= 0; i-- {
		event := sess.Source[i]
		record, err := p.resolvePayload(event)
		if err != nil {
			if errors.Is(err, contextstate.ErrPayloadRevoked) {
				state.elisions = append(state.elisions, Elision{Ref: record.Ref, Reason: ElisionReasonRevoked})
				continue
			}
			return PlanResult{}, err
		}
		if IsReasoningEvent(event) {
			state.elisions = append(state.elisions, Elision{Ref: record.Ref, Reason: ElisionReasonReasoningRedacted})
			continue
		}
		state.admit(event, record)
	}

	final, err := e.EstimateTokens(provider.Request{Messages: state.messages})
	if err != nil {
		final = 0
	}
	return PlanResult{
		Request:         provider.Request{Messages: state.messages},
		Elisions:        state.elisions,
		EstimatedTokens: final,
	}, nil
}

// admit decides whether event's payload enters messages: a full
// insertion while budget allows it, a stub for a RetentionCompliance
// payload once budget is spent, or a drop. An estimator error on a
// trial insertion is treated as "does not fit," never a Plan-level
// failure; only a payload-resolution failure fails Plan. A non-nil
// spooler receives the full record.Data for the two budget-driven
// drop paths, best-effort: a memory.Spool error leaves the returned
// Elision's SpoolRef empty and never fails admit.
func (s *planState) admit(event contextstate.SourceEvent, record contextstate.PayloadRecord) {
	if !s.overBudget {
		candidate := prepend(s.messages, event.Role, record.Data)
		if tokens, err := s.e.EstimateTokens(provider.Request{Messages: candidate}); err == nil && tokens <= s.budget {
			s.messages = candidate
			return
		}
		s.overBudget = true
	}
	if record.Retention == contextstate.RetentionCompliance {
		stub := StubContent(record.Data)
		candidate := prepend(s.messages, event.Role, stub)
		if tokens, err := s.e.EstimateTokens(provider.Request{Messages: candidate}); err == nil && tokens <= s.budget {
			s.elisions = append(s.elisions, Elision{
				Ref:      record.Ref,
				Reason:   ElisionReasonRetentionExpired,
				Kept:     len(stub),
				SpoolRef: spoolRecord(s.ctx, s.spooler, record),
			})
			s.messages = candidate
			return
		}
	}
	s.elisions = append(s.elisions, Elision{
		Ref:      record.Ref,
		Reason:   ElisionReasonWindowOverflow,
		SpoolRef: spoolRecord(s.ctx, s.spooler, record),
	})
}

// spoolRecord writes record.Data to spooler under record.Ref.SubjectID
// and returns the reference. A nil spooler or a Spool.Spool error
// returns an empty string; the caller never fails on either.
func spoolRecord(ctx context.Context, spooler *memory.Spool, record contextstate.PayloadRecord) string {
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
// issued between two Plan calls is visible on the very next call.
// On contextstate.ErrPayloadRevoked, it makes one more
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
	return record, nil
}
