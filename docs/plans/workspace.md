# workspace plan

## Goal

`workspace` confines all filesystem access to one root directory, so
a tool or agent that reads and writes files cannot escape its
sandbox through traversal or a symlink. The confinement runs at the
syscall level through `os.Root`, not through a path check the caller
performs before a separate syscall. A read also carries a size bound,
so one hostile file cannot exhaust the process memory.

This plan covers two changes, built in order:

- Change one, confinement, file mode, and the read bound. Move
  enforcement to `os.Root`. Drop the caller-supplied file mode. Add
  `Options`, `OpenWith`, and a configurable bound on the bytes one
  read returns.
- Change two, secret-path denial. Wire `secretpath` into the
  `Workspace`. `docs/plans/secrets.md` owns the reasoning for change
  two; this plan owns the resulting `workspace` surface.

## Scope

Inside:

- `Open(root string) (*Workspace, error)`: resolves `root` to an
  absolute, symlink-free real path, opens it with `os.OpenRoot`, and
  returns a handle bound to the open root. `root` must exist and be
  a directory.
- `OpenWith(opts Options) (*Workspace, error)`: the same as `Open`,
  plus the read bound and (change two) the optional secret-path
  denial policy. `OpenWith` calls `opts.Validate()` first and returns
  that error unchanged. `Open` is `OpenWith(Options{Root: root})`.
- `Options`: `Root string`, `MaxReadBytes int64`, and (change two)
  `Deny *secretpath.Matcher`.
- `(Options) Validate() error`: `Root` must not be blank.
  `MaxReadBytes` must be `Unbounded`, zero, or a positive value at or
  under `maxReadLimit`; any other value returns `ErrInvalidLimit`.
  Zero selects `DefaultMaxReadBytes`. (change two) `Deny` may be nil,
  which means no path is denied.
- `(*Workspace) Root() string`: returns the resolved root path.
- `(*Workspace) Close() error`: closes the held `os.Root`. Close is
  idempotent, matching `os.Root.Close`.
- `(*Workspace) ReadFile(path string) ([]byte, error)`: reads a file
  named relative to the root, under the workspace's own bound. It is
  `ReadFileLimit(path, 0)` and adds no logic of its own. (change two)
  Returns `ErrSecretPath` when `deny` is non-nil and `deny.Matches`
  reports true on the cleaned root-relative path. The non-nil test is
  required, because `Matches` on a nil `*Matcher` panics.
- `(*Workspace) ReadFileLimit(path string, limit int64) ([]byte, error)`:
  the same read with a per-call bound. A zero `limit` uses the
  workspace's `MaxReadBytes`. A positive `limit` replaces it, up or
  down. `Unbounded` removes it for this call only. Any other value
  returns `ErrInvalidLimit`, and the method opens no file.
- Both read methods route through `os.Root.Open`, not
  `os.Root.ReadFile`, because `os.Root.ReadFile` reads the whole file
  and takes no limit. The body is: `resolve`, the deny check, then
  `os.Root.Open`, then `classify` on the open error, then
  `io.LimitReader(f, limit+1)`, then `io.ReadAll`. A result longer
  than `limit` returns `ErrTooLarge`. The `limit+1` read is what
  separates "exactly at the bound" from "over the bound"; a read of
  `limit` bytes alone cannot tell the two apart. An `Unbounded` call
  skips the `io.LimitReader` and calls `io.ReadAll` on the file.
- `os.Root.Open` returns an `*os.File` already confined by the same
  root, so the escape check is unchanged: the standard library's
  `os.Root.ReadFile` is itself `Open` plus a read. `List` already uses
  `os.Root.Open`, so the routing has a precedent in this package. The
  deny check runs before the open, so a denied path never opens a
  file descriptor.
- Both read methods close the file with `defer f.Close()` placed
  immediately after the open error check. The descriptor closes on the
  success path, on the `ErrTooLarge` path, and on any read error.
- `(*Workspace) WriteFile(path string, data []byte) error`: writes a
  file relative to the root, through `os.Root.WriteFile`, creating
  any missing parent directory under the root through
  `os.Root.MkdirAll`. (change two) Returns `ErrSecretPath` when `deny`
  is non-nil and `deny.Matches` reports true on the cleaned
  root-relative path. The non-nil test is required, because `Matches`
  on a nil `*Matcher` panics.
- `(*Workspace) List(path string) ([]os.DirEntry, error)`: lists a
  directory relative to the root, through `os.Root.Open` plus
  `(*os.File).ReadDir(-1)`. The opened file closes before List
  returns. (change two) Returns `ErrSecretPath` when `deny` is non-nil
  and `deny.Matches` reports true on the cleaned root-relative path.
  The non-nil test is required, because `Matches` on a nil `*Matcher`
  panics.
- `(*Workspace) Stat(path string) (os.FileInfo, error)`: stats a path
  relative to the root, through `os.Root.Stat`. (change two) Returns
  `ErrSecretPath` when `deny` is non-nil and `deny.Matches` reports
  true on the cleaned root-relative path. The non-nil test is
  required, because `Matches` on a nil `*Matcher` panics.
- The denial check runs after `resolve` and before the `os.Root`
  call, on `resolve`'s output. `docs/plans/secrets.md` owns the
  ordering rationale and the test vectors.
- `resolve(path string) (string, error)` in `confine.go`: turns the
  caller's path into a cleaned, root-relative path for the `os.Root`
  call. It is pure string work and touches no file. It keeps the
  absolute-path branch that `workspace/confine.go` has today:
  `if filepath.IsAbs(path) { joined = filepath.Clean(path) }`, and
  otherwise joins `path` onto the root and cleans the result. It then
  runs `withinRoot` on `joined`, rejects a result that is not the root
  or under it, and returns the path relative to the root. An empty
  path resolves to `.`.
- The absolute-path branch is load-bearing, not cosmetic. Without it,
  `resolve` joins the absolute path onto the root, so `os.Root`
  receives the relative name `etc/secret-outside-root`. Bare `os.Root`
  would have rejected the absolute form.
  A measured prototype returned `fs.ErrNotExist` from `ReadFile`, and
  nil from both `WriteFile` and `Stat`. That `WriteFile` created
  `<root>/etc/secret-outside-root` instead of refusing. Deleting the
  branch is a worse defect than the one this plan fixes.
- `classify(path string, err error) error` in `confine.go`: maps an
  `os.Root` confinement refusal onto `ErrEscape`. It unwraps to
  `*fs.PathError` and compares the inner error text to the package
  constant `rootEscapeText`. A match returns an error wrapping both
  `ErrEscape` and the original `*fs.PathError`, through the two-verb
  `%w` form. Any other error passes through unchanged.
- Sentinels: `ErrEscape` for a path that resolves outside the root;
  `ErrTooLarge` for a file over the effective read bound;
  `ErrInvalidLimit` for a rejected bound value; `ErrSecretPath`
  (change two) for a path the `Deny` matcher matches. `ErrTooLarge`
  wraps nothing. It is a policy refusal by this package, not a
  filesystem error, so no `*fs.PathError` is available to wrap.
- Constants: `DefaultMaxReadBytes int64 = 10 << 20`, the bound a
  workspace uses when the caller sets nothing; `Unbounded int64 = -1`,
  the explicit opt-out. The unexported `maxReadLimit` is
  `math.MaxInt64 - 1`, so `limit+1` cannot overflow.
- Ordering inside a read method is fixed: `resolve` first, the deny
  check second, the bound third. A path that is both denied and over
  the bound returns `ErrSecretPath`. The deny check needs no file, and
  the bound cannot be measured without opening one, so the cheaper and
  stricter refusal wins. One path always produces one answer.
- The limit-value check runs before `resolve`, because an invalid
  limit is a caller bug and touches no path. `ErrInvalidLimit`
  therefore beats `ErrEscape` and `ErrSecretPath`.
- File layout: `workspace.go` holds `Workspace`, `Options`,
  `Validate`, `Open`, `OpenWith`, `Root`, `Close`, and the unexported
  `validateLimit` helper; `confine.go` holds `resolve`, `withinRoot`,
  and `classify`; `read.go`, `write.go`, `list.go`, and `stat.go` each
  hold one concern. `validateLimit(v int64) error` enforces the one
  limit rule: `Unbounded`, zero, or a positive value at or below
  `maxReadLimit` passes; any other value returns `ErrInvalidLimit`.
  `Options.Validate` calls `validateLimit` on `MaxReadBytes`.
  `read.go` holds `ReadFile`, `ReadFileLimit`, and the unexported
  `effectiveLimit` helper, which calls `validateLimit` on the per-call
  limit and then maps a passing value onto the value the read uses.
- The bound fails closed, which diverges from `contextbudget.Limits`
  on purpose. In `contextbudget` a zero `MaxBytes` means no cap,
  because that package accounts for a cooperative budget the caller
  owns. This bound is a defense against hostile input, on a path
  phase 71 makes model-reachable. A zero value there is an unset
  field, so a zero-means-unbounded rule would ship an uncapped
  workspace to every caller who never read this plan. `Options{}`
  therefore yields `DefaultMaxReadBytes`.
- `Unbounded` is a named constant, not a bool and not a bare negative
  number. A reader sees `MaxReadBytes: workspace.Unbounded` at the
  call site and needs no table to read it. A separate
  `UnboundedReads bool` would make two fields express one value and
  would need a conflict rule. A bare `-1` would read as a mistake.

Outside:

- A size bound on `WriteFile`. The caller already holds the bytes in
  memory, so a cap refuses a cost the process has already paid. Disk
  growth is the operating system's quota, not this package's.
- An entry-count bound on `List`. Filling a directory with millions
  of entries needs write access to the confined root, which an
  attacker who already has it does not need this bound to abuse. No
  named threat exists, so no bound ships. Add one when a caller names
  the threat.
- A streaming reader, such as `OpenReader`. A caller that must handle
  a file larger than memory needs one, and no caller does today.
  `ReadFileLimit` covers the bounded-read case.
- Process execution, git operations, or archive handling.
- Any caching, locking, or concurrent-write coordination beyond what
  the OS filesystem already gives. `os.Root` is safe for concurrent
  use by several goroutines, so a `Workspace` value is too.
- `os.Root.Chmod`, `Chown`, and `Chtimes`. The stdlib documents the
  three as race-vulnerable on Unix. `WriteFile` sets its mode at
  create time through `os.Root.WriteFile`, so no post-create mode
  change is needed, and the package exposes none.
- Deletion, rename, and symlink creation. `os.Root` offers `Remove`,
  `Rename`, and `Symlink`. This package adds none of them without a
  caller.
- Exposing `os.Root.FS()`. A raw `fs.FS` would bypass the change-two
  `Deny` policy, so the package keeps one policy point and exposes
  no `fs.FS`. See `docs/plans/secrets.md`.
- A `Workspace` is not safe against a hostile root directory. Quoting
  the `os.Root` documentation: it "does not prohibit traversal of
  filesystem boundaries, Linux bind mounts, /proc special files, or
  Unix device files". A root that already contains a bind mount, a
  `/proc` entry, or a device node stays reachable through this
  package. `os.Root` closes the check-then-use gap. It does not make
  the package safe against a root an attacker prepared.
- The `Deny` matcher is a name policy, not a content policy. See the
  residual-risk list in `docs/plans/secrets.md`.
- A labelled `ErrEscape` under a concurrent attacker. A deterministic
  escape reports `ErrEscape`. A refusal that arrives while an attacker
  swaps a path component may report the raw syscall error instead. A
  measured prototype produced `too many levels of symbolic links`
  (`ELOOP`) on roughly 20 percent of racing writes. `os.Root` exhausts
  its symlink-retry budget and reports that, not an escape. The
  refusal still fails closed. Only the error's class is unlabelled.
- Parity with bare `os.Root` on an in-root symlink. The lexical
  pre-check rejects an in-root path that traverses `..` through an
  in-root symlink, which `os.Root` alone would permit. This is
  pre-existing behavior. After this change the pre-check is its only
  source, so this plan records it as deliberate.

## API

Every entry lands in `api/workspace.txt` through `make api-update`.

Surface after change one:

- `const DefaultMaxReadBytes int64 = 10 << 20`
- `const Unbounded int64 = -1`
- `var ErrEscape = errors.New("workspace: path escapes root")`
- `var ErrTooLarge = errors.New("workspace: file exceeds read limit")`
- `var ErrInvalidLimit = errors.New("workspace: invalid read limit")`
- `type Workspace struct { ... }` (fields unexported)
- `type Options struct { Root string; MaxReadBytes int64 }`
- `func (o Options) Validate() error`
- `func Open(root string) (*Workspace, error)`
- `func OpenWith(opts Options) (*Workspace, error)`
- `func (w *Workspace) Root() string`
- `func (w *Workspace) Close() error`
- `func (w *Workspace) ReadFile(path string) ([]byte, error)`
- `func (w *Workspace) ReadFileLimit(path string, limit int64) ([]byte, error)`
- `func (w *Workspace) WriteFile(path string, data []byte) error`
- `func (w *Workspace) List(path string) ([]os.DirEntry, error)`
- `func (w *Workspace) Stat(path string) (os.FileInfo, error)`

Surface added by change two:

- `var ErrSecretPath = errors.New("workspace: path is a secret path")`
- `Options` gains the field `Deny *secretpath.Matcher`

Change-one delta against the locked `api/workspace.txt`:

- Removed: `func (w *Workspace) WriteFile(path string, data []byte, perm os.FileMode) (error)`
- Added: `func (w *Workspace) WriteFile(path string, data []byte) (error)`
- Added: `func (w *Workspace) Close() (error)`
- Added: `func (w *Workspace) ReadFileLimit(path string, limit int64) ([]byte, error)`
- Added: `func OpenWith(opts Options) (*Workspace, error)`
- Added: `func (o Options) Validate() (error)`
- Added: `type Options struct` with the fields `Root` and
  `MaxReadBytes`. `api_surface` prints one line per exported field, so
  the rendered line count is larger than the symbol count.
- Added: `const DefaultMaxReadBytes`, `const Unbounded`,
  `var ErrTooLarge`, `var ErrInvalidLimit`
- No method signature changes apart from `WriteFile`. `ReadFile`
  keeps `(path string) ([]byte, error)`, so no existing call site
  changes.

`Options` and `OpenWith` move into change one, because the read bound
needs a place to live at open time. Change two then adds one field and
one sentinel to a struct that already exists. `docs/plans/secrets.md`
states the same split.

Reasoning behind the shape:

- `Workspace` holds an `*os.Root`, so it holds an open file
  descriptor. A file descriptor has a lifecycle, so `Close` is
  required, not optional. A long-lived process that opens one
  workspace per task leaks one descriptor per task without it. The
  descriptor is the price of syscall-level confinement, and `Close`
  is the honest way to state that price in the API.
- `WriteFile` drops `perm`. This package is reachable by a
  model-driven tool loop, so `perm` would become a model-supplied
  value. The setuid bit, the setgid bit, the sticky bit, and a
  world-writable mode are all expressible in an `os.FileMode` today,
  and nothing rejects them. The alternative, an allowlist mask,
  keeps a parameter that no caller in this module sets to anything
  but `0o600`, and it leaves a knob a model can still turn. Dropping
  the parameter removes the class of failure instead of narrowing
  it.
- The hardcoded modes are `0o600` for a file and `0o700` for a
  directory. The two are consistent: owner-only, no group bit, no
  other bit, no setuid or setgid bit.
- The mode applies at create only. Measurement: `os.Root.WriteFile`
  left a pre-existing `0o644` file at `0o644` after a `0o600` write,
  and `os.Root.MkdirAll` left an existing directory's mode alone. The
  doc comment on `WriteFile` must state the enforceable rule: it
  creates a new file with `0o600`, and it does not tighten an existing
  file. `TestWriteFileMode` pins both halves, so no comment claims a
  rule without an enforcer.
- `Options` plus `OpenWith` follows the `Options.Validate` pattern
  the repo already uses in `dispatch` and `a2aack`. `Open` stays as
  the one-argument form.
- `Open("")` changes behavior on change two. Measured on the current
  tree, `Open("")` succeeds and binds the working directory, because
  `filepath.Abs("")` returns it. `Open` now routes through
  `Options.Validate`, so `Open("")` fails instead. An implicit
  working-directory sandbox is a footgun, so the change is
  deliberate.
- The per-call override is a second method, not a variadic option on
  `ReadFile` and not a field the caller mutates. A second method keeps
  `ReadFile`'s signature and every existing call site. This module
  uses no functional-option pattern, so adding one here would be
  drift. Mutating a shared `Workspace` field per call is not safe for
  concurrent use, and `os.Root` is.
- `DefaultMaxReadBytes` is 10 MiB. A source file, a config file, or a
  dotenv file is far under it, and a model context cannot hold 10 MiB
  of text anyway. The value is a named constant, so a caller that
  disagrees overrides it in one place.
- Two enforcement shapes are rejected by name. `Stat` first, then
  `ReadFile`, is check-then-use: the file grows between the two calls.
  It is the same defect class `os.Root` is here to remove. Read
  everything, then measure the length, spends the memory the bound
  exists to protect. Only the `io.LimitReader` shape refuses before
  the allocation happens.

The lexical pre-check survives, in reduced form. Two facts decide it:

- `os.Root` does not report every caller-expressible escape as an
  escape, and its answer depends on disk state. Measured on go1.26.0,
  bare `os.Root` classifies `a/../../secret` three ways. A missing
  first component gives `fs.ErrNotExist`. A first component that is an
  existing directory gives `path escapes from parent`. A first
  component that is an existing file gives `not a directory`. The
  current tests assert `ErrEscape` for that vector.
- The pre-check makes the answer state-independent. One
  caller-expressible path gets one verdict, whatever the disk holds.
  That is the reason to keep it, and the measurement strengthens the
  case rather than weakening it.
- The pre-check is cheap and does no filesystem work, so it adds no
  new check-then-use gap. It classifies; it does not enforce.

`resolveDeepestExisting` is deleted. It is the part that did
filesystem work between the check and the syscall, so it is the part
that created the gap. `os.Root` now owns every symlink decision.
`withinRoot` stays and works on the lexically cleaned path only.

`ErrEscape` keeps a narrowed meaning for callers. A deterministic
escape reports `ErrEscape`, in both the lexical case and the syscall
case, because `classify` wraps the `os.Root` refusal with `ErrEscape`.
A refusal that arrives under a concurrent path swap may report the raw
syscall error instead, unlabelled. See the Scope-Outside entry on
concurrent attackers.

The text comparison in `classify` is brittle by nature, because the
standard library exports no sentinel for the refusal. The failure mode
of that brittleness is safe: an unmatched text still fails closed, and
only the error's class changes. `TestSymlinkEscapeFinalComponent` and
`TestSymlinkEscapeIntermediateComponent` pin the mapping. Both are
deterministic, so a change in the standard library's wording turns a
test red instead of turning a guarantee off. The two race tests below
cannot pin it, because they assert no error class at all.

## Tests

Tests live in `workspace/workspace_test/`, table-driven where the case
set grows.

Existing tests that must change, and why:

- `workspace_test.go`, every `w.WriteFile(...)` call: seven call
  sites drop the `perm` argument. `WriteFile` takes two arguments
  after change one.
- `workspace_test.go`, every `workspace.Open` call: add
  `t.Cleanup(func() { w.Close() })`. A `Workspace` now holds a
  descriptor.
- `TestTraversalEscape`: existing assertions stay, and the table gains
  two vectors. Vector one keeps `a/../../secret` with no `a` on disk.
  Vector two uses the same string with `a` created as a real directory
  in the root first. Both must report `ErrEscape`. Without vector two
  nothing proves the pre-check normalizes the disk states that bare
  `os.Root` distinguishes. Keep the absolute-path vector, which pins
  the `filepath.IsAbs` branch in `resolve`.
- `TestSymlinkEscapeFinalComponent` and
  `TestSymlinkEscapeIntermediateComponent`: existing assertions stay,
  and each gains one assertion. The returned error must match
  `errors.Is(err, ErrEscape)` and must also satisfy
  `errors.As(err, &pathErr)` for a `*fs.PathError`. That pair is what
  `classify` promises through its two-verb `%w` wrap. These two tests
  are deterministic, so they are the pin for `rootEscapeText`.
- `TestHappyPathRoundTrip` and `TestListRootItself`: unchanged
  assertions, minus the `perm` argument.

New unit cases in `workspace_test.go`:

- `TestWriteFileMode`: `WriteFile("a/b/c.txt", data)` creates the file
  with mode `0o600` and the parent directories with mode `0o700`.
  Assert `info.Mode().Perm()` for the file and for both directories.
- `TestWriteFileKeepsExistingMode`: create `wide.txt` at `0o644`
  outside the package API, then call `WriteFile("wide.txt", data)`.
  Assert the file still reports `0o644` and holds the new data. This
  pins the measured `os.Root.WriteFile` behavior: the mode applies at
  create only, so `WriteFile` never tightens an existing file.
- `TestClose`: after `Close`, `ReadFile`, `WriteFile`, `List`, and
  `Stat` each return an error matching `fs.ErrClosed`. A second
  `Close` returns nil.
- `TestEmptyPath`: `Stat("")` behaves as `Stat(".")` and reports a
  directory, proving `resolve` maps an empty path to `.` before the
  `os.Root` call, which rejects an empty name.
- `TestOpenRejectsBlankRoot`: `Open("")` returns an error, and
  `OpenWith(Options{})` returns the same error. This pins that
  `OpenWith` calls `Validate`, and that `Open("")` no longer binds
  the working directory.

New file `workspace/workspace_test/read_limit_test.go`. Each case
writes its fixture file outside the package API, so the file size is
exact.

- `TestDefaultReadLimit`: open with `Open(root)`, which sets no
  limit. A file of `DefaultMaxReadBytes+1` bytes returns an error
  matching `ErrTooLarge`. This proves the zero value is bounded, not
  unbounded.
- `TestUnboundedOptIn`: open with
  `OpenWith(Options{Root: root, MaxReadBytes: workspace.Unbounded})`.
  The same oversized file reads back in full, byte length asserted.
- `TestConfiguredLimit`: open with `MaxReadBytes: 8`. A 8-byte file
  reads back. A 9-byte file returns `ErrTooLarge`.
- `TestExactLimit`: a file of exactly the effective limit succeeds and
  returns every byte. This is the case the `limit+1` read exists for.
- `TestOneByteOver`: a file of the effective limit plus one byte
  returns an error matching `ErrTooLarge` under `errors.Is`, and
  returns a nil byte slice.
- `TestPerCallOverrideRaises`: a workspace with `MaxReadBytes: 8`
  reads a 64-byte file through `ReadFileLimit(path, 128)`.
- `TestPerCallOverrideLowers`: the same workspace opened with
  `MaxReadBytes: 4096` returns `ErrTooLarge` from
  `ReadFileLimit(path, 8)` on the same 64-byte file.
- `TestPerCallUnbounded`: `ReadFileLimit(path, workspace.Unbounded)`
  reads the oversized file from a default-bounded workspace.
- `TestPerCallZeroUsesWorkspaceLimit`: `ReadFileLimit(path, 0)` and
  `ReadFile(path)` return the same result on both a passing and a
  failing file.
- `TestInvalidLimit`, table-driven: `ReadFileLimit(path, -2)` and
  `ReadFileLimit(path, math.MaxInt64)` each return `ErrInvalidLimit`.
  `Options{Root: root, MaxReadBytes: -2}.Validate()` returns the same
  sentinel, and `OpenWith` returns it unchanged. A fourth case adds
  `ReadFileLimit("../outside.txt", -2)`: it returns `ErrInvalidLimit`,
  not `ErrEscape`, pinning that an invalid limit beats an escaping
  path.
- `TestOverLimitClosesFile`: count the process's open descriptors
  before and after 200 `ReadFile` calls that each return
  `ErrTooLarge`. The two counts must be equal. Count by reading
  `/proc/self/fd` on Linux, and skip the test where that path is
  absent. This is the pin for the deferred `Close` on the error path.
- `TestSecretPathBeatsLimit` (change two): a workspace with a `Deny`
  matcher for `.env` and `MaxReadBytes: 1` reads `.env`. The error
  matches `ErrSecretPath` and does not match `ErrTooLarge`. This pins
  the stated order between the deny check and the bound.
- `TestEscapeBeatsLimit`: `ReadFile("../outside.txt")` on a
  1-byte-limited workspace returns `ErrEscape`, not `ErrTooLarge`.
- Change-two `Options.Validate` and secret-denial cases live in
  `workspace/workspace_test/secret_test.go`. See
  `docs/plans/secrets.md`; do not duplicate them here.

New file `workspace/workspace_test/confine_integration_test.go`. It
holds the two check-then-use cases. Each one races a real attacker
goroutine against a real workspace call. Each skips on Windows,
where symlink creation needs a privilege.

- `TestWriteFollowsNoSwappedSymlink`. This is the case the old code
  loses. Setup: workspace root `R` with directory `R/staging`, and
  an unrelated directory `O` outside `R` holding `O/target` with
  content `outside-original`. Goroutine one loops 500 times calling
  `w.WriteFile("staging/out.txt", []byte("payload"))`. Goroutine two
  loops for the same period, each turn removing `R/staging/out.txt`
  and then creating it again as a symlink to `O/target`. The race
  window in the old code is exact: `resolveDeepestExisting` rejoins
  the not-yet-existing final component unresolved, so `resolve`
  returns a path that passes the check, and the attacker then makes
  that path a symlink before `os.WriteFile` runs. The test asserts one
  invariant only: `O/target` still holds `outside-original` after both
  goroutines finish. It asserts nothing about the error set.
- The error set is deliberately unasserted. A measured prototype run
  of this loop returned ok=66993, `ErrEscape`=92387, and other=40620.
  Every sampled `other` was
  `openat staging/out.txt: too many levels of symbolic links`.
  `os.Root` exhausts its symlink-retry budget under a concurrent swap
  and reports `ELOOP` instead of an escape. An error assertion here
  flakes red on roughly 20 percent of iterations. Widening the
  accepted set to nil, `ErrEscape`, `syscall.ELOOP`, and
  `fs.ErrNotExist` would pass, but it pins a retry-budget detail of
  the standard library, not a property this package owns. The content
  invariant is the security property, so it is the only assertion.
- `TestReadFollowsNoSwappedDirectory`. Setup: `R/data/secret.txt`
  with content `inside`, and `O/secret.txt` with content `outside`.
  Goroutine one loops 500 times calling
  `w.ReadFile("data/secret.txt")`. Goroutine two loops, each turn
  renaming `R/data` aside and putting a symlink to `O` in its place,
  then reversing the swap. The test asserts one invariant only: no
  successful read ever returns `outside`. It asserts nothing about the
  error set, for the reason given above.
- This test is red against the current code, not documentary. The
  reviewer measured the current `ReadFile` returning the outside
  content 12198 times in 2 seconds under this attacker. The builder
  must expect a failing test before the `os.Root` change lands, and a
  passing one after.
- Both cases bound themselves by iteration count and by a 200
  millisecond deadline, whichever comes first, so the suite stays
  fast. Build the deadline with `context.WithTimeout` and check
  `ctx.Err()` each loop turn. Do not call `time.Sleep`: the Semgrep
  rule `sdk.go.tests-no-time-sleep` bans it in any `_test.go` file.
  The race detector is not the mechanism here, because it does
  not see filesystem races. The repeated one-sided invariant is the
  mechanism.
- Each case has setup, two goroutines, and assertions. Put the
  directory and file setup in a helper in the same file, so no test
  function passes the 80-line structure limit.
- A note in the file states the structural claim these two cases
  support: after this change `resolve` performs no filesystem
  access, so there is no window between the check and the syscall to
  race.

New file `workspace/workspace_test/workspace_bench_test.go`:

- `BenchmarkReadFile` over a small file in the root. Record the
  allocation count when the change lands. No allocation budget is
  asserted, because the syscall dominates. `io.ReadAll` grows its
  buffer, so the count may rise against the `os.Root.ReadFile` form.
  Record the new number; do not assert one.

## Verification

- `make verify` passes: gofmt, vet, tests under the race detector,
  the doc gate, the structure gate, the plan gate, the deps gate,
  the API gate, the Semgrep scan, and the probe suite.
- Coverage for `workspace` reaches the 85 percent floor, and the
  total floor holds.
- `make api-update` runs, and the `api/workspace.txt` diff lands in
  the same change as the code.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass. Change two needs the `policy/layers.json` row
  `"workspace": ["secretpath"]`, which this plan lands ahead of the
  code.
- `go.mod` needs no change. It declares `go 1.25.0`, and every
  `os.Root` method used here exists in Go 1.25: `WriteFile`,
  `MkdirAll`, `Stat`, `Open`, and `Close`. The behavior
  measurements in this plan come from the local go1.26.0 toolchain.
- `rootEscapeText` is a named constant for one reason: it gives the
  brittleness one named point. No Semgrep rule requires it. The rules
  `sdk.go.no-enum-string-literals` and
  `sdk.go.hash-prefix-centralized` cover enum values and hash
  prefixes, and neither applies here.
- `docs/packages/workspace.md` changes in the same commit as the code.
  Four places are wrong after change one. Line 23 carries the `perm`
  parameter in the `WriteFile` signature. Lines 36 to 41 state the
  deleted `resolveDeepestExisting` behavior as an invariant. Line 81
  passes `0o600` in the usage example. Any claim that a read is
  unbounded is false. Replace the symlink invariant with the `os.Root`
  one, and add `Close`, `Options`, `OpenWith`, `ReadFileLimit`,
  `DefaultMaxReadBytes`, `Unbounded`, `ErrTooLarge`, and
  `ErrInvalidLimit` to the surface list.
- `AGENTS.md` describes each package's surface, so its `workspace`
  entry names the read bound in the same commit.
- The read bound adds no import edge. `io` and `math` are standard
  library, so `policy/layers.json` keeps the row
  `"workspace": ["secretpath"]` unchanged.
- `Options.Validate` and `ReadFileLimit` both call the unexported
  `validateLimit` helper, so no comment states the limit rule alone.
  `effectiveLimit` calls `validateLimit` before it maps a passing
  value onto the value the read uses.

## Test gap: sibling-prefix escape

The separator term in `withinRoot` (`workspace/confine.go:40`) is what
stops a sibling-prefix escape. A root of `/tmp/x/root` must not admit
`/tmp/x/root-evil/secret.txt`. Removing that term keeps the whole
`workspace/workspace_test` suite green, so the mutation survives
today.

This is a test-only change. No production code changes.

New case in `workspace/workspace_test/workspace_test.go`,
`TestSiblingPrefixEscape`:

- Create `<tmp>/root` and `<tmp>/root-evil/secret.txt` under one
  `t.TempDir()`.
- Open a `Workspace` at `<tmp>/root`.
- Cover all four methods that call `resolve`: `ReadFile`, `WriteFile`,
  `List`, and `Stat`. Each takes a relative path resolving into
  `root-evil`, for example `../root-evil/secret.txt`. Each must return
  an error matching `errors.Is(err, ErrEscape)`.
- `WriteFile` takes `(string, []byte, os.FileMode)` today. Pass the
  mode, or the test does not compile. Drop the mode argument if the
  signature change planned above lands first.
- Point `List` at `../root-evil` so it names a real directory. A
  passing escape check must beat the successful listing.
- Assert the returned bytes are empty on the `ReadFile` call. A
  passing error check then cannot hide a leaked read.
- Assert `root-evil` holds no new file after the `WriteFile` call.
  `WriteFile` is the highest-severity case: the mutant plants a file
  in the sibling directory.

The case kills the mutation that drops the separator term from
`withinRoot`. Without that term the resolved sibling path passes the
prefix compare, and all four calls succeed.

The case stays valid after the planned `os.Root` change above.
`os.Root` refuses the same path, and `classify` maps the refusal onto
`ErrEscape`, so the assertions do not change.

Verification for this case:

- `go test -race ./workspace/...` passes.
- `make verify` passes. `workspace` holds the 85 coverage floor.
- `python3 scripts/check_plan.py`, `python3 scripts/check_prose.py`,
  and `python3 scripts/check_structure.py` pass. Put the directory
  setup in a helper, so the test function stays under 80 lines.
- `docs/plans/agentloop.md` and the `policy/layers.json` row adding
  `schema` to `agentloop` stay out of this commit. They belong to the
  concurrent `agentloop` change and need their own plan review.
