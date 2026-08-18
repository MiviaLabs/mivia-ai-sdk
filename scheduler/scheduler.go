package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// Job is a caller-supplied invocable: an agent run, a flow run, a
// tool call, or any closure. scheduler ships no implementation and
// imports no package whose type a Job might wrap.
type Job func(ctx context.Context) error

// Sentinel errors for Scheduler operations; test with errors.Is.
var (
	// ErrBlankID is Add's error for a blank id (empty after
	// strings.TrimSpace).
	ErrBlankID = errors.New("scheduler: id must not be blank")
	// ErrNilSchedule is Add's error for a nil Schedule.
	ErrNilSchedule = errors.New("scheduler: schedule must not be nil")
	// ErrNilJob is Add's error for a nil Job.
	ErrNilJob = errors.New("scheduler: job must not be nil")
	// ErrDuplicateID is Add's error for an id already registered.
	ErrDuplicateID = errors.New("scheduler: id already registered")
)

// entry is one registered job and its next fire time. pending marks
// an entry whose first Next call has not yet run on Run's loop
// goroutine; Run computes it on the next iteration so every Next call
// funnels through that one goroutine, never through Add's caller.
type entry struct {
	sched   Schedule
	job     Job
	next    time.Time
	pending bool
}

// Scheduler holds a job set. Built only through New. Safe for
// concurrent Add, Remove, and Run; a mutex guards the job map,
// matching tools.Registry's concurrency shape.
type Scheduler struct {
	mu      sync.Mutex
	entries map[string]*entry
	wake    chan struct{}
}

// New creates an empty Scheduler.
func New() *Scheduler {
	return &Scheduler{
		entries: make(map[string]*entry),
		wake:    make(chan struct{}, 1),
	}
}

// Add registers job under id, to fire at each time sched.Next
// reports. Add itself never calls sched.Next: it registers the entry
// pending, and Run's own loop goroutine computes the first Next(time.Now())
// the next time that loop wakes, so every Next call on sched funnels
// through Run's single goroutine, even when a caller shares one
// stateful Schedule value across multiple Add calls. Rejects a blank
// id (empty after strings.TrimSpace) with ErrBlankID, a nil sched
// with ErrNilSchedule, a nil job with ErrNilJob, and a duplicate id
// with ErrDuplicateID. Add called while Run is blocked in its sleep
// wakes the loop early through a non-blocking send on the wake
// channel, outside the mutex-held critical section.
func (s *Scheduler) Add(id string, sched Schedule, job Job) error {
	if strings.TrimSpace(id) == "" {
		return ErrBlankID
	}
	if sched == nil {
		return ErrNilSchedule
	}
	if job == nil {
		return ErrNilJob
	}

	s.mu.Lock()
	if _, ok := s.entries[id]; ok {
		s.mu.Unlock()
		return ErrDuplicateID
	}
	s.entries[id] = &entry{sched: sched, job: job, pending: true}
	s.mu.Unlock()

	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// Remove removes id. Returns whether id was present, matching
// tools.Registry.Remove's exact contract: removing an absent id is
// not a fault.
func (s *Scheduler) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return false
	}
	delete(s.entries, id)
	return true
}
