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
// guesses between pending and blocked. It returns
// false, nil, not an error, when the key already holds a record at
// or above seq, or when the stored record is terminal: a duplicate or
// late-arriving submission is a no-op against a finished task, not a
// failure. On first insert, Admit sets CreatedBy and CreatedAt from
// actor and now; on a rebase over an existing non-terminal record, it
// carries CreatedBy/CreatedAt forward unchanged. Every successful
// write sets UpdatedBy to actor and UpdatedAt to now.
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
			l.emit(ctx, AdmittedEvent, fmt.Sprintf("key %s admitted at sequence %d", key, seq))
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
