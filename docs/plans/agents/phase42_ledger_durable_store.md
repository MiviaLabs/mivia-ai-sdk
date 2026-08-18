# Phase 42: ledger durable store (modernc.org/sqlite)

Status: future. Plan-only; round 1 of plan review found issues; this
is the revision that answers them.
`ledger` (phase 34) has shipped; see `docs/plans/ledger.md`. This
phase extends `ledger` with a second `Store` implementation. It does
not touch `Ledger`, `TaskState`, or any existing sentinel error.

This plan covers `SQLiteStore` only. A related but separate concern,
a bounded-entry-cap knob on `MemStore`, moved to its own plan,
`docs/plans/agents/phase42b_memstore_bounded_cap.md`, so each phase
stays independently reviewable and revertible. See that document for
the `MemStoreOptions` design and its idempotency-preserving eviction
rule.

## Revision note (third revision, plan-review round 1)

The plan-reviewer found two issues with the second revision, both
fixed in this revision:

- The second revision bundled two unrelated concerns, the tag-gated
  `SQLiteStore` addition and the default-build `MemStoreOptions`
  bounded-entry-cap knob, into one phase. This revision splits the
  knob out into `docs/plans/agents/phase42b_memstore_bounded_cap.md`,
  where its idempotency-preserving eviction design and test now live
  too (see that document's Revision note for the reason the eviction
  design changed shape).
- The second revision's `make verify-ledger-sqlite` target (see
  Verification below) ran the tag-gated `SQLiteStore` test suite with
  no coverage requirement attached, so the entire tag-gated
  implementation shipped with no enforced coverage floor. This
  revision adds an explicit coverage check to that target, holding
  `SQLiteStore` to the same 85% floor every other package in this
  module holds to.

## Revision note (second revision)

The first draft of this plan recommended a stdlib-only file-based
`Store` and left a database-backed `Store` blocked on a user decision.
The user chose the database-backed option and initially named
`github.com/tursodatabase/go-libsql`. Research for that revision found
`go-libsql` needs `CGO_ENABLED=1`, a C toolchain, and ships prebuilt
native libraries for four platform combinations only, no Windows.

The user has since reconsidered, given that finding, and named
`modernc.org/sqlite` instead: the same driver
`/home/mac/projects/mivialabs/mivia-agent`'s own durable ledger and
event store already use in production (`internal/storage/sqlite.go`,
pinned at `modernc.org/sqlite v1.54.0` in that repository's `go.mod`).
This revision drops every `go-libsql` reference and designs the store
around `modernc.org/sqlite` instead. The stdlib-only file-based option
stays declined, for the record, unchanged from the first revision.

## Goal

Give `ledger` a second, durable `Store` implementation, backed by
`modernc.org/sqlite`, so a task record survives a process restart on
one host. `SQLiteStore` is strictly optional. `MemStore` stays the
default `Store` `ledger.New` falls back to when a caller passes a nil
`Store`, exactly as it does today, and this phase changes neither
`New`'s signature nor its nil-`Store` default. A caller who never
constructs a `*SQLiteStore` never imports `modernc.org/sqlite`: the
package is gated behind the `ledger_sqlite` build tag (see "The
build-tag question" below), so `MemStore`-only code pays no
dependency cost, no larger `go.sum`, and no larger binary, whether it
is built inside this module or vendored as this SDK's own dependency
by a downstream consumer.

`SQLiteStore` is a thin `Store`-interface adapter over an existing,
well-established SQL engine, not an application that owns its
database. It ships the smallest schema `TaskState` needs, created
idempotently on first open, and no migration tooling, no
schema-version table, and no ORM-shaped abstraction layer. See
"Schema and `CompareAndSwap` mapping" below for the exact, single-table
shape and "Scope" for this boundary stated as an explicit exclusion.

This phase does not touch `MemStore`. A bounded-entry-cap knob for
`MemStore` is a separate concern, planned in
`docs/plans/agents/phase42b_memstore_bounded_cap.md`, since it needs
no third-party import and stands on its own review.

## What `modernc.org/sqlite` actually is, verified before designing against it

This plan does not carry the `go-libsql` design forward with a
find-and-replace. It re-verified the real driver against its own
source and against `mivia-agent`'s own working, shipped usage before
writing anything below.

- `modernc.org/sqlite` is a pure-Go transpile of SQLite's own C source
  (via the `modernc.org/ccgo` toolchain, itself a build-time-only
  dependency of the driver, not a runtime one). It needs no cgo and no
  C toolchain. Confirmed directly: pulling the module (`go mod tidy`
  against a probe importing it, pinned to `v1.54.0` to match
  `mivia-agent`'s own pin) produces a dependency closure with zero
  cgo-only build constraints anywhere in it, unlike `go-libsql`'s
  `//go:build cgo` file.
- It registers a `database/sql` driver named `"sqlite"`
  (`sql.Register(driverName, newDriver())`, `driverName = "sqlite"`,
  confirmed by reading the module's own `sqlite.go`), not `"libsql"`.
  `mivia-agent`'s `internal/storage/sqlite.go` opens it with
  `sql.Open("sqlite", sqliteDSN(path))`, confirming the driver name
  and the DSN shape used in real, shipped code.
- `mivia-agent`'s `internal/storage/sqlite_dsn.go` builds its DSN as
  the file path plus a query string of `_pragma=` parameters:
  `_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=
  foreign_keys(1)&_pragma=busy_timeout(5000)`, with a documented
  escape hatch (a `file:` URI form) for a path containing a literal
  `?`, since the driver splits a DSN at the first literal `?` to
  separate the filename from its query parameters. `internal/storage/
  sqlite.go`'s `OpenSQLite` additionally executes the same four
  `PRAGMA` statements explicitly after open, with a comment stating
  this is "per-connection parity:" the DSN-level `_pragma=` params
  apply per new pooled connection, so the explicit `PRAGMA` calls
  after open are not redundant, they cover the first connection before
  the pool has cycled.
- `mivia-agent`'s `OpenSQLite` also calls `db.SetMaxOpenConns(8)` and
  `db.SetMaxIdleConns(8)`. SQLite-family engines serialize writers
  regardless of the Go-level connection pool size; a bounded pool plus
  `busy_timeout` together are what keeps a concurrent writer from
  returning `SQLITE_BUSY` immediately instead of waiting its turn.
  This plan mirrors both settings, not only the `PRAGMA`s, since they
  solve the same real, previously-hit problem together.
- The real dependency closure was checked with a probe `go mod tidy`
  pinned to `v1.54.0` (see "go.mod and go.sum" below for the full,
  verified list): `modernc.org/libc`, `modernc.org/mathutil`,
  `modernc.org/memory`, `github.com/dustin/go-humanize`,
  `github.com/google/uuid` (already allowed from the `mcp` exception),
  `github.com/mattn/go-isatty`, `github.com/ncruces/go-strftime`,
  `github.com/remyoudompheng/bigfft`, and `golang.org/x/sys` (already
  allowed). This is a materially different, and smaller, closure than
  `go-libsql`'s: no `.a` files, no cgo LDFLAGS, no antlr parser.

## Two options were weighed; Option B is chosen, now with a pure-Go driver

### Option A (declined): a stdlib-only file-based `Store`

A `FileStore` backed by one JSON file per instance, coordinated with
an `O_CREATE|O_EXCL` lock file for single-writer, single-host safety.
Zero new dependency, but single-host only, no lock-staleness recovery,
and a whole-file rewrite per write. The user reviewed this option and
chose Option B; this plan does not build `FileStore`.

### Option B (chosen): a `Store` backed by `modernc.org/sqlite`

A new `SQLiteStore` type inside the `ledger` package, backed by
`modernc.org/sqlite` through the standard `database/sql` package.
`SQLiteStore` is ordinary `database/sql` code once the driver is
blank-imported, not a bespoke API surface, the same shape
`mivia-agent`'s own `internal/storage.SQLite` already proves in
production. The DSN is a plain file path, or `":memory:"` for an
in-process database with no file, both standard SQLite conventions
this driver honors like any other SQLite-family driver.

## The build-tag question, reconsidered

The first, `go-libsql`-based revision gated the file behind
`//go:build ledger_libsql && cgo`, for two reasons: keeping the
default build free of a C-toolchain requirement, and keeping the
default build free of the driver's prebuilt native libraries. Neither
reason applies to `modernc.org/sqlite`: it needs no cgo, ships no
native binary, and builds identically on every platform Go itself
supports, Windows included. Carrying the old `&& cgo` half of the
build constraint forward would be stale rationale attached to a driver
it no longer describes.

A build tag is still kept, renamed to `ledger_sqlite`, dropping the
`&& cgo` clause entirely (`//go:build ledger_sqlite` alone), for a
different, narrower reason than before: dependency-footprint
isolation for a same-package addition, not cgo or platform avoidance.

- `ledger`'s `LibsqlStore`... now `SQLiteStore`... lives in the same
  package as `MemStore`, not a separate importable package.
  `a2aclient` and `mcp` never needed a build tag for their own
  third-party imports, because each is already its own package: a
  caller who only wants `envelope` or `tools` never imports
  `a2aclient` or `mcp` in the first place, so their dependency cost
  lands only on a caller who explicitly imports one of them. `ledger`
  does not have that natural boundary for a same-package addition: a
  caller who imports `ledger` only for `MemStore` and `Ledger` would,
  without a build tag, still pull `modernc.org/sqlite` and its
  transitive closure (nine modules, none of them tiny; `modernc.org/
  libc` alone is a substantial transpiled C standard library) into
  their `go.sum` and their binary, whether or not they ever call
  `NewSQLiteStore`.
- `//go:build ledger_sqlite` keeps that cost opt-in at compile time,
  matching the opt-in-by-import property `a2aclient` and `mcp` get for
  free from being separate packages, without moving `SQLiteStore` into
  a new top-level package this phase does not otherwise call for. A
  caller who wants `SQLiteStore` builds and tests with
  `-tags ledger_sqlite`; every other caller of `ledger` never sees the
  dependency at all, in `go build`, in `go vet`, or in `go.sum`
  resolution for their own module once this SDK is vendored or built
  as a dependency.
- This is a real, considered tradeoff, not the old cgo rationale
  copied forward unchanged: a caller who wants `SQLiteStore` pays one
  extra `-tags` flag, a materially smaller cost than the old
  `CGO_ENABLED=1`-plus-C-toolchain requirement, in exchange for every
  `MemStore`-only caller keeping a smaller dependency graph.

`make verify`, `go build ./...`, and the default `go test ./...`
remain unaffected: none of them pass `-tags ledger_sqlite`, so
`sqlite_store.go` never compiles into the default build, and
`policy/layers.json`'s existing `"ledger": ["machine", "events"]` row
still fully describes every internal edge the default build reaches.
Building and testing the SQLite-backed store is a separate, documented
command: `go test -tags ledger_sqlite ./ledger/...` (no `-race`
requirement beyond what the store's own race test already states
below; SQLite's own single-writer serialization is exercised by that
test, not worked around by it).

Semgrep does not evaluate Go build tags; `semgrep/sdk-standards.yml`'s
scoped exception below applies to `sqlite_store.go` by its file path,
independent of whether a given `go build` invocation would include the
file. The two mechanisms (the Go build tag and the Semgrep path scope)
answer different questions and both must hold: the build tag keeps the
dependency out of the default binary, and the Semgrep scope keeps the
import restricted to the one named module even when the tag is used.

### Schema and `CompareAndSwap` mapping

Unchanged in shape from the `go-libsql` draft: libSQL is a SQLite
fork, and `modernc.org/sqlite` is SQLite itself, so the same table,
the same two-statement `CompareAndSwap` mapping, and the same
`Rev`-bump-inline approach all carry over verbatim. Neither driver
needs a different SQL dialect for this schema; the only real
differences are the driver name (`"sqlite"`, not `"libsql"`), the DSN
shape (a plain path plus `_pragma=` params, not a URL-scheme-switched
DSN), and the `PRAGMA` statements `mivia-agent`'s own usage adds for
correctness under concurrent access, folded in below.

One table, created by `SQLiteStore`'s own constructor with
`CREATE TABLE IF NOT EXISTS`, so no external migration tool is needed:

```sql
CREATE TABLE IF NOT EXISTS ledger_tasks (
    key         TEXT PRIMARY KEY,
    status      TEXT NOT NULL,
    sequence    INTEGER NOT NULL,
    owner       TEXT NOT NULL DEFAULT '',
    fence       INTEGER NOT NULL DEFAULT 0,
    lease_until TEXT NOT NULL DEFAULT '',
    needs       TEXT NOT NULL DEFAULT '[]',
    blocked_by  TEXT NOT NULL DEFAULT '',
    task        BLOB,
    rev         INTEGER NOT NULL DEFAULT 0
);
```

`lease_until` is stored as `time.Time.Format(time.RFC3339Nano)` text,
matching `TaskState.LeaseUntil`'s round-trip needs without a lossy
integer conversion. `needs` is the JSON encoding of
`[]IdempotencyKey`. `task` is the JSON encoding of `TaskState.Task`,
`encoding/json`'s best-effort marshaling of the caller's `any` value.
This is a real, disclosed limitation `MemStore` does not share:
`MemStore` holds `Task` as a live Go value with no serialization
boundary; `SQLiteStore` can only durably store a `Task` value
`encoding/json.Marshal` can encode and a matching `Unmarshal` target
can decode back into the caller's own type. A caller storing a
channel, a function, or an unexported-field-only struct in `Task`
cannot use `SQLiteStore`; `MemStore` stays the store for that case.

`CompareAndSwap(ctx, key, old, new)` maps onto one of two SQL
statements, chosen by the same zero-value-`old` check `MemStore`
already uses (`ledger/store.go:73`):

- When `old` is the zero value (`Sequence == 0 && Status == "" &&
  Fence == 0 && Rev == 0`), insert-if-absent:

  ```sql
  INSERT INTO ledger_tasks
      (key, status, sequence, owner, fence, lease_until, needs, blocked_by, task, rev)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
  ON CONFLICT(key) DO NOTHING
  ```

  A `RowsAffected` of 1 is `true, nil`; 0 (the key already has a row)
  is `false, nil`, with no error, matching `MemStore`'s own contract
  for a losing insert-if-absent attempt.

- Otherwise, a conditional update carrying every compare field in its
  `WHERE` clause, and the `Rev` bump inline in the same statement:

  ```sql
  UPDATE ledger_tasks
  SET status = ?, sequence = ?, owner = ?, fence = ?, lease_until = ?,
      needs = ?, blocked_by = ?, task = ?, rev = rev + 1
  WHERE key = ? AND sequence = ? AND status = ? AND fence = ? AND rev = ?
  ```

  binding `new`'s fields first, then `key` and `old`'s four compare
  fields last. A `RowsAffected` of 1 is `true, nil`; 0 (no row matched
  the key and the full compare tuple, whether the key is absent or the
  tuple is stale) is `false, nil`, again with no error. This is a
  single statement; SQLite serializes writers at the statement level,
  so no explicit `BEGIN`/`COMMIT` wrapper is needed around either
  branch: each branch is already one atomic write, and the `busy_
  timeout` pragma below is what makes a concurrent writer wait its
  turn instead of failing immediately with `SQLITE_BUSY`.

This gives `SQLiteStore` the exact same `(Sequence, Status, Fence,
Rev)` compare contract `ledger/store.go:20` documents for every
`Store` implementation, with `Rev`'s bump happening inside the SQL
statement itself instead of in Go, so a concurrent writer against the
same row from a second process is still serialized correctly by the
database, not only by an in-process mutex the way `MemStore` and
Option A's `FileStore` design would need.

`Load(ctx, key)` runs one `SELECT ... WHERE key = ?`; `sql.ErrNoRows`
maps to `found == false, err == nil`, matching `Store.Load`'s
contract. `Range(ctx, fn)` runs one unfiltered `SELECT`, materializes
every row into a Go slice before closing the `*sql.Rows` cursor, then
calls `fn` once per row after the cursor is closed, matching the
no-reentrant-call rule `ledger/store.go:22` states and the same
collect-then-iterate shape Option A's `FileStore` design already used
for the same reason.

### The DSN and the pragmas, mirrored from `mivia-agent`'s own fix

`sqliteDSN(path string) string`, an unexported helper in `ledger/
sqlite_store.go`, mirrors `mivia-agent`'s `internal/storage/
sqlite_dsn.go` algorithm exactly, including the reason stated in that
file's own comment: the driver splits a DSN at the first literal `?`,
so a path containing `?` would silently truncate to the wrong
filename without this escape:

```go
const pragmaDSNParams = "_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

func sqliteDSN(path string) string {
	if strings.Contains(path, "?") {
		return "file:" + url.PathEscape(path) + "?" + pragmaDSNParams
	}
	return path + "?" + pragmaDSNParams
}
```

`NewSQLiteStore`'s constructor additionally executes the same four
`PRAGMA` statements explicitly after `sql.Open`, for the same
per-connection-parity reason `mivia-agent`'s own comment states, and
calls `db.SetMaxOpenConns(8)` and `db.SetMaxIdleConns(8)`, mirroring
`OpenSQLite`'s own pool sizing. `ledger_tasks` carries no foreign-key
relationship of its own; `foreign_keys=ON` is still set, matching
`mivia-agent`'s exact pragma set rather than trimming one for a
one-table schema that does not need it today.

### Connection lifecycle

- `NewSQLiteStore(path string) (*SQLiteStore, error)` — opens `path`
  through `sql.Open("sqlite", sqliteDSN(path))` (lazy; no connection
  yet), sets the pool sizes and pragmas above, runs the
  `CREATE TABLE IF NOT EXISTS` statement, then `Ping`s once to fail
  fast on a bad path or a permissions error, instead of surfacing that
  failure on the first `Load` or `CompareAndSwap` call. `path` may be
  `":memory:"` for an in-process database with no file, the driver's
  own standard SQLite convention, useful for a test that wants no
  filesystem dependency at all.
- `(*SQLiteStore) Close() error` — closes the underlying `*sql.DB`.
  Idempotent, matching `database/sql`'s own `Close` contract.

`MemStore` stays the default, zero-dependency `Store`, and `New`'s
nil-`Store` fallback is unchanged. `SQLiteStore` is an additional,
opt-in implementation a caller builds and passes to `New` explicitly,
behind the `ledger_sqlite` build tag.

## Scope

Inside: `SQLiteStore`, `NewSQLiteStore`, `(*SQLiteStore) Close`, its
`Store` interface implementation (`Load`, `CompareAndSwap`, `Range`),
the `ledger_tasks` schema, the `sqliteDSN` helper, and the
`ledger_sqlite` build-tag boundary.

Outside: Option A's `FileStore`, declined above. Outside: any remote
or replicated SQLite mode; `modernc.org/sqlite` is a local-file (or
in-memory) driver only, unlike `go-libsql`'s remote-Turso and
embedded-replica modes, so this phase carries no remote connection
constructor and no `LibsqlOption`-shaped option type forward from the
declined `go-libsql` draft. A caller who needs a remote, replicated
`Store` builds one against a different driver, outside this phase's
scope. Outside: any migration tool between `MemStore` and
`SQLiteStore`; a caller who wants to move a running `MemStore`'s state
onto a SQLite-backed store uses `Snapshot` and `Restore`
(`ledger/snapshot.go`), which already work against any `Store`,
`SQLiteStore` included, with no `SQLiteStore`-specific code. Outside:
schema migration tooling, a schema-version tracking table, or any
ORM-shaped abstraction over `database/sql`. `SQLiteStore` is a thin
`Store`-interface adapter, one table, created idempotently with
`CREATE TABLE IF NOT EXISTS` on first open; it is not an application
that owns and evolves its own database over time, and this phase
builds nothing beyond what satisfying `Store` against SQLite needs.
Outside: any change to `Ledger`, `TaskState`, or any sentinel error.
Outside: an eviction or capacity knob on `SQLiteStore`; a durable,
disk-backed store has no equivalent in-process-growth concern the way
`MemStore` does, since its cost lives on disk, not in the process's
own heap. Outside: `MemStoreOptions`, `NewMemStoreWithOptions`, and
any change to `MemStore`; see
`docs/plans/agents/phase42b_memstore_bounded_cap.md` for that
follow-on phase.

## API

One part lands in `api/ledger.txt`, tag-gated: `SQLiteStore` compiles
and locks only when the `ledger_sqlite` tag is used;
`make api-update`'s own invocation for this phase runs with
`-tags ledger_sqlite` so the symbols below actually appear in the
generated lock, matching how a tag-gated file's exported surface is
captured.

- `type SQLiteStore struct { ... }` (unexported fields) — a `Store`
  backed by a local SQLite database file (or `":memory:"`) through
  `modernc.org/sqlite`, reached over `database/sql`.
- `func NewSQLiteStore(path string) (*SQLiteStore, error)` — opens
  `path` (a file path, or `":memory:"`), creates the schema if absent,
  sets the connection pool size and the WAL/synchronous/foreign-keys/
  busy-timeout pragmas, and `Ping`s once to fail fast.
- `func (s *SQLiteStore) Close() error` — closes the underlying
  `*sql.DB`. Idempotent.
- `func (s *SQLiteStore) Load(ctx context.Context, key IdempotencyKey)
  (TaskState, bool, error)` — matches `Store.Load`'s contract exactly.
- `func (s *SQLiteStore) CompareAndSwap(ctx context.Context, key
  IdempotencyKey, old TaskState, new TaskState) (bool, error)` — the
  two-statement mapping described above, matching `Store.
  CompareAndSwap`'s `(Sequence, Status, Fence, Rev)` compare contract
  and its `Rev`-bump-on-every-write rule exactly.
- `func (s *SQLiteStore) Range(ctx context.Context, fn
  func(TaskState) bool) error` — matches `Store.Range`'s
  materialize-then-iterate, no-reentrant-call contract exactly.

No change to `Store`, `Ledger`, `TaskState`, `Snapshot`, or any
existing sentinel error. No `LibsqlOption`-shaped type; this revision
drops it along with the remote and embedded-replica modes it existed
to configure.

## Tests

Test files for `SQLiteStore` live directly in `ledger/`, as
`package ledger`, not in `ledger/ledger_test/`. This breaks from
`ledger`'s established flat `<pkg>/<pkg>_test/` layout for the same
reason `docs/plans/mcp.md`'s Tests section states for its own package:
`sdk-standards.yml`'s scoped third-party-import exception below
applies to `/ledger/*.go` only, not to a nested `ledger_test/`
directory, so a test file that imports `modernc.org/sqlite` (or that
needs the driver registered to open a `"sqlite"` DSN through
`database/sql` directly, for a white-box assertion against the raw
schema) must sit in the same directory the exception covers. Every
file below also carries `//go:build ledger_sqlite`, so it compiles and
runs only under `go test -tags ledger_sqlite ./ledger/...`, never
under the module's default `go test ./...`.

- `sqlite_store_test.go` — red-green cases against a `t.TempDir()`
  file path and against `":memory:"`: the same case shape `mem_
  store_test.go` already runs against `MemStore` (`ledger/ledger_test/
  mem_store_test.go`): `CompareAndSwap` rejects a mismatched
  `(Sequence, Status, Fence, Rev)` tuple, accepts a zero-value `old`
  against an absent key, and bumps `Rev` by one on every successful
  write; `Range` visits every record exactly once. Adds
  `SQLiteStore`-specific cases: `Load` against a key with no row
  returns `found == false, err == nil`; a `Task` value `encoding/
  json.Marshal` cannot encode (for example a value holding a channel)
  returns a non-nil error from `CompareAndSwap`, proving the disclosed
  JSON-serialization limit is a real, surfaced error, not a silent
  data loss; a second `*SQLiteStore` opened against the same file path
  after the first is `Close`d still `Load`s every prior record, the
  same "survives past one Go value's lifetime" proxy Option A's
  declined design used for "survives a process restart"; `sqliteDSN`
  cases for a plain path and for a path containing a literal `?`,
  asserting the `file:`-URI escape branch produces a DSN the driver
  actually opens against the intended file, not a truncated one.
- `sqlite_store_race_test.go` — two goroutines call `CompareAndSwap`
  concurrently against the same file-backed `*SQLiteStore` for the
  same key, run under `go test -tags ledger_sqlite -race
  ./ledger/...`; asserts exactly one winner, matching `ledger`'s
  existing `*_race_test.go` convention (`ledger/ledger_test/
  claim_race_test.go` and its four siblings) for the concurrent-write
  proof. This test is the concrete proof the `busy_timeout` pragma
  above does its job: without it, a second writer's statement would
  fail immediately with `SQLITE_BUSY` instead of waiting and then
  losing the compare cleanly through `RowsAffected == 0`.
- `sqlite_store_scenario_test.go` — reuses `durablefence.Scenario`,
  wired against a `Ledger` constructed with `New(sqliteStore, bus)`
  instead of `New(nil, bus)`, and calls `durablefence.RunAll`. This
  proves `SQLiteStore` satisfies every claim, takeover, and fence
  invariant `MemStore` already proves, matching `ledger.md`'s existing
  `durablefence` composition and Option A's declined design's same
  reuse plan. No new `durablefence` wiring is needed beyond building a
  `Scenario` literal against `SQLiteStore`-backed `Ledger` methods, the
  same shape `ledger`'s own `ledger_test/scenario_test.go` already
  builds against a `MemStore`-backed `Ledger`.
- `sqlite_store_bench_test.go` — `CompareAndSwap` throughput against
  `SQLiteStore` versus the existing `admit_bench_test.go` baseline
  against `MemStore`, reporting ops/sec and allocs/op with no fixed
  budget: `SQLiteStore`'s per-call disk I/O and pragma-driven fsync
  behavior are the expected slower path, and this benchmark records
  that gap rather than gating on it, the same rationale Option A's
  declined `FileStore` design gave for its own benchmark.

## Verification

- `make verify` passes for the default build: gofmt, vet, the default
  test run, the doc gate, the structure gate, the Semgrep scan and
  probes, and the coverage floor all run as they do today.
  `sqlite_store*.go` never compiles without `-tags ledger_sqlite`, so
  it contributes zero lines to this default coverage run, and this
  phase changes no other default-build file, so the default coverage
  floor is unaffected by this phase.
- A second, explicitly documented command verifies the tag-gated code:
  `go test -tags ledger_sqlite -race -cover ./ledger/...`. Unlike the
  `go-libsql` draft, this needs no C toolchain and no `CGO_ENABLED=1`;
  it runs on every platform the module's default build already
  targets. This command is not part of the default `make verify`
  target; the builder adds a separate Makefile target (for example
  `make verify-ledger-sqlite`) that runs it, documented in this same
  change, so the command has one canonical name instead of living only
  in a comment. This target must check the reported `ledger` package
  coverage from the `-tags ledger_sqlite` run against the same 85%
  floor `make verify`'s default-build coverage block enforces: the
  tag-gated build compiles `sqlite_store*.go` into the `ledger`
  package alongside every default-build file, so this one command's
  coverage number is the true, complete `ledger` coverage for that
  build, including `SQLiteStore`. A tag-gated build reporting below 85%
  fails `make verify-ledger-sqlite`, exactly as a default-build report
  below 85% fails `make verify`. This closes the gap the first plan
  review round found: without this check, `SQLiteStore` shipped with
  no enforced coverage floor at all.
- `python3 scripts/check_structure.py` passes: `sqlite_store.go` stays
  at or below 500 lines and every function at or below 80 lines, the
  same limit every other file in this module holds to, regardless of
  its build tag; the gate scans file text, not a `go build` output, so
  the tag does not exempt the file from this check.
- `api/ledger.txt` gains `SQLiteStore`, `NewSQLiteStore`, and
  `(*SQLiteStore) Close`'s exported peers (`Load`, `CompareAndSwap`,
  `Range`) through `make api-update -tags ledger_sqlite` (or the
  equivalent tag-aware invocation the builder adds), committed in the
  same change as the code.
- `policy/layers.json`'s `"ledger": ["machine", "events"]` row is
  unchanged: `modernc.org/sqlite` is a third-party module, not an
  internal package edge, so it is governed by the mechanisms below,
  not by `policy/layers.json`.

### AGENTS.md: the third exception, now authorized, naming the corrected module

The user has explicitly authorized this exception, naming `modernc.org/
sqlite` specifically, superseding the earlier `go-libsql` naming. The
builder edits this exact line in `AGENTS.md`, in the same change as
the code, extending the existing two-exception sentence to three:

```text
Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
`google.golang.org/grpc`; `mcp` may import
`github.com/modelcontextprotocol/go-sdk`; `ledger` may import
`modernc.org/sqlite`, behind the `ledger_sqlite` build tag only; no
other package may add a third-party import without its own plan
review.
```

The `ledger` bullet in `AGENTS.md`'s Layout section (currently
"Imports machine and events only.") gains a second sentence, matching
the shape of the existing `mcp/` bullet:

```text
- `ledger/` — the durable-task-admission primitive: idempotency-keyed
  admission, a leased claim with a monotonic fence, renewal, a stale
  takeover, and dependency-driven blocking on failure. Imports machine
  and events internally. `SQLiteStore`, behind the `ledger_sqlite`
  build tag, additionally imports the pure-Go `modernc.org/sqlite`
  driver externally; the default build never compiles it.
```

### Semgrep: scoped stdlib-only exception

Mirrors `docs/plans/mcp.md`'s pattern exactly, for the same rule, with
`ledger` in place of `mcp`, naming `modernc.org/sqlite` in place of
the earlier draft's `go-libsql` path. The build to implement, by the
builder, in `semgrep/sdk-standards.yml`:

- Add `"/ledger/*.go"` to `sdk.go.stdlib-only-imports`'s
  `paths.exclude` list, alongside the existing `a2aclient` and `mcp`
  exclusions.
- Add a new rule, `sdk.go.ledger-scoped-third-party-import`,
  `severity: ERROR`, scoped to `paths.include: ["/ledger/*.go"]`. It
  reuses the same `pattern-regex` that finds a dotted-domain import
  string, and adds a `pattern-not-regex` permitting only
  `"modernc\.org/sqlite(/[^"\n]*)?"` in addition to the existing
  module-path exemptions the `a2aclient`- and `mcp`-scoped rules each
  carry for their own module. Any other third-party import inside
  `ledger/*.go` still fires this rule, so the rest of `ledger`'s own
  files (`ledger.go`, `store.go`, `task_state.go`, and so on) stay
  held to the same stdlib-only bar they hold to today; only the one
  named import path is permitted anywhere in the directory.

`scripts/check_semgrep_probes.py` gains a new probe case, parallel to
the existing `a2aclient`- and `mcp`-scoped blocks: a `ledger/`
subdirectory under the probe's temp root, holding
`viol_ledger_other_import.go` (importing an unrelated third-party
path) and `clean_modernc_sqlite_import.go` (importing
`modernc.org/sqlite`). Both basenames register in `expected` against
`sdk.go.ledger-scoped-third-party-import`, and an explicit assertion
block, parallel to the `mcp` one, proves: the new rule fires on
`viol_ledger_other_import.go` and stays silent on
`clean_modernc_sqlite_import.go`; `sdk.go.stdlib-only-imports` fires
on neither, proving the scoped exclude took effect; and the existing
outside-the-directory probe files still prove
`sdk.go.stdlib-only-imports` fires normally outside all three scoped
directories.

### go.mod and go.sum: the closed dependency allowlist, extended (re-verified)

No Go-version change lands in this phase. `go.mod` already declares
`go 1.25.0`; `modernc.org/sqlite v1.54.0`'s own `go.mod` targets an
older floor this module's own `go 1.25.0` already clears.

`go.mod` gains a `require` line for `modernc.org/sqlite`, plus the
indirect lines `go mod tidy` adds beneath it. This revision re-ran the
verification against the real module, pinned to `v1.54.0` to match
`mivia-agent`'s own pin, superseding every `go-libsql`-derived number
the first revision recorded:

```text
require modernc.org/sqlite v1.54.0

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
```

`go.sum` carries hash lines, several `/go.mod`-only, for the wider
resolved build list `go mod tidy` produced beyond the `require` block
above: `github.com/google/pprof`, `github.com/hashicorp/golang-lru/v2`,
`golang.org/x/mod`, `golang.org/x/tools` (already allowed),
`modernc.org/ccgo/v4`, `modernc.org/cc/v4`, `modernc.org/fileutil`,
`modernc.org/gc/v2`, `modernc.org/gc/v3`, `modernc.org/goabi0`,
`modernc.org/opt`, `modernc.org/sortutil`, `modernc.org/strutil`, and
`modernc.org/token`. These are `modernc.org/libc`'s and `modernc.org/
sqlite`'s own build-time transpilation toolchain dependencies
(`cc`/`ccgo` generate the Go source the driver ships from C, at the
driver's own release time, not at this module's build time), present
in the resolved module graph `go.sum` records even though this
module's own build does not invoke them. `golang.org/x/sync` and
`golang.org/x/sys` are already in `ALLOWED_MODULES` from the `a2aclient`
and `mcp` exceptions; `github.com/google/uuid` is already allowed from
`mcp`'s own closure.

`scripts/check_gomod.py`'s `check_gosum` function checks every module
path in `go.sum`, not only `go.mod`'s `require` block, so
`ALLOWED_MODULES` must cover the full set above, not only the nine
directly required modules. `ALLOWED_MODULES` gains the following,
beyond what `a2aclient` and `mcp` already permit:

```python
ALLOWED_MODULES |= {
    "modernc.org/sqlite",
    "modernc.org/libc",
    "modernc.org/mathutil",
    "modernc.org/memory",
    "modernc.org/ccgo/v4",
    "modernc.org/cc/v4",
    "modernc.org/fileutil",
    "modernc.org/gc/v2",
    "modernc.org/gc/v3",
    "modernc.org/goabi0",
    "modernc.org/opt",
    "modernc.org/sortutil",
    "modernc.org/strutil",
    "modernc.org/token",
    "github.com/dustin/go-humanize",
    "github.com/mattn/go-isatty",
    "github.com/ncruces/go-strftime",
    "github.com/remyoudompheng/bigfft",
    "github.com/google/pprof",
    "github.com/hashicorp/golang-lru/v2",
    "golang.org/x/mod",
}
```

None of the `go-libsql`-derived entries the first revision recorded
(`github.com/tursodatabase/go-libsql`, `github.com/antlr4-go/antlr/v4`,
`github.com/libsql/sqlite-antlr4-parser`, `github.com/pkg/errors`,
`golang.org/x/exp`, `gotest.tools`) are needed; this revision does not
carry any of them into `ALLOWED_MODULES`.

The builder runs `go mod tidy` once `ledger/sqlite_store.go` imports
`modernc.org/sqlite`, records the resulting `require` and `go.sum`
module set, and reconciles `ALLOWED_MODULES` against that real output
in the same change, trimming any entry above `go mod tidy` does not
actually add, the same reconciliation step `docs/plans/mcp.md` records
for its own allowlist. `check_gomod.py`'s module docstring, which
today names only the `a2aclient` and `mcp` exceptions, gains one
sentence naming the `ledger` exception too, in the same change,
stating the `ledger_sqlite` build-tag qualifier the other two
exceptions do not carry and naming `modernc.org/sqlite`, not
`go-libsql`.

### Summary

`make verify` passes for the default build once this phase lands,
unchanged, since the tag-gated `SQLiteStore` file never enters that
build. A second, explicit verification command
(`go test -tags ledger_sqlite -race -cover ./ledger/...`, or the
Makefile target that wraps it) must also pass, including its 85%
coverage check, before this phase is considered done, covering: the
`SQLiteStore` code and its tag-gated tests; the `go.mod` and `go.sum`
additions; the `check_gomod.py` allowlist extension and docstring
update; the two `semgrep/sdk-standards.yml` rule changes and the new
`check_semgrep_probes.py` case (these three run under the default,
non-tag-gated `make verify` since Semgrep does not evaluate build
tags); the `AGENTS.md` exception-sentence and `ledger/` layout-bullet
edits; and the `docs/architecture.md` and `docs/plans/ledger.md` doc
updates, naming `SQLiteStore` next to `MemStore` with the same
documented Task-serialization and build-tag-boundary limits this plan
states. `docs/protocol-design.md` does not change: this phase adds no
message-semantics rule to the envelope wire format. The `MemStore`
bounded-entry-cap knob is a separate phase; see
`docs/plans/agents/phase42b_memstore_bounded_cap.md`.
