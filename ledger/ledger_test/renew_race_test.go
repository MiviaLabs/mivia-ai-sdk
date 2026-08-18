package ledger_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// barrierStore wraps a ledger.Store, counts CompareAndSwap calls, and,
// once armed, blocks the first two Load callers until both have
// called Load. This forces two racing callers to read the identical
// starting record before either writes, guaranteeing a genuine
// CompareAndSwap collision instead of relying on incidental timing.
type barrierStore struct {
	ledger.Store
	casCalls  int64
	armed     int32
	loadsSeen int64
	gate      chan struct{}
}

func newBarrierStore(s ledger.Store) *barrierStore {
	return &barrierStore{Store: s, gate: make(chan struct{})}
}

// arm resets the barrier and enables Load gating for the next two calls.
func (b *barrierStore) arm() {
	b.gate = make(chan struct{})
	atomic.StoreInt64(&b.loadsSeen, 0)
	atomic.StoreInt32(&b.armed, 1)
}

func (b *barrierStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	v, ok, err := b.Store.Load(ctx, key)
	if atomic.LoadInt32(&b.armed) == 1 {
		if atomic.AddInt64(&b.loadsSeen, 1) == 2 {
			close(b.gate)
		}
		<-b.gate
	}
	return v, ok, err
}

func (b *barrierStore) CompareAndSwap(ctx context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	atomic.AddInt64(&b.casCalls, 1)
	return b.Store.CompareAndSwap(ctx, key, old, new)
}

// TestRenewRaceBothSucceedWithRetry admits and claims a key with a
// short lease, then races two goroutines both calling Renew with the
// same owner and fence. Both calls return nil: the Rev counter makes
// one goroutine's first CompareAndSwap attempt fail even though its
// old carries the identical (Sequence, Status, Fence) triple, proving
// the conflict is detected; the retry-and-reclassify contract then
// makes that goroutine retry until it succeeds. RenewedEvent fires
// exactly twice. The wrapped Store's CompareAndSwap count exceeds
// two, proving at least one retry happened. Run under go test -race.
func TestRenewRaceBothSucceedWithRetry(t *testing.T) {
	ctx := context.Background()
	store := newBarrierStore(ledger.NewMemStore())
	bus := events.New()
	var renewed int64
	if err := bus.Subscribe(ledger.RenewedEvent, func(_ context.Context, _ events.Event) error {
		atomic.AddInt64(&renewed, 1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l, err := ledger.New(store, bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")

	beforeRace := atomic.LoadInt64(&store.casCalls)
	store.arm()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = l.Renew(ctx, "k1", "owner-a", fence, fixedLease, fixedNow)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Renew %d: %v", i, err)
		}
	}
	if atomic.LoadInt64(&renewed) != 2 {
		t.Fatalf("RenewedEvent fired %d times, want 2", renewed)
	}
	// The barrier forces both goroutines to read the same starting
	// record, so one CompareAndSwap must lose and retry: at least
	// three calls total for the race (two initial, one retry).
	raceCalls := atomic.LoadInt64(&store.casCalls) - beforeRace
	if raceCalls <= 2 {
		t.Fatalf("CompareAndSwap called %d times during the race, want more than 2", raceCalls)
	}
}
