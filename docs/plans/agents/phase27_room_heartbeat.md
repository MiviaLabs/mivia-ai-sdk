# Phase 27: room membership staleness

Status: ready to build. Builds on the shipped `room` package
(docs/plans/room.md) and the shipped `heartbeat` package
(docs/plans/heartbeat.md). Independent of phase 26; the two phases
touch different packages and share no code.

## Goal

`Room` tracks admission only: is an id on the roster. It never tracks
activity: has this id been seen recently. This phase adds
`Room.StaleMembers`, which reports which current roster members have
gone silent, backed by a caller-supplied `*heartbeat.Monitor`.

## Scope

Inside: one new method, `Room.StaleMembers`, and one new sentinel,
`ErrNoMonitor`. `StaleMembers` intersects a `Monitor`'s `Dead` result
with `Room`'s own roster, under `Room`'s own read lock, so a concurrent
`Remove` cannot produce a torn read.

Outside: a `Room`-owned `Monitor`. A `Beat` passthrough method. An
automatic beat inside `Accepts`. See the design sections below for
each of these three exclusions and why.

### Ownership: the caller supplies the `Monitor`; `Room` does not own one

Two shapes were possible. `Room` could own a `*heartbeat.Monitor`
internally, built inside `New`. Or `Room` could take a
`*heartbeat.Monitor` as a method argument, owned and shared by the
caller.

`Room.New(id, founder string) (*Room, error)` takes no configuration
today; every field `New` sets comes from its two arguments. Giving
`Room` an internal `Monitor` means `New` must also decide a timeout,
which is caller-specific: two different rooms plausibly want two
different staleness windows, and a moderator, not `Room`, is the
actor who should set that policy. Changing `New`'s signature to add a
timeout is also a breaking change to every existing call site, for a
feature `Room`'s existing callers may not want at all.

The caller-supplied shape avoids both problems. `Room.New` is
unchanged. A caller who wants staleness tracking builds one
`*heartbeat.Monitor`, with the timeout it wants, and passes it to
`StaleMembers`. A caller who does not want the feature never builds a
`Monitor` and never calls `StaleMembers`; `Room`'s existing surface
and every existing call site stay exactly as they are.

### Reconciliation: `Room`'s roster is the source of truth

A `Monitor` tracks whatever ids get a `Beat` call; it knows nothing
about room membership. `Room`'s roster tracks membership; it knows
nothing about activity. The two can disagree in two ways, and this
plan states the rule for each.

A member is removed from the roster, `Remove` or `Leave`, while a
shared `Monitor` still holds a beat record for that id. `Room` does
not call `hb.Forget` from `Remove` or `Leave`, because `Room` holds no
reference to the caller's `Monitor` in either method; the two stay
decoupled. `StaleMembers` still reports only current roster members,
by construction: it intersects `Dead` with `Members()`, so a removed
id never appears in its output regardless of what the `Monitor` still
holds. A caller that wants the `Monitor`'s own tracked set to shrink
alongside the roster calls `hb.Forget(id)` itself at the same call
site as `Remove` or `Leave`. This plan recommends that pairing; it
does not enforce it, because `Room` does not hold the `Monitor`
reference needed to enforce it.

A member is admitted to the roster, `Admit`, before any `Beat` call
ever names that id. `Monitor.Dead` only returns an id that "has
beaten at least once and is now past the timeout"; an id with zero
beats is absent from `Dead` entirely, the same way it is absent from
`Alive`. A brand-new member with no recorded activity is never
"stale" under this rule; it is simply unknown to the `Monitor`, which
is the correct reading. `StaleMembers` needs no special case for this;
plain set intersection already produces the right answer.

### `Accepts` stays free of an automatic beat

`Accepts` is a pure admission gate today: given a message, it reports
whether the room accepts it, with no side effect beyond the read it
already performs. Recording a beat automatically inside `Accepts`
would give a function that looks read-only a hidden write effect, and
would force every existing `Accepts` caller, including every existing
test, to either pass a `*heartbeat.Monitor` argument it may not have
or tolerate a nil-check on every call.

An explicit call costs one line for a caller that wants the tracking
and nothing for a caller that does not:

```go
if err := r.Accepts(msg); err == nil {
    _ = hb.Beat(msg.Signer, time.Now())
}
```

`msg.Signer` is already the same id space `Room`'s roster keys by, so
this beat and `StaleMembers`'s later intersection line up without any
translation. `Room` gains no `Beat` passthrough method, because
`hb.Beat` already does the whole job; a one-line wrapper with no
added logic is an indirection with no caller need behind it, which
AGENTS.md's Building blocks rule rejects.

## API

The surface below lands in `api/room.txt` via `make api-update`.

- `func (r *Room) StaleMembers(hb *heartbeat.Monitor, now time.Time) ([]string, error)`
  — returns the sorted, defensively copied list of current roster
  members that `hb.Dead(now)` also reports. A nil `hb` returns `nil`
  and `ErrNoMonitor`, checked before `r` touches its own lock. The
  method takes its own read lock around the roster read and the
  intersection, so a concurrent `Remove`, `Admit`, or `Promote` cannot
  interleave with the computed result.
- `var ErrNoMonitor` — the sentinel `StaleMembers` returns when `hb` is
  nil. Test with `errors.Is`, matching every other `room` sentinel.

No other exported symbol changes. `Accepts`, `Admit`, `Remove`,
`Leave`, `Promote`, `Members`, `IsMember`, and `ID` are unchanged.

The expected `api/room.txt` diff:

```text
+ func (r *Room) StaleMembers(hb *heartbeat.Monitor, now time.Time) ([]string, error)
+ var ErrNoMonitor
```

### Import change

`room` gains `heartbeat`. The `policy/layers.json` row becomes
`"room": ["envelope", "heartbeat"]`. `heartbeat`'s own row stays
`["events"]`; it gains no new import, and it does not import `room`.

## Tests

New test files. `room` keeps its existing flat layout (`room_test.go`,
`integration_test.go` sit directly in `room/`, not in a
`room_test/` subdirectory), so the new files match that convention:

- `room/liveness_test.go` — the red-green cases, package `room`, so
  the tests reach the unexported `members` map only through the
  exported surface. Assertions come first; the builder confirms they
  fail against the unchanged `Room`, then implements `StaleMembers` to
  green. Cases:
  - A nil `hb`: `errors.Is(err, room.ErrNoMonitor)`, and the returned
    slice is nil.
  - A `Monitor` with no beat ever recorded for any member: an empty
    slice, nil error.
  - A `Monitor` where one current member is past the timeout and one
    is within it: the returned slice names only the past-timeout
    member.
  - A `Monitor` that tracks an id `Remove` already dropped from the
    roster, past the timeout: the returned slice does not name that
    id, proving the roster, not the `Monitor`, is the source of truth
    for who counts as a member.
  - A `Monitor` that has never seen a freshly `Admit`ted member: the
    returned slice does not name that member, proving "never beat" is
    not the same as "stale."
  - Two current members past the timeout, one moderator and one
    plain member: the returned slice is sorted and names both,
    proving `StaleMembers` does not filter by role.
- `room/liveness_integration_test.go` — a real `Room` built with
  `New`, a real `*heartbeat.Monitor`, and real signed
  `envelope.Message` values run through `Accepts`, matching the
  explicit-beat pattern the Scope section recommends. Cases:
  - Admit two members, beat one of them, advance past the timeout, and
    assert `StaleMembers` names exactly the member that never beat
    again after its one beat, while the other, beaten more recently,
    is absent.
  - Remove a member after it went stale, and assert a second
    `StaleMembers` call no longer names it, proving the roster check
    re-runs on every call instead of caching a snapshot.
  - Concurrent goroutines: some call `Accepts` then `hb.Beat` on
    success, some call `Admit`, `Remove`, and `Promote`, against one
    shared `Room` and one shared `Monitor`. A final `StaleMembers` call
    returns without panic or a torn read. Synchronize with
    `sync.WaitGroup`, never `time.Sleep`, matching `room`'s existing
    concurrent roster test in `integration_test.go`. Run under
    `go test -race`.

No new benchmark file. `Room` carries no existing benchmark for
`Members` or `Accepts`, and `StaleMembers` does the same
constant-factor map-and-sort work those methods already do; adding a
benchmark for this one method alone, with no existing baseline to
compare against, would not be a measurement, only a number.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `room` and for the total, with
  `StaleMembers`'s new lines counted in.
- The `room` row in `policy/layers.json` gains `heartbeat`. The row
  change lands with this plan update, before the code.
- `heartbeat`'s row in `policy/layers.json` stays `["events"]`;
  `heartbeat` gains no new import and does not import `room`.
- `api/room.txt` gains `StaleMembers` and `ErrNoMonitor`, through
  `make api-update` in the same change as the code. No other line in
  `api/room.txt` changes. `api/heartbeat.txt` stays unchanged.
- `go test -race ./room/...` passes, covering the concurrent
  integration case.
- `docs/architecture.md`'s `room/` bullet gains one sentence
  describing `StaleMembers` and the `heartbeat` import edge, in the
  same change as the code.
- `docs/packages/room.md` gains `StaleMembers` under Functions and
  methods, `ErrNoMonitor` under Sentinel errors, and one invariant
  line stating the roster-is-source-of-truth rule, in the same change
  as the code.
- This phase adds no conformance vector. `Room` still carries no JSON
  wire form of its own; `StaleMembers` reads in-memory state only.
