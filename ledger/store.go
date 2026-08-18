package ledger

import (
	"context"
	"sync"
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
type MemStore struct {
	mu    sync.Mutex
	tasks map[IdempotencyKey]TaskState
}

// NewMemStore builds an empty MemStore ready for use.
func NewMemStore() *MemStore {
	return &MemStore{tasks: make(map[IdempotencyKey]TaskState)}
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
// with ok false and no error.
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
		m.tasks[key] = new
		return true, nil
	}
	if cur.Sequence != old.Sequence || cur.Status != old.Status || cur.Fence != old.Fence || cur.Rev != old.Rev {
		return false, nil
	}
	new.Rev = cur.Rev + 1
	m.tasks[key] = new
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
