package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// Ledger is the durable-task-admission handle: admission, lease
// ownership, fencing, dependency blocking, and snapshot persistence
// over one Store.
type Ledger struct {
	store Store
	bus   *events.Bus
}

// New builds a Ledger over store. A nil store defaults to
// NewMemStore. A nil bus disables events, matching the flow and
// machine emit contract.
func New(store Store, bus *events.Bus) (*Ledger, error) {
	if store == nil {
		store = NewMemStore()
	}
	return &Ledger{store: store, bus: bus}, nil
}

// emit publishes name onto the bus with data, when a bus is set. A
// missing subscriber or a bus error is silently ignored; the caller
// owns the bus and decides what to log. Emit never fails the caller.
func (l *Ledger) emit(ctx context.Context, name events.Name, data string) {
	if l.bus == nil {
		return
	}
	_ = l.bus.Emit(ctx, events.Event{Name: name, Data: data})
}

// admitEligible reports whether a submission at seq may admit or
// rebase over cur. A terminal record never rebases. A submission at
// or below the recorded sequence never re-admits.
func admitEligible(cur TaskState, seq Sequence) bool {
	return !isTerminalStatus(cur.Status) && seq > cur.Sequence
}

// Admit records a task once per key. It CAS-admits a StatusPending
// record when the key is absent or the stored sequence is lower and
// the stored Status is StatusPending or StatusClaimed. A need whose
// record already holds StatusFailed or StatusBlocked lands the new
// record StatusBlocked, with BlockedBy naming that need, so a
// dependent that arrives after its dependency's failure never
// claims: late admission blocks, like Complete's dependent scan. A
// Store fault while reading a need fails Admit; admission never
// guesses between pending and blocked. After the record inserts,
// Admit re-reads its needs and blocks the record when a need failed
// in that window; see recheckNeeds. A Store fault on that re-read
// returns the error and leaves the record StatusPending. It returns
// false, nil, not an error, when the key already holds a record at
// or above seq, or when the stored record is terminal: a duplicate or
// late-arriving submission is a no-op against a finished task, not a
// failure. On first insert, Admit sets CreatedBy and CreatedAt from
// actor and now; on a rebase over an existing non-terminal record, it
// carries CreatedBy/CreatedAt forward unchanged. Every successful
// write sets UpdatedBy to actor and UpdatedAt to now. A rebase carries
// Fence forward from the stored record unchanged, and clears Owner and
// LeaseUntil, so the next Claim bumps past a dispossessed owner's
// token.
func (l *Ledger) Admit(ctx context.Context, actor Actor, key IdempotencyKey, seq Sequence, task any, now time.Time, needs ...IdempotencyKey) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("ledger: idempotency key must not be empty")
	}
	needsCopy := append([]IdempotencyKey(nil), needs...)
	for {
		cur, found, err := l.store.Load(ctx, key)
		if err != nil {
			return false, err
		}
		var old TaskState
		createdBy := actor
		createdAt := now
		if found {
			if !admitEligible(cur, seq) {
				return false, nil
			}
			old = cur
			createdBy = cur.CreatedBy
			createdAt = cur.CreatedAt
		}
		blocker, blocked, err := l.blockingNeed(ctx, needsCopy)
		if err != nil {
			return false, err
		}
		next := TaskState{
			Key:       key,
			Status:    StatusPending,
			Sequence:  seq,
			Needs:     needsCopy,
			Task:      task,
			Fence:     old.Fence,
			CreatedBy: createdBy,
			CreatedAt: createdAt,
			UpdatedBy: actor,
			UpdatedAt: now,
		}
		if blocked {
			next.Status = StatusBlocked
			next.BlockedBy = blocker
		}
		ok, err := l.store.CompareAndSwap(ctx, key, old, next)
		if err != nil {
			return false, err
		}
		if ok {
			if blocked {
				l.emit(ctx, BlockedEvent, fmt.Sprintf("key %s blocked by %s at admission", key, blocker))
				return true, nil
			}
			blockedLate, err := l.recheckNeeds(ctx, actor, now, key, needsCopy)
			if err != nil {
				return false, err
			}
			if !blockedLate {
				l.emit(ctx, AdmittedEvent, fmt.Sprintf("key %s admitted at sequence %d", key, seq))
			}
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
	}
}

// blockingNeed returns the first need whose record already holds
// StatusFailed or StatusBlocked. A never-admitted or live need blocks
// nothing. A Store fault while reading a need returns that error, so
// Admit never guesses between pending and blocked.
func (l *Ledger) blockingNeed(ctx context.Context, needs []IdempotencyKey) (IdempotencyKey, bool, error) {
	for _, n := range needs {
		st, found, err := l.store.Load(ctx, n)
		if err != nil {
			return "", false, err
		}
		if found && (st.Status == StatusFailed || st.Status == StatusBlocked) {
			return n, true, nil
		}
	}
	return "", false, nil
}

// blockingAncestor walks the transitive Needs closure breadth-first
// from needs and returns the first key holding StatusFailed or
// StatusBlocked. A never-admitted need is skipped, matching
// blockingNeed. A Store fault or a canceled ctx returns that error, so
// Claim and Takeover fail closed and grant nothing. The seen set
// admits each key once, so a cyclic graph terminates; Admit accepts a
// self-need, so a record can name itself. The walk makes only Load
// calls and runs outside any Range callback, so it never reenters
// Store from inside Range. See "Transitive blocking" in
// docs/plans/ledger.md.
func (l *Ledger) blockingAncestor(ctx context.Context, needs []IdempotencyKey) (IdempotencyKey, bool, error) {
	seen := make(map[IdempotencyKey]bool, len(needs))
	queue := make([]IdempotencyKey, 0, len(needs))
	enqueue := func(keys []IdempotencyKey) {
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				queue = append(queue, k)
			}
		}
	}
	enqueue(needs)
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		st, found, err := l.store.Load(ctx, k)
		if err != nil {
			return "", false, err
		}
		if !found {
			continue
		}
		if st.Status == StatusFailed || st.Status == StatusBlocked {
			return k, true, nil
		}
		enqueue(st.Needs)
	}
	return "", false, nil
}

// recheckNeeds closes the race between a record's own insert and a
// need's failure walk. blockingNeed read every need before the insert
// landed, so a need that failed in that window was missed. A fresh
// read that finds a failed or blocked need blocks the inserted record
// through blockOne, which emits BlockedEvent. A fresh read that finds
// every need live does not prove this record's own dependents are
// safe, because blockOne walks nothing; Claim's blockingAncestor check
// is the backstop for them. It returns whether it blocked the record.
func (l *Ledger) recheckNeeds(ctx context.Context, actor Actor, now time.Time, key IdempotencyKey, needs []IdempotencyKey) (bool, error) {
	blocker, blocked, err := l.blockingNeed(ctx, needs)
	if err != nil {
		return false, err
	}
	if !blocked {
		return false, nil
	}
	cur, found, err := l.store.Load(ctx, key)
	if err != nil {
		return false, err
	}
	if !found || isTerminalStatus(cur.Status) {
		return false, nil
	}
	return true, l.blockOne(ctx, actor, now, key, cur, blocker)
}

// State returns the current record for key. The bool is a found
// signal: true when key has a record, false when it does not. State
// never returns an error for a missing key; only Load failing against
// the Store returns an error.
func (l *Ledger) State(ctx context.Context, key IdempotencyKey) (TaskState, bool, error) {
	return l.store.Load(ctx, key)
}

// Blocked returns the blocking ancestor when key's status is
// StatusBlocked. The bool means "key is currently blocked"; it is
// false both for a never-admitted key and for an admitted, unblocked
// key. A caller who needs to tell those two apart calls State first.
func (l *Ledger) Blocked(ctx context.Context, key IdempotencyKey) (IdempotencyKey, bool, error) {
	cur, found, err := l.store.Load(ctx, key)
	if err != nil {
		return "", false, err
	}
	if !found || cur.Status != StatusBlocked {
		return "", false, nil
	}
	return cur.BlockedBy, true, nil
}
