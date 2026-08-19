package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// stressKind names one ledger mutating operation the storm issues.
type stressKind int

const (
	stressAdmit stressKind = iota
	stressClaim
	stressRenew
	stressRelease
	stressTakeover
	stressComplete
)

// stressOp is one recorded operation: its inputs, its recorded output,
// and the invoke/return timestamps the linearizability check orders on.
type stressOp struct {
	kind     stressKind
	key      ledger.IdempotencyKey
	seq      ledger.Sequence
	owner    ledger.OwnerID
	fenceIn  ledger.FenceToken
	status   machine.Status
	now      time.Time
	lease    time.Duration
	invoke   int64
	ret      int64
	ok       bool
	fenceOut ledger.FenceToken
	err      error
}

// modelRec is the reduced, observable record the linearizability model
// tracks per key. Rev and audit fields sit outside the model: the
// ledger governs only the fields listed here.
type modelRec struct {
	status     machine.Status
	seq        ledger.Sequence
	fence      ledger.FenceToken
	owner      ledger.OwnerID
	leaseUntil time.Time
	blockedBy  ledger.IdempotencyKey
}

// modelTerminal mirrors ledger.isTerminalStatus: a finished record
// never rebases and every mutating method skips it.
func modelTerminal(s machine.Status) bool {
	return s == ledger.StatusCompleted || s == ledger.StatusFailed || s == ledger.StatusBlocked
}

// cloneModel copies the model state so one search branch cannot alias
// another.
func cloneModel(st map[ledger.IdempotencyKey]modelRec) map[ledger.IdempotencyKey]modelRec {
	out := make(map[ledger.IdempotencyKey]modelRec, len(st))
	for k, v := range st {
		out[k] = v
	}
	return out
}

// modelApply advances st by one operation and returns the result the
// ledger must report for that operation in a sequential run. It is the
// independent model the checker measures the real ledger against.
func modelApply(st map[ledger.IdempotencyKey]modelRec, op *stressOp) (bool, ledger.FenceToken, error) {
	switch op.kind {
	case stressAdmit:
		return modelAdmit(st, op)
	case stressClaim:
		return modelClaim(st, op)
	case stressRenew:
		return modelRenew(st, op)
	case stressRelease:
		return modelRelease(st, op)
	case stressTakeover:
		return modelTakeover(st, op)
	case stressComplete:
		return modelComplete(st, op)
	}
	return false, 0, nil
}

// modelAdmit mirrors ledger.Admit: a terminal record or a sequence at
// or below the stored one is a no-op; anything else rebases to pending.
func modelAdmit(st map[ledger.IdempotencyKey]modelRec, op *stressOp) (bool, ledger.FenceToken, error) {
	cur, found := st[op.key]
	if found && (modelTerminal(cur.status) || op.seq <= cur.seq) {
		return false, 0, nil
	}
	st[op.key] = modelRec{status: ledger.StatusPending, seq: op.seq}
	return true, 0, nil
}

// modelClaim mirrors ledger.Claim, returning the bumped fence on a
// successful claim.
func modelClaim(st map[ledger.IdempotencyKey]modelRec, op *stressOp) (bool, ledger.FenceToken, error) {
	if op.owner == "" {
		return false, 0, ledger.ErrEmptyOwner
	}
	cur, found := st[op.key]
	if !found {
		return false, 0, ledger.ErrNoKey
	}
	switch cur.status {
	case ledger.StatusPending:
	case ledger.StatusClaimed:
		if cur.leaseUntil.After(op.now) {
			return false, 0, ledger.ErrLeaseActive
		}
	default:
		return false, 0, ledger.ErrNotClaimed
	}
	cur.fence++
	cur.status = ledger.StatusClaimed
	cur.owner = op.owner
	cur.leaseUntil = op.now.Add(op.lease)
	st[op.key] = cur
	return true, cur.fence, nil
}

// modelTakeover mirrors ledger.Takeover: staleness is checked before
// the claimed-status precondition, unlike Claim.
func modelTakeover(st map[ledger.IdempotencyKey]modelRec, op *stressOp) (bool, ledger.FenceToken, error) {
	if op.owner == "" {
		return false, 0, ledger.ErrEmptyOwner
	}
	cur, found := st[op.key]
	if !found {
		return false, 0, ledger.ErrNoKey
	}
	if cur.leaseUntil.After(op.now) {
		return false, 0, ledger.ErrNotStale
	}
	if cur.status != ledger.StatusClaimed {
		return false, 0, ledger.ErrNotClaimed
	}
	cur.fence++
	cur.owner = op.owner
	cur.leaseUntil = op.now.Add(op.lease)
	st[op.key] = cur
	return true, cur.fence, nil
}

// modelRenew mirrors ledger.Renew, which checks the status before the
// fence.
func modelRenew(st map[ledger.IdempotencyKey]modelRec, op *stressOp) (bool, ledger.FenceToken, error) {
	cur, found := st[op.key]
	if !found {
		return false, 0, ledger.ErrNoKey
	}
	if cur.status != ledger.StatusClaimed {
		return false, 0, ledger.ErrNotClaimed
	}
	if cur.fence != op.fenceIn {
		return false, 0, ledger.ErrFenced
	}
	cur.leaseUntil = op.now.Add(op.lease)
	st[op.key] = cur
	return true, 0, nil
}

// modelRelease mirrors ledger.Release, which checks the fence before
// the status.
func modelRelease(st map[ledger.IdempotencyKey]modelRec, op *stressOp) (bool, ledger.FenceToken, error) {
	cur, found := st[op.key]
	if !found {
		return false, 0, ledger.ErrNoKey
	}
	if cur.fence != op.fenceIn {
		return false, 0, ledger.ErrFenced
	}
	if cur.status != ledger.StatusClaimed {
		return false, 0, ledger.ErrNotClaimed
	}
	cur.status = ledger.StatusPending
	cur.owner = ""
	cur.leaseUntil = time.Time{}
	st[op.key] = cur
	return true, 0, nil
}

// modelComplete mirrors ledger.Complete. The stress key set is flat, so
// a StatusFailed completion blocks no dependent.
func modelComplete(st map[ledger.IdempotencyKey]modelRec, op *stressOp) (bool, ledger.FenceToken, error) {
	if op.status != ledger.StatusCompleted && op.status != ledger.StatusFailed {
		return false, 0, ledger.ErrUnknownStatus
	}
	cur, found := st[op.key]
	if !found {
		return false, 0, ledger.ErrNoKey
	}
	if cur.fence != op.fenceIn {
		return false, 0, ledger.ErrFenced
	}
	if cur.status != ledger.StatusClaimed {
		return false, 0, ledger.ErrNotClaimed
	}
	cur.status = op.status
	st[op.key] = cur
	return true, 0, nil
}

// sameErr reports whether recorded and want are the same outcome: both
// nil, or recorded wraps want.
func sameErr(recorded, want error) bool {
	if recorded == nil || want == nil {
		return recorded == nil && want == nil
	}
	return errors.Is(recorded, want)
}

// stateString serializes the model state for memoization. Keys sort so
// equal states always produce the identical string.
func stateString(st map[ledger.IdempotencyKey]modelRec) string {
	keys := make([]string, 0, len(st))
	for k := range st {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		r := st[ledger.IdempotencyKey(k)]
		fmt.Fprintf(&b, "%s|%s|%d|%d|%s|%d|%s;",
			k, r.status, r.seq, r.fence, r.owner, r.leaseUntil.UnixNano(), r.blockedBy)
	}
	return b.String()
}

// linearizable reports whether ops yield to a legal sequential ordering
// against the model, honoring the real-time rule that one op finishing
// before another starts must come first.
func linearizable(ops []stressOp, initial map[ledger.IdempotencyKey]modelRec) bool {
	memo := make(map[string]bool)
	return searchLinear(ops, fullMask(ops), memo, cloneModel(initial))
}

// searchLinear walks the linearization search with memoization. mask
// marks the ops not yet placed.
func searchLinear(ops []stressOp, mask uint64, memo map[string]bool, st map[ledger.IdempotencyKey]modelRec) bool {
	if mask == 0 {
		return true
	}
	key := fmt.Sprintf("%d|%s", mask, stateString(st))
	if v, ok := memo[key]; ok {
		return v
	}
	for i := range ops {
		if mask&(1<<i) == 0 {
			continue
		}
		if !invokedFirst(i, ops, mask) {
			continue
		}
		next := cloneModel(st)
		ok, fence, want := modelApply(next, &ops[i])
		matched := sameErr(ops[i].err, want) && fence == ops[i].fenceOut
		if ops[i].kind == stressAdmit {
			matched = matched && ok == ops[i].ok
		}
		if !matched {
			continue
		}
		if searchLinear(ops, mask&^(1<<i), memo, next) {
			memo[key] = true
			return true
		}
	}
	memo[key] = false
	return false
}

// invokedFirst reports whether op i may be the next one placed: no
// other pending op j returned strictly before i was invoked.
func invokedFirst(i int, ops []stressOp, mask uint64) bool {
	for j := range ops {
		if j == i || mask&(1<<j) == 0 {
			continue
		}
		if ops[j].ret < ops[i].invoke {
			return false
		}
	}
	return true
}

// fullMask builds the all-ops-pending bitmask for len(ops) ops.
func fullMask(ops []stressOp) uint64 {
	var m uint64
	for i := range ops {
		m |= 1 << i
	}
	return m
}

// stressClock hands out strictly increasing timestamps and wall-clock
// values. now and invoke/return draw from one counter, so ordering pins
// the real-time relation between timestamps.
type stressClock struct {
	counter int64
	tick    time.Duration
	base    time.Time
}

// now returns the next wall-clock instant.
func (c *stressClock) now() time.Time {
	return c.base.Add(c.tick * time.Duration(atomic.AddInt64(&c.counter, 1)))
}

// stamp returns the next ordering timestamp.
func (c *stressClock) stamp() int64 {
	return atomic.AddInt64(&c.counter, 1)
}

// runStorm drives goroutines goroutines each issuing opsEach mutations
// over l against keys, returning every recorded operation.
func runStorm(ctx context.Context, l *ledger.Ledger, keys []ledger.IdempotencyKey, goroutines, opsEach int, seed int64) []stressOp {
	clk := &stressClock{base: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), tick: time.Millisecond}
	ops := make([]stressOp, goroutines*opsEach)
	ready := make(chan struct{})
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(gi int) {
			defer func() { done <- struct{}{} }()
			<-ready
			rng := rand.New(rand.NewSource(seed + int64(gi)))
			owner := ledger.OwnerID(fmt.Sprintf("owner-%d", gi))
			fences := make(map[ledger.IdempotencyKey]ledger.FenceToken)
			actor := testActor
			for j := 0; j < opsEach; j++ {
				op := &ops[gi*opsEach+j]
				op.kind = stressKind(rng.Intn(6))
				op.key = keys[rng.Intn(len(keys))]
				op.owner = owner
				op.now = clk.now()
				op.lease = time.Duration(1+rng.Intn(5)) * clk.tick
				op.invoke = clk.stamp()
				switch op.kind {
				case stressAdmit:
					op.seq = 2
					op.ok, op.err = l.Admit(ctx, actor, op.key, op.seq, nil, op.now)
				case stressClaim:
					op.fenceOut, op.err = l.Claim(ctx, actor, op.key, op.owner, op.lease, op.now)
					if op.err == nil {
						fences[op.key] = op.fenceOut
					}
				case stressRenew:
					op.fenceIn = fences[op.key]
					op.err = l.Renew(ctx, actor, op.key, op.owner, op.fenceIn, op.lease, op.now)
				case stressRelease:
					op.fenceIn = fences[op.key]
					op.err = l.Release(ctx, actor, op.key, op.owner, op.fenceIn, op.now)
				case stressTakeover:
					op.fenceOut, op.err = l.Takeover(ctx, actor, op.key, op.owner, op.lease, op.now)
					if op.err == nil {
						fences[op.key] = op.fenceOut
					}
				case stressComplete:
					op.fenceIn = fences[op.key]
					op.status = ledger.StatusCompleted
					if rng.Intn(2) == 0 {
						op.status = ledger.StatusFailed
					}
					op.err = l.Complete(ctx, actor, op.key, op.owner, op.fenceIn, op.status, op.now)
				}
				op.ret = clk.stamp()
			}
		}(i)
	}
	close(ready)
	for i := 0; i < goroutines; i++ {
		<-done
	}
	return ops
}

// initialModel builds the model state matching mustAdmit for each key.
func initialModel(keys []ledger.IdempotencyKey) map[ledger.IdempotencyKey]modelRec {
	st := make(map[ledger.IdempotencyKey]modelRec, len(keys))
	for _, k := range keys {
		st[k] = modelRec{status: ledger.StatusPending, seq: 1}
	}
	return st
}

// sentinelErrs is every ledger sentinel a storm op may return.
var sentinelErrs = []error{
	ledger.ErrLeaseActive,
	ledger.ErrFenced,
	ledger.ErrNotStale,
	ledger.ErrNotClaimed,
	ledger.ErrNoKey,
	ledger.ErrUnknownStatus,
	ledger.ErrEmptyOwner,
}

// isSentinel reports whether err is nil or one of the sentinels.
func isSentinel(err error) bool {
	if err == nil {
		return true
	}
	for _, s := range sentinelErrs {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// TestLedgerStressLinearizable races every mutating operation across a
// shared key space, records the full history, and checks the history
// linearizes against an independent model. Run under go test -race.
func TestLedgerStressLinearizable(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	keys := []ledger.IdempotencyKey{"k0", "k1", "k2"}
	for _, k := range keys {
		mustAdmit(t, l, ctx, k, 1)
	}
	ops := runStorm(ctx, l, keys, 6, 5, 1)
	for i := range ops {
		if !isSentinel(ops[i].err) {
			t.Fatalf("op %d (%d on %s) returned unexpected error %v", i, ops[i].kind, ops[i].key, ops[i].err)
		}
	}
	if !linearizable(ops, initialModel(keys)) {
		t.Fatalf("recorded history of %d ops is not linearizable", len(ops))
	}
}

// TestLedgerStressRecordsStayValid drives a larger storm and asserts
// every final record still satisfies TaskState.Validate, so no racy
// interleaving left a structurally invalid record behind. Run under
// go test -race.
func TestLedgerStressRecordsStayValid(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	keys := []ledger.IdempotencyKey{"k0", "k1", "k2", "k3"}
	for _, k := range keys {
		mustAdmit(t, l, ctx, k, 1)
	}
	ops := runStorm(ctx, l, keys, 8, 12, 2)
	for i := range ops {
		if !isSentinel(ops[i].err) {
			t.Fatalf("op %d (%d on %s) returned unexpected error %v", i, ops[i].kind, ops[i].key, ops[i].err)
		}
	}
	for _, k := range keys {
		st, found, err := l.State(ctx, k)
		if err != nil || !found {
			t.Fatalf("State(%s): found=%v err=%v", k, found, err)
		}
		if err := st.Validate(); err != nil {
			t.Fatalf("State(%s) = %+v fails Validate: %v", k, st, err)
		}
	}
}
