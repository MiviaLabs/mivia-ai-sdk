package ledger

import (
	"context"
	"fmt"

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
// the stored Status is StatusPending or StatusClaimed. It returns
// false, nil, not an error, when the key already holds a record at
// or above seq, or when the stored record is terminal: a duplicate or
// late-arriving submission is a no-op against a finished task, not a
// failure.
func (l *Ledger) Admit(ctx context.Context, key IdempotencyKey, seq Sequence, task any, needs ...IdempotencyKey) (bool, error) {
	needsCopy := append([]IdempotencyKey(nil), needs...)
	for {
		cur, found, err := l.store.Load(ctx, key)
		if err != nil {
			return false, err
		}
		var old TaskState
		if found {
			if !admitEligible(cur, seq) {
				return false, nil
			}
			old = cur
		}
		next := TaskState{
			Key:      key,
			Status:   StatusPending,
			Sequence: seq,
			Needs:    needsCopy,
			Task:     task,
		}
		ok, err := l.store.CompareAndSwap(ctx, key, old, next)
		if err != nil {
			return false, err
		}
		if ok {
			l.emit(ctx, AdmittedEvent, fmt.Sprintf("key %s admitted at sequence %d", key, seq))
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
	}
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
