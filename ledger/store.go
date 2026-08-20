package ledger

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Store is the pluggable record backend for TaskState rows. A
// conforming Store compares old against the stored record on
// (Sequence, Status, Fence, Rev), not on full-struct or Task-field
// equality: Task is a caller-owned any value with no defined equality
// across implementations, and a real backend keys its
// compare-and-swap off a version or sequence column, not a full-row
// comparison.
//
// CompareAndSwap with a zero-value old means insert-if-absent: it
// succeeds only when the key has no stored record yet. On every
// successful CompareAndSwap, a conforming Store sets the stored
// record's Rev to one more than the stored record's prior Rev (a
// newly inserted record starts at Rev zero), regardless of which
// other fields the write changed. This closes the blind spot a
// (Sequence, Status, Fence) compare key leaves for Renew: two
// concurrent Renew calls on the same key and fence would otherwise
// read the identical triple and both succeed, silently dropping the
// first writer's lease extension.
//
// Range supports the dependent scan and Snapshot. fn must not call
// any other Store method on the same Store from inside the callback:
// Range may hold a lock for the duration of the iteration, and a
// reentrant Load or CompareAndSwap call from within fn can deadlock
// against it. fn returns false to stop the iteration early.
type Store interface {
	Load(ctx context.Context, key IdempotencyKey) (TaskState, bool, error)
	CompareAndSwap(ctx context.Context, key IdempotencyKey, old TaskState, new TaskState) (bool, error)
	Range(ctx context.Context, fn func(TaskState) bool) error
}

// MemStore is the shipped in-memory Store, mutex-guarded. It is the
// default backend when New receives a nil Store.
//
// With a positive MemStoreOptions.MaxEntries, MemStore deletes
// records to hold its entry count near the cap. It never deletes a
// StatusClaimed record whose LeaseUntil is after MemStoreOptions.Now.
// See MemStoreOptions.MaxEntries and store_eviction.go.
//
// evictQueue is a permutation of the keys of tasks: CompareAndSwap
// appends on the insert branch only, and eviction deletes a key from
// both structures together.
type MemStore struct {
	mu         sync.Mutex
	tasks      map[IdempotencyKey]TaskState
	maxEntries int
	evictQueue []IdempotencyKey
	fenceFloor FenceToken
	now        func() time.Time
}

// MemStoreOptions configures a MemStore built through
// NewMemStoreWithOptions.
type MemStoreOptions struct {
	// MaxEntries caps the number of records MemStore holds. Zero
	// means unbounded, matching NewMemStore's existing behavior
	// exactly. A positive MaxEntries deletes a record once the entry
	// count exceeds the cap. A deleted record is gone: Load and Range
	// report found false for its key.
	//
	// Deletion has three consequences. Idempotency becomes a bounded
	// window, because Admit accepts a deleted key again and the task
	// can run a second time. A deleted failed or blocked need stops
	// blocking its dependents, because a need that is not found
	// blocks nothing. A record can be deleted between Admit and
	// Claim, or after its lease expired while its owner still works,
	// so Claim, Renew, Release, Takeover, and Complete can return
	// ErrNoKey for a key the caller admitted.
	//
	// MaxEntries bounds the records that hold no live lease. It does
	// not bound the records that hold one: a StatusClaimed record
	// whose LeaseUntil is after Now is never deleted. A caller who
	// needs a hard memory bound must bound its own concurrency. A
	// caller who needs permanent idempotency must leave MaxEntries at
	// zero or use a durable Store.
	MaxEntries int
	// Now supplies the clock eviction reads to decide whether a
	// record's lease is live. A nil Now resolves to time.Now.
	Now func() time.Time
}

// NewMemStore builds an empty, unbounded MemStore ready for use.
func NewMemStore() *MemStore {
	m, _ := NewMemStoreWithOptions(MemStoreOptions{})
	return m
}

// NewMemStoreWithOptions builds an empty MemStore honoring opts. It
// returns a wrapped ErrInvalidMaxEntries for a negative MaxEntries. A
// nil opts.Now resolves to time.Now.
func NewMemStoreWithOptions(opts MemStoreOptions) (*MemStore, error) {
	if opts.MaxEntries < 0 {
		return nil, fmt.Errorf("ledger: MemStoreOptions.MaxEntries must not be negative, got %d: %w", opts.MaxEntries, ErrInvalidMaxEntries)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &MemStore{
		tasks:      make(map[IdempotencyKey]TaskState),
		maxEntries: opts.MaxEntries,
		now:        now,
	}, nil
}

// Load returns the stored record for key. The bool reports whether a
// record exists.
func (m *MemStore) Load(ctx context.Context, key IdempotencyKey) (TaskState, bool, error) {
	if err := ctx.Err(); err != nil {
		return TaskState{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.tasks[key]
	return v, ok, nil
}

// CompareAndSwap compares old against the stored record's (Sequence,
// Status, Fence, Rev) tuple and, on a match, stores new with Rev set
// to one more than the prior stored Rev. A zero-value old against an
// absent key inserts new at Rev zero. Any other mismatch, including
// old against an absent key when old is not the zero value, fails
// with ok false and no error. An insert raises new.Fence to the
// store-wide fence floor, so a key's fence never decreases across
// deletion and re-admission.
func (m *MemStore) CompareAndSwap(ctx context.Context, key IdempotencyKey, old TaskState, new TaskState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.tasks[key]
	if !ok {
		if old.Sequence != 0 || old.Status != "" || old.Fence != 0 || old.Rev != 0 {
			return false, nil
		}
		new.Rev = 0
		if new.Fence < m.fenceFloor {
			new.Fence = m.fenceFloor
		}
		m.tasks[key] = new
		m.evictQueue = append(m.evictQueue, key)
		m.evictOverCap(key)
		return true, nil
	}
	if cur.Sequence != old.Sequence || cur.Status != old.Status || cur.Fence != old.Fence || cur.Rev != old.Rev {
		return false, nil
	}
	new.Rev = cur.Rev + 1
	m.tasks[key] = new
	m.evictOverCap(key)
	return true, nil
}

// Range calls fn once per stored record, in no defined order. It
// stops early when fn returns false. Range holds its lock for the
// duration of the call; fn must not call back into the MemStore.
func (m *MemStore) Range(ctx context.Context, fn func(TaskState) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.tasks {
		if !fn(v) {
			break
		}
	}
	return nil
}
