package agentloop

import (
	"context"
	"testing"
	"time"
)

func TestBatchOrderContextRoundTrip(t *testing.T) {
	order := newBatchOrder([]int{2, 0})
	ctx := withBatchOrder(context.Background(), order)
	got, ok := batchOrderFromContext(ctx)
	if !ok || got != order {
		t.Fatalf("batchOrderFromContext = %v, %v; want the attached order", got, ok)
	}
	if _, ok := batchOrderFromContext(context.Background()); ok {
		t.Fatal("empty context reported a batch order")
	}
	if _, ok := batchOrderFromContext(nil); ok {
		t.Fatal("nil context reported a batch order")
	}
}

func TestBatchOrderDispatchedIsSortedCopy(t *testing.T) {
	src := []int{3, 1, 2}
	order := newBatchOrder(src)
	src[0] = 99 // the ledger must have copied
	got := order.dispatchedList()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("dispatched = %v, want sorted copy [1 2 3]", got)
	}
	got[0] = 99
	if again := order.dispatchedList(); again[0] != 1 {
		t.Fatal("dispatched returned a shared slice")
	}
}

func TestBatchOrderSettleIsIdempotentAndWakesWaiters(t *testing.T) {
	order := newBatchOrder([]int{0, 1})
	ch := order.changeSignal()
	order.settle(0)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("settle did not wake the changed waiter")
	}
	if !order.isSettled(0) || order.isSettled(1) {
		t.Fatalf("settled state wrong: 0=%v 1=%v", order.isSettled(0), order.isSettled(1))
	}
	// A second settle of the same index must not close the fresh channel.
	fresh := order.changeSignal()
	order.settle(0)
	select {
	case <-fresh:
		t.Fatal("idempotent re-settle closed the fresh channel")
	default:
	}
}

func TestBatchOrderUnsettledBefore(t *testing.T) {
	order := newBatchOrder([]int{0, 2, 4})
	if !order.unsettledBefore(3) {
		t.Fatal("indices 0 and 2 are unsettled; unsettledBefore(3) must be true")
	}
	order.settle(0)
	order.settle(2)
	if order.unsettledBefore(3) {
		t.Fatal("all dispatched indices below 3 settled; want false")
	}
	// Index 1 is not dispatched; it must never count as outstanding.
	if order.unsettledBefore(2) {
		t.Fatal("only dispatched indices may count as outstanding")
	}
	if !order.unsettledBefore(5) {
		t.Fatal("index 4 is dispatched and unsettled")
	}
}
