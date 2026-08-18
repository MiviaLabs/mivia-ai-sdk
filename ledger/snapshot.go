package ledger

import (
	"context"
	"fmt"
)

// Snapshot is a point-in-time copy of every record in a Store.
type Snapshot struct {
	Tasks []TaskState
}

// Snapshot gathers a point-in-time copy of every record in Store
// through Range.
func (l *Ledger) Snapshot(ctx context.Context) (Snapshot, error) {
	var tasks []TaskState
	if err := l.store.Range(ctx, func(t TaskState) bool {
		tasks = append(tasks, t)
		return true
	}); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Tasks: tasks}, nil
}

// Validate runs TaskState.Validate over every entry.
func (s Snapshot) Validate() error {
	for i, t := range s.Tasks {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("ledger: snapshot entry %d: %w", i, err)
		}
	}
	return nil
}

// Restore inserts every Snapshot record into Store through
// CompareAndSwap. It is meant for MemStore cold-start or test setup.
// It returns an error the first time a key already has a record; a
// caller restoring into a fresh Ledger never hits this path.
func (l *Ledger) Restore(ctx context.Context, s Snapshot) error {
	for _, t := range s.Tasks {
		ok, err := l.store.CompareAndSwap(ctx, t.Key, TaskState{}, t)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("ledger: restore: key %q already has a record", t.Key)
		}
	}
	return nil
}
