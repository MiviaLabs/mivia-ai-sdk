# Phase 42: ledger durable store (Turso libsql)

Status: future. Plan-only; it has not yet gone through plan review.
`ledger` (phase 34) has shipped; see `docs/plans/ledger.md`. This
phase extends `ledger` with a second `Store` implementation. It does
not touch `Ledger`, `TaskState`, or any existing sentinel error.

## Revision note

An earlier draft of this plan recommended a stdlib-only file-based
`Store` and left a database-backed `Store` as a design sketch blocked
on a user decision. The user has since reviewed both options and
chosen the database-backed option, naming
`github.com/tursodatabase/go-libsql`, the Turso-maintained Go driver
for libSQL (a SQLite fork), specifically. This exception is now
authorized. This revision commits to that choice and designs it in
full. The stdlib-only option is kept below only as the declined
alternative, for the record, the way `docs/plans/mcp.md` keeps its
superseded stdlib-reimplementation draft on record.

## A correction from research: this driver is not cgo-free

The task that authorized this phase described `go-libsql` as "a
pure-Go, cgo-free libsql client." That description does not match the
driver's real, current source. This plan verified the claim against
the actual module before writing anything else, the same way
`docs/plans/mcp.md` verified the MCP Go SDK's real API before its own
plan was written, and the real facts differ from the assumption:

- `go-libsql`'s only non-test source file, `libsql.go`, carries
  `//go:build cgo` at its top. Under `CGO_ENABLED=0`, the package
  compiles to zero exported symbols: no `Connector`, no
  `NewEmbeddedReplicaConnector`, nothing. There is no `!cgo` stub
  file. Any code that references those symbols fails to compile, not
  merely to run, in a `CGO_ENABLED=0` build.
- The module's own `README.md` states this directly: "`go-libsql`
  uses `CGO` to make calls to LibSQL. You must build your binaries
  with `CGO_ENABLED=1`."
- The module ships precompiled, prebuilt native static libraries
  (`.a` files) under `lib/{linux_amd64,linux_arm64,darwin_amd64,
  darwin_arm64}/`, roughly 34 to 52 MB per platform, linked in through
  `cgo LDFLAGS`. Only those four platform combinations are supported
  today; the module's own `README.md` states Windows and other
  architectures are not yet supported.
- This was confirmed by pulling the real module (verified against a
  probe module's `go.mod`/`go.sum` built with `go mod tidy` against
  `github.com/tursodatabase/go-libsql`, dated pseudo-version
  `v0.0.0-20260424063416-3051e37e6e04`) and reading `libsql.go`'s
  build constraint and cgo preamble directly, not from documentation
  alone.

This is a real constraint this plan must design around, not a detail
to gloss over. Left unconstrained, this dependency would force
`CGO_ENABLED=1` and a C toolchain onto every contributor's default
build the moment `ledger` imports it, breaking `go build ./...` and
`make verify` for anyone without one, and would silently pull two
platform combinations' worth of prebuilt binaries into every build
that does have cgo, even a build that never touches the
libsql-backed store. The design below isolates the dependency behind
an explicit, opt-in Go build tag so the default build stays exactly
as stdlib-only and cgo-free as it is today. See "The build-tag
boundary" below.

## Two options were weighed; Option B is now chosen

### Option A (declined): a stdlib-only file-based `Store`

A `FileStore` backed by one JSON file per instance, coordinated with
an `O_CREATE|O_EXCL` lock file for single-writer, single-host safety.
Zero new dependency, but single-host only, no lock-staleness
recovery, and a whole-file rewrite per write. The user reviewed this
option and chose Option B instead; this plan does not build `FileStore`.

### Option B (chosen): a `Store` backed by Turso's `go-libsql` driver

A new `LibsqlStore` type inside the `ledger` package, backed by
`github.com/tursodatabase/go-libsql` through the standard
`database/sql` package. libSQL is SQLite-compatible; `go-libsql`
registers itself as a `database/sql` driver named `"libsql"`
(`sql.Register("libsql", driver{})`), so `LibsqlStore` is ordinary
`database/sql` code once the driver is imported, not a bespoke
libsql-specific API surface.

The driver's `driver.OpenConnector` (verified in `libsql.go`) already
switches on the DSN's URL scheme, so one code path covers every mode
the user asked about:

- `":memory:"` or a `file:` scheme — a local libSQL file, SQLite's own
  on-disk format. No network. Matches "sqlite" in the user's own
  framing.
- `libsql:`, `https:`, or `http:` scheme, with an `authToken` query
  parameter — a live connection to a remote Turso database. This is
  the same driver, the same `Store` implementation, no second code
  path.
- A third mode, embedded replica, needs its own connector:
  `libsql.NewEmbeddedReplicaConnector(dbPath, primaryURL, opts...)`
  builds a local file that stays synced from a remote primary, either
  on demand (`Connector.Sync`) or on a timer (the driver's own
  `WithSyncInterval` option, verified against `libsql.go`'s `Option`
  list). `sql.OpenDB(connector)` turns that into the same `*sql.DB`
  shape the other two modes use.

### The build-tag boundary

`ledger/libsql_store.go` and its test files carry the build
constraint `//go:build ledger_libsql && cgo`, a custom tag combined
with Go's own automatic `cgo` tag. This is stricter than the plain
`//go:build cgo` `go-libsql` itself uses:

- `ledger_libsql` keeps the file out of every default `go build`,
  `go vet`, and `go test` invocation, even on a machine where cgo is
  available. A contributor who never asks for the libsql-backed store
  never links the 34-to-52-MB native library and never needs a C
  toolchain. This is the opt-in boundary a caller crosses on purpose,
  the same way a caller crosses `MemStore` versus this store on
  purpose today, but enforced at compile time, not only at the `Store`
  interface level.
- `cgo` (Go's built-in tag, true exactly when `CGO_ENABLED=1` and a C
  toolchain was found) keeps the file from referencing undefined
  `go-libsql` symbols on a machine that explicitly asks for
  `-tags ledger_libsql` but has no cgo available: the file drops out
  cleanly instead of failing with a confusing "undefined: libsql.X"
  error two packages deep.

`make verify`, `go build ./...`, and the default `go test ./...` are
unaffected by this phase: none of them pass `-tags ledger_libsql`, so
`libsql_store.go` never compiles into the default build, and
`policy/layers.json`'s existing `"ledger": ["machine", "events"]` row
still fully describes every internal edge the default build reaches.
Building and testing the libsql-backed store is a separate, documented
command: `go test -tags ledger_libsql -race ./ledger/...`.

Semgrep does not evaluate Go build tags; `semgrep/sdk-standards.yml`'s
scoped exception below applies to `libsql_store.go` by its file path,
independent of whether a given `go build` invocation would include the
file. The two mechanisms (the Go build tag and the Semgrep path scope)
answer different questions and both must hold: the build tag keeps the
dependency out of the default binary, and the Semgrep scope keeps the
import restricted to the one named module even when the tag is used.

### Schema and `CompareAndSwap` mapping

One table, created by `LibsqlStore`'s own constructor with
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
matching `TaskState.LeaseUntil`'s round-trip needs without a
lossy integer conversion. `needs` is the JSON encoding of
`[]IdempotencyKey`. `task` is the JSON encoding of `TaskState.Task`,
`encoding/json`'s best-effort marshaling of the caller's `any` value.
This is a real, disclosed limitation `MemStore` does not share:
`MemStore` holds `Task` as a live Go value with no serialization
boundary; `LibsqlStore` can only durably store a `Task` value
`encoding/json.Marshal` can encode and a matching `Unmarshal` target
can decode back into the caller's own type. A caller storing a
channel, a function, or an unexported-field-only struct in `Task`
cannot use `LibsqlStore`; `MemStore` stays the store for that case.

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
  single statement; SQLite-family engines (libSQL included) serialize
  writers at the statement level, so no explicit `BEGIN`/`COMMIT`
  wrapper is needed around either branch: each branch is already one
  atomic write.

This gives `LibsqlStore` the exact same `(Sequence, Status, Fence,
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

### Connection lifecycle

- `NewLibsqlStore(dsn string) (*LibsqlStore, error)` — opens `dsn`
  through `sql.Open("libsql", dsn)` (lazy; no connection yet), runs
  the `CREATE TABLE IF NOT EXISTS` statement, then `Ping`s once to
  fail fast on a bad DSN, an unreachable remote host, or a rejected
  auth token, instead of surfacing that failure on the first `Load` or
  `CompareAndSwap` call. Covers both the local-file and the direct
  remote-Turso modes, since the driver's own `OpenConnector` already
  branches on `dsn`'s scheme.
- `NewLibsqlEmbeddedReplicaStore(dbPath, primaryURL string, opts
  ...LibsqlOption) (*LibsqlStore, error)` — builds a
  `libsql.NewEmbeddedReplicaConnector(dbPath, primaryURL, ...)`
  connector, translating each `LibsqlOption` into the driver's own
  `libsql.Option` internally, then `sql.OpenDB(connector)`. Runs the
  same `CREATE TABLE IF NOT EXISTS` and `Ping` steps as
  `NewLibsqlStore`.
- `(*LibsqlStore) Close() error` — closes the underlying `*sql.DB`.
  Idempotent, matching `database/sql`'s own `Close` contract.

Neither constructor's exported signature carries a `go-libsql` type.
`LibsqlOption` is `ledger`'s own type, translated internally, the same
discipline `docs/plans/mcp.md`'s API section states for its own SDK
wrapping: "none re-exports an SDK type directly, so a caller of this
package needs no import of the SDK itself." A caller of
`NewLibsqlStore` or `NewLibsqlEmbeddedReplicaStore` needs no import of
`github.com/tursodatabase/go-libsql`; only `ledger/libsql_store.go`
itself does.

`MemStore` stays the default, zero-dependency `Store` and the default
`New` falls back to when a nil `Store` is passed; `LibsqlStore` is an
additional, opt-in implementation a caller builds and passes to `New`
explicitly, behind the `ledger_libsql` build tag. This phase makes no
change to `New`'s nil-`Store` default.

## Scope

Inside: `LibsqlStore`, `NewLibsqlStore`, `NewLibsqlEmbeddedReplicaStore`,
`LibsqlOption`, `WithLibsqlAuthToken`, `WithLibsqlSyncInterval`,
`(*LibsqlStore) Close`, its `Store` interface implementation (`Load`,
`CompareAndSwap`, `Range`), the `ledger_tasks` schema, and the
`ledger_libsql` build-tag boundary.

Outside: Option A's `FileStore`, declined above. Outside: any
migration tool between `MemStore` and `LibsqlStore`; a caller who
wants to move a running `MemStore`'s state onto a libsql-backed store
uses `Snapshot` and `Restore` (`ledger/snapshot.go`), which already
work against any `Store`, `LibsqlStore` included, with no
`LibsqlStore`-specific code. Outside: exposing every `go-libsql`
connector option through `LibsqlOption`; this phase covers an auth
token and a sync interval only, the two options the embedded-replica
mode needs to be useful; a caller needing a `go-libsql` option this
phase does not wrap builds a `*sql.DB` against the driver directly and
constructs `LibsqlStore` around it, a seam this phase's API section
below states explicitly. Outside: any change to `Ledger`, `TaskState`,
or any sentinel error.

## API

The surface below lands in `api/ledger.txt` via `make api-update`. It
is compiled and locked only when the `ledger_libsql` tag is used;
`make api-update`'s own invocation for this phase runs with
`-tags ledger_libsql` so the symbols below actually appear in the
generated lock, matching how a tag-gated file's exported surface is
captured.

- `type LibsqlStore struct { ... }` (unexported fields) — a `Store`
  backed by a libSQL database through `github.com/tursodatabase/
  go-libsql`, reached over `database/sql`. Supports a local file, a
  remote Turso database, or a synced embedded replica, chosen by which
  constructor built it.
- `func NewLibsqlStore(dsn string) (*LibsqlStore, error)` — opens
  `dsn` (a `file:` path, `:memory:`, or a `libsql:`/`https:`/`http:`
  remote URL with an `authToken` query parameter), creates the schema
  if absent, and `Ping`s once to fail fast.
- `func NewLibsqlEmbeddedReplicaStore(dbPath, primaryURL string, opts
  ...LibsqlOption) (*LibsqlStore, error)` — opens a local embedded
  replica at `dbPath` synced from `primaryURL`, creates the schema if
  absent, and `Ping`s once to fail fast.
- `type LibsqlOption` — a functional option for
  `NewLibsqlEmbeddedReplicaStore`, translated internally into the
  driver's own option type; no `go-libsql` type appears in its
  signature.
- `func WithLibsqlAuthToken(token string) LibsqlOption` — sets the
  auth token the embedded-replica connector uses against `primaryURL`.
- `func WithLibsqlSyncInterval(d time.Duration) LibsqlOption` — sets
  the connector's periodic auto-sync interval; omitted means
  sync-on-demand only (`Sync`, kept unexported: this phase does not
  need to expose a manual sync trigger since `Store`'s own interface
  has no such method, and every `CompareAndSwap` and `Load` already
  reads and writes through the connection the driver keeps current).
- `func (s *LibsqlStore) Close() error` — closes the underlying
  `*sql.DB`. Idempotent.
- `func (s *LibsqlStore) Load(ctx context.Context, key IdempotencyKey)
  (TaskState, bool, error)` — matches `Store.Load`'s contract exactly.
- `func (s *LibsqlStore) CompareAndSwap(ctx context.Context, key
  IdempotencyKey, old TaskState, new TaskState) (bool, error)` — the
  two-statement mapping described above, matching `Store.
  CompareAndSwap`'s `(Sequence, Status, Fence, Rev)` compare contract
  and its `Rev`-bump-on-every-write rule exactly.
- `func (s *LibsqlStore) Range(ctx context.Context, fn
  func(TaskState) bool) error` — matches `Store.Range`'s
  materialize-then-iterate, no-reentrant-call contract exactly.

No change to `Store`, `MemStore`, `Ledger`, `TaskState`, `Snapshot`,
or any existing sentinel error.

## Tests

Test files for `LibsqlStore` live directly in `ledger/`, as
`package ledger`, not in `ledger/ledger_test/`. This breaks from
`ledger`'s established flat `<pkg>/<pkg>_test/` layout for the same
reason `docs/plans/mcp.md`'s Tests section states for its own
package: `sdk-standards.yml`'s scoped third-party-import exception
below applies to `/ledger/*.go` only, not to a nested `ledger_test/`
directory, so a test file that imports `go-libsql` (or that needs the
driver registered to open a `"libsql"` DSN through `database/sql`
directly, for a white-box assertion against the raw schema) must sit
in the same directory the exception covers. Every file below also
carries `//go:build ledger_libsql && cgo`, so it compiles and runs
only under `go test -tags ledger_libsql ./ledger/...`, never under the
module's default `go test ./...`.

- `libsql_store_test.go` — red-green cases against a local file DSN in
  a `t.TempDir()` (no network, no Turso credentials needed): the same
  case shape `mem_store_test.go` already runs against `MemStore`
  (`ledger/ledger_test/mem_store_test.go`): `CompareAndSwap` rejects a
  mismatched `(Sequence, Status, Fence, Rev)` tuple, accepts a
  zero-value `old` against an absent key, and bumps `Rev` by one on
  every successful write; `Range` visits every record exactly once.
  Adds `LibsqlStore`-specific cases: `Load` against a key with no row
  returns `found == false, err == nil`; a `Task` value
  `encoding/json.Marshal` cannot encode (for example a value holding a
  channel) returns a non-nil error from `CompareAndSwap`, proving the
  disclosed JSON-serialization limit is a real, surfaced error, not a
  silent data loss; a second `*LibsqlStore` opened against the same
  file path after the first is `Close`d still `Load`s every prior
  record, the same "survives past one Go value's lifetime" proxy
  Option A's declined design used for "survives a process restart".
- `libsql_store_race_test.go` — two goroutines call `CompareAndSwap`
  concurrently against the same file-backed `*LibsqlStore` for the
  same key; asserts exactly one winner, matching `ledger`'s existing
  `*_race_test.go` convention (`ledger/ledger_test/claim_race_test.go`
  and its four siblings) for the concurrent-write proof, run under
  `go test -tags ledger_libsql -race ./ledger/...`.
- `libsql_store_scenario_test.go` — reuses `durablefence.Scenario`,
  wired against a `Ledger` constructed with `New(libsqlStore, bus)`
  instead of `New(nil, bus)`, and calls `durablefence.RunAll`. This
  proves `LibsqlStore` satisfies every claim, takeover, and fence
  invariant `MemStore` already proves, matching `ledger.md`'s existing
  `durablefence` composition and Option A's declined design's same
  reuse plan.
- `libsql_store_remote_test.go` — an integration test against a real
  Turso database, skipped unless two environment variables are both
  set: `LEDGER_LIBSQL_TEST_URL` and `LEDGER_LIBSQL_TEST_TOKEN`. Runs
  the same core `CompareAndSwap`/`Load`/`Range` case shape
  `libsql_store_test.go` runs locally, over a real remote connection,
  proving the DSN-scheme branch this plan's Schema section states
  ("`libsql:`, `https:`, or `http:` scheme") against a real Turso
  endpoint, not only the driver's own claim. This test is not run in
  this repository's own `make verify`, since it needs a live external
  credential this repository does not provision; a caller who wants
  to run it exports both variables and runs `go test -tags
  ledger_libsql ./ledger/... -run TestLibsqlStoreRemote`. This mirrors
  the environment-gated-skip pattern real-service Go driver test
  suites commonly use.
- `libsql_embedded_replica_test.go` — builds a
  `NewLibsqlEmbeddedReplicaStore` against a local `t.TempDir()` file
  and a fixture primary the test itself serves through a second,
  in-process `*LibsqlStore` reachable over a loopback libsql URL if
  the driver's remote scheme supports a loopback target, otherwise
  (if it does not) this case is folded into
  `libsql_store_remote_test.go`'s environment-gated skip instead; the
  builder confirms which shape the driver's real, released API
  supports before writing this file's final case list, the same
  "verify before writing" discipline this plan's own research applied
  to the driver's cgo requirement above.

## Verification

- `make verify` passes unchanged for the default build: `libsql_
  store*.go` never compiles without `-tags ledger_libsql`, so gofmt,
  vet, the default test run, the doc gate, the structure gate, the
  Semgrep scan and probes, and the coverage floor all run exactly as
  they do today, with zero new lines counted into the coverage
  computation.
- A second, explicitly documented command verifies the tag-gated code:
  `go test -tags ledger_libsql -race ./ledger/...`, run on a machine
  with `CGO_ENABLED=1` and a C toolchain, covering
  `libsql_store_test.go`, `libsql_store_race_test.go`, and
  `libsql_store_scenario_test.go` at minimum; the remote and
  embedded-replica test files run only when their own environment
  gates are satisfied. This command is not part of the default
  `make verify` target; the builder adds a separate Makefile target
  (for example `make verify-ledger-libsql`) that runs it, documented
  in this same change, so the command has one canonical name instead
  of living only in a comment.
- `python3 scripts/check_structure.py` passes: `libsql_store.go` stays
  at or below 500 lines and every function at or below 80 lines, the
  same limit every other file in this module holds to, regardless of
  its build tag; the gate scans file text, not a `go build` output, so
  the tag does not exempt the file from this check.
- `api/ledger.txt` gains `LibsqlStore`, `NewLibsqlStore`,
  `NewLibsqlEmbeddedReplicaStore`, `LibsqlOption`,
  `WithLibsqlAuthToken`, and `WithLibsqlSyncInterval` through
  `make api-update -tags ledger_libsql` (or the equivalent tag-aware
  invocation the builder adds), committed in the same change as the
  code.
- `policy/layers.json`'s `"ledger": ["machine", "events"]` row is
  unchanged: `go-libsql` is a third-party module, not an internal
  package edge, so it is governed by the mechanisms below, not by
  `policy/layers.json`.

### AGENTS.md: the third exception, now authorized

The user has explicitly authorized this exception; it is no longer
blocked. The builder edits this exact line in `AGENTS.md`, in the same
change as the code, extending the existing two-exception sentence to
three:

```text
Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
`google.golang.org/grpc`; `mcp` may import
`github.com/modelcontextprotocol/go-sdk`; `ledger` may import
`github.com/tursodatabase/go-libsql`, behind the `ledger_libsql` build
tag only; no other package may add a third-party import without its
own plan review.
```

The `ledger` bullet in `AGENTS.md`'s Layout section (currently
"Imports machine and events only.") gains a second sentence, matching
the shape of the existing `mcp/` bullet:

```text
- `ledger/` — the durable-task-admission primitive: idempotency-keyed
  admission, a leased claim with a monotonic fence, renewal, a stale
  takeover, and dependency-driven blocking on failure. Imports machine
  and events internally. `LibsqlStore`, behind the `ledger_libsql`
  build tag, additionally imports the Turso `go-libsql` driver
  (`github.com/tursodatabase/go-libsql`) externally; the default build
  never compiles it.
```

### Semgrep: scoped stdlib-only exception

Mirrors `docs/plans/mcp.md`'s pattern exactly, for the same rule, with
`ledger` in place of `mcp`. The build to implement, by the builder, in
`semgrep/sdk-standards.yml`:

- Add `"/ledger/*.go"` to `sdk.go.stdlib-only-imports`'s
  `paths.exclude` list, alongside the existing `a2aclient` and `mcp`
  exclusions.
- Add a new rule, `sdk.go.ledger-scoped-third-party-import`,
  `severity: ERROR`, scoped to `paths.include: ["/ledger/*.go"]`. It
  reuses the same `pattern-regex` that finds a dotted-domain import
  string, and adds a `pattern-not-regex` permitting only
  `"github\.com/tursodatabase/go-libsql(/[^"\n]*)?"` in addition to
  the existing module-path exemptions the `a2aclient`- and
  `mcp`-scoped rules each carry for their own module. Any other
  third-party import inside `ledger/*.go` still fires this rule, so
  the rest of `ledger`'s own files (`ledger.go`, `store.go`,
  `task_state.go`, and so on) stay held to the same stdlib-only bar
  they hold to today; only the one named import path is permitted
  anywhere in the directory.

`scripts/check_semgrep_probes.py` gains a new probe case, parallel to
the existing `a2aclient`- and `mcp`-scoped blocks: a `ledger/`
subdirectory under the probe's temp root, holding
`viol_ledger_other_import.go` (importing an unrelated third-party
path) and `clean_go_libsql_import.go` (importing
`github.com/tursodatabase/go-libsql`). Both basenames register in
`expected` against `sdk.go.ledger-scoped-third-party-import`, and an
explicit assertion block, parallel to the `mcp` one, proves: the new
rule fires on `viol_ledger_other_import.go` and stays silent on
`clean_go_libsql_import.go`; `sdk.go.stdlib-only-imports` fires on
neither, proving the scoped exclude took effect; and the existing
outside-the-directory probe files still prove
`sdk.go.stdlib-only-imports` fires normally outside all three scoped
directories.

### go.mod and go.sum: the closed dependency allowlist, extended

No Go-version change lands in this phase. `go.mod` already declares
`go 1.25.0`; `go-libsql`'s own `go.mod` declares `go 1.20`, so this
module's floor already meets it.

`go.mod` gains a `require` line for
`github.com/tursodatabase/go-libsql`, plus the indirect lines
`go mod tidy` adds beneath it. This plan verified the actual set
`go mod tidy` produces, against a probe module importing
`database/sql` and `github.com/tursodatabase/go-libsql` (a blank
import, the way a real driver registration works), the same shape
`ledger/libsql_store.go` needs:

```text
require github.com/tursodatabase/go-libsql v0.0.0-20260424063416-3051e37e6e04

require (
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/libsql/sqlite-antlr4-parser v0.0.0-20240327125255-dbf53b6cbf06 // indirect
	golang.org/x/exp v0.0.0-20230515195305-f3d0a9c9a5cc // indirect
	golang.org/x/sync v0.6.0 // indirect
)
```

The version pin above is the pseudo-version this plan resolved at
research time; the builder pins whatever `go mod tidy` resolves to at
build time and records the real version in the same change, the same
way `docs/plans/mcp.md` pinned `v1.7.0` for its own SDK dependency.

`go.sum` additionally carries `/go.mod`-only hash lines for
`github.com/pkg/errors` and `gotest.tools`, modules the resolved
build list references but no imported package reaches at build time,
the same shape `docs/plans/mcp.md`'s own two `/go.mod`-only entries
took for its dependency closure. `golang.org/x/sync` and
`github.com/google/go-cmp` are already in `ALLOWED_MODULES` from the
`a2aclient` and `mcp` exceptions.

`scripts/check_gomod.py`'s `ALLOWED_MODULES` set gains the new module
paths this phase's dependency closure adds, beyond what `a2aclient`
and `mcp` already permit:

```python
ALLOWED_MODULES |= {
    "github.com/tursodatabase/go-libsql",
    "github.com/antlr4-go/antlr/v4",
    "github.com/libsql/sqlite-antlr4-parser",
    "github.com/pkg/errors",
    "golang.org/x/exp",
    "gotest.tools",
}
```

The builder runs `go mod tidy` once `ledger/libsql_store.go` imports
`github.com/tursodatabase/go-libsql`, records the resulting `require`
and `go.sum` module set, and reconciles `ALLOWED_MODULES` against that
real output in the same change, trimming any entry above `go mod
tidy` does not actually add, the same reconciliation step
`docs/plans/mcp.md` records for its own allowlist. `check_gomod.py`'s
module docstring, which today names only the `a2aclient` and `mcp`
exceptions, gains one sentence naming the `ledger` exception too, in
the same change, stating the `ledger_libsql` build-tag qualifier the
other two exceptions do not carry.

### Summary

`make verify` passes, unchanged, once this phase lands, because the
tag-gated file never enters the default build. A second, explicit
verification command
(`go test -tags ledger_libsql -race ./ledger/...`, or the Makefile
target that wraps it) must also pass before this phase is considered
done, covering: the `LibsqlStore` code and its tag-gated tests; the
`go.mod` and `go.sum` additions; the `check_gomod.py` allowlist
extension and docstring update; the two `semgrep/sdk-standards.yml`
rule changes and the new `check_semgrep_probes.py` case (these three
run under the default, non-tag-gated `make verify` since Semgrep does
not evaluate build tags); the `AGENTS.md` exception-sentence and
`ledger/` layout-bullet edits; and the `docs/architecture.md` and
`docs/plans/ledger.md` doc updates, naming `LibsqlStore` next to
`MemStore` with the same documented Task-serialization and
build-tag-boundary limits this plan states. `docs/protocol-design.md`
does not change: this phase adds no message-semantics rule to the
envelope wire format.
