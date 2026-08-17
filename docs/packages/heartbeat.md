# Package reference: heartbeat

The heartbeat package tracks liveness by time. A sender beats on its
own schedule. `Monitor` tracks the last beat per id and reports which
ids have gone silent past a fixed timeout. The exported surface below
mirrors `api/heartbeat.txt`.

## Types

- `Monitor` — tracks last-seen time per id against a fixed timeout.
  Safe for concurrent use. The zero value is not usable; create one
  with `New`.

## Constants

- `MissedEvent` — the event kind a caller emits after it reads a dead
  id from `Dead`. It is an `events.Name` constant.

## Functions and methods

- `New(timeout)` — creates a `Monitor` with a fixed timeout.
- `Monitor.Beat(id, at)` — records `at` as the last-seen time for
  `id`.
- `Monitor.Alive(id, now)` — reports whether `id` is live at `now`.
- `Monitor.Dead(now)` — returns the ids that have gone silent past the
  timeout.
- `Monitor.Forget(id)` — removes `id` from the tracked set.

## Sentinel errors

Use `errors.Is` to test these.

- `ErrNoTimeout` — `New` got a non-positive timeout.
- `ErrNoID` — `Beat` got a blank id, after `strings.TrimSpace`.
- `ErrStaleBeat` — `Beat` got an `at` strictly before the id's last
  recorded time.

## Invariants

- `New` rejects a non-positive timeout with `ErrNoTimeout`.
- `Beat` rejects a blank id, after trim, with `ErrNoID`.
- `Beat` rejects an `at` strictly earlier than the id's previously
  recorded time with `ErrStaleBeat`, and leaves the stored time
  unchanged. An `at` equal to or after the previous time overwrites
  it.
- `Alive` requires at least one prior beat for `id`. An id with no
  recorded beat is never alive.
- `Alive` reports true when `now.Sub(last) <= timeout`. A beat
  timestamped after `now`, from clock skew, makes the difference
  negative, which always satisfies the comparison, so the id reads as
  alive.
- `Dead` returns the sorted, defensively copied ids that have beaten
  at least once and are now past the timeout, `now.Sub(last) >
  timeout`.
- `Dead` is level-triggered and at-least-once. It returns the same id
  on every call until `Forget` or a new `Beat` changes the state.
  `Monitor` performs no internal dedup beyond the map it already
  keys by id.
- `Forget` removes `id` from the tracked set. Forgetting an id that
  was never beaten is a no-op.
- `Monitor` never emits `MissedEvent` itself. A caller reads `Dead`
  and emits the event through its own `events.Bus`.

## Why this shape

`Monitor` takes the caller's `time.Time` on every method instead of
owning a clock. This keeps a test deterministic: the test drives a
fake clock and asserts `Alive` and `Dead` at exact instants. The same
`Monitor`, with no internal clock dependency, also serves a real
caller such as `agent.Run`, which passes wall-clock time. One
implementation, two callers, no clock abstraction needed.

## Cross-references

- [room.md](room.md) — `Room.StaleMembers` takes a caller-supplied
  `Monitor` and cross-checks its dead ids against the room roster.
- [agent.md](agent.md) — `Agent.Run` takes an optional `Monitor`
  parameter and beats one id per gated step.

## Usage

```go
mon, err := heartbeat.New(30 * time.Second)
if err != nil {
    // timeout was zero or negative
}
now := time.Now()
_ = mon.Beat("worker-1", now)
alive := mon.Alive("worker-1", now.Add(10*time.Second)) // true
dead := mon.Dead(now.Add(time.Minute))                  // ["worker-1"]
mon.Forget("worker-1")
```
