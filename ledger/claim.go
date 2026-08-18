package ledger

import (
	"context"
	"fmt"
	"time"
)

// Claim claims a StatusPending record, or a StatusClaimed record
// whose LeaseUntil is at or before now. LeaseUntil versus now is the
// only staleness signal Claim reads; ledger keeps no heartbeat or
// other liveness state. Claim bumps FenceToken and sets LeaseUntil to
// now.Add(lease).
//
// Claim first calls Store.Load. It returns ErrNoKey when the key has
// no record, checked before any status precondition and before any
// CompareAndSwap call: a never-admitted key is never eligible for
// Claim. It returns ErrLeaseActive when another owner's LeaseUntil is
// still after now. A record in a terminal or blocked status is also
// ineligible; Claim reports that with ErrNotClaimed, matching
// Takeover's vocabulary for a non-claimable status.
func (l *Ledger) Claim(ctx context.Context, actor Actor, key IdempotencyKey, owner OwnerID, lease time.Duration, now time.Time) (FenceToken, error) {
	for {
		cur, found, err := l.store.Load(ctx, key)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, ErrNoKey
		}
		switch cur.Status {
		case StatusPending:
		case StatusClaimed:
			if cur.LeaseUntil.After(now) {
				return 0, ErrLeaseActive
			}
		default:
			return 0, ErrNotClaimed
		}
		fence := cur.Fence + 1
		next := cur
		next.Status = StatusClaimed
		next.Owner = owner
		next.Fence = fence
		next.LeaseUntil = now.Add(lease)
		next.UpdatedBy = actor
		next.UpdatedAt = now
		ok, err := l.store.CompareAndSwap(ctx, key, cur, next)
		if err != nil {
			return 0, err
		}
		if ok {
			l.emit(ctx, ClaimedEvent, fmt.Sprintf("key %s claimed by %s", key, owner))
			return fence, nil
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
}

// Renew extends LeaseUntil to now.Add(lease). Renew first calls
// Store.Load. It returns ErrNoKey when the key has no record, checked
// before the ErrNotClaimed and ErrFenced checks and before any
// CompareAndSwap call. It returns ErrNotClaimed when the record's
// Status is not StatusClaimed. It returns ErrFenced when fence does
// not match the current record. On a losing CompareAndSwap whose
// fresh reload still shows the caller's fence owning a StatusClaimed
// record, Renew retries; a fresh reload that fails either check
// returns the matching sentinel error instead.
func (l *Ledger) Renew(ctx context.Context, actor Actor, key IdempotencyKey, owner OwnerID, fence FenceToken, lease time.Duration, now time.Time) error {
	for {
		cur, found, err := l.store.Load(ctx, key)
		if err != nil {
			return err
		}
		if !found {
			return ErrNoKey
		}
		if cur.Status != StatusClaimed {
			return ErrNotClaimed
		}
		if cur.Fence != fence {
			return ErrFenced
		}
		next := cur
		next.LeaseUntil = now.Add(lease)
		next.UpdatedBy = actor
		next.UpdatedAt = now
		ok, err := l.store.CompareAndSwap(ctx, key, cur, next)
		if err != nil {
			return err
		}
		if ok {
			l.emit(ctx, RenewedEvent, fmt.Sprintf("key %s renewed by %s", key, owner))
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// Release returns a claimed record to StatusPending. Release first
// calls Store.Load. It returns ErrNoKey when the key has no record,
// checked before the ErrFenced and ErrNotClaimed checks and before
// any CompareAndSwap call. It returns ErrFenced on a stale token. It
// returns ErrNotClaimed when the record's Status is not
// StatusClaimed. On a losing CompareAndSwap whose fresh reload still
// shows the caller's fence owning a StatusClaimed record, Release
// retries; a fresh reload that fails either check returns the
// matching sentinel error instead.
func (l *Ledger) Release(ctx context.Context, actor Actor, key IdempotencyKey, owner OwnerID, fence FenceToken, now time.Time) error {
	for {
		cur, found, err := l.store.Load(ctx, key)
		if err != nil {
			return err
		}
		if !found {
			return ErrNoKey
		}
		if cur.Fence != fence {
			return ErrFenced
		}
		if cur.Status != StatusClaimed {
			return ErrNotClaimed
		}
		next := cur
		next.Status = StatusPending
		next.Owner = ""
		next.LeaseUntil = time.Time{}
		next.UpdatedBy = actor
		next.UpdatedAt = now
		ok, err := l.store.CompareAndSwap(ctx, key, cur, next)
		if err != nil {
			return err
		}
		if ok {
			l.emit(ctx, ReleasedEvent, fmt.Sprintf("key %s released by %s", key, owner))
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// Takeover claims a StatusClaimed record whose LeaseUntil is at or
// before now: the same staleness signal Claim reads, applied through
// the same Store.CompareAndSwap call. Takeover bumps FenceToken past
// the prior value, fencing the dispossessed owner's token.
//
// Takeover first calls Store.Load. It returns ErrNoKey when the key
// has no record, checked before the ErrNotStale and ErrNotClaimed
// checks and before any CompareAndSwap call. It returns ErrNotStale
// when LeaseUntil is still after now. It returns ErrNotClaimed for a
// StatusPending or terminal record: Takeover never admits or claims a
// never-claimed record; a caller uses Claim for that.
func (l *Ledger) Takeover(ctx context.Context, actor Actor, key IdempotencyKey, owner OwnerID, lease time.Duration, now time.Time) (FenceToken, error) {
	for {
		cur, found, err := l.store.Load(ctx, key)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, ErrNoKey
		}
		if cur.LeaseUntil.After(now) {
			return 0, ErrNotStale
		}
		if cur.Status != StatusClaimed {
			return 0, ErrNotClaimed
		}
		fence := cur.Fence + 1
		next := cur
		next.Owner = owner
		next.Fence = fence
		next.LeaseUntil = now.Add(lease)
		next.UpdatedBy = actor
		next.UpdatedAt = now
		ok, err := l.store.CompareAndSwap(ctx, key, cur, next)
		if err != nil {
			return 0, err
		}
		if ok {
			l.emit(ctx, TakenOverEvent, fmt.Sprintf("key %s taken over by %s", key, owner))
			return fence, nil
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
}
