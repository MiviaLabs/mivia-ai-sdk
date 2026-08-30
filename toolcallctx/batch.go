package toolcallctx

import (
	"context"
	"sort"
	"sync"
)

// BatchOrder is the per-turn dispatch ledger an agent loop publishes to the
// tools it runs. The dispatched set is the exact list of provider tool-call
// indices the loop hands to workers, fixed serially BEFORE any worker
// starts; Settle marks one index finished for ANY reason - the tool ran to
// completion, the call was rejected before the tools layer saw it, or the
// batch aborted before the call was claimed. The publishing loop guarantees
// every dispatched index settles exactly once, and that a call whose tool
// DID run settles only after the tool returned.
//
// A tool that orders shared per-turn work by call index can therefore wait
// exactly: a dispatched, unsettled predecessor is either running or not yet
// scheduled - never a permanent hole - so no grace timer is needed to tell
// a scheduling gap from a skipped call.
type BatchOrder struct {
	mu         sync.Mutex
	dispatched []int
	settled    map[int]bool
	changed    chan struct{}
}

// NewBatchOrder builds the ledger for one batch. dispatched is copied and
// sorted; indices absent from it are not part of the batch's contract.
func NewBatchOrder(dispatched []int) *BatchOrder {
	d := append([]int(nil), dispatched...)
	sort.Ints(d)
	return &BatchOrder{
		dispatched: d,
		settled:    make(map[int]bool, len(d)),
		changed:    make(chan struct{}),
	}
}

// Dispatched returns the sorted dispatched indices as a copy.
func (b *BatchOrder) Dispatched() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]int(nil), b.dispatched...)
}

// Settle marks index finished. Idempotent; every call past the first for
// the same index is a no-op, so defer-based settlement composes with
// explicit abort-path settlement.
func (b *BatchOrder) Settle(index int) {
	b.mu.Lock()
	if b.settled[index] {
		b.mu.Unlock()
		return
	}
	b.settled[index] = true
	ch := b.changed
	b.changed = make(chan struct{})
	b.mu.Unlock()
	close(ch)
}

// Settled reports whether index has settled.
func (b *BatchOrder) Settled(index int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.settled[index]
}

// Changed returns a channel that is closed on the next settlement after
// this call. Waiters re-fetch after each wake: the channel is swapped on
// every Settle, so one settlement wakes every current waiter exactly once.
func (b *BatchOrder) Changed() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.changed
}

// UnsettledBefore reports whether any dispatched index below limit has not
// settled yet.
func (b *BatchOrder) UnsettledBefore(limit int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range b.dispatched {
		if d >= limit {
			break
		}
		if !b.settled[d] {
			return true
		}
	}
	return false
}

type batchOrderKey struct{}

// WithBatchOrder attaches a batch's dispatch ledger to ctx.
func WithBatchOrder(ctx context.Context, order *BatchOrder) context.Context {
	return context.WithValue(ctx, batchOrderKey{}, order)
}

// BatchOrderFromContext extracts the batch dispatch ledger from ctx.
func BatchOrderFromContext(ctx context.Context) (*BatchOrder, bool) {
	if ctx == nil {
		return nil, false
	}
	val, ok := ctx.Value(batchOrderKey{}).(*BatchOrder)
	return val, ok && val != nil
}
