package flow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sentinel errors for Monitor operations; test with errors.Is.
var (
	// ErrStaleBeat is the sentinel for a Beat whose at is before the
	// id's previously recorded time. A caller that gets ErrStaleBeat
	// hit a benign race and may retry past it or ignore it.
	ErrStaleBeat = errors.New("flow: beat is older than last recorded time")
)

// Monitor tracks last-seen time per id against a fixed timeout.
// Mutex-guarded, safe for concurrent use. The zero value is not
// usable; create a Monitor with New. Monitor holds no clock of its
// own; every method takes the caller's time.Time.
type Monitor struct {
	mu      sync.Mutex
	timeout time.Duration
	last    map[string]time.Time
}

// NewMonitor creates a Monitor with a fixed timeout. A non-positive timeout
// wraps ErrInvalidOptions.
func NewMonitor(timeout time.Duration) (*Monitor, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOptions, "Timeout: must be positive")
	}
	return &Monitor{timeout: timeout, last: make(map[string]time.Time)}, nil
}

// Beat records at as the last-seen time for id. A blank id after
// TrimSpace wraps ErrInvalidOptions. An at strictly before the id's
// previously recorded time wraps ErrStaleBeat and leaves the stored
// time unchanged. An at equal to or after the previously recorded time
// overwrites it.
func (m *Monitor) Beat(id string, at time.Time) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "ID: must not be blank")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.last[id]; ok && at.Before(prev) {
		return ErrStaleBeat
	}
	m.last[id] = at
	return nil
}

// Alive reports whether id has beaten at least once and
// now.Sub(last) <= timeout. An id with no recorded beat is never
// alive. A beat timestamped after now (clock skew) makes
// now.Sub(last) negative, which is always <= timeout, so the id
// reads as alive; this is deliberate. See docs/history/heartbeat.md.
func (m *Monitor) Alive(id string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	last, ok := m.last[id]
	if !ok {
		return false
	}
	return now.Sub(last) <= m.timeout
}

// Dead returns the sorted, defensively copied ids that have beaten at
// least once and are now past the timeout. Dead is level-triggered
// and at-least-once: it returns the same id on every call until
// Forget or a new Beat changes the state. Monitor performs no
// internal dedup.
func (m *Monitor) Dead(now time.Time) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.last))
	for id, last := range m.last {
		if now.Sub(last) > m.timeout {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Forget removes id from the tracked set, for a clean departure.
// Forgetting an id that was never beaten is a no-op.
func (m *Monitor) Forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.last, id)
}
