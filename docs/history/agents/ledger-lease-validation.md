# Ledger lease validation

Closes the lease-writing hole `a7c89af` named and left open. It is the
sibling of the `Admit` gap that commit fixed.

## Goal

Make every `ledger` write path refuse two inputs. It must refuse a
lease duration at or below zero. It must refuse a record
`TaskState.Validate` rejects.

After this change `Claim`, `Renew`, and `Takeover` cannot store a
record `Snapshot.Encode` or `Restore` fails on. A lease at or below
zero closes the moment it opens, so no method grants one.

The change does not make a granted lease outlive any real clock. `now`
is caller-supplied. Neither check compares `LeaseUntil` to a second
clock. A caller that passes a stale `now` still writes an expired
lease. A caller that passes a `now` in the past can still shrink a
live lease. That is caller arithmetic, not a ledger invariant.

## Scope

Inside: `ledger/claim.go`, `ledger/errors.go`, the three lease-writing
methods, one new sentinel, one new unexported helper, two doc files,
and one new test file.

Outside: `Admit`, `Release`, `Complete`, `blockOne`, `Restore`,
`Snapshot`, the `Store` implementations, and every caller package. No
new package. No new import. No `policy/layers.json` row.

Also outside: two sentences in
`docs/history/agents/maintenance-addenda-batch.md`, at line 104 and at
line 147. Both are historical claims about a finished batch. Both were
true when that batch landed. The addendum in `docs/history/ledger.md`
supersedes them, following the convention that plan set at line 1924.

The exclusion covers those two lines only. Line 174 of the same file
is different. It reads "The two self-need rows set that field instead
of an `admits` entry or a `mustAdmit` call." That is a claim about the
present fixture, not about a past batch. The fixture shift makes it
false, so update it. See "Plan sentences that become false".

### The two candidate fixes are not equivalent

Two fixes were considered. Both are needed. Each reaches a branch the
other cannot.

Fix A mirrors `Admit`: call `next.Validate()` before
`Store.CompareAndSwap`.

Fix B validates the argument: reject a non-positive `lease` at the
method boundary.

Fix A alone is not enough. `TaskState.Validate` rejects only a *zero*
`LeaseUntil`. With a real clock and a zero lease, `now.Add(lease)`
equals `now`. `Validate` accepts that record. The claim is stale the
instant it returns.

Measured against the current tree, `Claim` at a real `now` with a zero
lease returns fence 1 and no error. `TaskState.Validate` accepts the
stored record. `Takeover` against it at the same `now` then returns
fence 2 and no error. A second `Claim` by another owner at the same
instant also succeeds and fences the first owner out. `Claim` promises
ownership until `LeaseUntil`; a zero lease makes that promise void on
return.

Fix B alone is not enough either. Its strongest counter-case is a
field carried forward from the `Store`. `next := cur` copies every
field, so a defect already in the record survives the write.

Measured: plant a record with `Status` `StatusClaimed` and `BlockedBy`
`"x"` through `Store.CompareAndSwap`, which runs no validation.
`Renew` returns nil. `Takeover` returns fence 8 and nil. The stored
record then fails `Snapshot.Encode` on the `BlockedBy` rule. A lease
argument check cannot see any of that.

A second counter-case exists but carries less weight. A `now` before
the Go zero instant with a positive lease still lands on the zero
instant. Measured: `time.Time{}.Add(-time.Hour)` is not zero, and
adding one hour to it is zero. `Admit` then `Claim` at that clock
stores a zero `LeaseUntil`. Treat this as a footnote. A clock before
year one is not a realistic input for this SDK. Do not keep Fix A on
this case alone; keep it on the carried-forward case above.

### A non-positive lease is a defect, not a capability

No caller and no test passes a non-positive lease. The lease argument
is one of `fixedLease` (one hour), `scenarioLease` (one hour),
`sqliteScenarioLease` (one hour), `ledgerLease` (one minute),
`time.Minute`, `opts.Lease`, or `op.lease`.

`op.lease` in `ledger/ledger_test/stress_test.go:354` is
`time.Duration(1+rng.Intn(5)) * clk.tick` with a one-millisecond tick,
so it is always positive.

Two callers already enforce the rule in their own boundaries.
`taskrun.Run` rejects `opts.Lease <= 0` with `ErrNoLease` at
`taskrun/taskrun.go:71`. `runconfig` rejects a non-positive ledger
lease at `runconfig/internal.go:69`.

A third site defaults rather than enforces. `dispatch/options.go:131`
permits a zero `ReplayLease`, and `resolveReplayLease` turns zero into
`DefaultReplayLease`. `dispatch` also reaches `ledger` only through
`taskrun`, so it is an indirect caller.

The rule therefore already exists twice, outside the package that owns
it, plus one defaulter. A direct `ledger` caller bypasses all three.
Move the rule into `ledger`.

### One rule, one enforcer

Put the lease rule in one unexported helper, not in three copies.
`workspace.validateLimit` at `workspace/workspace.go:97` is the same
shape: `Options.Validate` and `effectiveLimit` both call it, so the
read-bound rule has one enforcer.

## API

One new exported symbol.

```go
// ErrInvalidLease is returned by Claim, Renew, or Takeover when lease
// is not positive. A lease at or below zero closes the moment it
// opens, so the record it would write is stale on return.
var ErrInvalidLease = errors.New("ledger: lease must be positive")
```

One new unexported helper in `ledger/claim.go`. Place it above
`Claim`'s doc comment, or below `Release`. Do not place it between
`Claim`'s doc comment and `func Claim`: that breaks
`scripts/check_docs.py` with "Claim lacks doc comment".

```go
// validateLease enforces the one lease rule: a lease must be
// positive. Claim, Renew, and Takeover all call it, so the rule has
// one enforcer. See validateLimit in workspace for the same shape.
func validateLease(lease time.Duration) error {
	if lease <= 0 {
		return ErrInvalidLease
	}
	return nil
}
```

### Call sites

`Claim` (`ledger/claim.go:30`): call `validateLease(lease)` after the
`owner == ""` check and before the retry loop. Keep `ErrEmptyOwner`
first, so existing empty-owner rows stay green.

`Takeover` (`ledger/claim.go:186`): same placement, same order.

`Renew` (`ledger/claim.go:92`): call `validateLease(lease)` at the top,
before the retry loop.

All three checks run before any `Store` call. That matches
`ErrEmptyOwner` in `Claim` and `ErrUnknownStatus` in `Complete`, which
are argument checks with the same placement.

`Claim`, `Renew`, and `Takeover` each call `next.Validate()`
immediately before `l.store.CompareAndSwap`, and return that error.
`Claim` and `Takeover` return `0` with it. This mirrors `Admit` at
`ledger/ledger.go:113`.

### Public contract change

Three methods that used to succeed now return an error. State it
plainly:

- `Claim`, `Renew`, and `Takeover` return `ErrInvalidLease` for a
  lease at or below zero. They used to grant.
- `Claim`, `Renew`, and `Takeover` return a `Validate` error when the
  record they would write is invalid. They used to write it.

### Doc comments that become false

Each of the three doc comments enumerates every failure. Each
enumeration is now incomplete. Grep term: `ledger/claim.go`.

- `ledger/claim.go:9` — the `Claim` doc comment. Add `ErrInvalidLease`
  and the `Validate` refusal.
- `ledger/claim.go:83` — the `Renew` doc comment. Same two additions.
- `ledger/claim.go:169` — the `Takeover` doc comment. Same two
  additions.

### Package doc sentences that become false

Grep terms: `lease`, `Validate`, `Claim`, `Renew`, `Takeover` in
`docs/packages/ledger.md`.

- `docs/packages/ledger.md:79` — the `Ledger.Claim` entry. Add
  `ErrInvalidLease` and the `Validate` refusal.
- `docs/packages/ledger.md:86` — the `Ledger.Renew` entry. Same.
- `docs/packages/ledger.md:90` — the `Ledger.Takeover` entry. Same.
- `docs/packages/ledger.md:117` — the error list. Add an
  `ErrInvalidLease` bullet naming its message and its pinning test.
- `docs/packages/ledger.md:166` — the `TaskState.Validate` invariant
  bullet. Its last sentence reads "`Admit`, `Restore`, and
  `Snapshot.Validate` call it." That becomes false. Replace it with
  "`Admit`, `Claim`, `Renew`, `Takeover`, `Restore`, and
  `Snapshot.Validate` call it."
- `docs/packages/ledger.md:285` — the example's `Claim` error comment.
  It reads "// ErrNoKey, ErrEmptyOwner, or ErrLeaseActive". The
  enumeration is now short by one. Add `ErrInvalidLease`. An example
  that enumerates errors is a doc site like any other.

### Plan sentences that become false

Grep terms in `docs/history/`: `hole stays open`, `run no validation`,
`every production caller`, `self-need`, `plantSelfNeed`. The first
three terms cannot surface a fixture description. The last two can.

- `docs/history/ledger.md:181` — the `Claim` API entry. Add the lease
  check.
- `docs/history/ledger.md:188` — the `Renew` API entry. Add the lease
  check.
- `docs/history/ledger.md:194` — the `Takeover` API entry. Add the lease
  check.
- `docs/history/ledger.md:1238` — the `TestClaimRejectsBlockingAncestor`
  row list. It reads "a self-need where `S` names `S`". The shift
  makes that false. The row now plants `S2` naming `S2` and claims
  `S`. Reword it, and reword the sentence after it that says the row
  "plants the record straight through the `Store`".
- `docs/history/agents/maintenance-addenda-batch.md:174` — "The two
  self-need rows set that field instead of an `admits` entry or a
  `mustAdmit` call." The shift makes that false. The Claim row now
  sets `plantSelfNeeds` and an `admits` entry. The Takeover row sets
  `plantSelfNeeds` and a `before` hook calling `mustAdmit`. Reword it.
- `docs/history/ledger.md`, "That hole stays open and needs its own
  change." Already replaced by this plan change.
- `docs/history/ledger.md`, "Those three are every production caller
  after the fix." Already replaced by this plan change.

Both replacements are quoted in the `docs/history/ledger.md` addendum
"The lease write paths validate". The builder writes the code and the
remaining doc sites, not those two sentences.

### API lock diff

`api/ledger.txt` gains one line. `validateLease` is unexported and
never appears.

```diff
   var ErrFenced
+  var ErrInvalidLease
   var ErrInvalidMaxEntries
```

Run `make api-update` and commit the `api/ledger.txt` diff in the same
change.

## Tests

One new file: `ledger/ledger_test/lease_validation_test.go`.

### TestLeaseMustBePositive

Table-driven over six rows: `Claim`, `Renew`, and `Takeover`, each
with lease `0` and lease `-time.Second`.

Branch reached: the new `validateLease` call in each method. In
`Claim` and `Takeover` it sits after the `owner == ""` check; in
`Renew` it sits at the top.

Fixture trace. Each row admits `k1` at `fixedNow` and passes a
non-empty owner, so `ErrEmptyOwner` never fires first. The `Renew` and
`Takeover` rows claim `k1` first under `fixedLease`, so the record is
`StatusClaimed` and the caller holds the live fence. The guard runs
before `Store.Load`. No row therefore depends on the record's status
to reach the guard. The setup exists so the "record did not move"
assertion has something to measure.

Assertions per row: `errors.Is(err, ledger.ErrInvalidLease)`, and the
stored record is unchanged. For `Claim` the record stays
`StatusPending` with an empty `Owner`. For `Renew` and `Takeover` the
stored `LeaseUntil`, `Owner`, and `Fence` are unchanged.

Negative control: delete the `validateLease` call from any one method
and its two rows fail, because the call returns nil and the record
moves. Change `lease <= 0` to `lease < 0` and the three zero-lease
rows fail.

### TestLeaseWriteRejectsInvalidRecord

Table-driven over three rows. Each row proves the `next.Validate()`
call reaches a defect `validateLease` cannot see.

Row one, `Claim` with a pre-epoch clock. Set `preEpoch` to
`time.Time{}.Add(-time.Hour)` and `lease` to `time.Hour`. `Admit` at
`preEpoch` writes `StatusPending`, which `Validate` accepts, because
the `Owner` and `LeaseUntil` rules apply only to `StatusClaimed`.
`Claim` then finds `StatusPending`, walks no needs, passes
`validateLease`, and sets `next.LeaseUntil` to
`preEpoch.Add(time.Hour)`, which is the zero instant. `next.Status` is
`StatusClaimed`, so `Validate` hits the zero-`LeaseUntil` rule at
`ledger/task_state.go:113`.

Row two, `Renew` over a planted record. Plant `Status`
`StatusClaimed`, `Owner` `"o"`, `Fence` `7`, `LeaseUntil`
`time.Unix(100, 0)`, and `BlockedBy` `"x"` straight through
`Store.CompareAndSwap`, which runs no validation. `Renew` with fence
`7` passes the `StatusClaimed` check and the fence check, so control
reaches the swap site. `next := cur` carries `BlockedBy` forward, so
`Validate` hits the BlockedBy-outside-StatusBlocked rule at
`ledger/task_state.go:106`.

Row three, `Takeover` over the same planted record at
`time.Unix(200, 0)`, taking it for owner `"thief"`. The status check
passes. `LeaseUntil` is
`time.Unix(100, 0)`, which is not after `now`, so `ErrNotStale` does
not fire. `Needs` is empty, so the ancestor walk is clean.
`validateLease` passes. `next` carries `BlockedBy` forward, so
`Validate` rejects at the same rule.

Assertions, row one. `Claim` returns a non-nil error naming the zero
`LeaseUntil`. The record stays `StatusPending` with an empty `Owner`.
`Snapshot` followed by `Encode` succeeds.

Assertions, rows two and three. The call returns a non-nil error. The
stored record is unchanged. Row two asserts `LeaseUntil` still reads
`time.Unix(100, 0)`; `Renew` writes no other governed field. Row three
asserts `Owner` still reads `"o"`, `Fence` still reads `7`, and
`LeaseUntil` still reads `time.Unix(100, 0)`.

Do not assert `Encode` succeeds on rows two and three. The planted
record is itself `Encode`-rejecting. `Encode` returns the same
`BlockedBy` error with the fix and without it. The assertion carries
no signal, and it fails against correct code.

Measured against the current tree, all three rows succeed today. Row
one then stores a zero `LeaseUntil`, which `Encode` rejects.

Rows two and three do not make the record `Encode`-rejecting. The
plant was already `Encode`-rejecting before the call ran. What the
call does today is mutate a record it should have refused. Row two
overwrites `LeaseUntil` with `time.Unix(260, 0)`. Row three overwrites
`Owner` with `"thief"`, `Fence` with `8`, and `LeaseUntil` with
`time.Unix(260, 0)`.

Negative control, row one. Delete the `next.Validate()` call from
`Claim`. The call returns nil. The record moves to `StatusClaimed`
with a zero `LeaseUntil`, and `Encode` then fails.

Negative control, rows two and three. Delete the `next.Validate()`
call from `Renew` or from `Takeover`. The call returns nil, and the
"record is unchanged" assertion fails on the overwritten fields above.

### Existing tests whose expectation changes

Two rows, found by grepping `plantSelfNeed` across the tree. Both
plant a record that names itself in `Needs`, then claim that same key.
`next.Validate()` rejects the carried-forward `Needs`, so both rows
fail unchanged.

- `ledger/ledger_test/transitive_block_test.go:99`, the
  `"self-need with no failure"` row of `ancestorCases`.
- `ledger/ledger_test/transitive_block_takeover_test.go:67`, the
  `"self-need with no failure terminates"` row of
  `takeoverShapeCases`.

Move the plant one hop away from the claimed key. Plant the self-need
on `"S2"`, then admit `"S"` naming `"S2"` as a need. Keep `claim: "S"`
and `wantStatus: StatusClaimed` unchanged.

This does not weaken either row. The row proves the `seen` set in
`blockingAncestor` terminates the walk. The walk still enqueues
`"S2"`, pops it, reads its `Needs` of `["S2"]`, and re-enqueues
nothing only because `seen` blocks it. Without the `seen` set the walk
still never returns. The record `Claim` writes now names `"S2"`, not
itself, so `Validate` accepts it.

Measured against the current tree, the shifted fixture claims `"S"`
with fence 1 and takes it over with fence 2. Both written records pass
`TaskState.Validate`.

Do not delete or weaken any assertion. If the diff nets an assertion
decrease, add the missing assertion instead. The test-tampering gate
reports that as TT04.

### Tests that do not change

- `ledger/ledger_test/stress_test.go`. The model needs no lease
  guard. The generator's lease is `time.Duration(1+rng.Intn(5)) *
  clk.tick` at line 354, with a one-millisecond tick. It is therefore
  always positive, so the new production branch is unreachable in the
  storm. A model branch the generator cannot reach is an unreachable
  positive control.
- `ledger/ledger_test/stress_test.go`, the `next.Validate()` half. The
  storm admits with no needs, so `Needs` is always empty. `Owner` is
  always non-empty. `LeaseUntil` is always `base + n*tick + lease`,
  which is never zero. `BlockedBy` is set only by `blockOne`, always
  together with `StatusBlocked`. No storm record fails `Validate`.
- `ledger/ledger_test/metamorphic_test.go`. It uses `fixedLease`,
  which is one hour.
- `taskrun`, `subagent`, `runconfig`, `agent`, `e2e`, `dispatch`, and
  `docs/examples`. Every lease they pass is positive.
- `durablefence`. Its `Scenario` takes no lease.

### Coverage

`ledger` reads 98.0 with `-coverpkg=./ledger/...`. The mutation floor
is 91. The change adds four error branches. `validateLease` holds one
branch on one operator site, reached through three call sites. The
three `next.Validate()` returns are the other three branches.
`TestLeaseMustBePositive` reaches the helper through all three call
sites. `TestLeaseWriteRejectsInvalidRecord` covers the three
`next.Validate()` returns. No branch lands untested, so the floor
holds.

## Verification

- `go test ./ledger/...` — both new tests red before the fix, green
  after.
- `go test -race ./ledger/...`.
- `go build -tags ledger_sqlite ./ledger/...` — the tag-gated store
  must compile.
- `make api-update`, then commit the one-line `api/ledger.txt` diff.
- `python3 scripts/check_plan.py`.
- `python3 scripts/check_prose.py`.
- `python3 scripts/check_docs.py` — `ErrInvalidLease` needs a doc
  comment starting with its own name.
- `make verify` — the full gate, including the coverage floor and the
  mutation gate.

No `policy/layers.json` diff. No new conformance vector: the change
touches no wire semantics.
