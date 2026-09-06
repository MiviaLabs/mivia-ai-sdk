# Maintenance addenda batch

## Goal

Close six small correctness and contract gaps found by review. Each
item fixes one defect or adds one missing proof. No item adds a
package, a type, or an abstraction.

The batch lands as one commit on `build/maintenance-addenda-batch`.

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
package the deps gate exempts.

## Item 1: ledger.Admit calls TaskState.Validate

### Defect

`Admit` writes a record it never validates. `ledger/ledger.go:68`
builds `next` and passes it straight to `Store.CompareAndSwap`.
`Admit(ctx, actor, "k", 1, nil, now, "k")` returns `true, nil`. The
stored record then fails `TaskState.Validate` at
`ledger/task_state.go:98`. `Snapshot` succeeds, `Encode` fails, and
`Restore` fails the same way. A live ledger cannot round-trip its own
snapshot.

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
`Decode`, and `Restore` then all succeed. `Restore` runs
`TaskState.Validate` per record at `ledger/snapshot.go`, which is the
call that fails today for a self-need record.

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

### Tests

No new test. `TestValidateSplitBrainLoad` already covers the branch.

## Item 5: agentrun.ValidateMatrix equivalence test

### Gap

`agentrun/matrix.go:99` and `agentrun/matrix.go:199` re-implement
flow's declaration-order scan. `flow/runner.go:93` owns the original.
Nothing compares the two. `grep -rl "flow.Run(" agentrun/` returns
nothing.

### What is observable

`ValidateMatrix` returns only an error. It exposes no order value.
The item as written cannot compare two order values without a
production change. It can compare something stronger and still
honest: the set of transition rows each scan demands, attributed to
the same units, on the same definition.

`flow.Run` exposes its own walk two ways. `onCheckpoint` fires once
per resolved unit and carries `Checkpoint.Status`, the status the run
rests on after that unit. `Confirm` fires for a singleton and a
one-member panel only. `flow/runner.go:14` states that `Run` skips
`Confirm` for a panel of two or more members. So `Confirm` alone
cannot record a wave. The test uses both, and asserts the `Confirm`
gap explicitly.

### The result

The two scans agree. No divergence was found, so the STOP condition
does not apply. No production change is needed.

### The fixture

Add `agentrun/agentrun_test/matrix_equivalence_test.go`, external
package `agentrun_test`. It reuses `mustFlow`, `mustMachine`, and
`assertMatrixFails` from `matrix_test.go`.

Statuses: `queued` (initial), `sx`, `gathered`, `routed`, `done`.

Steps, in declaration order:

- `root`, `To: sx`, no needs. The singleton.
- `panelA`, `To: gathered`, `Needs: ["root"]`.
- `panelB`, `To: gathered`, `Needs: ["root"]`.
- `router`, `To: routed`, `Needs: ["panelA", "panelB"]`, with a
  `Route` returning `["finish"]`.
- `finish`, `To: done`, `Needs: ["router"]`.

Panels: one panel, `{"panelA", "panelB"}`.

Two fixture constraints were found by running it:

- `flow.New` rejects a panel member that is a direct dependent of a
  routed step. The route therefore sits on `router`, below the panel,
  not on `root`.
- The route must return every direct dependent. `ValidateMatrix`
  walks the all-run path and does not model a route exclusion. A
  route that excludes a sibling would make the two orders differ by
  design, not by defect.

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
   order equals `["root", "router", "finish"]`. This pins the
   documented `Confirm` gap for a wave.
2. Build a machine holding only the recorded status chain, one row
   per checkpoint. Assert `ValidateMatrix` returns nil. This proves
   the simulator demands no row outside what the run consumed.
3. For each recorded chain link, build the complete machine minus
   that one row. Assert `ValidateMatrix` fails and names both
   statuses. This proves the simulator demands every row the run
   consumed, at the same point in the walk.

Assertions two and three together pin a two-way equivalence between
the two scans.

### Reachability trace

The recorded chain on the working copy is
`[sx gathered routed done]`. Each drop-one case failed with the
expected message:

- Drop `queued -> sx`: `agentrun: step "root": no transition from
  "queued" to "sx"`.
- Drop `sx -> gathered`: `agentrun: step "panelA panelB": no
  transition from "sx" to "gathered"`.
- Drop `gathered -> routed`: `agentrun: step "router": no transition
  from "gathered" to "routed"`.
- Drop `routed -> done`: `agentrun: step "finish": no transition from
  "routed" to "done"`.

Each message names the unit the simulator reached and the exact pair
it demanded. Every failure is a live positive control: none of the
four passed vacuously. Verified on the working copy.

`joinIDs` at `agentrun/matrix.go` produces the `"panelA panelB"`
label, which is how the wave identifies itself.

## Item 6a: envelope.Sign calls Validate

### Fix

In `envelope/sign.go:14`, call `m.Validate()` after the key-length
check and before the `Signer` assignment. Return the error.

`Message.Validate` at `envelope/message.go:108` never requires
`Signer` or `Signature`. `validateSignature` at
`envelope/message.go:185` returns nil when both fields are empty. So
`Validate` runs correctly on an unsigned message, and the order is
safe. Verified by reading `validateSignature` and by running the
suite.

### One production branch becomes unreachable

`Validate` rejects a NaN or infinite `Confidence`. That is the only
input that makes `json.Marshal` fail on a `Message`. After the change,
the `marshal for signing` branch in `Sign` cannot fire. Keep it as a
defensive branch. Envelope coverage measured 99.4 percent on the
working copy, and `Sign` measured 91.7 percent. Both clear the floor.

No new conformance vector. The vectors in
`envelope/testdata/vectors/` pin `Encode` and `Decode`. This change
adds no schema rule and no new `Validate` rule.

### Every Sign call site that signs a Validate-rejected message

Grepped `.Sign(` across the tree: 37 sites in 27 files. Then applied
the change and ran the whole suite. Exactly three sites break.

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

For the last two, add a local test helper in each package:

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
the public surface.

Both replacements were written and run. The whole suite passes.

## Item 6b: machine Fire sentinels

### Fix

Add `machine/errors.go` with two sentinels:

- `ErrNoTransition = errors.New("machine: no transition")`
- `ErrGuardRejected = errors.New("machine: guard rejected move")`

In `machine/definition.go:150`, wrap with `%w`:

- `fmt.Errorf("%w from %q on %q", ErrNoTransition, from, trig)`
- `fmt.Errorf("%w from %q on %q", ErrGuardRejected, from, trig)`

The rendered text is byte-identical to today's text. Verified: the
whole suite passes with no other test touched.

Run `make api-update`. `api/machine.txt` gains both names.

### The grep for the two error texts

Grepped `"no transition"` and `"guard rejected"` across `.go` and
`.md`. Matches split into three groups.

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
`read_limit_test.go:62` and `:63`. No new test function.

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

The rename removes a test function name, so the tampering gate fires
`TT01`. Add this trailer to the commit, after re-verifying the diff:

```
Allow-Test-Change: TT01 zero-value Bus behavior changed by design; the
replacement pins the new guard and keeps the old nil-Emit assertion
```

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
across `.go` and `.md`. Two sites carry the text.

- `room/room.go:33`, the declaration.
- `docs/packages/room.md:49`, which quotes it.

Three sites reference the symbol only and need no change:
`room/room.go:156`, `room/room.go:159`, and
`room/integration_test.go:182` and `:184`.

## Plan addenda

Write these addenda with the code, in the same commit. Each names the
exact sentence that changes.

- `docs/plans/ledger.md:457` — the sentence that defers the `Admit`
  validation gap. See the addendum in that file.
- `docs/plans/ledger.md:200` — the `Takeover` check order.
- `docs/plans/envelope.md:17` — the sentence saying validation is
  called by `Encode` and `Decode`.
- `docs/plans/discovery.md:44` — the `Validate` rule list.
- `docs/plans/machine.md` — the `Fire` sentinels.
- `docs/plans/workspace.md:63` — the blank-root rule.
- `docs/plans/events.md` — the zero-value `Bus` rule.
- `docs/plans/room.md` — the `ErrUnsigned` meaning.
- `docs/plans/agentrun.md:104` — the new equivalence test.
- `docs/plans/identity.md` — the `Load` comment claim.

## Package doc updates

These describe shipped behavior, so they change with the code, not
before it.

- `docs/packages/ledger.md:128` — add that a terminal record returns
  `ErrNotClaimed` even while its lease is still live.
- `docs/packages/discovery.md:30` — add the padding rule to the
  invariant list.
- `docs/packages/machine.md:80` — name `ErrNoTransition` and
  `ErrGuardRejected`.
- `docs/packages/workspace.md:35` and `:103` — name `ErrBlankRoot`.
- `docs/packages/events.md:21` and `:59` — the zero value is usable.
- `docs/packages/room.md:49` — the new `ErrUnsigned` text.

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

Run in order:

- `make api-update`, then commit the `api/` diff in the same change.
- `make verify`.
- `go test -race ./ledger/... ./flow/... ./agentrun/...`.

Do not run `go test -fuzz` with default parallelism. Seeded smoke runs
under plain `go test` are fine.

Gates this batch touches:

- `scripts/check_api.py` sees two new symbols in two lock files.
- `scripts/check_plan.py` sees the plan addenda.
- `scripts/check_prose.py` sees the new prose.
- `scripts/check_test_tampering.py` fires `TT01` for the events test
  rename and may fire `TT01` for the envelope test rename. Re-verify
  each finding against the diff before adding a trailer.
- The coverage floor holds. Envelope measured 99.4 percent with the
  change applied.

No gate is weakened, no limit raised, and no exclusion widened.

`make verify-fast` passed on a working copy carrying all six items.

## Commit

One commit on `build/maintenance-addenda-batch`.

Subject:

```
fix(sdk): close six Validate, ordering, and sentinel gaps
```

Body:

```
Admit now validates the record it writes, so a live ledger can
round-trip its own snapshot. Takeover checks status before lease
staleness, so a completed record returns ErrNotClaimed as its doc
promises. discovery.Validate rejects a padded capability entry that
Match can never hit. identity.Load carries a truthful comment on its
Validate call. agentrun gains an equivalence test proving
ValidateMatrix demands exactly the transition rows flow.Run consumes.
envelope.Sign validates first; machine.Fire, workspace.Options, and
room gain or reword sentinels; the zero events.Bus is usable.

Two tests are replaced because the behavior they pinned changed by
design. TestZeroValueBusPinsConstructorOnly pinned the zero-Bus panic
this change removes. TestSignRejectsUnserializableMessage pinned a
marshal branch Sign's new Validate call makes unreachable. Both
replacements keep or raise the assertion count. Two ledger fixture
rows now plant their self-need record through the Store, because
Admit rejects one.

See docs/plans/agents/maintenance-addenda-batch.md.

Allow-Test-Change: TT01 zero-value Bus behavior changed by design; the
replacement pins the new guard and keeps the old nil-Emit assertion

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
```
