package agentloop

import (
	"context"
	"sort"
	"sync"
)

// batchOrder is the per-turn dispatch ledger an agent loop publishes to the
// tools it runs. The dispatched set is the exact list of provider tool-call
// indices the loop hands to workers, fixed serially BEFORE any worker
// starts; settle marks one index finished for ANY reason - the tool ran to
// completion, the call was rejected before the tools layer saw it, or the
// batch aborted before the call was claimed. The publishing loop guarantees
// every dispatched index settles exactly once, and that a call whose tool
// DID run settles only after the tool returned.
//
// A tool that orders shared per-turn work by call index can therefore wait
// exactly: a dispatched, unsettled predecessor is either running or not yet
// scheduled - never a permanent hole - so no grace timer is needed to tell
// a scheduling gap from a skipped call.
type batchOrder struct {
	mu         sync.Mutex
	dispatched []int
	settled    map[int]bool
	changed    chan struct{}
}

// newBatchOrder builds the ledger for one batch. dispatched is copied and
// sorted; indices absent from it are not part of the batch's contract.
func newBatchOrder(dispatched []int) *batchOrder {
	d := append([]int(nil), dispatched...)
	sort.Ints(d)
	return &batchOrder{
		dispatched: d,
		settled:    make(map[int]bool, len(d)),
		changed:    make(chan struct{}),
	}
}

// dispatched returns the sorted dispatched indices as a copy.
func (b *batchOrder) dispatchedList() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]int(nil), b.dispatched...)
}

// settle marks index finished. Idempotent; every call past the first for
// the same index is a no-op, so defer-based settlement composes with
// explicit abort-path settlement.
func (b *batchOrder) settle(index int) {
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

// settled reports whether index has settled.
func (b *batchOrder) isSettled(index int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.settled[index]
}

// changed returns a channel that is closed on the next settlement after
// this call. Waiters re-fetch after each wake: the channel is swapped on
// every settle, so one settlement wakes every current waiter exactly once.
func (b *batchOrder) changeSignal() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.changed
}

// unsettledBefore reports whether any dispatched index below limit has not
// settled yet.
func (b *batchOrder) unsettledBefore(limit int) bool {
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

// withBatchOrder attaches a batch's dispatch ledger to ctx.
func withBatchOrder(ctx context.Context, order *batchOrder) context.Context {
	return context.WithValue(ctx, batchOrderKey{}, order)
}

// batchOrderFromContext extracts the batch dispatch ledger from ctx.
func batchOrderFromContext(ctx context.Context) (*batchOrder, bool) {
	if ctx == nil {
		return nil, false
	}
	val, ok := ctx.Value(batchOrderKey{}).(*batchOrder)
	return val, ok && val != nil
}
