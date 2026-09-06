# Plan: room

Status: shipped. The roster and admission surface below is live; see
"Addendum: maintenance batch — drop StaleMembers and the heartbeat
edge" for the membership-staleness surface this file originally
shipped and later removed.

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
Allowed imports are `envelope` alone (policy/layers.json); the
`heartbeat` edge shipped with `StaleMembers` and was dropped with it,
see "Addendum: maintenance batch — drop StaleMembers and the
heartbeat edge" below.
Accepts verifies signatures itself so callers cannot skip authentication.
The lock gains the six sentinel `var` lines when api_surface learns vars;
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

## Addendum: maintenance batch — liveness surfaces stay as documented public API

### Goal

- Decide the fate of the liveness vocabulary with no internal
  caller. Doc-only.

### Scope

- Verified: `room.StaleMembers` and `room.ErrNoMonitor`
  (`room/liveness.go:13,25`) have no callers outside room's tests.
  `heartbeat.MissedEvent` has zero users. The real consumer,
  `subagent/heartbeattool.go:63`, calls `monitor.Dead` directly.
  `policy/pending_wiring.json` says nothing about these symbols; the
  orphan gate covers packages, not exported symbols.
- Direction: keep, no wiring. Wiring is cheap import-wise, because
  the `subagent` layers row already allows `room`. But
  `HeartbeatTool` binds one monitor on purpose; adding a `*room.Room`
  parameter couples two concerns into one tool and filters `OpDead`
  output through a roster the caller may not have. Trimming would
  delete a public composition primitive this SDK exists to export.
  Both surfaces stay, as documented public API for application code.

### Addendum tests

- None. No behavior changes. The existing room tests already pin
  `StaleMembers` and `ErrNoMonitor`.

### Addendum verification

- No code, API, or policy diff. `python3 scripts/check_plan.py`,
  `scripts/check_prose.py`, and `scripts/check_labels.py` pass.

## Addendum: ErrUnsigned covers a failed signature check

Part of the maintenance addenda batch. See
docs/plans/agents/maintenance-addenda-batch.md, item 6e.

`room/room.go:33` declared `ErrUnsigned` with the text "unsigned
message cannot be admitted". `room/room.go:159` also wraps that
sentinel when a present signature does not verify. The old text denied
that second case.

New text: "message is unsigned or its signature does not verify".

The sentinel keeps its name, so no lock changes. Only the message
string changes. `docs/packages/room.md:49` quotes the old text and is
updated with the code. Three sites reference the symbol alone and need
no change.

### Addendum tests

- No new test. `room/integration_test.go:182` and `:184` match the
  sentinel with `errors.Is`, not by string, so both keep passing.
- The case "invalid payload, validly signed" now builds its message
  with a local `signBypassingValidate` helper, because
  `envelope.Sign` validates first. See docs/plans/envelope.md,
  "Addendum: Sign validates a normalized copy".

### Addendum verification

- `make verify` passes. The `room` coverage floor holds.
- No `api/` diff: the lock records symbol names, not error strings.
- No `policy/layers.json` diff.

## Addendum: maintenance batch — drop StaleMembers and the heartbeat edge

This addendum is one of three that ship in one commit. The other two
are "Addendum: maintenance batch — return an unnamed ack resolver" in
`docs/plans/a2aack.md` and "Addendum: maintenance batch — delegate the
ref-form check to contextstate" in `docs/plans/envelope.md`. Each one
removes a dependency edge that exists for one symbol.

### Addendum goal

Remove `room`'s only reason to import `heartbeat`. The `room` row in
`policy/layers.json` becomes `["envelope"]`.

### Addendum scope

This addendum reverses the decision recorded above in "Addendum:
maintenance batch — liveness surfaces stay as documented public API".
That addendum kept `StaleMembers` and `ErrNoMonitor` and priced the
alternative as wiring effort alone. It never priced the import edge.

The import edge is the cost that decides it. `room` imports
`heartbeat`, and `heartbeat` imports `events`. One function with no
caller therefore pulls two packages into the build of every consumer
of `room`. Without it `room` is a leaf over `envelope` alone.

The earlier addendum's second argument still holds and now cuts the
other way. `subagent.HeartbeatTool` binds one monitor on purpose, so
passing it a `*room.Room` would couple two concerns. That reasoning
shows the function has no internal caller today and none later.

The lost capability is small. Application code that wants the
intersection writes it over `Room.Members` and `Monitor.Dead`, both
exported, in three lines. `room` keeps every roster and admission
method.

Verified by grep before this addendum:

- `command grep -rl StaleMembers --include='*.go' .` lists four files,
  all under `room/`.
- `command grep -rln "mivia-ai-sdk/heartbeat" room/` lists the same
  four files, so `liveness.go` is the only production import site.
- The real liveness consumer is `subagent/heartbeattool.go:63`. It
  calls `monitor.Dead` and never touches `room`.

Code changes:

- Delete `room/liveness.go`. `StaleMembers` and `ErrNoMonitor` go with
  it.
- Delete `room/liveness_test.go`.
- Delete `room/liveness_integration_test.go`.
- Edit `room/bench_test.go`. Delete `buildThousandMemberRoom`,
  `BenchmarkStaleMembersThousandMembers`, and
  `TestStaleMembersAllocBudget`, at current lines 15 through 77. Drop
  the `heartbeat` and `time` imports; nothing else in the file uses
  them.
- Keep `room/bench_test.go`. `buildThousandMemberRoomWithPoster` and
  `BenchmarkAcceptsThousandMembers`, at current lines 79 through 124,
  survive unchanged. Its remaining imports are `crypto/ed25519`,
  `encoding/hex`, `fmt`, `testing`, `envelope`, and `room`.
- `baseMessage` stays in `room/integration_test.go`. The surviving
  benchmark still calls it.

Out of scope: `heartbeat` itself. It keeps every method and every
other importer.

### Addendum API

`api/room.txt` loses two lines and gains none:

```text
- func (r *Room) StaleMembers(hb *heartbeat.Monitor, now time.Time) ([]string, error)
- var ErrNoMonitor
```

`api/heartbeat.txt` does not change.

The `policy/layers.json` row change:

```text
- "room": ["envelope", "heartbeat"]
+ "room": ["envelope"]
```

No other row changes for this edge. The row narrows in the builder's
commit, not in this plan update. Narrowing it before the import is
gone fails `scripts/check_deps.py`.

### Addendum tests

No new test. The deleted tests cover only the deleted function.

`scripts/check_test_tampering.py` fires on this commit. Expected
findings:

- `TT01` for each dropped test or benchmark function:
  `TestStaleMembersNilMonitor`, `TestStaleMembersNoBeatsRecorded`,
  `TestStaleMembersMixedAliveAndStale`,
  `TestStaleMembersRosterIsSourceOfTruthForRemoval`,
  `TestStaleMembersNeverBeatIsNotStale`,
  `TestStaleMembersSortedAcrossRoles`,
  `TestStaleMembersAcceptsThenBeatPattern`,
  `TestStaleMembersDropsRemovedMemberOnNextCall`,
  `TestStaleMembersConcurrentAccess`,
  `TestStaleMembersAllocBudget`, and
  `BenchmarkStaleMembersThousandMembers`.
- `TT04` for the net decrease in assertion sites.
- `TT05` and `TT06` may also fire on the removed comparisons.
- `TT11`, the gate-infrastructure guard, fires on the whole commit.
  `_GATE_INFRA_PREFIXES` at
  `scripts/test_tampering_rules_infra.py:10` lists `policy/`, and this
  commit edits `policy/layers.json` beside real code files. The
  doc-companion exception covers only `docs/plans/` and
  `docs/packages/` markdown, so it does not apply.

The justification for every test-class finding above, `TT01` through
`TT06`, is one fact: each deleted test
asserts on `StaleMembers`, and `StaleMembers` is deleted in the same
commit. No surviving behavior loses coverage. The proposed reason text
is "removes tests for StaleMembers because the function itself is
deleted in this commit".

`TT11` takes a different trailer from the rest. It needs
`Allow-Gate-Change`, not `Allow-Test-Change`, and fifteen significant
words, not six. See `_GATE_CHANGE_MIN_WORDS` at
`scripts/test_tampering_override.py:21`. Its justification is its own:
the two narrowed `policy/layers.json` rows must ride in the same
commit as the deleted imports they described. Splitting them fails
`scripts/check_deps.py` on one side or the other. Ten of the last
three hundred commits carry `Allow-Gate-Change: TT11` for this shape.

The builder must not add an override trailer of either kind. The
builder reports the findings and stops. The orchestrator verifies the
diff and issues the trailers.

### Addendum verification

Commands:

- `make verify`.
- `python3 scripts/check_plan.py`.
- `python3 scripts/check_deps.py` passes with the narrowed `room` row.
- `python3 scripts/check_api.py` after `make api-update`.
- `python3 scripts/check_docs.py`.
- `python3 scripts/check_orphan_packages.py`.
- `python3 scripts/check_prose.py`.
- `python3 scripts/check_test_tampering.py`.

Orphan result, checked against `policy/layers.json` before this
addendum: `heartbeat` keeps `agent`, `agentrun`, `runconfig`, and
`subagent` as importers. `events` keeps all eleven of its
importers, and `room` was never one of them. Neither package becomes
an orphan. `policy/pending_wiring.json` does
not change.

Coverage: `room` and the total must stay at or above 85. The commit
deletes the covered lines and their tests together, so the ratio moves
little. Confirm the number; do not assume it.

Doc sites, all found by grep and all landing in the same commit:

- `docs/architecture.md`, mermaid map: delete the `room --> heartbeat`
  edge line.
- `docs/architecture.md`, the `room/` bullet: delete the
  `StaleMembers`, `ErrNoMonitor`, and `room` imports `heartbeat`
  sentences.
- `docs/architecture.md`, the `heartbeat/` bullet: delete the
  `room.Room.StaleMembers` clause and name the real importer set,
  `agent`, `agentrun`, `runconfig`, and `subagent`. The present text
  says `agent` and `room` both import it, which is already stale.
  Naming `agent` alone would be fresh drift, because the same file
  draws four `--> heartbeat` edges after the `room` one goes.
- `docs/packages/room.md`: delete the `Room.StaleMembers` method
  bullet, the `ErrNoMonitor` failure-mode bullet, and the
  `StaleMembers` invariant bullet.
- `docs/packages/heartbeat.md`: delete the `room.md` cross-reference
  bullet.
- `.agents/skills/test-review/SKILL.md`: the allowed-edge list says
  `room` imports `envelope` and `heartbeat`. Change it to `envelope`
  only and delete the `StaleMembers` clause.

`docs/README.md` and `AGENTS.md` name no `room` to `heartbeat` edge;
grep confirms this. Leave both unchanged.
