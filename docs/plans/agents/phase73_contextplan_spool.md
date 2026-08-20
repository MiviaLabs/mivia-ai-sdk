# Phase 73: contextplan spools its own overflow

Status: shipped. Folded into `docs/plans/contextplan.md`'s
"Correctness fix: contextplan spools its own overflow" section; this
file stays as the historical design record, not the active contract.
Builds on the shipped `contextplan` (phase 66) and `spool` (phase 67)
packages. Phase numbering note: phase 71
(`docs/plans/agents/phase71_filetools.md`) and phase 72
(`docs/plans/agents/phase72_runconfig_blocks.md`) are separate,
concurrent, plan-only changes to `subagent` and `runconfig`. This
plan uses phase 73 to avoid a collision with either and touches
neither package.

## Goal

Wire `contextplan.Planner` to `spool.Spool`, so a payload `Plan` elides
for budget reasons lands in durable, principal-scoped storage instead
of only a `contextstate.MemStore` ref that can itself evict. Both
`docs/plans/contextplan.md` and `docs/plans/spool.md` already name
`contextplan` as spool's expected consumer. This phase is that wiring,
not a new idea.

## Scope

Inside:

- `contextplan` gains one new import edge to `spool` in
  `policy/layers.json`.
- `NewPlanner` gains a third parameter, `spooler *spool.Spool`. A nil
  `spooler` is valid: `Plan` behaves exactly as it does today, byte
  for byte, when `spooler` is nil.
- `Plan` writes a dropped or stubbed payload's full `record.Data` to
  the wired `Spool`, for the two budget-driven elision reasons only:
  `ElisionReasonWindowOverflow` and `ElisionReasonRetentionExpired`.
  Every other reason skips the spool write.
- `Elision` gains one new field, `SpoolRef string`, carrying the
  `spool.Spool.Spool` reference on a successful write. Empty when no
  write was attempted or the write failed.
- The spool principal is `record.Ref.SubjectID`, the field
  `contextstate.ContentRef` already carries for exactly this
  ownership purpose. `Plan` invents no new principal parameter and
  adds no context-injected principal.
- A spool write failure never fails `Plan`. `SpoolRef` stays empty on
  a `Spool.Spool` error, matching this SDK's existing best-effort
  diagnostic pattern (`agent.EmitMessageDelivered`,
  `agent.EmitMessageAcked`). This covers every named `Spool.Spool`
  sentinel: `ErrGrantTooLarge` (a payload bigger than the wired
  `Spool`'s budget) and `ErrPrincipalConflict` (a second `SubjectID`
  spooling byte-identical content already granted to a first) both
  leave `SpoolRef` empty and `Plan`'s error nil, the same as any other
  `ContentStore.Put` failure.
- `docs/plans/contextplan.md` and `docs/plans/spool.md` gain the
  matching Correctness-fix-style subsections once this phase ships,
  following the fold-in convention `docs/plans/agents/PHASES.md`
  already uses for a shipped phase.
- Every existing `NewPlanner(store, cache)` call site updates in the
  same commit to pass `nil` as the third argument, or a real
  `*spool.Spool` where a test's new case wires one. The two-argument
  form appears at: `agentloop/agentloop_test/loop_integration_test.go:67`
  (the one caller outside `contextplan`);
  `contextplan/contextplan_test/elision_test.go:77,80,83`;
  `contextplan/contextplan_test/plan_test.go:17,53,96,145,179,207,242,
  285,321,333,346,370,390,410`;
  `contextplan/contextplan_test/plan_resolution_test.go:25,52,109,176,
  212,263`; `contextplan/contextplan_test/plan_revoked_test.go:16,64,
  109,137,162,198`; `contextplan/contextplan_test/property_plan_test.go:49`;
  and `contextplan/contextplan_test/plan_integration_test.go:20`. All
  are test files, exempt from `check_deps.py`, but not exempt from
  compiling; the builder greps `NewPlanner(` across the module before
  calling this bullet done, since a missed call site fails
  `make verify-fast` outright on the very first `go vet`.

Outside:

- A new principal parameter or a `spool.WithPrincipal`-style
  context-injected principal for `Plan`. `record.Ref.SubjectID`
  already names the content's owner; a second principal channel would
  duplicate that field for no stated caller need. `NewPlanner`'s two
  existing parameters, `store` and `cache`, stay wired at
  construction; `spooler` joins them the same way, not through a
  per-call parameter on `Plan`.
- Spooling `ElisionReasonReasoningRedacted` content. Reasoning
  redaction is deliberate exclusion, not a budget accident: `Plan`
  resolves the payload only to build a typed `ContentRef`, and the
  content never entered `Request.Messages` on purpose. Spooling it
  would hand a retrievable reference to exactly the content redaction
  exists to keep out of reach. This phase never calls `Spool.Spool`
  for this reason.
- Spooling `ElisionReasonRevoked` content. `contextstate.MemStore.Get`
  already denied this content as revoked. Writing it to `Spool` would
  reopen, through a second channel, exactly what the store's
  fail-closed `Get` closed. This phase never calls `Spool.Spool` for
  this reason, and states so as a deliberate security decision, not
  an oversight.
- Any tool surface. No `SpoolTool`-wrapped retrieval path joins
  `contextplan`, `subagent`, or any other package in this phase. A
  caller that wants a spooled elision back reads `Elision.SpoolRef`
  and calls `Spool.Load` itself, with its own principal check.
- Any change to `spool`'s own package. `Spool.Spool`,
  `Spool.Load`, `ContentStore`, and the grant-eviction policy stay as
  shipped. `contextplan` is a new caller, not a new requirement on
  `spool`.
- Any change to the `*memory.Store` decode cache's role. It stays a
  same-process cache only; spooling does not touch it.
- Mivia's operator-facing spool retrieval UI and its own principal
  mapping. Those stay in `mivia-agent`, matching the same exclusion
  `docs/plans/spool.md`'s own Scope section already states.

## API

The surface below lands in `api/contextplan.txt` via `make
api-update`, in the same change as the code. No `api/spool.txt`
change.

- `func NewPlanner(store *contextstate.MemStore, cache *memory.Store, spooler *spool.Spool) (*Planner, error)`
  — breaking change to the locked two-parameter form. A nil `store`
  or nil `cache` is an error, unchanged from today. A nil `spooler` is
  valid: `Plan` never calls `Spool.Spool` when `spooler` is nil, and
  every existing `contextplan` test and caller keeps its current
  behavior once it passes `nil` for the new parameter. The doc comment
  states the nil-safe contract explicitly, in the same sentence style
  as `contextbudget.Limits`'s "a zero field means no cap."
- `type Elision struct { Ref contextstate.ContentRef; Reason ElisionReason; Kept int; SpoolRef string }`
  — `SpoolRef` is the reference `spool.Spool.Spool` returned, set only
  when `Reason` is `ElisionReasonWindowOverflow` or
  `ElisionReasonRetentionExpired`, `Planner` carries a non-nil
  `spooler`, and the write succeeded. Empty in every other case,
  including a failed write.
- `(Planner) Plan`'s doc comment gains one clause: a wired `Spool`
  receives the full payload behind every `ElisionReasonWindowOverflow`
  and `ElisionReasonRetentionExpired` entry, keyed to
  `record.Ref.SubjectID`, best-effort, never failing `Plan`.

No other exported symbol changes. `ElisionReason`,
`ElisionReasonWindowOverflow`, `ElisionReasonRetentionExpired`,
`ElisionReasonReasoningRedacted`, `ElisionReasonRevoked`, `Window`,
`PlanResult`, `Calibrate`, `Calibrated`, and `IsReasoningEvent` stay as
locked.

### Internal shape (not locked, builder's discretion within it)

- `Planner` gains one more unexported field, holding `spooler`,
  alongside `store` and `cache`. `Planner`'s doc comment currently
  says "its two dependencies guard their own state"; the builder
  updates that count to three in the same change.
- `admit`, the package-level helper that decides a full insertion, a
  stub, or a drop, needs `ctx context.Context` and the `Planner`'s
  `spooler` field to reach `Spool.Spool`. The builder may turn `admit`
  into a method on `*Planner`, or keep it a function and pass both in;
  either satisfies this plan. `Plan`'s own `ctx` parameter is the one
  `admit` forwards to `Spool.Spool`, so no new context value or
  context key joins this phase.
- The `Plan` loop's `ElisionReasonReasoningRedacted` and
  `ElisionReasonRevoked` branches stay exactly as shipped: neither
  calls `admit`, so neither can reach the new spool write by
  construction, not by an added conditional.

## Tests

`contextplan/contextplan_test/`, extending the existing external test
package.

- `plan_spool_test.go` (new file, keeping `plan_test.go` under the
  500-line limit):
  - Nil spooler: `NewPlanner(store, cache, nil)` then `Plan` over a
    session that overflows the window. Every `Elision.SpoolRef` stays
    empty. This proves the nil-safe fallback the API section
    promises.
  - Window overflow spools: a wired `*spool.Spool` over an in-memory
    `ContentStore` test double. A session over budget. The dropped
    event's `Elision.SpoolRef` is non-empty, and `Spool.Load` with
    principal `record.Ref.SubjectID` and that ref returns the same
    bytes `contextstate.MemStore.Get` would have returned.
  - Retention-expired spools: a `RetentionCompliance` payload past its
    stub turn. `Elision.Kept` is still the stub length, unchanged from
    today, and `Elision.SpoolRef` is also non-empty, carrying the
    payload's full content, not the truncated stub.
  - Reasoning redacted never spools: a wired `*spool.Spool` with a
    `ContentStore` test double whose `Put` fails the test if called.
    A reasoning-kind event's `Elision.SpoolRef` stays empty and the
    test double's `Put` never runs.
  - Revoked never spools: same `Put`-fails-the-test double. A revoked
    event's `Elision.SpoolRef` stays empty and `Put` never runs. This
    is the security-relevant case this plan's Scope section names.
  - Spool write failure does not fail Plan: a `ContentStore` test
    double whose `Put` always errors. `Plan` still returns a nil
    error and the full, otherwise-correct `PlanResult`; the affected
    `Elision.SpoolRef` stays empty.
  - Spool budget does not fail Plan: one payload larger than a small
    `*spool.Spool`'s `maxGrantBytes` (triggers `ErrGrantTooLarge`).
    Leaves `SpoolRef` empty and `Plan`'s error nil.
  - Principal conflict does not fail Plan: `contextstate.Digest` keys
    a record by `Data` alone, so two `Put` calls with byte-identical
    `Data` and different `SubjectID` resolve to one record in one
    `*contextstate.MemStore`, not two — this case cannot trigger
    `ErrPrincipalConflict` from inside a single store. The test
    instead builds two separate `*contextstate.MemStore` instances,
    each holding one byte-identical, over-budget payload under a
    different `SubjectID`, runs `Plan` against each store with one
    shared `*spool.Spool`, and asserts the second `Plan` call's
    `SpoolRef` stays empty while its own error stays nil:
    `spool.Spool`'s grant map is keyed by the shared `ContentStore`'s
    ref, so the second store's write collides with the first store's
    grant even though the two `MemStore`s never share state directly.
    The shared `ContentStore` double must compute `ref` deterministically
    from `data`, for example sha256 of the bytes, matching
    `spool/spool_test/spool_test.go`'s `fakeStore` — not from call
    order or a counter. A call-order-keyed double never collides, so
    the test would pass without ever reaching `ErrPrincipalConflict`.
  - Principal is the content's own subject: two payloads with
    different `Ref.SubjectID` values, both over budget, spooled
    through one shared `*spool.Spool`. `Spool.Load` for the first
    payload's ref with the second payload's `SubjectID` fails with
    `spool.ErrWrongPrincipal`. Proves `Plan` never reuses one
    caller-level principal across payloads.
  - Existing `plan_test.go` cases stay green unchanged, called with
    `spooler` set to `nil`, proving the added parameter changes
    nothing for a caller that opts out.
  - Concurrent use with a wired spool: extends `plan_test.go`'s
    existing N-goroutine, one-shared-`*Planner` concurrency case (run
    under `go test -race`) with a non-nil, shared `*spool.Spool`. Each
    goroutine's own overflowed events resolve to a non-empty
    `SpoolRef` that its own `Spool.Load` call, with that event's
    `SubjectID`, round-trips. `spool.Spool` is already mutex-guarded
    and separately race-tested in `spool/spool_test/`; this case
    proves `contextplan`'s new call site adds no race of its own.
- `plan_spool_integration_test.go` (new file): seeds a
  `*contextstate.MemStore` with a session that overflows a small
  `Window`, wires a real `*spool.Spool` over a `memory.Store` (which
  already satisfies `spool.ContentStore`), runs `Plan`, then resolves
  every non-empty `Elision.SpoolRef` back through `Spool.Load` and
  asserts the recovered bytes match the original session content.
  This is the cut-now, retrieve-later round trip the phase exists
  for.
- No new test targets an empty `record.Ref.SubjectID` reaching
  `Spool.Spool`. `contextstate.PayloadRecord.Validate`, run by every
  `MemStore.Put`, already rejects an empty `SubjectID` before a record
  can enter the store `Plan` reads from.

## Verification

- `policy/layers.json` gains `spool` to the `contextplan` row, so the
  row reads `["contextstate", "provider", "memory", "spool"]`. This
  row must land before any code in this phase.
- `make verify` passes: gofmt, vet, tests, doc gate, structure gate,
  Semgrep scan, and probes.
- `go test -race ./contextplan/... ./spool/...` passes, including the
  new spool-wiring cases.
- `api/contextplan.txt` lands via `make api-update`, matching the API
  section above exactly. `api/spool.txt` shows no diff.
- Coverage floor of 85 holds for `contextplan` and for the total.
- `python3 scripts/check_plan.py` passes: `docs/plans/contextplan.md`
  gains this phase's contract, folded in the same way earlier phases
  folded into their package plan, so the gate's per-package plan file
  keeps matching the shipped code.
- `python3 scripts/check_deps.py` passes with the new `spool` edge on
  the `contextplan` row and no edge added anywhere else.
- `python3 scripts/check_prose.py` and `python3 scripts/check_labels.py`
  pass on this file and on the updated `docs/plans/contextplan.md`.
- `docs/plans/spool.md` gains one short note, in its Goal section,
  that `contextplan` now consumes it as its expected caller, replacing
  the present "it does not yet consume spool" line.
- `docs/packages/contextplan.md`, if it lists `NewPlanner`'s
  signature or the `Elision` fields, changes in the same commit as
  the code, per the docs-maintenance rule that a doc disagreeing with
  code is a bug.
