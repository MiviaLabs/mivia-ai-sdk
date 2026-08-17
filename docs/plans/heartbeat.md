# Plan: heartbeat

Status: planned. No phase contract yet; this is a new top-level
package. See docs/architecture.md for the module map this package
joins.

## Goal

Track liveness by time. A sender beats on its own schedule. A
receiver tracks the last beat per id and declares an id dead once its
last beat is older than a fixed timeout. Monitor is the receiver
side.

This package has no caller in this repo today. It ships now as a leaf
primitive, the same pattern discovery and identity followed before
agent.New wired them in. Unlike discovery, no phase plan in this repo
currently scopes a liveness requirement that would consume heartbeat.
Discovery shipped in phase 11 and agent.New wired it in the very next
phase, phase 12, with a real merged caller. Heartbeat has no such
scoped consumer; it is a plausible future building block for agent
execution work, once that work is scoped, not yet named in any phase
contract.

The reason to build it now is direct: the orchestrator asked for a
generic, reusable heartbeat and liveness primitive for this SDK, the
same way room and machine ship as reusable primitives ahead of any one
agent workflow that composes them. Building it now, as a small,
independently testable leaf package, keeps it ready for whichever
future phase needs liveness tracking, without forcing a speculative
dependency on an unscheduled execution loop.

## Scope

Inside: the Monitor type, a fixed per-monitor timeout, a beat record
per id, an alive check, a dead-id report, and a forget operation.

Outside: a sender or ticker abstraction, goroutine ownership, and
transport coupling. Monitor never starts a goroutine and never reads
the system clock. Every method takes the caller's time.Time. This
matches flow's checkpoint pattern: no hidden clock, so tests stay
deterministic with no sleeps.

Dead is at-least-once and level-triggered by design. It reports every
id past the timeout on every call, with no internal dedup and no
one-shot tracking. A caller that polls Dead in a loop sees the same
dead id on every poll until the caller calls Forget or a new Beat
arrives for that id. This matches the package's "no internal clock, no
hidden state beyond last-seen" principle: Monitor holds one fact per
id, the last-seen time, and derives Alive and Dead from it fresh on
every call. Deduplication of repeated dead reports is the caller's
job, not Monitor's.

Alive treats a beat timestamped ahead of `now` as alive. This is
deliberate, not a bug. `now.Sub(last)` goes negative when `at` is
later than `now`, and a negative duration is always `<= timeout`, so
a future-dated beat reads as alive. This covers clock skew between the
sender's clock and the checker's clock without extra logic; Monitor
does not try to detect or reject skew.

Outside: envelope, room, flow, and machine integration. Monitor does
not gate an envelope.Message, does not know about room roles, and
does not drive a flow.Run. A caller wires Monitor into those blocks
from the outside. This mirrors machine's split from room: machine
models states, room models membership, and neither imports the
other.

The package imports events only, for one typed event-name constant.
This matches machine/events.go importing events for MoveEvent. No
import of envelope, room, flow, or machine. Stdlib only beyond
events: errors, fmt, sort, strings, sync, time. The policy row is
`"heartbeat": ["events"]`.

## API

The surface below is the lock target. It lands in `api/heartbeat.txt`
via make api-update.

- `const MissedEvent events.Name = "heartbeat.missed"` — the event
  kind a caller emits after it observes a dead id. Heartbeat is the
  concern that owns the name, so the constant lives here, matching
  machine.MoveEvent.
- `var ErrNoTimeout` — the sentinel for a non-positive timeout passed
  to New.
- `var ErrNoID` — the sentinel for a blank id (empty after TrimSpace)
  passed to Beat.
- `var ErrStaleBeat` — the sentinel for a Beat whose `at` is before
  the id's previously recorded time. An out-of-order beat must not
  resurrect a stale id, so Beat rejects it instead of silently
  ignoring it.
- `func New(timeout time.Duration) (*Monitor, error)` — creates a
  Monitor with a fixed timeout. A non-positive timeout wraps
  ErrNoTimeout.
- `type Monitor struct` — tracks last-seen time per id against the
  fixed timeout. Mutex-guarded, safe for concurrent use. The zero
  value is not usable; create a Monitor with New. This matches
  room.Room and events.Bus.
- `func (m *Monitor) Beat(id string, at time.Time) error` — records
  `at` as the last-seen time for id. A blank id after TrimSpace wraps
  ErrNoID. An `at` strictly before the id's previously recorded time
  wraps ErrStaleBeat and leaves the stored time unchanged. An `at`
  equal to or after the previously recorded time overwrites it.
- `func (m *Monitor) Alive(id string, now time.Time) bool` — reports
  whether id has beaten at least once and `now.Sub(last) <= timeout`.
  An id with no recorded beat is never alive. A beat timestamped
  after `now` (clock skew) makes `now.Sub(last)` negative, which is
  always `<= timeout`, so the id reads as alive. This is deliberate;
  see the Scope section.
- `func (m *Monitor) Dead(now time.Time) []string` — returns the
  sorted, defensively copied ids that have beaten at least once and
  are now past the timeout. This matches room.Members's shape: sort
  then copy, no shared backing array with the internal map. Dead is
  level-triggered and at-least-once: it returns the same id on every
  call until Forget or a new Beat changes the state. Monitor performs
  no internal dedup; see the Scope section.
- `func (m *Monitor) Forget(id string)` — removes id from the
  tracked set, for a clean departure. Forgetting an id that was never
  beaten is a no-op; Forget has no error return.

ErrNoID and ErrStaleBeat stay separate sentinels. A caller that gets
ErrNoID has a bug: it built a Beat call with a blank id, and that
call should fail loud and stop, not retry. A caller that gets
ErrStaleBeat hit a benign race: two beats for the same id arrived out
of order, most likely from concurrent senders or a reordered network
path, and the caller retries past it or ignores it. The two errors
need `errors.Is` branches that do different things: one is a stop
condition, the other is a pass-through. Collapsing them into one
sentinel would force every caller to re-parse the error text to tell
the two apart. No current plan shows phase 13's execution loop
branching on this yet, but the distinction costs one extra sentinel
and pays for itself the first time a caller needs to tell a coding
bug apart from a benign race to retry past.

The expected lock content:

```text
package heartbeat
  func (m *Monitor) Alive(id string, now time.Time) (bool)
  func (m *Monitor) Beat(id string, at time.Time) (error)
  func (m *Monitor) Dead(now time.Time) ([]string)
  func (m *Monitor) Forget(id string)
  func New(timeout time.Duration) (*Monitor, error)
  type Monitor struct {
}
  const MissedEvent events.Name = "heartbeat.missed"
  var ErrNoID
  var ErrNoTimeout
  var ErrStaleBeat
```

Monitor holds no clock of its own. Every method that compares times
takes the caller's own time.Time, named `at` for a beat and `now` for
a read. This keeps the package free of a hidden dependency on
time.Now and lets a test drive the clock by hand, matching
flow.Checkpoint's phase-25 pattern.

Monitor is intentionally minimal: two primitives, record a beat and
ask who is silent. No sender, no ticker, and no MissedEvent emission
inside the package. A caller reads Dead and emits MissedEvent through
its own events.Bus; Monitor never holds a Bus and never imports more
than the Name type from events. This mirrors machine, which defines
MoveEvent but never emits it itself.

## File layout

- `heartbeat/doc.go` — package doc and file map, mirroring
  machine/doc.go's style.
- `heartbeat/monitor.go` — Monitor, New, Beat, Alive, Dead, Forget,
  and the three sentinel errors.
- `heartbeat/events.go` — the MissedEvent constant, one line,
  mirroring machine/events.go exactly.

## Tests

Test files live in `heartbeat/heartbeat_test/`:

- `monitor_test.go` — the red-green cases for New, Beat, Alive, Dead,
  and Forget, as one Test function per method: TestNew, TestBeat,
  TestAlive, TestDead, TestForget. Each is table-driven and stays
  under the 80-line function cap in scripts/check_structure.py.
  Assertions come first. The builder confirms they fail on the empty
  package, then implements the code to green.
  - TestNew: a positive timeout, a zero timeout (ErrNoTimeout), a
    negative timeout (ErrNoTimeout).
  - TestBeat: a blank id (ErrNoID), a whitespace-only id (ErrNoID), a
    fresh id, a later `at` that overwrites, an equal `at` that
    overwrites, an earlier `at` that returns ErrStaleBeat and leaves
    the last-seen time unchanged.
  - TestAlive: an id that never beat (false), within the timeout
    (true), exactly at the timeout boundary (true), just past the
    timeout (false), and a beat timestamped after the check's `now`
    (clock skew: `now` before the recorded `at`, reads as alive
    because `now.Sub(last)` is negative).
  - TestDead: no ids tracked (empty slice), a mix of alive and dead
    ids (sorted dead-only list), Dead after Forget removes a dead id,
    and Dead called twice in a row with no Beat or Forget between
    calls, asserting the same dead id appears in both results
    (level-triggered, at-least-once, no internal dedup).
  - TestForget: a tracked id, an untracked id (no-op, no panic).
- `monitor_concurrent_test.go` — two cases, each asserting a concrete
  outcome, not just the absence of a race:
  1. N goroutines call Beat for the same id with strictly increasing
     `at` values, then join. Alive and the stored last-seen time must
     reflect the final (largest) `at`, proving concurrent Beats on one
     id serialize correctly instead of merely not crashing.
  2. N goroutines each call Beat for a distinct id concurrently, some
     past the timeout and some not, then join. Dead's result set must
     equal exactly the expected set of past-timeout ids, proving the
     concurrent writes are all visible to a later read.
  Run under `go test -race`; a race or panic still fails the test, but
  the assertions above are the primary check, not a byproduct.
- `monitor_bench_test.go` — benchmark Beat and Dead over one thousand
  tracked ids. AllocsPerRun states the budget for Dead's sorted copy.
  The builder records the measured baseline in this file.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for heartbeat and for the total.
- The heartbeat row in policy/layers.json lists events only. The row
  lands with this plan, before the code.
- `api/heartbeat.txt` lands through make api-update in the same
  change as the code. The lock matches the surface in the API
  section.
- `go test -race ./heartbeat/...` passes for the concurrent test file.
- The phase adds no conformance vectors. Heartbeat carries no wire
  format; it tracks in-memory time, not envelope bytes.
- docs/architecture.md gains the heartbeat entry in the package map.
- AGENTS.md's Layout section gains a one-line heartbeat entry.
