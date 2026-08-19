// Package spool stores oversized content under a principal-scoped
// grant and hands the caller a bounded view plus a reference. See
// docs/plans/spool.md.
package spool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// maxViewBytes bounds the view Spool.Spool returns for a direct
// caller. SpoolTool applies its own caller-chosen maxBytes instead;
// this constant only governs Spool.Spool's own truncation.
const maxViewBytes = 4096

// Sentinel errors for Spool and SpoolTool; test with errors.Is.
var (
	// ErrUnknownRef is Load's error for a ref with no live grant, and
	// for a live grant whose ContentStore.Get fails.
	ErrUnknownRef = errors.New("spool: unknown ref")
	// ErrWrongPrincipal is Load's error when principal does not match
	// the grant's recorded principal.
	ErrWrongPrincipal = errors.New("spool: wrong principal")
	// ErrNoPrincipal is SpoolTool's error for a ctx with no principal
	// attached, when the inner result needs a grant.
	ErrNoPrincipal = errors.New("spool: no principal in context")
	// ErrNoBudget is NewSpool's error for a non-positive maxGrantBytes.
	ErrNoBudget = errors.New("spool: maxGrantBytes must be positive")
	// ErrGrantTooLarge is Spool's error when data alone exceeds
	// maxGrantBytes: no eviction can ever make room for it.
	ErrGrantTooLarge = errors.New("spool: content exceeds grant budget")
	// ErrPrincipalConflict is Spool's error when the store returns a
	// ref already granted to a different principal. A content-addressed
	// ContentStore returns the same ref for identical bytes regardless
	// of caller; without this check, a second principal spooling the
	// same content would silently take over the first principal's
	// grant.
	ErrPrincipalConflict = errors.New("spool: ref already granted to a different principal")
	// ErrExpired is Load's error for a grant whose expiry passed. The
	// grant drops on that Load, freeing its budget.
	ErrExpired = errors.New("spool: grant expired")
	// ErrInvalidExpiry is SpoolExpiring's error for a non-positive ttl.
	ErrInvalidExpiry = errors.New("spool: ttl must be positive")
	// ErrNilSpool is ReadOutputTool's error for a nil Spool.
	ErrNilSpool = errors.New("spool: spool is required")
	// ErrInvalidLimit is ReadOutputTool's error for a non-positive
	// maxPageBytes.
	ErrInvalidLimit = errors.New("spool: maxPageBytes must be positive")
	// ErrBadArguments is ReadOutputTool's error for a malformed
	// argument decode, a mistyped Run call, a negative offset, or a
	// negative limit.
	ErrBadArguments = errors.New("spool: bad arguments")
)

// ContentStore is the storage a Spool writes spooled bytes to and
// reads them back from. memory.Store satisfies this interface with no
// import needed on either side; a caller wires the two together.
type ContentStore interface {
	Put(content []byte) (ref string, err error)
	Get(ref string) ([]byte, error)
}

// grant records which principal may read back one ref's content, the
// content's byte size for budget bookkeeping, and an optional expiry.
// A zero expires means no expiry.
type grant struct {
	principal string
	size      int
	expires   time.Time
}

// expired reports whether g's expiry passed. A zero expiry never
// expires.
func (g grant) expired(now time.Time) bool {
	return !g.expires.IsZero() && !g.expires.After(now)
}

// Spool stores oversized content under a principal-scoped grant and
// returns a bounded view plus a reference to the full content. The
// zero value is not usable; create a Spool with NewSpool.
// Mutex-guarded, safe for concurrent use.
type Spool struct {
	mu            sync.Mutex
	store         ContentStore
	maxGrantBytes int
	total         int
	grants        map[string]grant
	order         []string
}

// NewSpool creates a Spool backed by store, tracking grants under a
// maxGrantBytes budget. A non-positive maxGrantBytes wraps
// ErrNoBudget.
func NewSpool(store ContentStore, maxGrantBytes int) (*Spool, error) {
	if maxGrantBytes <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrNoBudget, maxGrantBytes)
	}
	return &Spool{
		store:         store,
		maxGrantBytes: maxGrantBytes,
		grants:        make(map[string]grant),
	}, nil
}

// Spool writes data to the underlying store, grants principal the
// right to read it back, and returns a bounded view of data plus the
// content's reference. Spool evicts the oldest grants, by insertion
// order, until the new grant fits the byte budget. Spool wraps
// ErrGrantTooLarge when data alone exceeds maxGrantBytes, and
// ErrPrincipalConflict when the store's ref already belongs to a
// different principal's live grant.
func (s *Spool) Spool(ctx context.Context, principal string, data []byte) (view string, ref string, err error) {
	if len(data) > s.maxGrantBytes {
		return "", "", fmt.Errorf("%w: %d bytes exceeds budget %d", ErrGrantTooLarge, len(data), s.maxGrantBytes)
	}

	ref, err = s.store.Put(data)
	if err != nil {
		return "", "", err
	}

	s.mu.Lock()
	err = s.recordGrant(ref, principal, len(data), time.Time{})
	s.mu.Unlock()
	if err != nil {
		return "", "", err
	}

	return buildView(data, maxViewBytes, ref), ref, nil
}

// SpoolExpiring writes data, grants principal read-back, and sets a
// time-to-live on the grant. A non-positive ttl wraps ErrInvalidExpiry
// before any store write. Re-spooling an existing ref under the same
// principal refreshes the expiry, the same way Spool refreshes
// insertion order.
func (s *Spool) SpoolExpiring(ctx context.Context, principal string, data []byte, ttl time.Duration) (view string, ref string, err error) {
	if ttl <= 0 {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidExpiry, ttl)
	}
	if len(data) > s.maxGrantBytes {
		return "", "", fmt.Errorf("%w: %d bytes exceeds budget %d", ErrGrantTooLarge, len(data), s.maxGrantBytes)
	}

	ref, err = s.store.Put(data)
	if err != nil {
		return "", "", err
	}

	s.mu.Lock()
	err = s.recordGrant(ref, principal, len(data), time.Now().Add(ttl))
	s.mu.Unlock()
	if err != nil {
		return "", "", err
	}

	return buildView(data, maxViewBytes, ref), ref, nil
}

// Expire marks one live grant expired immediately. Unknown ref wraps
// ErrUnknownRef.
func (s *Spool) Expire(ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[ref]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownRef, ref)
	}
	g.expires = time.Now()
	s.grants[ref] = g
	return nil
}

// GrantExpiry reports a grant's expiry. The second return is false
// for no live grant. A zero time means no expiry.
func (s *Spool) GrantExpiry(ref string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[ref]
	if !ok {
		return time.Time{}, false
	}
	return g.expires, true
}

// recordGrant registers ref under principal and evicts the oldest
// grants, in insertion order, until the budget fits the new grant. It
// wraps ErrPrincipalConflict, leaving state unchanged, when ref
// already has a live grant for a different principal. Called with
// s.mu held.
func (s *Spool) recordGrant(ref, principal string, size int, expires time.Time) error {
	if existing, ok := s.grants[ref]; ok {
		if existing.principal != principal {
			return fmt.Errorf("%w: %s", ErrPrincipalConflict, ref)
		}
		s.removeFromOrder(ref)
		s.total -= existing.size
		delete(s.grants, ref)
	}
	for s.total+size > s.maxGrantBytes && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		s.total -= s.grants[oldest].size
		delete(s.grants, oldest)
	}
	s.grants[ref] = grant{principal: principal, size: size, expires: expires}
	s.order = append(s.order, ref)
	s.total += size
	return nil
}

// dropGrantLocked removes one grant, freeing its budget. Called with
// s.mu held.
func (s *Spool) dropGrantLocked(ref string) {
	g, ok := s.grants[ref]
	if !ok {
		return
	}
	s.removeFromOrder(ref)
	s.total -= g.size
	delete(s.grants, ref)
}

// removeFromOrder drops ref from the insertion-order slice. Called
// with s.mu held.
func (s *Spool) removeFromOrder(ref string) {
	for i, r := range s.order {
		if r == ref {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

// Load returns the full bytes stored under ref. It wraps
// ErrUnknownRef when no live grant matches ref, ErrWrongPrincipal
// when principal does not match the grant's recorded principal, even
// on an expired grant, and ErrExpired when the right principal's
// grant expired: that grant drops on this Load, freeing its budget.
// When the grant is live but the underlying ContentStore.Get fails
// (for example, the store's own independent budget evicted the blob
// first), Load wraps that error under ErrUnknownRef too: a live grant
// whose bytes are gone is, from the caller's view, an unknown ref.
func (s *Spool) Load(ctx context.Context, principal, ref string) ([]byte, error) {
	s.mu.Lock()
	g, ok := s.grants[ref]
	if ok {
		if g.principal != principal {
			s.mu.Unlock()
			return nil, fmt.Errorf("%w: %s", ErrWrongPrincipal, ref)
		}
		if g.expired(time.Now()) {
			s.dropGrantLocked(ref)
			s.mu.Unlock()
			return nil, fmt.Errorf("%w: %s", ErrExpired, ref)
		}
	}
	s.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRef, ref)
	}

	data, err := s.store.Get(ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnknownRef, ref, err)
	}
	return data, nil
}

// buildView returns data truncated to maxBytes, naming ref, when data
// is longer than maxBytes; else it returns data unchanged. The
// truncated prefix passes through bytes.ToValidUTF8 with an empty
// replacement, which drops every invalid byte, not the trailing
// partial rune alone: a binary payload's view may collapse to little
// more than the marker, while Spool.Load still returns the stored
// bytes.
func buildView(data []byte, maxBytes int, ref string) string {
	if len(data) <= maxBytes {
		return string(data)
	}
	prefix := bytes.ToValidUTF8(data[:maxBytes], nil)
	return fmt.Sprintf("%s [truncated, ref=%s]", string(prefix), ref)
}
