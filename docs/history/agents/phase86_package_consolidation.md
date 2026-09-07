# Phase 86: Package consolidation

Fold single-concern peer packages into their consumers. Move test-only
fixtures out of the public surface. Quarantine orphaned packages behind
an `x/` sub-module. The review finding that orders this work classifies
every package and cycles-checks every merge against
`policy/layers.json`.

## Goal

Reduce the public package count from forty-nine to roughly thirty.
Keep the code, the tests, and the layering behavior-equivalent. A fold
moves files and rewrites import paths. It does not change behavior.
Peer SDKs of this size hide plumbing under `internal/`. This SDK ships
every helper at the top level. The review finding applies Go's own
module-layout guidance and the merge rule: combine packages the user
must import together.

## Scope

Merge, cycle-checked, no behavior change:

- `identity` into `envelope`.
- `hooks` into `events`.
- `a2aack` into `a2aclient`.
- `contextsummary` into `contextplan`.
- `usage` and `providerregistry` into `provider`.
- `secretpath` into `workspace`.
- `taskrun` into `ledger`.
- `trigger` into `scheduler`.
- `discovery` and `heartbeat` into `flow`, the workflow package.
- `toolcallctx` unexported inside `agentloop`.
- `spool` into `memory`, one memory package.

Keep split, per the finding: `contextref`, `contextbudget`.

Test fixtures out of the public surface:

- `durablefence` to `ledger/ledgertest`.
- `a2aloopback` to `a2aclient/a2atest`.
- `e2e` to `internal/e2e`.

Quarantine behind an `x/` sub-module with its own `go.mod`:
`contextstate`, `contextsession`, `longtermmemory`, `skills`,
`envfile`, `runconfig`.

Deviation from the finding: `channel` stays in the core module. The
finding lists it as internal-shaped, but `agentrun`, `subagent`, and
`e2e` import it from non-test code. Quarantining it would break the
composition layer. The finding's own rule, no behavior change, wins.

Known entanglements the plan must resolve:

- `agentloop/agentloop_test` imports `contextsession` and
  `contextstate`. Those integration tests move into the `x/` module,
  beside the packages they exercise.
- `workspace/workspace_test` imports `envfile`. That usage moves with
  the quarantine or the test seeds the environment directly, without
  dropping an assertion.
- The sibling consumer repo imports `envfile`, `skills`, and `mcp`.
  The quarantine changes those import paths to `x/...`. The sibling
  repo needs a follow-up import-path bump. This plan records the
  breakage; it cannot fix the other checkout.

Collisions to resolve at fold time: `hooks.Handler` and `hooks.New`
against `events`; `trigger.New` against `scheduler`;
`heartbeat.New` against `flow`; `contextsummary.ErrNoMessages`
against `contextplan`; `taskrun.ErrNoKey` against `ledger` (its own
comment forbids merging the sentinels); `spool.ErrNoBudget` and
`spool.ErrUnknownRef` against `memory`. Rename the incoming side.

## API

Every fold is an API change on the moved surface. The lock path moves
with the symbols: `make api-update` regenerates `api/`, and the
`api/<old>.txt` lock is deleted with the package. Colliding symbols
rename on the incoming side. `toolcallctx`'s exported symbols become
unexported; its pending_symbols entries are removed. Renamed or
re-pathed symbols re-key their `policy/pending_symbols.json` entries.
Removed packages drop their `policy/pending_wiring.json` entries.

## Tests

Test files move with their package. A folded package's external test
directory becomes test files of the target's own test layout. Test
function names stay stable where the target has no clash. Where a
clash exists, rename the incoming test and record the rename in the
commit message. No test is deleted, skipped, or weakened. Coverage
stays at or above the 85 percent floor for every package.

## Verification

Run `make verify-fast` after every fold and commit per fold. Run
`make verify` before the final report. Update `docs/architecture.md`'s
package count and map, `docs/README.md`, `AGENTS.md`'s layout list,
and the `docs/packages/` reference pages in the same change. An
adversarial review pass runs over the whole diff before the final
commit.

## Renames

A fold that lands inside a target package with an existing name of
the same shape renames the incoming symbol. Nine renames from this
phase:

| Old name | New name |
| --- | --- |
| `hooks.Handler` | `events.HookHandler` |
| `hooks.New` | `events.NewRegistry` |
| `contextsummary.ErrNoMessages` | `contextplan.ErrNoMessagesToSummarize` |
| `usage.New` | `provider.NewAccumulator` |
| `providerregistry.New` | `provider.NewRegistry` |
| `taskrun.ErrNoKey` | `ledger.ErrNoTaskKey` |
| `trigger.New` | `scheduler.NewRegistry` |
| `heartbeat.New` | `flow.NewMonitor` |
| `spool.ErrNoBudget` | `memory.ErrNoGrantBudget` |
