package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// Run blocks, firing each registered job at its next scheduled time,
// until ctx is canceled or expires. Returns ctx.Err() on that exit.
// Run fires each due job in its own goroutine and waits for every
// in-flight goroutine to finish before it returns, so Run never leaks
// a goroutine past its own return. A nil bus means no event emits;
// see JobFailedEvent. Run is safe to call once per Scheduler value at
// a time. A second concurrent Run call on the same Scheduler is
// caller error and is not defended against, matching flow.Run's own
// single-caller assumption.
func (s *Scheduler) Run(ctx context.Context, bus *events.Bus) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		s.computePending()
		timer, timerC := s.sleepTimer()

		select {
		case <-ctx.Done():
			stopTimer(timer)
			return ctx.Err()
		case <-s.wake:
			stopTimer(timer)
		case <-timerC:
			s.fireDue(ctx, bus, &wg)
		}
	}
}

// computePending runs sched.Next(time.Now()) for every entry Add left
// pending, on Run's own loop goroutine, and clears pending. This is
// the only place a freshly Added entry's Schedule.Next runs, so a
// Schedule shared across Add calls never sees Next called from two
// goroutines at once.
func (s *Scheduler) computePending() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if !e.pending {
			continue
		}
		e.next = e.sched.Next(now)
		e.pending = false
	}
}

// sleepTimer builds a time.Timer that fires at the earliest
// registered entry's next time, or returns a nil timer and a nil
// channel when no entry is scheduled. Run's select never reads a nil
// channel, so the timer branch simply never fires while the job set
// is empty.
func (s *Scheduler) sleepTimer() (*time.Timer, <-chan time.Time) {
	earliest, ok := s.earliestNext()
	if !ok {
		return nil, nil
	}
	wait := time.Until(earliest)
	if wait < 0 {
		wait = 0
	}
	timer := time.NewTimer(wait)
	return timer, timer.C
}

// stopTimer stops timer, tolerating a nil timer from an empty job set.
func stopTimer(timer *time.Timer) {
	if timer != nil {
		timer.Stop()
	}
}

// earliestNext returns the earliest non-zero next fire time across
// every registered entry, and whether any such entry exists.
func (s *Scheduler) earliestNext() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var earliest time.Time
	found := false
	for _, e := range s.entries {
		if e.next.IsZero() {
			continue
		}
		if !found || e.next.Before(earliest) {
			earliest = e.next
			found = true
		}
	}
	return earliest, found
}

// dueJob is one entry's id and Job, collected by fireDue for firing
// outside the entries-map lock.
type dueJob struct {
	id  string
	job Job
}

// fireDue collects every entry due at the current time, recomputes
// each one's next fire time through sched.Next(now) before releasing
// the lock, and drops an entry whose recomputed next is the zero
// time. It then fires each due job in its own goroutine, tracked by
// wg, so a slow Job never blocks the scheduling loop. The locked
// collection runs in an anonymous function with defer Unlock so a
// caller-supplied Schedule.Next panic cannot leak the mutex.
func (s *Scheduler) fireDue(ctx context.Context, bus *events.Bus, wg *sync.WaitGroup) {
	now := time.Now()

	due := func() []dueJob {
		s.mu.Lock()
		defer s.mu.Unlock()
		due := make([]dueJob, 0)
		for id, e := range s.entries {
			if e.next.IsZero() || e.next.After(now) {
				continue
			}
			due = append(due, dueJob{id: id, job: e.job})
			next := e.sched.Next(now)
			if next.IsZero() {
				delete(s.entries, id)
			} else {
				e.next = next
			}
		}
		return due
	}()

	for _, d := range due {
		wg.Add(1)
		go func(id string, job Job) {
			defer wg.Done()
			if err := job(ctx); err != nil {
				emitJobFailed(ctx, bus, id, err)
			}
		}(d.id, d.job)
	}
}

// emitJobFailed emits JobFailedEvent onto bus. It silently skips the
// emit for a nil bus, matching flow.emitStep's precedent: the caller
// owns the bus and decides what to log.
func emitJobFailed(ctx context.Context, bus *events.Bus, id string, err error) {
	if bus == nil {
		return
	}
	_ = bus.Emit(ctx, events.Event{
		Name: JobFailedEvent,
		Data: fmt.Sprintf("job %s failed: %v", id, err),
	})
}
