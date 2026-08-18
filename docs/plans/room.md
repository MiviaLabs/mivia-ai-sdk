# Plan: room

Status: shipped. The roster, admission, and membership-staleness
surface below are all live.

## Goal

Standing groups: the roster that envelope.Message.Room only names.
Membership, roles, and admission of messages.

## Scope

Inside: Room roster with moderator/member roles, moderator-gated Admit/
Remove/Promote, Leave, last-moderator protection, Accepts admission
gate (signature verification plus membership of signer and recipients).
Outside: message semantics (envelope), persistence, federation.

### Membership staleness

`Room` tracks admission only; it never tracks activity on its own.
`StaleMembers` reports current roster members that have gone silent,
backed by a caller-supplied `*heartbeat.Monitor`. It intersects the
`Monitor`'s `Dead` result with `Room`'s own roster, under `Room`'s own
lock, so a removed id never appears even if the `Monitor` still holds
a beat record for it.

`Room` does not own a `Monitor` itself. `New` takes no configuration,
and a staleness timeout is caller-specific: two rooms plausibly want
two different windows, a decision for the moderator, not `Room`. A
caller that wants tracking builds one `Monitor` and passes it to
`StaleMembers`; a caller that does not want it never builds one, and
`Room`'s existing surface stays unchanged.

`Accepts` stays a pure admission gate with no side effect. It records
no beat automatically, so it never forces a `Monitor` argument or a
nil check onto a caller who does not want tracking. A caller that
wants tracking beats explicitly after a successful `Accepts`:

```go
if err := r.Accepts(msg); err == nil {
    _ = hb.Beat(msg.Signer, time.Now())
}
```

`msg.Signer` is the same id space the roster keys by, so this beat and
`StaleMembers`'s intersection line up without translation. `Room`
gains no `Beat` passthrough method; `hb.Beat` already does the job.

The package imports `heartbeat`. The policy row is
`"room": ["envelope", "heartbeat"]`. `heartbeat`'s own row stays
`["events"]`; it gains no new import and does not import `room`.

## API

Room, Role types; New, sentinel errors; Admit/Remove/Promote/Leave/
IsMember/Members/ID/Accepts methods. Locked in `api/room.txt`.
Imports only envelope (policy/layers.json). Accepts verifies
signatures itself so callers cannot skip authentication. The lock
gains the six sentinel `var` lines when api_surface learns vars;
see gates.md; the api_surface fixes changed no symbol.

### The staleness API

- `func (r *Room) StaleMembers(hb *heartbeat.Monitor, now time.Time) ([]string, error)`
  returns the sorted, defensively copied current roster members that
  `hb.Dead(now)` also reports. A nil `hb` returns `nil` and
  `ErrNoMonitor`, checked before `r` touches its own lock.
- `var ErrNoMonitor` is the sentinel `StaleMembers` returns for a nil
  `hb`. Test with `errors.Is`, matching every other `room` sentinel.

No other exported symbol changes. The expected `api/room.txt` diff:

```text
+ func (r *Room) StaleMembers(hb *heartbeat.Monitor, now time.Time) ([]string, error)
+ var ErrNoMonitor
```

## Tests

Unit tables for every guard (non-moderator, stranger, duplicate, last
moderator), role-transition flows, and end-to-end group integration:
signed posting, attributed acks, thread chains, forgery rejection,
post-removal rejection, admission failure table.

Add a concurrent roster stress test: goroutines mix Admit, Promote,
Leave, Remove, and Accepts against one Room. Synchronize with
sync.WaitGroup; never time.Sleep. This makes `go test -race` exercise
the mutex-guarded roster instead of passing vacuously.

### The staleness tests

`room/liveness_test.go` and `room/liveness_integration_test.go`,
matching room's existing flat layout:

- `liveness_test.go` — a nil `hb` (`ErrNoMonitor`), no beats recorded
  (empty result), a mixed alive/stale current-member set, a stale id
  `Remove` already dropped from the roster (absent from the result), a
  freshly `Admit`ted member with no beat yet (absent, not stale), and a
  sorted two-member stale result across roles.
- `liveness_integration_test.go` — a real `Room`, a real `Monitor`,
  and real signed messages run through `Accepts`, paired with an
  explicit `hb.Beat` call on success, proving the recommended
  Accepts-then-Beat pattern. A second case proves a `Remove` after a
  member goes stale drops it from the next `StaleMembers` call. A
  third case mixes concurrent `Accepts`, `Beat`, `Admit`, `Remove`,
  and `Promote` calls against one shared `Room` and `Monitor`, under
  `go test -race`.

No new benchmark file; `StaleMembers` is a set intersection under an
already-benchmarked lock, with no allocation-sensitive hot path.

## Verification

`make verify` plus `go test -race ./...` for the mutex-guarded roster.

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `room` and for the total, with
  `StaleMembers`'s new lines counted in.
- The `room` row in `policy/layers.json` gains `heartbeat`. The row
  change lands with this plan update, before the code.
- `heartbeat`'s row in `policy/layers.json` stays `["events"]`; it
  gains no new import and does not import `room`.
- `api/room.txt` gains `StaleMembers` and `ErrNoMonitor`, through
  `make api-update` in the same change as the code. No other line
  changes; `api/heartbeat.txt` stays unchanged.
- `go test -race ./room/...` passes, covering the concurrent
  integration case.
- `docs/architecture.md`'s `room/` bullet and `docs/packages/room.md`
  gain `StaleMembers`, `ErrNoMonitor`, and the roster-is-source-of-
  truth invariant, in the same change as the code.
- This phase adds no conformance vector. `Room` still carries no JSON
  wire form of its own.
