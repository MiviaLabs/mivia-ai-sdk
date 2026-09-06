# Maintenance addenda batch

## Goal

Close six small correctness and contract gaps found by review. Each
item fixes one defect or adds one missing proof. No item adds a
package, a type, or an abstraction.

The batch lands as one commit on `build/maintenance-addenda-batch`.

## Where the work happens

The batch is built in an isolated git worktree checked out on
`build/maintenance-addenda-batch`. The shared checkout at
`/home/mac/projects/mivialabs/mivia-ai-sdk` holds other concurrent
efforts and is read-only reference for this batch. No file from
another effort can enter this commit, because the worktree carries
only this batch's edits. Run every gate from the worktree root.

## Scope

Inside the batch:

- `ledger.Admit` must validate the record it writes.
- `ledger.Takeover` must check status before lease staleness.
- `discovery.Card.Validate` must reject a padded capability entry.
- `identity.Load` must carry a truthful comment on its `Validate` call.
- `agentrun.ValidateMatrix` needs an equivalence test against `flow.Run`.
- Five small `Validate` and sentinel gaps in `envelope`, `machine`,
  `workspace`, `events`, and `room`.

Outside the batch:

- The `Claim`, `Takeover`, and `Renew` lease-validation hole. See
  item 1.
- Any route-exclusion or skipped-unit comparison in item 5.
- Any refactor not required by the six items.
- Any new package, new exported type, or new abstraction.
- Any change to a gate, a limit, or an exclusion.

Every claim below was checked against a working copy of the tree with
all six items applied. `make verify-fast` passed on that copy.

## API

Two lock files change. Run `make api-update` and commit the diff.

- `api/machine.txt` gains `var ErrGuardRejected` and `var ErrNoTransition`.
- `api/workspace.txt` gains `var ErrBlankRoot`.

No other lock file changes. Verified by running `make api-update` on
the working copy and diffing `api/`.

The changed sentinel text in `room.ErrUnsigned` does not touch a lock.
The lock records symbol names, not error strings.

No import edge changes. `policy/layers.json` needs no row. The new
test in item five lives in `agentrun/agentrun_test`, an external test
package the deps gate exempts. `policy/layers.json` already grants
`agentrun -> flow`.

## Item 1: ledger.Admit calls TaskState.Validate

### Defect

`Admit` writes a record it never validates. `ledger/ledger.go:68`
builds `next` and passes it straight to `Store.CompareAndSwap`.
`Admit(ctx, actor, "k", 1, nil, now, "k")` returns `true, nil`. The
stored record then fails `TaskState.Validate` at
`ledger/task_state.go:98`, which rejects a task naming itself in
`Needs`. `Snapshot` collects that record, `Snapshot.Encode` fails on
it, and `Restore` fails the same way.

### What the fix delivers, exactly

After the fix, `Admit` can no longer store a record its own snapshot
cannot encode. That is the whole claim.

The fix does not restore the snapshot round-trip property for the
ledger as a whole. Three other write paths can still store a record
`TaskState.Validate` rejects. See the next section.

### The sibling hole this batch leaves open

Three write paths set `LeaseUntil` from `now.Add(lease)` and run no
validation: `Claim` at `ledger/claim.go:66`, `Renew` at
`ledger/claim.go:108`, and `Takeover` at `ledger/claim.go:215`. A zero
`now` with a zero `lease` stores a `StatusClaimed` record with a zero
`LeaseUntil`. `TaskState.Validate` rejects that record at
`ledger/task_state.go:107`. `Encode` and `Restore` then fail exactly
as they fail for a self-need record.

Review reproduced all three with item 1 applied. `Renew` was missed in
the first draft of this plan: the survey reasoned about it instead of
reading it. See `.agents/memories/grep_beats_reasoning_for_completeness.md`.

Every other `CompareAndSwap` in `ledger/` is safe. `blockOne` always
sets `BlockedBy`. `Complete` moves `Status` only. `Release` moves
`Status` back to `StatusPending` and clears `Owner` and `LeaseUntil`
at `ledger/claim.go:151`, which moves the record toward validity, not
away from it. `Claim`, `Renew`, and `Takeover` are the whole remaining
hole.

The hole stays open in this batch. The user scoped the batch to six
named items, and this is not one of them. Closing it changes two more
public contracts and their doc comments. Plan it as its own change.

### Fix

In `Admit`, call `next.Validate()` after the `if blocked` block and
before `l.store.CompareAndSwap`. Return `false, err` on failure. Add
no inline self-need check.

`TaskState.Validate` has a value receiver, so `next.Validate()`
compiles on the local value. Its preconditions are met at that point.
`Key` is non-empty, checked at the top of `Admit`. `Status` is
`StatusPending`, or `StatusBlocked` with `BlockedBy` set. `Owner` and
`LeaseUntil` are only required for `StatusClaimed`, which `Admit`
never writes. `Validate` reads no other field.

### Doc comment change

`Admit`'s doc comment at `ledger/ledger.go:46` enumerates every
failure and every return. It gains a new error return, so the
enumeration must name it. Add this sentence after the sentence that
ends "like Complete's dependent scan":

> // Admit validates the record before it writes: a record
> // TaskState.Validate rejects, such as one naming itself in Needs,
> // returns false and that error.

### Package doc change

`docs/packages/ledger.md:66` carries the same enumeration for
`Ledger.Admit` and has the same omission. Add this sentence after the
sentence ending "never claims":

> It calls `TaskState.Validate` on the record before the write, so a
> record `Validate` rejects returns `false` and that error.

The `TaskState.Validate` bullet at `docs/packages/ledger.md:157` lists
what `Validate` rejects but names no caller. Add this sentence to the
end of that bullet:

> `Admit`, `Restore`, and `Snapshot.Validate` call it.

Those three are every production caller after the fix.
`Snapshot.Validate` at `ledger/snapshot.go:29` is the caller that
enforces the round-trip property, and `Restore` calls it again per
record at `ledger/snapshot.go:48`.

### Collateral

Two existing tests admit a self-need on purpose. They prove the `seen`
set inside `blockingAncestor` terminates. `Admit` can no longer build
that fixture.

- `ledger/ledger_test/transitive_block_test.go:144` in row
  "self-need with no failure".
- `ledger/ledger_test/transitive_block_takeover_test.go:67` in row
  "self-need with no failure terminates".

Both rows keep their name, their assertions, and their intent. Only
the fixture layer moves. Add two helpers to
`ledger/ledger_test/helpers_test.go`:

- `newLedgerOverStore(t, store)` builds a `Ledger` over a caller-held
  `Store`.
- `plantSelfNeed(t, store, ctx, key)` writes a `StatusPending` record
  naming itself in `Needs` straight through `Store.CompareAndSwap`.

`MemStore.CompareAndSwap` runs no validation, so the plant succeeds.
Give each case table a `plantSelfNeeds []ledger.IdempotencyKey` field.
The two self-need rows set that field instead of an `admits` entry or
a `mustAdmit` call. Both test bodies build a `ledger.NewMemStore()`,
wrap it with `newLedgerOverStore`, and plant before the rest of the
fixture runs.

Review confirmed the collateral is not vacuous. With a panic planted
at the dedup branch, both self-need rows still reach the seen-set
branch at `ledger/ledger.go:165`.

No other call site passes a self-need. Verified by grepping every
`.Admit(` site in the tree.

### Reachability trace

`TestAdmitRejectsSelfNeed` calls
`l.Admit(ctx, testActor, "k", 1, nil, fixedNow, "k")`. `needsCopy`
holds `"k"`. The record is not found, so `admitEligible` is skipped.
`blockingNeed` loads `"k"`, finds nothing, and returns
`blocked=false`. `next.Needs` holds `"k"` and `next.Key` is `"k"`.
The new call reaches the loop at `ledger/task_state.go:98` and the
branch at `task_state.go:99` returns the self-need error.

`TestAdmitSnapshotRoundTrips` admits `"root"` and `"dep"`, where
`"dep"` needs `"root"`. Both records pass `Validate`, so the new call
does not reject them. `Snapshot`, `Snapshot.Validate`, `Encode`,
`Decode`, and `Restore` then all succeed. The test proves that
records `Admit` accepts survive a snapshot round-trip. It does not
prove the property for records `Claim` or `Takeover` write.

Both tests were written and run against a working copy. Both pass.

## Item 2: ledger.Takeover checks status before lease

### Defect

`ledger/claim.go:198` checks `cur.LeaseUntil.After(now)` before
`ledger/claim.go:201` checks `cur.Status != StatusClaimed`. `Complete`
leaves `LeaseUntil` on the record. A `Claim`, `Complete`, `Takeover`
sequence inside the lease window returns `ErrNotStale`. The doc
comment at `claim.go:177` promises `ErrNotClaimed` for a terminal
record.

### Fix

Swap the two blocks so the status check runs first. This matches
`Claim`. No wire change and no new sentinel.

### Doc comment change

Rewrite the ordering sentences in the `Takeover` doc comment at
`ledger/claim.go:174` to the new order.

Old text:

> It returns ErrNoKey when the key has no record, checked before the
> ErrNotStale and ErrNotClaimed checks and before any CompareAndSwap
> call. It returns ErrNotStale when LeaseUntil is still after now. It
> returns ErrNotClaimed for a StatusPending or terminal record ... It
> returns ErrNotClaimed last when a key in the record's transitive
> Needs closure holds StatusFailed or StatusBlocked, checked after the
> ErrNotStale check and before any CompareAndSwap call.

New text:

> It returns ErrNoKey when the key has no record, checked before the
> ErrNotClaimed and ErrNotStale checks and before any CompareAndSwap
> call. It returns ErrNotClaimed for a StatusPending or terminal
> record, checked next: Takeover never admits or claims a
> never-claimed record, and a caller uses Claim for that. It returns
> ErrNotStale when LeaseUntil is still after now. It returns
> ErrNotClaimed last when a key in the record's transitive Needs
> closure holds StatusFailed or StatusBlocked, checked after the
> ErrNotStale check and before any CompareAndSwap call.

### Every ErrNotStale expectation in the tree

Grepped across `.go` and `.md`. Five test sites carry an expectation.
Review confirmed the set is complete.

- `ledger/ledger_test/takeover_test.go:39` in
  `TestTakeoverWhileLeaseLiveRejected`. The record is `StatusClaimed`,
  so the status check passes and the lease check still returns
  `ErrNotStale`. No change.
- `ledger/ledger_test/takeover_test.go:91` in
  `TestTakeoverAgainstTerminalRejected`. It calls `buildCompleted`,
  then takes over at `fixedNow.Add(2 * fixedLease)`. That instant is
  past `LeaseUntil`, so the lease check already falls through today.
  The test expects `ErrNotClaimed` and passes before and after the
  swap. No change.
- `ledger/ledger_test/takeover_race_test.go:47`. The losing `Takeover`
  races a winning `Claim`, which leaves the record `StatusClaimed`
  with a live lease. The status check passes and the lease check
  returns `ErrNotStale`. No change.
- `ledger/ledger_test/stress_test.go:147` in `modelTakeover`. This one
  changes. See below.
- `ledger/ledger_test/stress_test.go:408` lists `ledger.ErrNotStale`
  in `sentinelErrs`. The sentinel stays reachable. No change.

### The stress model changes

`modelTakeover` at `ledger/ledger_test/stress_test.go:136` mirrors the
old order. Its doc comment states the old order in words: "staleness
is checked before the claimed-status precondition, unlike Claim".
Reorder the two model blocks to match production and rewrite that
comment sentence.

New comment text:

> modelTakeover mirrors ledger.Takeover: the claimed-status
> precondition is checked before staleness, matching Claim's order.

The storm runs on fixed seeds, so the divergent case may not fire
today. The model is a specification mirror, so it must track the spec
regardless. The reordered model passed twenty runs under `-race` on
the working copy.

### Reachability trace

`TestTakeoverAgainstCompletedWithLiveLeaseRejected` calls
`buildCompleted`, which admits, claims at `fixedNow` with
`fixedLease`, and completes as `StatusCompleted`. `Complete` copies
`cur` into `next` and changes only `Status`, `UpdatedBy`, and
`UpdatedAt`, so `LeaseUntil` stays at `fixedNow.Add(fixedLease)`.
The test asserts that precondition before it acts. It then calls
`Takeover` at `fixedNow`. `LeaseUntil.After(fixedNow)` is true, so the
old order returns `ErrNotStale`. The new order reaches
`ledger/claim.go` status check first and returns `ErrNotClaimed`.
Verified: the test fails before the swap and passes after it.

## Item 3: discovery.Validate rejects a padded capability

### Defect

`discovery/card.go:53` trims each entry for its blank and duplicate
checks, then stores the padded original. `Match` at `card.go:79`
compares the stored string. `Card{Name: "x", Capabilities: []string{" deploy"}}`
passes `Validate` and `Match("deploy")` returns false.

### Fix

Add the padding check inside the `Validate` loop:

```go
if trimmed != capability {
    return errors.New("discovery: capability entry must not carry padding")
}
```

Place it after the duplicate loop and before `seen = append(...)`.
Order is load-bearing. An existing row,
`discovery/discovery_test/card_test.go` case "duplicate capability
differing only in padding is rejected", uses `{"read", " read "}` and
expects "duplicate capability". A padding check placed before the
duplicate loop would return the padding error and break that row.
With the padding check last, `" read "` still hits the duplicate
branch first.

Update the `Validate` doc comment to name the new rule.

### Doc comment stays valid

`Match`'s doc comment at `discovery/card.go:68` describes a padded
`need`, not a padded entry. The new rule does not make it stale. Leave
it unchanged.

### Reachability trace

Fixture `discovery/discovery_test/testdata/padded_capability.json`
holds `["read", " deploy"]`. `Parse` decodes it and calls `Validate`.
Iteration one takes `"read"`, which trims to itself and appends to
`seen`. Iteration two takes `" deploy"`, which trims to `"deploy"`.
The blank branch does not fire. The duplicate loop compares `"deploy"`
against `"read"` with `EqualFold` and does not match. The new branch
then sees `"deploy" != " deploy"` and returns the padding error.

The struct-literal row `{" deploy"}` reaches the same branch on the
first iteration, with an empty `seen`.

Grepped every fixture and every `Capabilities` literal in the tree. No
other site passes a padded entry. Verified: the whole suite passes
with the rule applied.

## Item 4: identity.Load comment

### Premise confirmed

`identity/identity.go:61` derives `pub` from `priv.Public()`. For an
`ed25519.PrivateKey`, that method returns a copy of `priv[32:]`, the
file's second half. It is not the seed-derived key. `Validate` at
`identity/identity.go:80` compares the seed-derived key against
`i.PublicKey`, and again against `i.PrivateKey[32:]`. A split-brain
file fails both comparisons. So the `Validate` call at
`identity/identity.go:66` can and does fail.

`identity/identity_test/validate_split_brain_test.go:64` drives `Load`
over three split-brain files and expects `ErrKeyInvalid`. The comment
at `identity/identity.go:63` invites deletion of a live security
check.

### Fix

Replace the three comment lines at `identity/identity.go:63` with one
line. No code change.

Old text:

> // Defensive: PublicKey is derived from priv above, so this cannot
> // fail for any id built here. It stays in case a future edit
> // changes how id is constructed.

New text:

> // Validate rejects a split-brain file whose second half is not the
> // seed-derived key. See validate_split_brain_test.go.

Wrapping across two lines for column width is acceptable.

`docs/packages/identity.md:22` stays accurate. It describes
`identity/identity.go:94`, which validates the identity, not a
message. Leave it unchanged.

### Tests

No new test. `TestValidateSplitBrainLoad` already covers the branch.

## Item 5: agentrun.ValidateMatrix equivalence test

### Gap

`agentrun/matrix.go:99` and `agentrun/matrix.go:200` re-implement
flow's declaration-order scan. `nextReadyGroup` at
`flow/runner.go:304` owns the original.
Nothing compares the two. `grep -rl "flow.Run(" agentrun/` returns
nothing.

### What is observable

`ValidateMatrix` returns only an error. It exposes no order value.
`api/agentrun.txt:13` confirms that. The error text, which names the
unit and the demanded status pair, is the only side channel.

`flow.Run` exposes its own walk two ways. `onCheckpoint` fires once
per resolved unit and carries `Checkpoint.Status`, the status the run
rests on after that unit. `Confirm` fires for a singleton and a
one-member panel only. `flow/runner.go:14` states that `Run` skips
`Confirm` for a panel of two or more members. Review confirmed that
skip in the code: `runWave` at `flow/wave.go:35` never calls
`confirmStep`, and `advanceGroup`'s `scanPanel` branch at
`flow/runner.go:144` bypasses `runSingleton`. So `Confirm` alone
cannot record a wave. The test uses both, and asserts the `Confirm`
gap explicitly.

### The claim this item proves

Assertions two and three together prove one property. The simulator
demands exactly the set of transition rows the run consumes,
attributed to the same units.

Set equality is the claim. Order equivalence is not.

### The residual gap

State the gap in the test file's own doc comment, so a later reader
does not over-read the result.

- Nothing here pins the two scans' relative ordering beyond what the
  row set forces. Two walk orders with identical demand sets are
  indistinguishable to these assertions.
- `walkSim`'s walk is machine-independent. It reads only `m.Initial()`;
  the machine affects `checkRow` alone.
- Route exclusions stay outside the comparison. So do skipped units.

### The fixture

Add `agentrun/agentrun_test/matrix_equivalence_test.go`, external
package `agentrun_test`. It reuses `mustFlow`, `mustMachine`, and
`assertMatrixFails` from `matrix_test.go`.

Statuses: `queued` (initial), `sx`, `sy`, `gathered`, `routed`,
`done`.

Steps, in declaration order:

- `root`, `To: sx`, no needs. The first singleton.
- `root2`, `To: sy`, no needs. The second singleton, declared after
  `root`.
- `panelA`, `To: gathered`, `Needs: ["root"]`.
- `panelB`, `To: gathered`, `Needs: ["root2"]`.
- `router`, `To: routed`, `Needs: ["panelA", "panelB"]`, with a
  `Route` returning `["finish"]`.
- `finish`, `To: done`, `Needs: ["router"]`.

Panels: one panel, `{"panelA", "panelB"}`.

### Why two independent roots

The graph must let declaration order decide something. A total order
cannot: with one root, no two units are ever ready at once.

`root` and `root2` have no needs, so both are ready at the start.
Declaration order alone picks `root` first. Review proved the fixture
discriminates: with `nextUnit`'s scan at `agentrun/matrix.go:100`
reversed, the test fails with

> assertion 2: step "root2": no transition from "queued" to "sy"

The earlier single-root fixture passed that same planted reversal.
That is why the fixture changed.

### Two fixture constraints found by running it

- `flow.New` rejects a panel member that is a direct dependent of a
  routed step. The route therefore sits on `router`, below the panel,
  not on a root.
- The route must return every direct dependent. `ValidateMatrix`
  walks the all-run path and does not model a route exclusion.

### The route is not a discriminator

Say this plainly, so the fixture list does not imply otherwise.

`grep -n "Route" agentrun/matrix.go` returns two comment lines and
nothing else. The simulator does not model `Route` at all. In
`flow/runner.go:119-132` a route that excludes nothing takes the same
status path as no route. Removing the route changes no assertion.

Keep the route anyway. It cheaply pins that a non-excluding route does
not perturb the chain. A route that excludes a sibling stays out of
scope, because the simulator walks the all-run path.

### The machine

`eqCompleteMachine(t, skip...)` builds one row for every ordered pair
of distinct statuses, minus the pairs named in `skip`. Each row gets a
distinct trigger, so `machine.New` accepts it. Every status stays
reachable from `queued`, so dropping one row still builds. The
complete machine lets `flow.Run` pick its own path, so the recorded
order is flow's, not the fixture's expectation.

### The three assertions

1. Run `flow.Run` over the complete machine with a recording
   `Confirm` and a recording `onCheckpoint`. Assert the `Confirm`
   order equals `["root", "root2", "router", "finish"]`. This pins the
   documented `Confirm` gap for a wave.
2. Build a machine holding only the recorded status chain, one row
   per checkpoint. Assert `ValidateMatrix` returns nil. This proves
   the simulator demands no row outside what the run consumed.
3. For each recorded chain link, build the complete machine minus
   that one row. Assert `ValidateMatrix` fails, and assert the error
   text names both statuses and the expected unit label. The unit
   label is the load-bearing half: `"panelA panelB"` pins that the
   simulator attributes the wave's row to the wave, not to a member.

Assertion 3 is specified once, here. The `agentrun` addendum repeats
this same wording.

### Reachability trace

The recorded chain on the working copy is
`[sx sy gathered routed done]`. Five links, five drop-one cases. Each
case failed with the expected message:

- Drop `queued -> sx`: `agentrun: step "root": no transition from
  "queued" to "sx"`.
- Drop `sx -> sy`: `agentrun: step "root2": no transition from "sx"
  to "sy"`.
- Drop `sy -> gathered`: `agentrun: step "panelA panelB": no
  transition from "sy" to "gathered"`.
- Drop `gathered -> routed`: `agentrun: step "router": no transition
  from "gathered" to "routed"`.
- Drop `routed -> done`: `agentrun: step "finish": no transition from
  "routed" to "done"`.

Each message names the unit the simulator reached and the exact pair
it demanded. Every failure is a live positive control: none of the
five passed vacuously. Verified on the working copy.

`joinIDs` at `agentrun/matrix.go` produces the `"panelA panelB"`
label, which is how the wave identifies itself.

### The result

The two scans agree on the demanded row set. No divergence was found,
so the user's STOP condition does not apply. No production change is
needed.

## Item 6a: envelope.Sign validates a normalized copy

### Fix

In `envelope/sign.go:14`, after the key-length check and before the
`Signer` assignment, validate a normalized copy:

```go
check := m
check.Signer = ""
check.Signature = ""
if err := check.Validate(); err != nil {
    return Message{}, err
}
```

Do not call `m.Validate()` on the message as supplied.

### Why the copy, not the message

`validateSignature` at `envelope/message.go:185` constrains `Signer`
and `Signature` whenever either field is non-empty. `Sign` overwrites
both fields immediately after. So validating them is both wrong and
harmful.

The natural re-sign idiom clears `Signature` and keeps `Signer`. That
is the same move `Sign` makes at `sign.go:22` and `VerifySignature`
makes at `sign.go:48`. Review probed all three idioms against a plain
`m.Validate()` call:

- Strip the signature, keep the signer, re-sign: fails with
  "signature must be 128 lowercase hex chars".
- Re-sign a fully signed message: passes.
- Preset a non-hex `Signer`: fails with "signer must be 64 lowercase
  hex chars".

No current caller uses the first idiom, so the suite stays green
either way. It is a latent trap, not a live break.

The normalized copy costs the same three lines. It validates exactly
the content that gets signed. All three idioms pass. The full suite
produces the identical three failures listed below. Coverage is
unchanged.

### One production branch becomes unreachable

`Validate` rejects a NaN or infinite `Confidence`. That is the only
input that makes `json.Marshal` fail on a `Message`. Review confirmed
there is no other candidate and no retargeting option: every
`Message` field is a `string`, a `[]string`, an `int`, a `bool`, or a
struct of those. `Confidence` is the only `float64`.

After the change, the `marshal for signing` branch in `Sign` cannot
fire. Keep it as a defensive branch. Envelope coverage measured 99.4
percent on the working copy, and `Sign` measured 91.7 percent. Both
clear the floor.

No new conformance vector. The vectors in
`envelope/testdata/vectors/` pin `Encode` and `Decode`. This change
adds no schema rule and no new `Validate` rule.

### The architecture-section obligation

AGENTS.md requires a change to message semantics to update
docs/architecture.md's "Why the envelope is shaped this way" section
in the same change. This change does not qualify.

That section at `docs/architecture.md:715` explains why the message
carries epistemic typing, provenance, acks, and a thread hash chain.
Item 6a adds no field, no schema rule, and no `Validate` rule. It
moves an existing gate earlier in the pipeline. The set of valid
messages on the wire is unchanged, so the section stays as written.

### Doc sites this change makes false

Four sites state the old contract. All four change.

`envelope/message.go:107`. Old text:

> // Validate checks all Message invariants. Called by Encode and Decode.

New text:

> // Validate checks all Message invariants. Called by Sign, Encode,
> // and Decode.

`docs/packages/envelope.md:106`. Old bullet:

> - `Sign` fails when the supplied key is not an ed25519 private key
>   of the expected length. Pinned by `envelope/sign_test.go`.

New bullet:

> - `Sign` fails when the supplied key is not an ed25519 private key
>   of the expected length. It also fails when the message fails
>   `Validate` with `Signer` and `Signature` cleared. Pinned by
>   `envelope/sign_test.go`.

`docs/architecture.md:695`. Old numbered step:

> 1. **Sign.** `envelope/sign.go`, `Sign(key, m)`: sets Signer and
>    Signature. The signature covers the canonical JSON of every field
>    except itself.

New numbered step:

> 1. **Sign.** `envelope/sign.go`, `Sign(key, m)`: validates, then sets
>    Signer and Signature. The signature covers the canonical JSON of
>    every field except itself.

Step 2 in that same list already says `Encode` "validates, then
marshals". The new step 1 carries the matching clause.

`docs/plans/envelope.md:19` says validation is "centralized in
Validate and called by Encode/Decode". The addendum for that file
corrects it.

### Every Sign call site that signs a Validate-rejected message

Grepped `.Sign(` across the tree: 37 sites in 27 files. Then applied
the change and ran the whole suite. Exactly three sites break. Review
confirmed the set is complete.

- `envelope/sign_test.go:90` in `TestSignRejectsUnserializableMessage`.
  It pins the `json.UnsupportedValueError` from a NaN `Confidence`.
  That branch is now unreachable through `Sign`. Replace the test with
  `TestSignRejectsInvalidMessage`. It asserts `Sign` returns the
  intent error for `Intent: "bogus"` and the confidence error for a
  NaN `Confidence`. This is a behavior change by design, not a
  weakening: the new test keeps two assertions where the old had one.
- `a2aack/a2aack_test/transport_error_test.go:148` in
  `TestTransportNewAckRejectsEmptyRestatement`. It needs a validly
  signed message with an empty payload. `a2aack/a2aack.go:130`
  verifies the signature, then `a2aack.go:136` builds the ack from
  `result.Payload`. Only an empty payload reaches the "restatement is
  required" branch in `envelope.NewAck`. `Sign` can no longer build
  that message.
- `room/integration_test.go:203` in the case "invalid payload, validly
  signed". `Room.Accepts` at `room/room.go:152` runs `Validate` before
  the signature check. The case proves a correct signature does not
  rescue invalid content.

### The local signing helper

For the last two sites, add a local test helper in each package:

```go
// signBypassingValidate signs m the way envelope.Sign does, minus
// Sign's Validate gate, so a case can build a message whose signature
// verifies and whose content Validate rejects.
func signBypassingValidate(t testing.TB, key ed25519.PrivateKey, m envelope.Message) envelope.Message
```

It sets `Signer` from the key, clears `Signature`, marshals, and signs
the canonical JSON, exactly as `envelope.Sign` does. Put one copy in
`a2aack/a2aack_test/helpers_test.go` and one in
`room/integration_test.go`. Two local copies beat one new exported
symbol in `envelope`. A test-only signing helper does not belong on
the public surface. No shared test package exists, so this is not a
copied-exported-type smell.

Guard both copies against silent drift. Each helper re-implements
envelope's canonical signing form. If that form ever changes, a copy
that still produces an unverifiable message must fail loudly. End
each helper with a self-check before it returns:

```go
if err := m.VerifySignature(); err != nil {
    t.Fatalf("signBypassingValidate: signature does not verify: %v", err)
}
```

Review confirmed the self-check works in both packages.

Both replacements were written and run. The whole suite passes.

## Item 6b: machine Fire sentinels

### Fix

Add `machine/errors.go` with two sentinels:

- `ErrNoTransition = errors.New("machine: no transition")`
- `ErrGuardRejected = errors.New("machine: guard rejected move")`

In `machine/definition.go:150`, wrap with `%w`:

- `fmt.Errorf("%w from %q on %q", ErrNoTransition, from, trig)`
- `fmt.Errorf("%w from %q on %q", ErrGuardRejected, from, trig)`

The rendered text is byte-identical to today's text. Review confirmed
that byte-for-byte. The whole suite passes with no other test touched.

Run `make api-update`. `api/machine.txt` gains both names.

### The grep for the two error texts

Grepped `"no transition"` and `"guard rejected"` across `.go` and
`.md`. Matches split into three groups. Review confirmed the
classification.

Machine's own text, which this item changes:

- `machine/definition.go:152` and `machine/definition.go:163`.
- `machine/machine_test/fire_test.go:82`, `:100`, and `:119`.
- `docs/packages/machine.md:80`.

Flow's own text, which this item does not touch. Flow builds a
different string, "no transition to status %q from %q", at
`flow/wave.go:104`:

- `flow/flow_test/panel_test.go:279`, `chain_test.go:136`,
  `run_test.go:205`, `run_test.go:230`,
  `checkpoint_resume_test.go:382`, `loop_edge_test.go:167`.
- `docs/packages/flow.md:557` and `docs/plans/flow.md:137`.

Agentrun's own text, also untouched. `agentrun/matrix.go:332` builds
"no transition from %q to %q":

- `agentrun/agentrun_test/options_test.go:147` matches agentrun's
  string, not machine's.

One doc example embeds machine's rendered text:

- `docs/examples/flow-fallback-admission.md:111`. The text does not
  change, so the file does not change.

### Test change

In `machine/machine_test/fire_test.go`, replace the three
`strings.Contains` assertions with `errors.Is` against the matching
sentinel. Add the `errors` import. The test names do not change.

## Item 6c: workspace ErrBlankRoot

### Fix

Add `ErrBlankRoot = errors.New("workspace: Root is blank")` beside the
other sentinels in `workspace/workspace.go`. Return it from
`Options.Validate` at `workspace/workspace.go:83` in place of the
inline `errors.New`. The text does not change.

Run `make api-update`. `api/workspace.txt` gains `var ErrBlankRoot`.

### Test changes

`workspace/workspace_test/read_limit_test.go` already carries a
`wantErr error` field checked with `errors.Is`. Add
`wantErr: workspace.ErrBlankRoot` to both blank-root rows at
`read_limit_test.go:62` and `:63`. No new test function. Review
confirmed both rows reach the blank-root branch and never reach
`validateLimit`.

`workspace/workspace_test/secret_test.go:249` asserts only a boolean.
Its own comment says `read_limit_test.go` owns the blank-root rows.
Leave it unchanged.

## Item 6d: events zero-value Bus

### Fix

`events/bus.go:64` assigns into `b.subs`. On a zero `Bus` that map is
nil, so `Subscribe` panics. Add the guard `trigger/registry.go:77`
already uses, inside the lock and before the append:

```go
if b.subs == nil {
    b.subs = make(map[Name][]Handler)
}
```

`Emit` at `events/bus.go:81` only reads the map, which is safe on nil.
`bus.go` declares no other method that touches `b.subs`. Verified by
grepping `.subs` across `events/`.

### Necessity, stated honestly

This item reverses a documented invariant. Say so plainly.

- Three doc sites state the old rule: `events/bus.go:41`,
  `docs/packages/events.md:21`, and `docs/packages/events.md:59`.
- One test pins it on purpose. `TestZeroValueBusPinsConstructorOnly`
  carries the comment "The invariant is constructor-only; New is the
  only sanctioned build".
- No in-tree caller can reach the panic. All 22 production
  `events.Bus` sites hold a `*events.Bus` built by `events.New`, or
  nil-check first. So this fixes no live defect.
- The change aligns `events.Bus` with `trigger.Registry`'s
  usable-zero-value idiom at `trigger/registry.go:77`.
- The user ordered this change directly, and ordered the doc comment
  update with it. That instruction is the authority for the reversal
  and for the `TT01` trailer. The plan does not self-authorize it.

### Doc comment change

Update the `Bus` doc comment at `events/bus.go:41`.

Old text:

> // The zero value is not usable; create a bus with New.

New text:

> // The zero value is usable: Subscribe builds the subscription set on
> // first use, and Emit on an empty set dispatches nothing.

### Test change

`events/events_test/events_test.go:85` holds
`TestZeroValueBusPinsConstructorOnly`, which asserts the panic. That
expectation is exactly what this item removes. Replace it with
`TestZeroValueBusSubscribesAndEmits`. The replacement subscribes on a
zero `Bus`, emits, and asserts the handler ran once. It keeps the old
test's second half, which asserts `Emit` on an untouched zero `Bus`
returns nil.

## Item 6e: room ErrUnsigned text

### Fix

`room/room.go:33` declares
`ErrUnsigned = errors.New("unsigned message cannot be admitted")`.
`room/room.go:159` also wraps it for a signature that does not verify,
which the old text denies. Reword the sentinel:

> errors.New("message is unsigned or its signature does not verify")

No new sentinel and no lock change.

### The grep for the old text

Grepped `"unsigned message cannot be admitted"` and `ErrUnsigned`
across `.go` and `.md`. Two sites carry the text. Review confirmed the
set is complete.

- `room/room.go:33`, the declaration.
- `docs/packages/room.md:49`, which quotes it.

Three sites reference the symbol only and need no change:
`room/room.go:156`, `room/room.go:159`, and
`room/integration_test.go:182` and `:184`.

## Plan addenda

Write these addenda with the code, in the same commit. Each names the
exact sentence that changes.

- `docs/plans/ledger.md:457` — the sentence that defers the `Admit`
  validation gap. The addendum also names the `Claim` and `Takeover`
  hole this batch leaves open.
- `docs/plans/ledger.md:200` — the `Takeover` check order.
- `docs/plans/envelope.md:19` — the sentence saying validation is
  called by `Encode` and `Decode`. The quoted clause sits at `:20`.
- `docs/plans/discovery.md:44` — the `Validate` rule list.
- `docs/plans/machine.md` — the `Fire` sentinels.
- `docs/plans/workspace.md:63` — the blank-root rule. The addendum
  also reconciles `docs/plans/workspace.md:1082`.
- `docs/plans/events.md` — the zero-value `Bus` rule and the user's
  instruction as its authority.
- `docs/plans/room.md` — the `ErrUnsigned` meaning.
- `docs/plans/agentrun.md:104` — the new equivalence test, its exact
  claim, and its residual gap.
- `docs/plans/identity.md` — the `Load` comment claim.

## Package doc updates

These describe shipped behavior, so they change with the code, not
before it.

- `docs/packages/ledger.md:66` — the `Ledger.Admit` entry names the
  new `Validate` rejection.
- `docs/packages/ledger.md:128` — add that a terminal record returns
  `ErrNotClaimed` even while its lease is still live.
- `docs/packages/ledger.md:157` — the invariant bullet names `Admit`,
  `Restore`, and `Snapshot.Validate` as callers of
  `TaskState.Validate`.
- `docs/packages/envelope.md:106` — the `Sign` failure list names the
  new `Validate` rejection.
- `docs/packages/discovery.md:30` — add the padding rule to the
  invariant list.
- `docs/packages/machine.md:80` — name `ErrNoTransition` and
  `ErrGuardRejected`.
- `docs/packages/workspace.md:35` — the `Options.Validate` entry names
  `ErrBlankRoot`.
- `docs/packages/workspace.md:103` — add an `ErrBlankRoot` bullet to
  the sentinel list, beside the `ErrInvalidLimit` entry.
- `docs/packages/events.md:21` and `:59` — the zero value is usable.
- `docs/packages/room.md:49` — the new `ErrUnsigned` text.
- `docs/architecture.md:695` — pipeline step one says `Sign`
  validates first.

## Tests

Every test below was written and run against a working copy of the
tree. Each one passes. Each fixture's trace to its branch is in the
item section above.

New tests:

- `ledger/ledger_test/admit_validate_test.go` — `TestAdmitRejectsSelfNeed`
  and `TestAdmitSnapshotRoundTrips`.
- `ledger/ledger_test/takeover_terminal_test.go` —
  `TestTakeoverAgainstCompletedWithLiveLeaseRejected`.
- `agentrun/agentrun_test/matrix_equivalence_test.go` —
  `TestValidateMatrixMatchesRunOrder`.
- `events/events_test/events_test.go` —
  `TestZeroValueBusSubscribesAndEmits`, replacing
  `TestZeroValueBusPinsConstructorOnly`.
- `envelope/sign_test.go` — `TestSignRejectsInvalidMessage`, replacing
  `TestSignRejectsUnserializableMessage`.

New fixture:

- `discovery/discovery_test/testdata/padded_capability.json` holding
  `["read", " deploy"]`.

New table rows:

- `discovery/discovery_test/card_test.go` — one `TestParse` row over
  the new fixture, and one `TestCardValidate` row over the
  struct-literal card `{" deploy"}`.
- `workspace/workspace_test/read_limit_test.go` — two existing rows
  gain `wantErr: workspace.ErrBlankRoot`.

Changed assertions in existing tests:

- `machine/machine_test/fire_test.go:82`, `:100`, `:119` move from
  `strings.Contains` to `errors.Is`.
- `ledger/ledger_test/transitive_block_test.go` and
  `transitive_block_takeover_test.go` move their two self-need rows
  from `Admit` to a planted store record.
- `ledger/ledger_test/stress_test.go` reorders `modelTakeover` and
  rewrites its comment.
- `a2aack/a2aack_test/transport_error_test.go` and
  `room/integration_test.go` build their signed-but-invalid message
  with a local helper instead of `envelope.Sign`.

No test is deleted, skipped, or weakened. Two are replaced because
the behavior they pinned changed by design. Both replacements keep or
raise the assertion count.

## Verification

Run in order, from the worktree root:

- `make api-update`, then commit the `api/` diff in the same change.
- `make verify`.
- `go test -race ./ledger/... ./flow/... ./agentrun/...`.
- `python3 scripts/check_prose.py` and `python3 scripts/check_plan.py`.

Do not run `go test -fuzz` with default parallelism. Seeded smoke runs
under plain `go test` are fine.

Gates this batch touches:

- `scripts/check_api.py` sees three new symbols in two lock files.
- `scripts/check_plan.py` sees the plan addenda.
- `scripts/check_prose.py` sees the new prose.
- `scripts/check_test_tampering.py` fires `TT01` for the events test
  rename and for the envelope test rename. Re-verify each finding
  against the diff before adding the trailer.
- The coverage floor holds. Envelope measured 99.4 percent with the
  change applied.

No gate is weakened, no limit raised, and no exclusion widened.

`make verify-fast` passed on a working copy carrying all six items.

## Staging

Stage exactly these paths. Stage nothing else. The worktree carries
only this batch, so `git add -A` from the worktree root is safe, but
review `git status` before the commit either way.

Production code:

- `ledger/ledger.go`
- `ledger/claim.go`
- `discovery/card.go`
- `identity/identity.go`
- `envelope/sign.go`
- `envelope/message.go`
- `machine/errors.go` (new)
- `machine/definition.go`
- `workspace/workspace.go`
- `events/bus.go`
- `room/room.go`

Tests:

- `ledger/ledger_test/admit_validate_test.go` (new)
- `ledger/ledger_test/takeover_terminal_test.go` (new)
- `ledger/ledger_test/helpers_test.go`
- `ledger/ledger_test/transitive_block_test.go`
- `ledger/ledger_test/transitive_block_takeover_test.go`
- `ledger/ledger_test/stress_test.go`
- `discovery/discovery_test/card_test.go`
- `discovery/discovery_test/testdata/padded_capability.json` (new)
- `agentrun/agentrun_test/matrix_equivalence_test.go` (new)
- `envelope/sign_test.go`
- `a2aack/a2aack_test/helpers_test.go`
- `a2aack/a2aack_test/transport_error_test.go`
- `room/integration_test.go`
- `machine/machine_test/fire_test.go`
- `workspace/workspace_test/read_limit_test.go`
- `events/events_test/events_test.go`

API locks:

- `api/machine.txt`
- `api/workspace.txt`

Package docs:

- `docs/packages/ledger.md`
- `docs/packages/envelope.md`
- `docs/packages/discovery.md`
- `docs/packages/machine.md`
- `docs/packages/workspace.md`
- `docs/packages/events.md`
- `docs/packages/room.md`
- `docs/architecture.md`

Plans:

- `docs/plans/ledger.md`
- `docs/plans/envelope.md`
- `docs/plans/discovery.md`
- `docs/plans/identity.md`
- `docs/plans/machine.md`
- `docs/plans/workspace.md`
- `docs/plans/events.md`
- `docs/plans/room.md`
- `docs/plans/agentrun.md`
- `docs/plans/agents/maintenance-addenda-batch.md`

## Commit

One commit on `build/maintenance-addenda-batch`.

Subject:

```
fix(sdk): close six Validate, ordering, and sentinel gaps
```

Body:

```
Admit now validates the record it writes, so it can no longer store a
record its own snapshot cannot encode. Claim and Takeover still write
a lease without validating it, so the ledger's snapshot round-trip is
not restored as a whole; that hole is named in the plan and left to
its own change. Takeover checks status before lease staleness, so a
completed record returns ErrNotClaimed as its doc promises.
discovery.Validate rejects a padded capability entry that Match can
never hit. identity.Load carries a truthful comment on its Validate
call. agentrun gains a test proving ValidateMatrix demands exactly the
set of transition rows flow.Run consumes, attributed to the same
units. envelope.Sign validates a normalized copy first; machine.Fire,
workspace.Options, and room gain or reword sentinels; the zero
events.Bus is usable, which reverses a documented invariant at the
user's direct instruction.

Two tests are replaced because the behavior they pinned changed by
design. Both replacements keep or raise the assertion count. Two
ledger fixture rows now plant their self-need record through the
Store, because Admit rejects one.

See docs/plans/agents/maintenance-addenda-batch.md.

Allow-Test-Change: TT01 two replacements, each by design: TestZeroValueBusPinsConstructorOnly pinned the zero-Bus panic the user instructed us to remove, and TestSignRejectsUnserializableMessage pinned a marshal branch that Sign's new Validate call makes unreachable; both replacements keep or raise the assertion count

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
```

The `TT01` trailer names both replacements on one line. The override
parser at `scripts/test_tampering_override.py:74` keys on the finding
ID, so a single trailer waives every `TT01` finding in the commit. A
reason naming only one replacement would waive the other silently.
See `.agents/memories/override_trailers_dont_carry_to_merge_commits.md`.
Re-issue the same trailer on a later merge commit if the gate fires
there again.
