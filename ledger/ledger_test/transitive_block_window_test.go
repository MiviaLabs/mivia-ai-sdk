package ledger_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// holdMarker is the context key one goroutine's calls carry so the
// two-stage hold store holds that goroutine's reads alone.
type holdMarker struct{}

// twoStageHoldStore wraps a ledger.Store and holds the first two Load
// calls for one key, each until its own release channel closes. It
// holds a call only when the caller's context carries holdMarker, so
// one goroutine's reads pause while another goroutine keeps running.
// loadHoldStore in admit_complete_race_test.go holds one call and has
// no such marker, so window one needs this second wrapper.
type twoStageHoldStore struct {
	ledger.Store
	holdKey ledger.IdempotencyKey
	mu      sync.Mutex
	stage   int
	entered []chan struct{}
	release []chan struct{}
}

// newTwoStageHoldStore wraps base and prepares two holds for key.
func newTwoStageHoldStore(base ledger.Store, key ledger.IdempotencyKey) *twoStageHoldStore {
	return &twoStageHoldStore{
		Store:   base,
		holdKey: key,
		entered: []chan struct{}{make(chan struct{}), make(chan struct{})},
		release: []chan struct{}{make(chan struct{}), make(chan struct{})},
	}
}

// markCtx returns a context whose Load calls the store may hold.
func markCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, holdMarker{}, true)
}

// Load forwards every call to the wrapped Store first, then holds a
// marked call for holdKey until the matching release channel closes.
func (h *twoStageHoldStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	st, found, err := h.Store.Load(ctx, key)
	if key != h.holdKey || ctx.Value(holdMarker{}) == nil {
		return st, found, err
	}
	h.mu.Lock()
	i := h.stage
	if i < len(h.entered) {
		h.stage++
	}
	h.mu.Unlock()
	if i < len(h.entered) {
		close(h.entered[i])
		<-h.release[i]
	}
	return st, found, err
}

// TestClaimRejectsAfterRecheckWindow drives window one. B's Admit call
// reads need A before A fails, and B's own recheck read is held open
// while C admits naming a still-pending B. B ends StatusBlocked
// through blockOne, which walks nothing, so C escapes as
// StatusPending. Claim(C) must refuse it. Run under go test -race.
func TestClaimRejectsAfterRecheckWindow(t *testing.T) {
	ctx := context.Background()
	store := newTwoStageHoldStore(ledger.NewMemStore(), "A")
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "A", 1)
	fenceA := mustClaim(t, l, ctx, "A", "owner-a")

	var admitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, admitErr = l.Admit(markCtx(ctx), testActor, "B", 1, nil, fixedNow, "A")
	}()

	// Stage one: B's pre-insert read of A is held, so A fails while B
	// is still absent from the store.
	<-store.entered[0]
	if err := l.Complete(ctx, testActor, "A", "owner-a", fenceA, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete(A): %v", err)
	}
	close(store.release[0])

	// Stage two: B's post-insert recheck read of A is held, so C
	// admits while B still reads StatusPending.
	<-store.entered[1]
	mustAdmit(t, l, ctx, "C", 1, "B")
	assertRecord(t, l, ctx, "B", ledger.StatusPending, "")
	close(store.release[1])
	<-done

	if admitErr != nil {
		t.Fatalf("Admit(B): %v", admitErr)
	}
	assertRecord(t, l, ctx, "B", ledger.StatusBlocked, "A")
	assertRecord(t, l, ctx, "C", ledger.StatusPending, "")

	if _, err := l.Claim(ctx, testActor, "C", "owner-c", fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Claim(C) = %v, want ErrNotClaimed", err)
	}
	assertRecord(t, l, ctx, "C", ledger.StatusBlocked, "B")
}

// TestClaimRejectsAfterSnapshotWindow drives window two. C admits
// naming B once the failure walk's Range snapshot has returned, so the
// snapshot never held C and nothing walks C after B blocks. Claim(C)
// must refuse it.
func TestClaimRejectsAfterSnapshotWindow(t *testing.T) {
	ctx := context.Background()
	store := &rangeTriggerStore{Store: ledger.NewMemStore()}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "A", 1)
	mustAdmit(t, l, ctx, "B", 1, "A")
	fenceA := mustClaim(t, l, ctx, "A", "owner-a")

	store.trigger = func() {
		mustAdmit(t, l, ctx, "C", 1, "B")
	}
	if err := l.Complete(ctx, testActor, "A", "owner-a", fenceA, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete(A): %v", err)
	}
	assertRecord(t, l, ctx, "B", ledger.StatusBlocked, "A")
	assertRecord(t, l, ctx, "C", ledger.StatusPending, "")

	if _, err := l.Claim(ctx, testActor, "C", "owner-c", fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Claim(C) = %v, want ErrNotClaimed", err)
	}
	assertRecord(t, l, ctx, "C", ledger.StatusBlocked, "B")
}
