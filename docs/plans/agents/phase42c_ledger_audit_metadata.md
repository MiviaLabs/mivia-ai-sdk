# Phase 42c: ledger audit metadata (actor and timestamps)

Status: plan only. Follow-on to phase 42
(`docs/plans/agents/phase42_ledger_durable_store.md`, shipped at
commit 2d6e1cb, merged at dddd8dc). This document supersedes
`docs/plans/agents/phase42c_sqlite_store_timestamps.md`, an
uncommitted planner draft from this same working session, never
committed to git, superseded before commit. That draft planned two
`SQLiteStore`-only, non-exported columns. The user widened the scope:
`created_at`/`updated_at` and `created_by`/`updated_by` become real
`TaskState` fields, threaded through `Ledger` by a new `Actor`
parameter on every mutating method. This is a bigger, exported-API
change, not a schema-only shortcut.

## Goal

Give every `TaskState` record a recorded creation time, last-update
time, creating actor, and last-updating actor. A caller who reads a
record through `Ledger.State`, `Snapshot`, or a raw `SQLiteStore`
query can then answer "when was this admitted" and "who performed
the last write," in Go and on disk alike. Split the `SQLiteStore` DDL
out of `ledger/sqlite_store.go` into its own file, so the schema is
easy to find and read on its own.

## Scope

Inside:

- A new exported type, `Actor`, a free-form caller-chosen identifier
  for whoever performs a write. `Actor` does not model a closed set
  of actor kinds; a caller may pass a user ID, an agent ID, or any
  other string it finds meaningful.
- Four new `TaskState` fields: `CreatedBy Actor`, `CreatedAt
  time.Time`, `UpdatedBy Actor`, `UpdatedAt time.Time`.
- An `actor Actor` parameter and, where absent today, a `now
  time.Time` parameter, added to `Admit`, `Claim`, `Renew`,
  `Release`, `Takeover`, and `Complete`.
- `Ledger` setting `CreatedBy`/`CreatedAt` once, on `Admit`'s insert
  path, and `UpdatedBy`/`UpdatedAt` on every successful write across
  all six methods.
- `SQLiteStore` schema growth: four new `TEXT` columns on
  `ledger_tasks`, and a startup migration for a database file created
  under the pre-this-phase column set.
- A new file, `ledger/sqlite_schema.go` (tag `ledger_sqlite`), holding
  the table DDL and the migration column list. `sqlite_store.go` keeps
  the write-path and migration-runner Go code; the schema text moves
  out.
- Updating every existing call site inside `ledger/` that calls one
  of the six methods, to pass the new argument(s). See "Call site
  checklist" below.

Outside:

- Any change to `Store`'s own interface signature
  (`Load`/`CompareAndSwap`/`Range` keep their current parameter
  lists). `CompareAndSwap` already takes a full `TaskState` for `new`
  and compares `old` on `(Sequence, Status, Fence, Rev)`; the new
  fields ride inside `TaskState` like every other field `Store`
  already forwards untouched. `MemStore` needs no code change beyond
  what a normal Go struct-literal field addition gives it for free.
- Any change to `Snapshot`, `Encode`, `Decode`, or `Restore`'s own
  function signatures. They already round-trip whatever `TaskState`
  contains, through `encoding/json` and through `Store.Range`/
  `CompareAndSwap`; the new fields need no code change there, only a
  new test asserting the round-trip actually carries them (see
  "Tests").
- A schema-version table or a general migration framework. Phase 42
  declined both ("no migration tooling, no schema-version table" in
  `docs/plans/agents/phase42_ledger_durable_store.md`'s Goal
  section). This phase's migration step is a second, equally fixed,
  hardcoded, idempotent `PRAGMA table_info` check plus up to four
  `ALTER TABLE ... ADD COLUMN` statements. It adds no version table
  and no general-purpose migration runner, so it stays inside that
  boundary.
- Any change to `machine`, `events`, or any package outside `ledger`.
- Any new import or `policy/layers.json` edge. `ledger`'s allowed
  imports stay `["machine", "events"]`.
- A new `TaskState.Validate` rule for the four new fields. A zero
  `CreatedAt`/`CreatedBy` or `UpdatedAt`/`UpdatedBy` is a valid value:
  it is what a `SQLiteStore`-migrated legacy row reads back as, and
  `Validate` has no invariant to state about it beyond what `Ledger`'s
  write path already guarantees for a record it wrote itself.
- Any change to `docs/protocol-design.md`. `ledger` carries no signed
  or threaded wire form; this phase changes no envelope semantics.

## Design decisions

### Where the actor and timestamp data lives

`TaskState` gains the four fields directly, set by `Ledger` before
each `Store.CompareAndSwap` call. This keeps `Store`'s own interface
untouched: `MemStore` already stores full `TaskState` values, so it
carries the new fields for free, and `SQLiteStore` gains four columns
mapped the same way it already maps every other `TaskState` field.
The alternative, adding an `actor` parameter to
`Store.CompareAndSwap` itself, would touch the shared `Store`
contract both backends implement, for no benefit: every field
`CompareAndSwap` needs is already reachable through `new TaskState`.

### Actor type

```go
// Actor is the caller-chosen identity of whoever performs a write:
// an external user ID, an agent ID, or any other identifier the
// caller finds meaningful. Ledger does not validate its shape.
type Actor string
```

Defined in `ledger/task_state.go`, next to `OwnerID`, following that
type's own precedent.

### Method signatures

Every mutating method gains `actor Actor` as the parameter
immediately after `ctx`. A method that does not already take `now
time.Time` gains one, as its new last parameter, for the same
testability reason `Claim`, `Renew`, and `Takeover` already take a
caller-supplied `now`: a test asserts an exact `UpdatedAt` value
without depending on the wall clock. `Admit`'s `now` lands
immediately before its variadic `needs ...IdempotencyKey`, since Go
requires a variadic parameter to be last.

Before and after, per method:

- `Admit(ctx, key, seq, task, needs...)` becomes
  `Admit(ctx, actor, key, seq, task, now, needs...)`.
- `Claim(ctx, key, owner, lease, now)` becomes
  `Claim(ctx, actor, key, owner, lease, now)`.
- `Renew(ctx, key, owner, fence, lease, now)` becomes
  `Renew(ctx, actor, key, owner, fence, lease, now)`.
- `Release(ctx, key, owner, fence)` becomes
  `Release(ctx, actor, key, owner, fence, now)`.
- `Takeover(ctx, key, owner, lease, now)` becomes
  `Takeover(ctx, actor, key, owner, lease, now)`.
- `Complete(ctx, key, owner, fence, status)` becomes
  `Complete(ctx, actor, key, owner, fence, status, now)`.

`Complete`'s failure path calls two internal, non-exported helpers
that also write records and so must thread the same `actor` and `now`
`Complete` received. Their signatures change to match:

- `blockDependents(ctx, failed)` becomes `blockDependents(ctx
  context.Context, actor Actor, now time.Time, failed IdempotencyKey)
  error`. `Complete` calls it with its own `actor`/`now` after its own
  write succeeds.
- `blockOne(ctx, k, cur, failed)` becomes `blockOne(ctx
  context.Context, actor Actor, now time.Time, k IdempotencyKey, cur
  TaskState, failed IdempotencyKey) error`. `blockDependents` calls it
  once per dependent, forwarding the same `actor`/`now` unchanged.

This is an exported-API break for every existing caller of these six
methods. There is no backward-compatible way to add a required
parameter to an exported method; a caller updates its call sites in
the same change that upgrades its `ledger` import.

`Owner` and `Actor` stay distinct fields with distinct meanings.
`Owner` names the current lease holder of a claimed record; `Actor`
names whoever performed the write that produced the current record
state, which may differ from `Owner` (for example, an operator
forcing a `Release` on another owner's behalf, once such a call
exists; today every write's `actor` and `owner` argument typically
match, but `Ledger` does not enforce that).

### Write-path rule

- `Admit`'s insert branch sets `CreatedBy = actor`, `CreatedAt = now`,
  `UpdatedBy = actor`, `UpdatedAt = now`, all from the same `now`
  value passed into that call.
- Every other successful write, across all six methods including
  `Admit`'s own rebase branch (a later `Admit` call that legally
  rebases a non-terminal record onto a higher `Sequence`), sets only
  `UpdatedBy = actor` and `UpdatedAt = now`. `CreatedBy` and
  `CreatedAt` are carried forward unchanged from the loaded record,
  the same way `Rev` is already forwarded.
- `blockDependents`/`blockOne` inside `Complete`'s failure path also
  count as a write to each dependent's record. They set that
  dependent's `UpdatedBy` to the same `actor` that called `Complete`
  on the failed key, and `UpdatedAt` to that call's `now`. This
  matches "the caller who caused this state change," since the
  dependent's own owner did not act.

### Timestamp source

`Claim`, `Renew`, `Takeover` already take `now`; this phase reuses
it unchanged for `UpdatedAt`. `Admit`, `Release`, and `Complete` gain
`now` as a new parameter instead of calling `time.Now()` inside
`Ledger`, for two reasons: consistency, since three of six methods
already establish the caller-supplied-clock convention, and
testability, since a test asserts an exact stored timestamp without
a `time.Now()` race or a sleep. `Ledger` never calls `time.Now()`
itself.

### MemStore

`MemStore.CompareAndSwap` stores the full `new TaskState` value into
its map, by value, on every successful write today
(`ledger/store.go`). `Actor` is a string type and `time.Time` is an
ordinary comparable struct; a plain Go value copy carries both
correctly. `MemStore` needs no code change. No shallow-copy or
zero-value hazard exists: a `TaskState` value assignment already
copies every field, old and new alike.

### SQLiteStore schema file

`ledger/sqlite_schema.go` (tag `ledger_sqlite`) holds:

- `createLedgerTasksTable`, moved from `sqlite_store.go`, updated
  with four new `TEXT` columns: `created_at`, `updated_at`,
  `created_by`, `updated_by`, each `NOT NULL DEFAULT ''`.
- `selectLedgerTaskColumns`, `insertLedgerTask`, `updateLedgerTask`,
  moved from `sqlite_store.go` and extended with the four new
  columns.
- `auditColumns`, a fixed `[]string` of the four new column names, in
  a fixed order, consumed by the migration check.

One file is enough. Four new columns and one migration check do not
justify a second file split between "base schema" and "migration
steps"; that split earns its place only if a third schema change
follows. Plain Go string constants, not `//go:embed` `.sql` files,
match this repository's existing convention: every SQL statement in
`sqlite_store.go` already lives as a Go string constant, and
`//go:embed` would introduce a second convention for the same kind of
text, plus a build-time file-presence dependency this stdlib-only,
no-ORM package does not otherwise have.

`sqlite_store.go` keeps the Go code: `NewSQLiteStore`, the migration
runner function, `CompareAndSwap`'s parameter binding, and
`scanTaskState`. It imports the constants from
`sqlite_schema.go` at compile time, as they are both `package
ledger`.

### Migration

`NewSQLiteStore` runs a `migrateAuditColumns(db *sql.DB) error`
helper right after the existing `CREATE TABLE IF NOT EXISTS` call and
before `Ping`, mirroring the shape phase 42c's superseded draft
already specified:

1. Query `PRAGMA table_info(ledger_tasks)`; collect the returned
   column names into a set.
2. For each name in `auditColumns`, in order, if absent from the set,
   run `ALTER TABLE ledger_tasks ADD COLUMN <name> TEXT NOT NULL
   DEFAULT ''`.
3. If any `ALTER TABLE` statement errors, `NewSQLiteStore` closes
   `db` and returns a wrapped error, matching the existing
   `CREATE TABLE` and `Ping` failure handling.

A fresh database never enters the `ALTER TABLE` branch: the base
`CREATE TABLE IF NOT EXISTS` already declares all four columns, so
`PRAGMA table_info` finds them present and the loop runs zero
`ALTER TABLE` statements. Opening an already-migrated file a second
time is equally a no-op, proving idempotency.

Concurrent open safety: `sqlite_store.go` already sets `PRAGMA
busy_timeout=5000` on every connection, so a second process opening
the same file mid-migration blocks on the first process's write lock
for up to five seconds and then sees the fully migrated schema,
never a torn, partially-altered one.

Backfill value for a pre-existing row: the empty string, for all
four columns, matching the superseded draft's reasoning for
`created_at`/`updated_at` and applied the same way to
`created_by`/`updated_by`. A migrated row has no true historical
actor or time; that history does not exist. A migration-time
timestamp would look identical to a real one to a later reader,
while an empty string is a visible, honest "no historical value"
signal. A row's `UpdatedBy`/`UpdatedAt` (and, for a still-live
record, `CreatedBy`/`CreatedAt`, which the `UPDATE` branch never
touches) reads as the zero value until that row's next successful
`Ledger` write through this phase's new code path.

### Call site checklist

Every direct caller of `Admit`, `Claim`, `Renew`, `Release`,
`Takeover`, or `Complete` inside this module needs a new `actor`
argument, and a new `now` argument where the method gained one. The
full list, found by grep across the module:

- `ledger/ledger_test/admit_test.go`
- `ledger/ledger_test/admit_race_test.go`
- `ledger/ledger_test/admit_bench_test.go`
- `ledger/ledger_test/admit_integration_test.go`
- `ledger/ledger_test/claim_test.go`
- `ledger/ledger_test/claim_race_test.go`
- `ledger/ledger_test/renew_race_test.go`
- `ledger/ledger_test/takeover_test.go`
- `ledger/ledger_test/takeover_race_test.go`
- `ledger/ledger_test/complete_test.go`
- `ledger/ledger_test/complete_race_test.go`
- `ledger/ledger_test/snapshot_test.go`
- `ledger/ledger_test/scenario_test.go`
- `ledger/ledger_test/context_test.go`
- `ledger/ledger_test/helpers_test.go` (the shared fixture builders
  other files in this list reuse; update it first)
- `ledger/sqlite_store_scenario_test.go`

`ledger/ledger_test/mem_store_test.go` and
`ledger/ledger_test/task_state_test.go` build `TaskState` literals
directly and call `Store.Load`/`CompareAndSwap`, not `Ledger`
methods; they need new field coverage (see "Tests") but no new
method-call argument.

`ledger/ledger_test/wire_fuzz_test.go`, `ledger/sqlite_store_test.go`,
`ledger/sqlite_store_race_test.go`, and
`ledger/sqlite_store_bench_test.go` call `ledger.Decode` or
`Store.CompareAndSwap`/`Load`/`Range` directly, never a `Ledger`
method; they need no call-site change from this checklist.
`ledger/sqlite_store_test.go` gains no new content in this phase: every
new `CompareAndSwap`-audit case and every new migration case moves
into the two new files described under "Tests"
(`ledger/sqlite_store_audit_test.go` and
`ledger/sqlite_store_migration_test.go`), so the existing file's line
count does not grow.

No other package in the module calls a `Ledger` method today:
`durablefence`'s `Scenario` interface (`durablefence/checks.go`)
defines its own `Claim`/`Release`/`Takeover` methods for a generic
claim/fence conformance kit unrelated to `ledger.Ledger`, and no
package under `agent/` imports `ledger` yet. Neither needs a change.

## API

`api/ledger.txt` changes are real and land through `make api-update`
in the same change as the code:

- New type: `type Actor string`.
- `TaskState` struct gains four fields: `CreatedBy Actor`, `CreatedAt
  time.Time`, `UpdatedBy Actor`, `UpdatedAt time.Time`.
- Changed method signatures on `*Ledger`: `Admit`, `Claim`, `Renew`,
  `Release`, `Takeover`, `Complete`, per "Method signatures" above.
- No change to `Store`, `MemStore`, `SQLiteStore`'s own method
  signatures, `Snapshot`, `Encode`, `Decode`, `Restore`, `State`,
  `Blocked`, or any sentinel error.

The builder runs `make api-update` (which already builds with `-tags
ledger_sqlite`, per `scripts/api_surface.go`'s `alwaysBuildTags`) and
commits the `api/ledger.txt` diff in the same change as the code.

## Tests

- `ledger/ledger_test/task_state_test.go`: extend
  `TaskState.Validate` table cases to include the four new fields at
  their zero value, asserting `Validate` accepts them (no new rule to
  violate).
- `ledger/ledger_test/admit_test.go`: `Admit` sets `CreatedBy` and
  `CreatedAt` from its `actor`/`now` arguments on first insert, and
  also sets `UpdatedBy`/`UpdatedAt` to the same values on that first
  write. A second `Admit` call that legally rebases the record (a
  higher `Sequence` against a non-terminal status) with a different
  `actor`/`now` leaves `CreatedBy`/`CreatedAt` unchanged and updates
  `UpdatedBy`/`UpdatedAt`.
- `ledger/ledger_test/claim_test.go`, `renew_race_test.go` (or its
  sibling non-race file), `takeover_test.go`, `complete_test.go`:
  each asserts its own method updates `UpdatedBy`/`UpdatedAt` and
  leaves `CreatedBy`/`CreatedAt` untouched from the prior `Admit`.
- `ledger/ledger_test/complete_test.go` or `complete_race_test.go`:
  `blockDependents` sets a blocked dependent's `UpdatedBy`/`UpdatedAt`
  to the failing `Complete` call's `actor`/`now`.
- `ledger/ledger_test/mem_store_test.go`: a `TaskState` literal
  carrying all four new fields round-trips through
  `MemStore.CompareAndSwap` and `Load` unchanged.
- `ledger/ledger_test/snapshot_test.go`: `Snapshot`/`Encode`/`Decode`
  round-trip the four new fields, extending the existing
  full-field-round-trip case.
- `ledger/sqlite_store_test.go` gains no new test functions in this
  phase. It stays at its pre-phase 429 lines, aside from whatever
  incidental line moves land through the unrelated schema-file split
  in "Scope" (`createLedgerTasksTable` and friends move to
  `ledger/sqlite_schema.go`, not into this file). Every new
  `CompareAndSwap`-audit case and every new migration case lands in
  one of the two new files below, so this file gets no headroom
  pressure from this phase.
- A new file, `ledger/sqlite_store_audit_test.go` (tag `ledger_sqlite`,
  matching the existing `sqlite_store_*_test.go` files' build-tag
  convention), holding two new `CompareAndSwap`-audit cases. Each case
  is the same shape as the sibling functions already in
  `sqlite_store_test.go` (raw `CompareAndSwap` call, raw-SQL
  column read-back, assert), 45 to 79 lines apiece there; keeping them
  in their own file avoids adding that same weight to a file already
  at 429 lines:
  - `TestCompareAndSwapInsertSetsCreatedAndUpdatedAudit`: a fresh
    `NewSQLiteStore`; `CompareAndSwap`'s insert branch through a
    `TaskState` carrying `CreatedBy`/`CreatedAt`/`UpdatedBy`/
    `UpdatedAt`; reads all four columns back with a raw query and
    asserts they match.
  - `TestCompareAndSwapUpdateChangesUpdatedAuditOnly`: insert, then
    update with a different `UpdatedBy`/`UpdatedAt`; asserts
    `created_at`/`created_by` are byte-identical to the insert-time
    values and `updated_at`/`updated_by` changed.
- A new file, `ledger/sqlite_store_migration_test.go` (tag
  `ledger_sqlite`, matching the existing `sqlite_store_*_test.go`
  files' build-tag convention), holding three new migration cases.
  Migration behavior is a distinct concern from `CompareAndSwap`
  audit-field behavior, so it gets its own file rather than sharing
  `sqlite_store_audit_test.go`:
  - `TestNewSQLiteStoreMigratesPreexistingSchema`: creates
    `ledger_tasks` with a raw `CREATE TABLE` matching the exact
    pre-this-phase column list (no audit columns), inserts one row,
    closes that `*sql.DB`, opens `NewSQLiteStore` against the same
    file, asserts no error, asserts the pre-existing row is still
    present with its other columns unchanged (via `Load` or a raw
    query), asserts `PRAGMA table_info` now lists all four audit
    columns, and asserts each reads back as the empty string for that
    row.
  - `TestNewSQLiteStoreMigrationIsIdempotent`: calls `NewSQLiteStore`
    twice against the same already-migrated file (closing the first
    store first), asserting the second open returns no error and
    performs no `ALTER TABLE` against a column that already exists.
  - `TestNewSQLiteStoreMigratesPartialSchema`: creates `ledger_tasks`
    with a raw `CREATE TABLE` at the pre-this-phase column list, then
    a raw `ALTER TABLE ledger_tasks ADD COLUMN created_at TEXT NOT
    NULL DEFAULT ''` and `ADD COLUMN created_by TEXT NOT NULL DEFAULT
    ''` before opening `NewSQLiteStore`, leaving `updated_at`/
    `updated_by` absent. Opens `NewSQLiteStore` against that file,
    asserts no error, asserts `PRAGMA table_info` lists all four audit
    columns afterward, and asserts `created_at`/`created_by` keep
    their pre-existing values (a raw query before and after the open
    matches) while only `updated_at`/`updated_by` get added.
- `ledger/sqlite_store_scenario_test.go` and
  `ledger/sqlite_store_race_test.go`: updated call sites only, per
  the checklist; both continue to exercise the fresh-schema migration
  path (zero `ALTER TABLE` statements) unmodified otherwise.
- `ledger/ledger_test/helpers_test.go`: shared fixture builders gain
  a default test `Actor` constant and pass it through every call they
  wrap, so the other updated test files can opt into asserting a
  specific `Actor`/`now` only where the test cares.

## Verification

- `go test -race -cover ./ledger/...` passes; `ledger`'s coverage and
  the module total stay at or above the 85% floor.
- `go test -tags ledger_sqlite -race -cover ./ledger/...`
  (`make verify-ledger-sqlite`) passes, including the two new
  `sqlite_store_audit_test.go` cases and the three new
  `sqlite_store_migration_test.go` cases, at or above the same floor.
- `make verify` passes: gofmt, vet, tests, the doc gate, the
  structure gate, the Semgrep scan and probes, and the coverage
  floor.
- `make api-update` (default, and confirmed identical under `-tags
  ledger_sqlite` per `scripts/api_surface.go`'s `alwaysBuildTags`)
  produces the `api/ledger.txt` diff listed under "API" above,
  committed in the same change as the code.
- `python3 scripts/check_structure.py` passes.
  `ledger/sqlite_schema.go`, the edited `ledger/sqlite_store.go`, the
  new `ledger/sqlite_store_audit_test.go`, and the new
  `ledger/sqlite_store_migration_test.go` all stay at or below 500
  lines; `migrateAuditColumns` and every other touched function stay
  at or below 80 lines. `ledger/sqlite_store_test.go` itself gains no
  new test functions in this phase, so it stays at its pre-phase 429
  lines (net of any incidental shrink from the DDL constants moving
  into `sqlite_schema.go`), leaving the same headroom it has today
  rather than growing toward the cap.
- `python3 scripts/check_deps.py` passes with no `policy/layers.json`
  change: `ledger`'s allowed imports stay `["machine", "events"]`.
- `python3 scripts/check_plan.py`, `python3 scripts/check_prose.py`,
  and `python3 scripts/check_labels.py` pass against this document.
- `docs/plans/ledger.md` gains one sentence cross-referencing this
  document, once the code lands, noting the `Actor` type and the four
  new `TaskState` fields.
- `docs/plans/agents/PHASES.md` line referencing the superseded,
  never-committed `phase42c_sqlite_store_timestamps.md` draft is
  rewritten to reference this document instead.
- No `docs/protocol-design.md` change: `ledger` carries no signed or
  threaded wire form.
- No `go.mod`, `go.sum`, or `semgrep/sdk-standards.yml` change: no new
  import, no new SQL string literal outside the existing
  constant-string convention this phase relocates, not invents.
