package ledger_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// admitSpec names one key and the needs it declares. The order of a
// case's admitSpec list is the admission order, so a spec naming a
// need admitted later reads that need as absent at its own admission.
// That order is how a case builds an escapee: a record left
// StatusPending whose ancestor is already blocked or failed.
type admitSpec struct {
	key   ledger.IdempotencyKey
	needs []ledger.IdempotencyKey
}

// ancestorCase is one row of TestClaimRejectsBlockingAncestor.
type ancestorCase struct {
	name          string
	completed     []ledger.IdempotencyKey
	failed        []ledger.IdempotencyKey
	admits        []admitSpec
	claim         ledger.IdempotencyKey
	wantErr       error
	wantStatus    machine.Status
	wantBlockedBy ledger.IdempotencyKey
	wantOther     map[ledger.IdempotencyKey]machine.Status
}

// buildTerminal admits, claims, and completes key at status.
func buildTerminal(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, status machine.Status) {
	t.Helper()
	mustAdmit(t, l, ctx, key, 1)
	fence := mustClaim(t, l, ctx, key, "owner-setup")
	if err := l.Complete(ctx, testActor, key, "owner-setup", fence, status, fixedNow); err != nil {
		t.Fatalf("Complete(%s, %s): %v", key, status, err)
	}
}

// ancestorCases returns the row set TestClaimRejectsBlockingAncestor
// drives. It lives beside the test rather than inside it, so the table
// can grow without pushing the test function past the length limit.
func ancestorCases() []ancestorCase {
	return []ancestorCase{
		{
			name:          "chain of two hops",
			failed:        []ledger.IdempotencyKey{"A"},
			admits:        []admitSpec{{"C", []ledger.IdempotencyKey{"B"}}, {"B", []ledger.IdempotencyKey{"A"}}},
			claim:         "C",
			wantErr:       ledger.ErrNotClaimed,
			wantStatus:    ledger.StatusBlocked,
			wantBlockedBy: "B",
		},
		{
			name:   "chain of three hops",
			failed: []ledger.IdempotencyKey{"A"},
			admits: []admitSpec{
				{"D", []ledger.IdempotencyKey{"C"}},
				{"C", []ledger.IdempotencyKey{"B"}},
				{"B", []ledger.IdempotencyKey{"A"}},
			},
			claim:         "D",
			wantErr:       ledger.ErrNotClaimed,
			wantStatus:    ledger.StatusBlocked,
			wantBlockedBy: "B",
			wantOther:     map[ledger.IdempotencyKey]machine.Status{"C": ledger.StatusPending},
		},
		{
			name:   "diamond with two paths to one leaf",
			failed: []ledger.IdempotencyKey{"A"},
			admits: []admitSpec{
				{"L", []ledger.IdempotencyKey{"X", "Y"}},
				{"X", []ledger.IdempotencyKey{"B"}},
				{"Y", []ledger.IdempotencyKey{"B"}},
				{"B", []ledger.IdempotencyKey{"A"}},
			},
			claim:         "L",
			wantErr:       ledger.ErrNotClaimed,
			wantStatus:    ledger.StatusBlocked,
			wantBlockedBy: "B",
		},
		{
			name:       "two-node cycle with no failure",
			admits:     []admitSpec{{"P", []ledger.IdempotencyKey{"Q"}}, {"Q", []ledger.IdempotencyKey{"P"}}},
			claim:      "P",
			wantStatus: ledger.StatusClaimed,
		},
		{
			name:       "self-need with no failure",
			admits:     []admitSpec{{"S", []ledger.IdempotencyKey{"S"}}},
			claim:      "S",
			wantStatus: ledger.StatusClaimed,
		},
		{
			name:      "completed sibling beside a blocked need",
			completed: []ledger.IdempotencyKey{"Sib"},
			failed:    []ledger.IdempotencyKey{"A"},
			admits: []admitSpec{
				{"L", []ledger.IdempotencyKey{"Sib", "B"}},
				{"B", []ledger.IdempotencyKey{"A"}},
			},
			claim:         "L",
			wantErr:       ledger.ErrNotClaimed,
			wantStatus:    ledger.StatusBlocked,
			wantBlockedBy: "B",
			wantOther:     map[ledger.IdempotencyKey]machine.Status{"Sib": ledger.StatusCompleted},
		},
		{
			name:       "need never admitted",
			admits:     []admitSpec{{"N", []ledger.IdempotencyKey{"ghost"}}},
			claim:      "N",
			wantStatus: ledger.StatusClaimed,
		},
	}
}

// TestClaimRejectsBlockingAncestor drives the transitive Needs walk
// Claim runs before it grants. Each row builds a graph, claims one
// key, and pins the result, the record's status after the attempt, and
// its BlockedBy. A walk that reads only direct needs fails the
// three-hop and diamond rows. A walk that treats StatusCompleted as
// blocking fails the completed-sibling row. A walk without its seen
// set never returns on the cycle and self-need rows.
func TestClaimRejectsBlockingAncestor(t *testing.T) {
	for _, tc := range ancestorCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			l := newLedger(t, nil)
			for _, k := range tc.completed {
				buildTerminal(t, l, ctx, k, ledger.StatusCompleted)
			}
			for _, k := range tc.failed {
				buildTerminal(t, l, ctx, k, ledger.StatusFailed)
			}
			for _, spec := range tc.admits {
				mustAdmit(t, l, ctx, spec.key, 1, spec.needs...)
			}
			_, err := l.Claim(ctx, testActor, tc.claim, "owner-claim", fixedLease, fixedNow)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Claim(%s) = %v, want nil", tc.claim, err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Claim(%s) = %v, want %v", tc.claim, err, tc.wantErr)
			}
			assertRecord(t, l, ctx, tc.claim, tc.wantStatus, tc.wantBlockedBy)
			for k, want := range tc.wantOther {
				assertStatus(t, l, ctx, k, want)
			}
		})
	}
}

// assertRecord fails the test unless key reads status and blockedBy.
func assertRecord(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, status machine.Status, blockedBy ledger.IdempotencyKey) {
	t.Helper()
	st, found, err := l.State(ctx, key)
	if err != nil {
		t.Fatalf("State(%s): %v", key, err)
	}
	if !found {
		t.Fatalf("State(%s): want found", key)
	}
	if st.Status != status {
		t.Fatalf("%s.Status = %q, want %q", key, st.Status, status)
	}
	if st.BlockedBy != blockedBy {
		t.Fatalf("%s.BlockedBy = %q, want %q", key, st.BlockedBy, blockedBy)
	}
}

// assertStatus fails the test unless key reads status.
func assertStatus(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, status machine.Status) {
	t.Helper()
	st, found, err := l.State(ctx, key)
	if err != nil {
		t.Fatalf("State(%s): %v", key, err)
	}
	if !found {
		t.Fatalf("State(%s): want found", key)
	}
	if st.Status != status {
		t.Fatalf("%s.Status = %q, want %q", key, st.Status, status)
	}
}

// TestClaimRejectsAfterBlockedAdmission drives window three at depth
// two, and it needs no store wrapper and no goroutine. C names an
// absent B, and D names C. A fails while B is still absent, so the
// failure walk finds nothing. B then admits naming failed A, so it
// inserts already StatusBlocked and walks nothing. C stays
// StatusPending, so a one-level check would grant D.
func TestClaimRejectsAfterBlockedAdmission(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "C", 1, "B")
	mustAdmit(t, l, ctx, "D", 1, "C")
	buildTerminal(t, l, ctx, "A", ledger.StatusFailed)
	mustAdmit(t, l, ctx, "B", 1, "A")

	assertRecord(t, l, ctx, "B", ledger.StatusBlocked, "A")
	assertRecord(t, l, ctx, "C", ledger.StatusPending, "")

	if _, err := l.Claim(ctx, testActor, "D", "owner-d", fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Claim(D) = %v, want ErrNotClaimed", err)
	}
	assertRecord(t, l, ctx, "D", ledger.StatusBlocked, "B")
}

// TestTakeoverRejectsBlockingAncestor pins the same rule at the second
// point of use. C claims while its need B is still absent. A then
// fails, and B admits naming A, so B inserts already StatusBlocked and
// walks nothing. C is StatusClaimed with a blocked need, so Takeover
// past the lease deadline must refuse it.
func TestTakeoverRejectsBlockingAncestor(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "C", 1, "B")
	mustClaim(t, l, ctx, "C", "owner-c")
	buildTerminal(t, l, ctx, "A", ledger.StatusFailed)
	mustAdmit(t, l, ctx, "B", 1, "A")
	assertRecord(t, l, ctx, "C", ledger.StatusClaimed, "")

	stale := fixedNow.Add(fixedLease)
	if _, err := l.Takeover(ctx, testActor, "C", "owner-d", fixedLease, stale); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Takeover(C) = %v, want ErrNotClaimed", err)
	}
	assertRecord(t, l, ctx, "C", ledger.StatusBlocked, "B")
}

// ancestorLoadFaultStore wraps a *ledger.MemStore and fails Load for
// one key. Every other call forwards to the MemStore under
// context.Background(), so the fault is the only failure a caller can
// observe.
type ancestorLoadFaultStore struct {
	base     *ledger.MemStore
	faultKey ledger.IdempotencyKey
}

func (a ancestorLoadFaultStore) Load(_ context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	if key == a.faultKey {
		return ledger.TaskState{}, false, errStoreBoom
	}
	return a.base.Load(context.Background(), key)
}

func (a ancestorLoadFaultStore) CompareAndSwap(_ context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	return a.base.CompareAndSwap(context.Background(), key, old, new)
}

func (a ancestorLoadFaultStore) Range(_ context.Context, fn func(ledger.TaskState) bool) error {
	return a.base.Range(context.Background(), fn)
}

// TestClaimAncestorWalkStoreFault proves Claim fails closed when the
// ancestor walk cannot read a need: it returns the Store error and
// leaves the record StatusPending and unclaimed.
func TestClaimAncestorWalkStoreFault(t *testing.T) {
	ctx := context.Background()
	base := ledger.NewMemStore()
	l, err := ledger.New(base, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "C", 1, "B")

	lf, err := ledger.New(ancestorLoadFaultStore{base: base, faultKey: "B"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := lf.Claim(ctx, testActor, "C", "owner-c", fixedLease, fixedNow); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Claim(C) = %v, want errStoreBoom", err)
	}
	assertRecord(t, l, ctx, "C", ledger.StatusPending, "")
	st, _, err := l.State(ctx, "C")
	if err != nil {
		t.Fatalf("State(C): %v", err)
	}
	if st.Owner != "" {
		t.Fatalf("C.Owner = %q, want empty", st.Owner)
	}
}

// ancestorCtxStore wraps a *ledger.MemStore and forwards the caller's
// context to Load for one key only. Every other call forwards under
// context.Background(), so a canceled context reaches the MemStore
// through the ancestor walk alone, never through Claim's own first
// Load.
type ancestorCtxStore struct {
	base   *ledger.MemStore
	ctxKey ledger.IdempotencyKey
}

func (a ancestorCtxStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	if key == a.ctxKey {
		return a.base.Load(ctx, key)
	}
	return a.base.Load(context.Background(), key)
}

func (a ancestorCtxStore) CompareAndSwap(_ context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	return a.base.CompareAndSwap(context.Background(), key, old, new)
}

func (a ancestorCtxStore) Range(_ context.Context, fn func(ledger.TaskState) bool) error {
	return a.base.Range(context.Background(), fn)
}

// TestClaimAncestorWalkContextCanceled proves a canceled context
// during the ancestor walk returns that error and grants nothing.
func TestClaimAncestorWalkContextCanceled(t *testing.T) {
	ctx := context.Background()
	base := ledger.NewMemStore()
	l, err := ledger.New(base, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "C", 1, "B")
	mustAdmit(t, l, ctx, "B", 1)

	lf, err := ledger.New(ancestorCtxStore{base: base, ctxKey: "B"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lf.Claim(canceled, testActor, "C", "owner-c", fixedLease, fixedNow); !errors.Is(err, context.Canceled) {
		t.Fatalf("Claim(C) = %v, want context.Canceled", err)
	}
	assertRecord(t, l, ctx, "C", ledger.StatusPending, "")
}

// TestClaimBlocksOnceEmitsOnce proves two Claim attempts against one
// escapee emit exactly one BlockedEvent for it. The second attempt
// fails on status, before the walk.
func TestClaimBlocksOnceEmitsOnce(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	var blockedC int64
	if err := bus.Subscribe(ledger.BlockedEvent, func(_ context.Context, ev events.Event) error {
		if strings.HasPrefix(ev.Data, "key C ") {
			atomic.AddInt64(&blockedC, 1)
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l := newLedger(t, bus)
	mustAdmit(t, l, ctx, "C", 1, "B")
	buildTerminal(t, l, ctx, "A", ledger.StatusFailed)
	mustAdmit(t, l, ctx, "B", 1, "A")

	for i := 0; i < 2; i++ {
		if _, err := l.Claim(ctx, testActor, "C", "owner-c", fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
			t.Fatalf("Claim(C) attempt %d = %v, want ErrNotClaimed", i+1, err)
		}
	}
	assertRecord(t, l, ctx, "C", ledger.StatusBlocked, "B")
	if got := atomic.LoadInt64(&blockedC); got != 1 {
		t.Fatalf("BlockedEvent for C fired %d times, want 1", got)
	}
}

// loadCountStore wraps a ledger.Store and counts every Load call.
type loadCountStore struct {
	ledger.Store
	loads atomic.Int64
}

func (c *loadCountStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	c.loads.Add(1)
	return c.Store.Load(ctx, key)
}

// TestClaimWalkLoadCount pins the walk's cost bound and its seen set.
// One Claim on a five-hop healthy chain loads six records: the claimed
// record plus its five ancestors, each exactly once.
func TestClaimWalkLoadCount(t *testing.T) {
	ctx := context.Background()
	store := &loadCountStore{Store: ledger.NewMemStore()}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	chain := buildKeys(6)
	for i, k := range chain {
		if i+1 < len(chain) {
			mustAdmit(t, l, ctx, k, 1, chain[i+1])
			continue
		}
		mustAdmit(t, l, ctx, k, 1)
	}

	store.loads.Store(0)
	if _, err := l.Claim(ctx, testActor, chain[0], "owner-c", fixedLease, fixedNow); err != nil {
		t.Fatalf("Claim(%s): %v", chain[0], err)
	}
	if got := store.loads.Load(); got != 6 {
		t.Fatalf("Load calls = %d, want 6", got)
	}
}
