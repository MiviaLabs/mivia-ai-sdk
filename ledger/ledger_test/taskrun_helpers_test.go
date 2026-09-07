package ledger_test

import (
	"context"
	"errors"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// taskrunFixedNow and taskrunFixedLease give deterministic tests a shared non-wall
// clock base, matching the ledger test conventions.
var taskrunFixedNow = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

const taskrunFixedLease = time.Hour

// runOpts returns a fully valid Options set on a fixed clock, so a
// test mutates only the field it checks.
func runOpts(l *ledger.Ledger) ledger.Options {
	return ledger.Options{
		Ledger: l,
		Actor:  "actor",
		Owner:  "owner",
		Lease:  taskrunFixedLease,
		Now: func() time.Time {
			return taskrunFixedNow
		},
	}
}

// errProbe is the deterministic failure probeStore returns.
var errProbe = errors.New("probe: compare and swap failed")

// probeStore wraps a ledger.Store to record the fence of the last
// CompareAndSwap and to fail a selected one.
type probeStore struct {
	ledger.Store
	fence    ledger.FenceToken
	calls    int
	fail     int
	failLoad int
}

// CompareAndSwap records the incoming fence and, on the fail-th call,
// returns errProbe instead of touching the wrapped store.
func (s *probeStore) CompareAndSwap(ctx context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	s.calls++
	s.fence = new.Fence
	if s.fail == s.calls {
		return false, errProbe
	}
	return s.Store.CompareAndSwap(ctx, key, old, new)
}

// Load returns errProbe on the failLoad-th call, else delegates.
func (s *probeStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	if s.failLoad > 0 {
		s.failLoad--
		if s.failLoad == 0 {
			return ledger.TaskState{}, false, errProbe
		}
	}
	return s.Store.Load(ctx, key)
}
