package ledger

import (
	"context"
	"fmt"
	"sort"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Complete accepts only StatusCompleted or StatusFailed. It returns
// ErrUnknownStatus when status is neither, checked first, before any
// Store call: an invalid status argument is a caller error
// independent of the record's state.
//
// Once status is valid, Complete calls Store.Load. It returns
// ErrNoKey when the key has no record, checked next, before the
// ErrFenced and ErrNotClaimed checks and before any CompareAndSwap
// call. It returns ErrFenced on a stale token. It returns
// ErrNotClaimed when the record's Status is not StatusClaimed,
// including a record Complete already moved to a terminal status: a
// second call against a terminal record never mutates it, even when
// the fence still matches.
//
// On StatusFailed, Complete walks the dependency graph and sets
// StatusBlocked, with BlockedBy set to the failed key, on every
// record that transitively names it in Needs. See blockDependents.
func (l *Ledger) Complete(ctx context.Context, key IdempotencyKey, owner OwnerID, fence FenceToken, status machine.Status) error {
	if status != StatusCompleted && status != StatusFailed {
		return ErrUnknownStatus
	}
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
		next.Status = status
		ok, err := l.store.CompareAndSwap(ctx, key, cur, next)
		if err != nil {
			return err
		}
		if ok {
			l.emit(ctx, CompletedEvent, fmt.Sprintf("key %s completed as %s by %s", key, status, owner))
			if status == StatusFailed {
				return l.blockDependents(ctx, key)
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// blockDependents walks the dependency graph transitively from a
// failed key and sets StatusBlocked on every dependent. It touches
// Store in exactly two passes: one Range call that copies every
// record into an in-memory list, then one CompareAndSwap per affected
// key. Between the two passes, an in-memory step computes transitive
// Needs membership from the list; it makes no further Store calls. A
// key whose loaded record already carries StatusBlocked is skipped,
// so an earlier failure's BlockedBy is never overwritten.
func (l *Ledger) blockDependents(ctx context.Context, failed IdempotencyKey) error {
	var list []TaskState
	if err := l.store.Range(ctx, func(t TaskState) bool {
		list = append(list, t)
		return true
	}); err != nil {
		return err
	}
	dependents := transitiveDependents(list, failed)
	byKey := make(map[IdempotencyKey]TaskState, len(list))
	for _, t := range list {
		byKey[t.Key] = t
	}
	for _, k := range dependents {
		cur, ok := byKey[k]
		if !ok || cur.Status == StatusBlocked {
			continue
		}
		next := cur
		next.Status = StatusBlocked
		next.BlockedBy = failed
		ok2, err := l.store.CompareAndSwap(ctx, k, cur, next)
		if err != nil {
			return err
		}
		if ok2 {
			l.emit(ctx, BlockedEvent, fmt.Sprintf("key %s blocked by %s", k, failed))
		}
	}
	return nil
}

// transitiveDependents scans list for every key that transitively
// names failed, or a key already found to depend on failed, in
// Needs. bad is the matching-target set, seeded with failed; blocked
// is the write-target set, empty until a record's Needs genuinely
// matches bad. A key already in blocked also joins bad, so a longer
// chain can match it. The scan repeats until a pass finds no new key,
// so a cycle across two or more records terminates instead of
// looping. An ordinary tree-shaped Needs graph never routes back to
// failed, so failed is absent from the result; a genuine cycle (for
// example A.Needs contains B, B.Needs contains A) routes back to
// failed through a real Needs edge, and failed joins blocked exactly
// like any other affected key. The returned slice is sorted for
// deterministic iteration.
func transitiveDependents(list []TaskState, failed IdempotencyKey) []IdempotencyKey {
	bad := map[IdempotencyKey]bool{failed: true}
	blocked := map[IdempotencyKey]bool{}
	for {
		added := false
		for _, t := range list {
			if blocked[t.Key] {
				continue
			}
			for _, n := range t.Needs {
				if bad[n] {
					blocked[t.Key] = true
					bad[t.Key] = true
					added = true
					break
				}
			}
		}
		if !added {
			break
		}
	}
	out := make([]IdempotencyKey, 0, len(blocked))
	for k := range blocked {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
