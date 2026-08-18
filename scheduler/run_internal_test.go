package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
)

// panicSchedule panics every time Next is called.
type panicSchedule struct{}

func (panicSchedule) Next(time.Time) time.Time {
	panic("schedule panic")
}

// TestFireDueSchedulePanicReleasesLock proves that if a caller-supplied
// Schedule.Next panics inside fireDue's locked collection, the mutex is
// still released. Without the deferred Unlock, this test deadlocks.
func TestFireDueSchedulePanicReleasesLock(t *testing.T) {
	s := New()
	s.entries["panic"] = &entry{
		sched: panicSchedule{},
		job:   func(context.Context) error { return nil },
		next:  time.Now().Add(-time.Second),
	}

	var wg sync.WaitGroup
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from Schedule.Next, got none")
		}
		// The fix is proven if the mutex can be locked again after
		// the panic. A leaked lock would make TryLock fail.
		if !s.mu.TryLock() {
			t.Fatal("mutex is still locked after Schedule.Next panic")
		}
		s.mu.Unlock()
	}()

	wg.Add(1)
	s.fireDue(context.Background(), nil, &wg)
}
