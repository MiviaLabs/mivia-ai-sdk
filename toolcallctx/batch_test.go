package toolcallctx_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/toolcallctx"
)

func TestBatchOrderContextRoundTrip(t *testing.T) {
	order := toolcallctx.NewBatchOrder([]int{2, 0})
	ctx := toolcallctx.WithBatchOrder(context.Background(), order)
	got, ok := toolcallctx.BatchOrderFromContext(ctx)
	if !ok || got != order {
		t.Fatalf("BatchOrderFromContext = %v, %v; want the attached order", got, ok)
	}
	if _, ok := toolcallctx.BatchOrderFromContext(context.Background()); ok {
		t.Fatal("empty context reported a batch order")
	}
	if _, ok := toolcallctx.BatchOrderFromContext(nil); ok {
		t.Fatal("nil context reported a batch order")
	}
}

func TestBatchOrderDispatchedIsSortedCopy(t *testing.T) {
	src := []int{3, 1, 2}
	order := toolcallctx.NewBatchOrder(src)
	src[0] = 99 // the ledger must have copied
	got := order.Dispatched()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("Dispatched = %v, want sorted copy [1 2 3]", got)
	}
	got[0] = 99
	if again := order.Dispatched(); again[0] != 1 {
		t.Fatal("Dispatched returned a shared slice")
	}
}

func TestBatchOrderSettleIsIdempotentAndWakesWaiters(t *testing.T) {
	order := toolcallctx.NewBatchOrder([]int{0, 1})
	ch := order.Changed()
	order.Settle(0)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Settle did not wake the Changed waiter")
	}
	if !order.Settled(0) || order.Settled(1) {
		t.Fatalf("settled state wrong: 0=%v 1=%v", order.Settled(0), order.Settled(1))
	}
	// A second settle of the same index must not close the fresh channel.
	fresh := order.Changed()
	order.Settle(0)
	select {
	case <-fresh:
		t.Fatal("idempotent re-settle closed the fresh channel")
	default:
	}
}

func TestBatchOrderUnsettledBefore(t *testing.T) {
	order := toolcallctx.NewBatchOrder([]int{0, 2, 4})
	if !order.UnsettledBefore(3) {
		t.Fatal("indices 0 and 2 are unsettled; UnsettledBefore(3) must be true")
	}
	order.Settle(0)
	order.Settle(2)
	if order.UnsettledBefore(3) {
		t.Fatal("all dispatched indices below 3 settled; want false")
	}
	// Index 1 is not dispatched; it must never count as outstanding.
	if order.UnsettledBefore(2) {
		t.Fatal("only dispatched indices may count as outstanding")
	}
	if !order.UnsettledBefore(5) {
		t.Fatal("index 4 is dispatched and unsettled")
	}
}
