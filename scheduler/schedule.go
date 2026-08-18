package scheduler

import (
	"sort"
	"time"
)

// Schedule reports the next fire time strictly after after. A
// Schedule with no further firing returns the zero time.Time; Run
// reads a zero result as "this entry never fires again" and stops
// scheduling it, without an error. Run calls Next only from its own
// single loop goroutine; an implementation need not guard against
// concurrent Next calls.
type Schedule interface {
	Next(after time.Time) time.Time
}

// everySchedule is the Schedule Every returns.
type everySchedule struct {
	d time.Duration
}

// Next returns after.Add(d).
func (e everySchedule) Next(after time.Time) time.Time {
	return after.Add(e.d)
}

// neverSchedule is the Schedule a non-positive Every duration
// returns: Next always reports the zero time.Time, so Scheduler.Add
// and Scheduler.Run treat it as an entry that never fires. See
// Every's doc comment for why Every does not panic in this build.
type neverSchedule struct{}

// Next always returns the zero time.Time.
func (neverSchedule) Next(after time.Time) time.Time {
	return time.Time{}
}

// Every builds a fixed-interval Schedule. Next(after) returns
// after.Add(d). A non-positive d has no sane firing rate; Every
// returns a Schedule whose Next always reports the zero time.Time,
// so the entry never fires. The plan for this package modeled Every
// on the standard library's time.NewTicker, which panics on a
// non-positive duration, but this module's packages return errors and
// never panic (see semgrep/sdk-standards.yml's
// sdk.go.no-panic-in-packages rule); Every keeps its plan-locked,
// error-free signature and encodes the same "reject this input"
// intent as an inert Schedule instead of a panic.
func Every(d time.Duration) Schedule {
	if d <= 0 {
		return neverSchedule{}
	}
	return everySchedule{d: d}
}

// atSchedule is the Schedule At returns. times is sorted ascending.
type atSchedule struct {
	times []time.Time
}

// Next returns the earliest entry in times strictly after after, or
// the zero time.Time once every entry is spent.
func (a atSchedule) Next(after time.Time) time.Time {
	for _, t := range a.times {
		if t.After(after) {
			return t
		}
	}
	return time.Time{}
}

// At builds a fixed, one-shot Schedule from times. At copies times
// and sorts the copy, so caller mutation of the input slice cannot
// change the schedule.
func At(times ...time.Time) Schedule {
	cp := make([]time.Time, len(times))
	copy(cp, times)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Before(cp[j]) })
	return atSchedule{times: cp}
}
