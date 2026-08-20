# workspace plan

## Goal

`workspace` confines all filesystem access to one root directory, so
a tool or agent that reads and writes files cannot escape its
sandbox through traversal or a symlink. The confinement runs at the
syscall level through `os.Root`, not through a path check the caller
performs before a separate syscall. A read also carries a size bound,
so one hostile file cannot exhaust the process memory.

This plan covers two changes, built in order:

- Change one, confinement, file mode, and the read bound. **Shipped.**
  Commit A is `f3adff3`, commit B is `3bf7496`, and two follow-ups
  are `78e1934` and `f8ec0fb`. Its sections below are kept as the
  record of what shipped. A builder starting change two changes
  nothing in change one.
- Change two, secret-path denial. Shipped. `secretpath` is wired into
  the `Workspace` through `Options.Deny`, and a denied path returns
  `ErrSecretPath`. `docs/plans/secrets.md` owns the reasoning for
  change two; this plan owns the resulting `workspace` surface. The
  `envfile.LoadBytes` half of `docs/plans/secrets.md` also shipped, so
  no part of that plan is deferred.

## Scope

### Build order

Change one lands as two commits. A builder executes commit A, runs
`make verify`, and may stop there. Commit B follows.

- Commit A, the security fix alone. Delete `resolveDeepestExisting`.
  Hold an `*os.Root` in `Workspace`. Add `Close` and `classify`. Add
  the two race tests. Add `t.Cleanup` closes to the existing
  `workspace` and `subagent` tests. Update `api/workspace.txt`,
  `docs/packages/workspace.md`, and the `AGENTS.md` entry.
  `WriteFile` keeps its `perm` parameter in commit A.
- Commit B, the surface work. Drop `perm` from `WriteFile`. Add
  `Options`, `OpenWith`, `Validate`, `ReadFileLimit`,
  `DefaultMaxReadBytes`, `Unbounded`, `ErrTooLarge`, and
  `ErrInvalidLimit`. Update `subagent` for the dropped parameter.

The split exists so the race test's discriminating power stays
checkable. `TestReadFollowsNoSwappedDirectory` is red before commit A
and green after it. Bundling an API break and a new subsystem into the
same commit would make a revert of the surface work also revert the
security fix.

### Inside

- `Open(root string) (*Workspace, error)`: resolves `root` to an
  absolute, symlink-free real path, opens it with `os.OpenRoot`, and
  returns a handle bound to the open root. `root` must exist and be
  a directory. (commit A)
- `OpenWith(opts Options) (*Workspace, error)`: the same as `Open`,
  plus the read bound and (change two) the optional secret-path
  denial policy. `OpenWith` calls `opts.Validate()` first and returns
  that error unchanged. `Open` is `OpenWith(Options{Root: root})`.
  (commit B)
- `Options`: `Root string`, `MaxReadBytes int64`, and (change two)
  `Deny *secretpath.Matcher`. (commit B)
- `(Options) Validate() error`: `Root` must not be blank.
  `MaxReadBytes` must be `Unbounded`, zero, or a positive value at or
  under `maxReadLimit`; any other value returns `ErrInvalidLimit`.
  Zero selects `DefaultMaxReadBytes`. (change two) `Deny` may be nil,
  which means no path is denied. (commit B)
- `(*Workspace) Root() string`: returns the resolved root path.
  (unchanged)
- `(*Workspace) Close() error`: closes the held `os.Root`. Close is
  idempotent, matching `os.Root.Close`. Close on a nil or zero-value
  `Workspace` returns nil, so a deferred Close placed before the error
  check cannot panic. Every other method already answers a zero value
  with `ErrEscape`. (commit A)
- `(*Workspace) ReadFile(path string) ([]byte, error)`: reads a file
  named relative to the root, under the workspace's own bound. After
  commit B it is `ReadFileLimit(path, 0)` and adds no logic of its
  own. Change two adds no check here. `ReadFile` inherits the deny
  check from `ReadFileLimit`.
- `(*Workspace) ReadFileLimit(path string, limit int64) ([]byte, error)`:
  the same read with a per-call bound. A zero `limit` uses the
  workspace's `MaxReadBytes`. A positive `limit` replaces it, up or
  down. `Unbounded` removes it for this call only. Any other value
  returns `ErrInvalidLimit`, and the method opens no file. (commit B)
  (change two) Calls `w.denied(resolved, path)` after `resolve` and
  before `os.Root.Open`, and returns its error.
- The deny check lives at four call sites and no more:
  `ReadFileLimit`, `WriteFile`, `List`, and `Stat`. `ReadFile` is
  `ReadFileLimit(path, 0)`, so a fifth check there would duplicate the
  rule and the two copies would drift. Each of the four calls one
  line: `if err := w.denied(resolved, path); err != nil { return err }`.
  The rule itself lives once, in `denied`. See "The deny check" below.
- Both read methods route through `os.Root.Open`, not
  `os.Root.ReadFile`, because `os.Root.ReadFile` reads the whole file
  and takes no limit. The body is: `resolve`, the deny check, then
  `os.Root.Open`, then `classify` on the open error, then
  `io.LimitReader(f, limit+1)`, then `io.ReadAll`. Commit A reads
  through `os.Root.Open` plus `io.ReadAll`, with no `io.LimitReader`;
  commit B inserts the bound. A result longer
  than `limit` returns `ErrTooLarge`. The `limit+1` read is what
  separates "exactly at the bound" from "over the bound"; a read of
  `limit` bytes alone cannot tell the two apart. An `Unbounded` call
  skips the `io.LimitReader` and calls `io.ReadAll` on the file.
- `os.Root.Open` returns an `*os.File` already confined by the same
  root, so the escape check is unchanged: the standard library's
  `os.Root.ReadFile` is itself `Open` plus a read. After commit A,
  `List` uses the same `os.Root.Open` routing, so all four methods
  share one shape. `List` uses plain `os.ReadDir` on an absolute path
  today; there is no `os.Root` anywhere in the package before commit
  A. The deny check runs before the open, so a denied path never
  opens a file descriptor.
- Both read methods close the file with `defer f.Close()` placed
  immediately after the open error check. The descriptor closes on the
  success path, on the `ErrTooLarge` path, and on any read error.
- A read of a directory returns the raw filesystem error, unwrapped.
  `resolve` maps an empty path to `.`, and `os.Root.Open(".")`
  succeeds and returns the root directory handle. `io.ReadAll` on that
  handle then reports `is a directory`. `ReadFile("")` and
  `ReadFile(".")` therefore both fail with that raw error, not with
  a package sentinel. No caller reads a directory, so no sentinel
  ships for it.
- `(*Workspace) WriteFile(path string, data []byte) error`: writes a
  file relative to the root, through `os.Root.WriteFile`, creating
  any missing parent directory under the root through
  `os.Root.MkdirAll`. The signature keeps `perm os.FileMode` in commit
  A and drops it in commit B. (change two) Calls
  `w.denied(resolved, path)` after `resolve` and before
  `os.Root.MkdirAll`, so a denied write creates no parent directory.
- `WriteFile` runs `classify` on both syscall errors: the
  `os.Root.MkdirAll` error and the `os.Root.WriteFile` error. Missing
  the first one is a real gap. An escaping intermediate symlink is
  refused by `MkdirAll` before `WriteFile` runs, and the measured
  refusal text is `mkdirat outdir/sub: path escapes from parent`.
  Unclassified, that returns a raw `*fs.PathError`, and
  `errors.Is(err, ErrEscape)` reports false on a real escape.
  (commit A)
- `(*Workspace) List(path string) ([]os.DirEntry, error)`: lists a
  directory relative to the root, through `os.Root.Open` plus
  `(*os.File).ReadDir(-1)`, with `classify` on the open error. It
  sorts the entries by filename, because `(*os.File).ReadDir` returns
  raw directory order where `os.ReadDir` sorts. The sort keeps the
  contract `subagent.WorkspaceListTool` already states. The opened
  file closes before List returns. (change two) Calls
  `w.denied(resolved, path)` after `resolve` and before
  `os.Root.Open`.
- `(*Workspace) Stat(path string) (os.FileInfo, error)`: stats a path
  relative to the root, through `os.Root.Stat`, with `classify` on the
  error. (change two) Calls `w.denied(resolved, path)` after `resolve`
  and before `os.Root.Stat`.
- `resolve(path string) (string, error)` in `confine.go`: turns the
  caller's path into a cleaned, root-relative path for the `os.Root`
  call. It is pure string work and touches no file. It keeps the
  absolute-path branch that `workspace/confine.go` has today:
  `if filepath.IsAbs(path) { joined = filepath.Clean(path) }`, and
  otherwise joins `path` onto the root and cleans the result. It then
  runs `withinRoot` on `joined`, rejects a result that is not the root
  or under it, and returns the path **relative to the root**. An empty
  path resolves to `.`. (commit A)
- The relative conversion is required, not cosmetic. `resolve` returns
  an absolute path today, and `os.Root` refuses an absolute name with
  `path escapes from parent`. Without the conversion, a legitimate
  in-root absolute path returns `ErrEscape`.
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
  `%w` form. Any other error passes through unchanged. Both tests are
  required: the type test alone would relabel every `*fs.PathError`,
  including `no such file or directory` and `permission denied`.
  (commit A)
- The text mapping is uniform across the three syscall wrappers this
  package uses. Measurement: `openat`, `statat`, and `mkdirat` each
  report the inner text `path escapes from parent`. Unrelated failures
  are textually distinct: `no such file or directory`, `not a
  directory`, `permission denied`, `empty path`, and `file already
  closed`. So the text compare cannot swallow an unrelated error.
- Sentinels: `ErrEscape` for a path that resolves outside the root;
  `ErrTooLarge` for a file over the effective read bound;
  `ErrInvalidLimit` for a rejected bound value; `ErrSecretPath`
  (change two) for a path the `Deny` matcher matches. `ErrTooLarge`
  wraps nothing. It is a policy refusal by this package, not a
  filesystem error, so no `*fs.PathError` is available to wrap.
- Constants: `DefaultMaxReadBytes int64 = 10 << 20`, the bound a
  workspace uses when the caller sets nothing; `Unbounded int64 = -1`,
  the explicit opt-out. The unexported `maxReadLimit` is
  `math.MaxInt64 - 1`, so `limit+1` cannot overflow. (commit B)
- `ReadFileLimit` has five stages, in this fixed order. One, validate
  the limit value through `effectiveLimit`. Two, `resolve`. Three, the
  deny check. Four, `os.Root.Open`. Five, measure the bytes against
  the bound. The shipped code already runs stages one, two, four, and
  five in that order; change two inserts stage three.
- Stage one and stage five are two different things, and this plan
  names them apart. Stage one validates the limit *value*, and it runs
  first because an invalid value is a caller bug that touches no path.
  Stage five enforces the limit, and it runs last because no bound can
  be measured before a file is open.
- The consequences are the precedence rows listed under "The deny
  check". `ErrInvalidLimit` beats `ErrEscape` and `ErrSecretPath`;
  `ErrSecretPath` beats `ErrTooLarge`. One path always produces one
  answer.
- File layout: `workspace.go` holds `Workspace`, `Options`,
  `Validate`, `Open`, `OpenWith`, `Root`, `Close`, and the unexported
  `validateLimit` helper; `confine.go` holds `resolve`, `withinRoot`,
  `classify`, and (change two) `denied` plus its symlink-walk helper;
  `read.go`, `write.go`, `list.go`, and `stat.go` each
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
- Change two's zero value points the other way, and the tension is
  deliberate. A nil `Deny` denies nothing. The two rules differ
  because `MaxReadBytes` has a safe default to fall back on and `Deny`
  has none: `secretpath` ships no built-in pattern list, so a
  fail-closed nil would have to refuse every path or invent a policy
  the caller never wrote. A fail-closed nil would also break
  `Open(root)`, which every existing caller uses.
- The compensating control sits one layer up. The phase-71 file-tool
  options carry their own `Deny *secretpath.Matcher`, and their
  `Validate` rejects a nil one. The model-reachable path therefore
  fails closed, while the library primitive stays usable without a
  policy. See `docs/plans/secrets.md`.
- `Unbounded` is a named constant, not a bool and not a bare negative
  number. A reader sees `MaxReadBytes: workspace.Unbounded` at the
  call site and needs no table to read it. A separate
  `UnboundedReads bool` would make two fields express one value and
  would need a conflict rule. A bare `-1` would read as a mistake.

### The deny check (change two)

One unexported method holds the whole rule:
`func (w *Workspace) denied(resolved, path string) error` in
`confine.go`. `resolved` is `resolve`'s output, the cleaned
root-relative name. `path` is the caller's raw string, used in the
error text only.

- `denied` returns nil at once when `w.deny` is nil. A workspace
  opened through `Open`, or through `OpenWith` with no `Deny`, does no
  extra work and makes no extra syscall.
- Stage one, the name check: `w.deny.Matches(resolved)`. A match
  returns `fmt.Errorf("%w: %s", ErrSecretPath, path)`.
- Stage two, the symlink walk: refuse any `resolved` that carries a
  symlink component. See below.
- The two stages run in that order. The name check is pure string work
  and is the common denial, so it costs no syscall. The walk exists to
  catch a permitted name that aliases a denied one. Both stages return
  the same sentinel, so the order changes the cost and the message
  text, never the verdict. A path that fails both is a denied name, so
  the name check's message is the accurate one for it.
- Neither stage opens a file descriptor at any of the four sites.
  `os.Root.Lstat` returns a `FileInfo` and no open file.
- The nil test lives here and nowhere else. Change two adds no
  remembered guard to a method body. `secretpath.Matcher.Matches` also
  becomes nil-safe in the same change, so the nil test is a
  walk-skipping optimization, not the last line between the package
  and a panic. See `docs/plans/secrets.md`.

The symlink walk:

- It walks every prefix of `resolved`, shortest first, and calls
  `w.r.Lstat` on each. For `a/b/c.txt` the prefixes are `a`, `a/b`,
  and `a/b/c.txt`.
- The final component is included. That is the load-bearing case: a
  permitted name such as `innocent.txt` may itself be the link to a
  denied file.
- A prefix whose `FileInfo.Mode()&fs.ModeSymlink` is non-zero returns
  `fmt.Errorf("%w: %s: symlink component", ErrSecretPath, path)`. The
  suffix separates the walk's refusal from the name check's. See
  "API".
- `resolved == "."` walks nothing. The root itself is the open
  `os.Root`, and it holds no component to test.
- A failing `Lstat` is not a symlink. `denied` ignores the error and
  continues the walk. A missing component, an unreadable parent, and a
  closed root all fall in that class.
- Ignoring the error is safe, and the reason is reachability, not
  state-independence. A failing `Lstat` cannot hide a reachable
  symlink, because `statat` and `openat` traverse the same chain from
  the same root descriptor. Any state that fails the walk's `Lstat`
  fails the following open with the same error. Three measured rows:
  `EACCES` under a `0o000` parent, `ENOTDIR` under a file, and
  `ENOENT` on a missing component.
- A later failure can never mask an earlier link, because the walk
  runs shortest first. A symlink is caught at its own prefix before
  any deeper `Lstat` runs.
- The walk's verdict is state-dependent by design. Reading whether a
  component is a symlink is a state read, and that read is the walk's
  whole job. The deny-before-existence row in "Precedence" is decided
  at stage one, by the name check, which returns before the walk runs.
- Cost: O(depth) `Lstat` syscalls per call, and only when `Deny` is
  non-nil and the name check permits the path. A workspace with no
  deny policy pays nothing, so change one's syscall-lean read path is
  unchanged for every caller that sets no `Deny`.

Why the walk exists, and what it does not close:

- Without it the name policy fails open through aliasing. Three
  measured cases, all with a `secrets/` deny list. A file symlink
  `innocent.txt` pointing at `secrets/key.pem` returns the secret from
  `ReadFile("innocent.txt")` with a nil error. A directory symlink
  `sdir` pointing at `secrets` returns the same secret from
  `ReadFile("sdir/key.pem")`. A write to `innocent.txt` overwrites
  `secrets/key.pem` through the link.
- The directory case is why the earlier mitigation was wrong. That
  plan advised denying the directory instead of the file. `sdir` is a
  denied directory, and the read still succeeded.
- No cheap atomic fix exists. `os.Root.OpenFile` ignores
  `syscall.O_NOFOLLOW`, measured on go1.26.0, so the open cannot
  refuse a link on its own.
- The walk is check-then-use. It closes the planted-checkout case,
  where a repository, an archive, or an earlier agent turn already
  placed the link. It does not close the concurrent-attacker case,
  where a writer that already holds access to the root swaps a
  component between the walk and the open.
- The residual race is a secrecy limit, not a confinement limit.
  `os.Root` confines the target to the root either way. The winning
  attacker reads a denied in-root file; the attacker does not leave
  the root.
- The limit belongs in code and in the package reference, not only in
  a plan. It goes in the `ErrSecretPath` doc comment and in
  `docs/packages/workspace.md`. See "Verification".
- The walk is a scope increase over the change-two plan first
  approved. It is accepted, because that plan's stated mitigation was
  measured false.

Precedence, pinned rows:

- An invalid limit beats an escape and beats a denial.
- A lexical escape beats a denial. The matcher never sees a path that
  leaves the root lexically.
- A denial beats a syscall escape. A denied name that is a link out of
  the root reports `ErrSecretPath`.
- A denial beats `ErrTooLarge`. The deny check needs no file; the
  bound needs an open one.
- A permitted name that links out of the root has two answers, and the
  policy picks which. With a nil `Deny` it reports `ErrEscape`, from
  inside `os.Root`. With `Deny` set it reports `ErrSecretPath`,
  because the walk refuses the link before `os.Root` sees it. This is
  the one row the walk turns over. `docs/plans/secrets.md` states the
  same two cases and pins them in one test.
- A dangling symlink follows that same rule, through a different
  mechanism. With a nil `Deny` it reports `fs.ErrNotExist`. With
  `Deny` set it reports `ErrSecretPath`, because the walk refuses any
  symlink component and never reaches the missing target.
- A denial beats non-existence. A denied path that does not exist
  reports `ErrSecretPath`, because the name check precedes the open.
  This is a property, not an accident: a denial leaks the policy and
  never the filesystem.
- A denial beats a closed workspace. `resolve` still succeeds after
  `Close`, so a denied path reports `ErrSecretPath` rather than
  `fs.ErrClosed`. A permitted path still reports `fs.ErrClosed`.
- The three sentinels stay distinct. `ErrSecretPath`, `ErrEscape`, and
  `fs.ErrNotExist` are not blurred into one answer. The matcher is
  caller-configured, and a deny decision made before the open reveals
  nothing about the filesystem, so no disclosure argument buys the
  loss of diagnosability.

### Callers this change touches

`subagent` is the only production caller of this package. Both commits
edit it.

- Commit A: `subagent/subagent_test/filetools_test.go`'s
  `openWorkspace` helper gains
  `t.Cleanup(func() { _ = ws.Close() })`. Without it every subagent
  file-tool test leaks one descriptor once `Workspace` holds an
  `os.Root`.
- Commit B: `subagent/workspacewritetool.go` drops the mode argument
  from its `t.ws.WriteFile(args.Path, []byte(args.Content), ...)`
  call, and deletes the now-unused `workspaceWriteFileMode` constant.
  The doc comments on `WorkspaceWriteTool` and on `Run` name the
  `0o600` mode; both must then name `workspace` as the source of the
  mode. `docs/plans/subagent.md` already anticipates this edit in its
  "Tracking the in-flight workspace migration" bullet.
- Commit B changes `subagent.WorkspaceReadTool`'s behavior. `Open`
  now yields `DefaultMaxReadBytes`, so a read over 10 MiB starts
  returning `ErrTooLarge` where it succeeded before. That tool's own
  `maxResultBytes` caps the result after the allocation, so it is not
  a substitute for the bound. The new default is deliberate: no
  model context holds 10 MiB of text.

### Outside

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
- Hard-link detection. A hard link carries no distinguishing mode bit,
  so the symlink walk cannot see it. Measured: `os.Link` from
  `secrets/key.pem` to `innocent.txt` leaves the walk permitting, and
  the read returns the secret. `docs/plans/secrets.md` records the
  scope and the reason.
- Resolving a link and re-matching its target. Rejected; see
  `docs/plans/secrets.md`.
- Case folding and Unicode normalization in the matcher. `Matches`
  goes through `path.Match`, which is byte-exact. A case-insensitive
  filesystem, such as the default on darwin or windows, opens the same
  file from `SECRETS/key.pem` while the matcher reports false on it.
  A normalizing filesystem does the same across NFC and NFD spellings.
  Change two adds no per-platform folding: that behavior belongs to
  `secretpath` and needs its own plan. The gap is recorded as a
  residual risk in `docs/plans/secrets.md`.
- A defense against a concurrent attacker on the symlink walk. See
  "The deny check".
- A labelled `ErrEscape` under a concurrent attacker. A deterministic
  escape reports `ErrEscape`. A refusal that arrives while an attacker
  swaps a path component may report the raw syscall error instead. A
  measured prototype produced `too many levels of symbolic links`
  (`ELOOP`) on roughly 20 percent of racing writes. `os.Root` exhausts
  its symlink-retry budget and reports that, not an escape. The
  refusal still fails closed. Only the error's class is unlabelled.
- Parity with bare `os.Root` on an in-root symlink. `resolve` runs
  `filepath.Clean` before any syscall, so a path that traverses `..`
  through an in-root symlink resolves lexically. Bare `os.Root`
  follows the link and then applies `..`, so it permits paths this
  package refuses. Measurement: bare `os.Root` accepts
  `sub/link/../mark.txt` where `sub/link` points to `../other`; the
  cleaned name `sub/mark.txt` does not exist. `filepath.Clean` is the
  source, not the lexical pre-check. See "Two facts decide it" below.
  This is pre-existing behavior and deliberate.
- Support for an absolute symlink whose target is inside the root.
  This is a behavior tightening, and it is deliberate. `os.Root`
  documents "Symbolic links must not be absolute". Measurement:
  `r.Open("inlink_abs")` returned `path escapes from parent` for a
  symlink whose absolute target lies inside the root. Today
  `filepath.EvalSymlinks` resolves it and the read succeeds. After
  commit A the read returns `ErrEscape`. A caller that needs the link
  followed uses a relative symlink.
- A root of `/` once failed closed. `withinRoot` indexed
  `p[len(root)]` for the separator, which is the character after the
  separator when `root` is `/`. `Stat("etc")` under a root of `/`
  returned `ErrEscape`. `withinRoot` now goes through `filepath.Rel`,
  which this plan already names as the correct classifier, so the
  limit is gone. `confine_internal_test.go` pins the root-`/` rows.

## API

Every entry lands in `api/workspace.txt` through `make api-update`.
Run `make api-update` once per commit, A and B.

Surface after commit A:

- `var ErrEscape = errors.New("workspace: path escapes root")`
- `type Workspace struct { ... }` (fields unexported)
- `func Open(root string) (*Workspace, error)`
- `func (w *Workspace) Root() string`
- `func (w *Workspace) Close() error`
- `func (w *Workspace) ReadFile(path string) ([]byte, error)`
- `func (w *Workspace) WriteFile(path string, data []byte, perm os.FileMode) error`
- `func (w *Workspace) List(path string) ([]os.DirEntry, error)`
- `func (w *Workspace) Stat(path string) (os.FileInfo, error)`

Commit A delta against the locked `api/workspace.txt`:

- Added: `func (w *Workspace) Close() (error)`
- No other line changes. `Close` is the whole delta.

Surface added by commit B:

- `const DefaultMaxReadBytes int64 = 10 << 20`
- `const Unbounded int64 = -1`
- `var ErrTooLarge = errors.New("workspace: file exceeds read limit")`
- `var ErrInvalidLimit = errors.New("workspace: invalid read limit")`
- `type Options struct { Root string; MaxReadBytes int64 }`
- `func (o Options) Validate() error`
- `func OpenWith(opts Options) (*Workspace, error)`
- `func (w *Workspace) ReadFileLimit(path string, limit int64) ([]byte, error)`

Commit B delta:

- Removed: `func (w *Workspace) WriteFile(path string, data []byte, perm os.FileMode) (error)`
- Added: `func (w *Workspace) WriteFile(path string, data []byte) (error)`
- Added: `type Options struct` with the fields `Root` and
  `MaxReadBytes`. `api_surface` prints one line per exported field, so
  the rendered line count is larger than the symbol count.
- No method signature changes apart from `WriteFile`. `ReadFile`
  keeps `(path string) ([]byte, error)`, so no existing call site
  changes.

Surface added by change two:

- `var ErrSecretPath = errors.New("workspace: path is a secret path")`
- `Options` gains the field `Deny *secretpath.Matcher`

`Options` and `OpenWith` land in commit B, because the read bound
needs a place to live at open time. Change two then adds one field and
one sentinel to a struct that already exists. `docs/plans/secrets.md`
states the same split.

Change two's lock delta is exactly two lines in `api/workspace.txt`:
the `ErrSecretPath` variable and the `Deny` field. `api_surface`
prints one line per exported struct field, so `Deny` renders on its
own line beside `Root` and `MaxReadBytes`.

`ErrSecretPath` wraps like `ErrEscape`, so the two sentinels read
alike. The name check uses
`fmt.Errorf("%w: %s", ErrSecretPath, path)`, matching
`confine.go:29`. The `path` it echoes is the caller's raw string, not
`resolve`'s output, again matching `ErrEscape`.

The symlink walk uses one sentinel and a different text:
`fmt.Errorf("%w: %s: symlink component", ErrSecretPath, path)`. The
two refusals are different facts, so one message for both would lie. A
permitted link to a permitted file is not a secret path; it is a
symlink. An agent cannot self-correct from a message that names the
wrong cause, and a human reads the deny patterns for a rule that is
not there. A second sentinel was rejected: it would add a lock line,
add a precedence row, and force every caller to test two values. One
sentinel plus distinct text costs nothing and stops the message from
lying.

Change two threatens no structure limit. `read.go` is 69 lines,
`write.go` 35, `list.go` 30, and `stat.go` 17; each gains one line.
`confine.go` is 72 lines and gains `denied` plus its walk helper, so
it stays far under the 500-line file limit and the 80-line function
limit.

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
  and `os.Root.MkdirAll(0o700)` yielded `rwx------` on a new
  directory and left an existing directory's mode alone. The doc
  comment on `WriteFile` must state the enforceable rule: it creates a
  new file with `0o600`, and it does not tighten an existing file.
  `TestWriteFileMode` pins both halves, so no comment claims a rule
  without an enforcer.
- `Options` plus `OpenWith` follows the `Options.Validate` pattern
  the repo already uses in `dispatch` and `a2aack`. `Open` stays as
  the one-argument form.
- `Open("")` changes behavior in commit B. Measured on the current
  tree, `Open("")` succeeds and binds the working directory, because
  `filepath.Abs("")` returns it. `Open` routes through
  `Options.Validate` from commit B on, so `Open("")` fails instead. An
  implicit working-directory sandbox is a footgun, so the change is
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

The lexical `withinRoot` pre-check survives as redundant defense. Its
accurate justification is narrow:

- `filepath.Clean` supplies the state-independence, not `withinRoot`.
  `resolve` runs `filepath.Clean(filepath.Join(root, path))` before any
  syscall, so `os.Root` never sees an un-normalized `a/../..`. A
  measurement of bare `os.Root` on that raw string describes an input
  `resolve` cannot produce.
- `filepath.Rel` alone supplies the escape verdict. A cleaned path
  outside the root yields a relative name starting with `..`, which
  `os.Root` refuses.
- The pre-check's one observable effect is the refusal message: the
  caller gets this package's `ErrEscape` text instead of a
  syscall-shaped `*fs.PathError`. Two reproductions confirm nothing
  else changes. Deleting the `withinRoot` call from `resolve` leaves
  the whole `workspace` suite green, and a 38-vector differential
  harness comparing verdicts with and without the pre-check reports
  zero differences.
- The pre-check is cheap and does no filesystem work, so it adds no
  new check-then-use gap. It classifies; it does not enforce.
- The fourth `TestTraversalEscape` vector does not pin the pre-check.
  It pins the state-independence of the verdict, which `filepath.Clean`
  owns.
- The pre-check was the sole cause of the root-`/` limit once recorded
  in Scope-Outside. `filepath.Rel` classifies `Stat("etc")` under a
  root of `/` correctly, so the pre-check now calls `filepath.Rel`
  itself and the limit is closed.

`resolveDeepestExisting` is deleted in commit A. It is the part that
did filesystem work between the check and the syscall, so it is the
part that created the gap. `os.Root` now owns every symlink decision.
`withinRoot` stays and works on the lexically cleaned path only. The
sibling-prefix protection comes from `withinRoot` and, independently,
from `os.Root`; a review confirmed both.

`os.Root` closes the intermediate-component gap. Measurement: the
exact swap attack over 1,371,896 iterations produced zero leaked
reads, and the refusals split across `path escapes from parent`,
`ENOENT`, and `ENOTDIR`. The write-side race left the outside file
intact.

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
`TestSymlinkEscapeIntermediateComponent` pin the mapping.
`TestClassifyPassesThrough` pins the other direction. All three are
deterministic, so a change in the standard library's wording turns a
test red instead of turning a guarantee off. The two race tests below
cannot pin it, because they assert no error class at all.

## Tests

Tests live in `workspace/workspace_test/`, table-driven where the case
set grows. One exception: `workspace/confine_internal_test.go` is
`package workspace`, because `withinRoot` is unexported and no
temporary directory can produce a root of `/`. It is the whitebox
pattern `flow/wave_select_internal_test.go` already sets. Commit A splits the escape and `classify` cases out of
`workspace_test.go` into `escape_test.go`, because the combined file
passes the 500-line structure limit. `workspace_test.go` keeps the
open, lifecycle, round-trip, and listing cases.

### Existing tests that must change

Commit A:

- `workspace_test.go`, every `workspace.Open` call: add
  `t.Cleanup(func() { _ = w.Close() })`. A `Workspace` now holds a
  descriptor.
- `subagent/subagent_test/filetools_test.go`, the `openWorkspace`
  helper: add `t.Cleanup(func() { _ = ws.Close() })` before it
  returns `ws`. Every subagent file-tool test uses that helper.
- `TestTraversalEscape`: existing assertions stay, and the table gains
  two vectors. Vector one keeps `a/../../secret` with no `a` on disk.
  Vector two uses the same string with `a` created as a real directory
  in the root first. Both must report `ErrEscape`. Vector two pins
  that the verdict does not depend on disk state, which
  `filepath.Clean` owns. Keep the absolute-path vector, which pins
  the `filepath.IsAbs` branch in `resolve`.
- `TestSymlinkEscapeFinalComponent` and
  `TestSymlinkEscapeIntermediateComponent`: existing assertions stay,
  and each gains one assertion. The returned error must match
  `errors.Is(err, ErrEscape)` and must also satisfy
  `errors.As(err, &pathErr)` for a `*fs.PathError`. That pair is what
  `classify` promises through its two-verb `%w` wrap. These two tests
  are deterministic, so they are the pin for `rootEscapeText`.
- `TestSymlinkEscapeIntermediateComponent` gains a write case:
  `WriteFile("link/sub/planted.txt", data, 0o600)` must return an
  error matching `ErrEscape` (drop the mode in commit B), and no file
  may appear at the symlink's
  outside target. The refusal comes from `os.Root.MkdirAll`, so this
  case is the only pin on the `MkdirAll` branch of `classify`.
- `TestHappyPathRoundTrip`: gains one vector. A read through an
  absolute in-root path,
  `w.ReadFile(filepath.Join(w.Root(), "a/b/c.txt"))`, returns the
  written bytes. This pins `resolve`'s root-relative conversion. An
  implementation that returns an absolute path from `resolve` turns
  this legitimate read into `ErrEscape`, and every other planned case
  still passes.
- `TestSiblingPrefixEscape` exists already in
  `workspace/workspace_test/workspace_test.go`. It needs no new case.
  It must keep passing after the migration, unchanged apart from the
  `t.Cleanup` close and the commit B argument drop.

Commit B:

- `workspace_test.go`, `confine_integration_test.go`, and
  `filetools_test.go`, every `WriteFile` call: drop the `perm`
  argument.
- `subagent/workspacewritetool.go` drops the same argument. See
  "Callers this change touches".

### New unit cases in `workspace_test.go`

Commit A:

- `TestClose`: after `Close`, `ReadFile`, `WriteFile`, `List`, and
  `Stat` each return an error matching `fs.ErrClosed`. A second
  `Close` returns nil. A zero-value `Workspace` and a nil
  `*Workspace` each return nil from `Close` without a panic.
- `TestEmptyPath`: `Stat("")` behaves as `Stat(".")` and reports a
  directory, proving `resolve` maps an empty path to `.` before the
  `os.Root` call, which rejects an empty name. It also asserts that
  `ReadFile("")` and `ReadFile(".")` both return a non-nil error that
  matches neither `ErrEscape` nor `fs.ErrNotExist`. That pins the
  directory read as a raw filesystem error.
- `TestClassifyPassesThrough`: `classify` must not relabel an
  unrelated error. Three cases, each asserting the error does **not**
  match `ErrEscape`.
  Case one: `ReadFile("missing.txt")` returns an error matching
  `fs.ErrNotExist`.
  Case two: a file under a `0o000` directory returns an error matching
  `fs.ErrPermission`. Skip this case when the test runs as root, where
  the mode does not apply.
  Case three: a dangling symlink inside the root returns an error
  matching `fs.ErrNotExist`. Measured text:
  `openat dangling: no such file or directory`.
  Without this test, an implementation that matches on
  `errors.As(err, &pathErr)` alone passes the whole suite and turns
  every permission and missing-file error into `ErrEscape`.
- `TestListSortsByName`: write eight files in a scrambled order and
  assert `List(".")` returns them sorted by filename. `TestListRootItself`
  holds one entry, so it cannot catch a lost sort.
- `TestListOnFile`: `List` on a regular file returns a raw filesystem
  error, not `ErrEscape`. The open succeeds, so the directory read is
  the stage that refuses.
- `TestAbsoluteSymlinkInsideRoot`: create a symlink inside the root
  whose target is an absolute path also inside the root. `ReadFile`
  on that link returns an error matching `ErrEscape`. This pins the
  deliberate tightening recorded in Scope-Outside.

Commit B:

- `TestWriteFileMode`: `WriteFile("a/b/c.txt", data)` creates the file
  with mode `0o600` and the parent directories with mode `0o700`.
  Assert `info.Mode().Perm()` for the file and for both directories.
- `TestWriteFileKeepsExistingMode`: create `wide.txt` at `0o644`
  outside the package API, then call `WriteFile("wide.txt", data)`.
  Assert the file still reports `0o644` and holds the new data. This
  pins the measured `os.Root.WriteFile` behavior: the mode applies at
  create only, so `WriteFile` never tightens an existing file.
- `TestOpenRejectsBlankRoot`: `Open("")` returns an error, and
  `OpenWith(Options{})` returns the same error. This pins that
  `OpenWith` calls `Validate`, and that `Open("")` no longer binds
  the working directory.

### New file `workspace/workspace_test/read_limit_test.go` (commit B)

Each case writes its fixture file outside the package API, so the file
size is exact.

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
- `TestOverLimitClosesFile`: pin the deferred `Close` on the
  `ErrTooLarge` path. Do one warm-up `ReadFile` first, so the runtime
  has opened whatever it opens once. Then count the entries in
  `/proc/self/fd`, run 200 `ReadFile` calls that each return
  `ErrTooLarge`, and count again. Assert the second count is not
  greater than the first. Do not assert exact equality: the process
  descriptor count moves under the runtime's own churn. Skip the test
  where `/proc/self/fd` is absent.
- `TestSecretPathBeatsLimit` (change two): a workspace with a `Deny`
  matcher for `.env` and `MaxReadBytes: 1` reads `.env`. The error
  matches `ErrSecretPath` and does not match `ErrTooLarge`. This pins
  the stated order between the deny check and the bound.
- `TestEscapeBeatsLimit`: `ReadFile("../outside.txt")` on a
  1-byte-limited workspace returns `ErrEscape`, not `ErrTooLarge`.
- Change-two `Options.Validate`, deny, and symlink-walk cases live in
  `workspace/workspace_test/secret_test.go`. See
  `docs/plans/secrets.md`; do not duplicate them here.
  `TestSecretPathBeatsLimit` stays in this file, because it is a
  precedence case against the bound.

### New file `workspace/workspace_test/confine_integration_test.go` (commit A)

It holds the two check-then-use cases. Each one races a real attacker
goroutine against a real workspace call. Each skips on Windows,
where symlink creation needs a privilege.

- `TestWriteFollowsNoSwappedSymlink`. This is the case the old code
  loses. Setup: workspace root `R` with directory `R/staging`, and
  an unrelated directory `O` outside `R` holding `O/target` with
  content `outside-original`. Goroutine one loops 500 times calling
  `w.WriteFile("staging/out.txt", []byte("payload"), 0o600)`, and
  drops the mode in commit B. Goroutine two
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
  passing one after. Commit A is the commit that flips it.
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

### New file `workspace/workspace_test/workspace_bench_test.go` (commit A)

- `BenchmarkReadFile` over a small file in the root. Record the
  allocation count when the change lands. No allocation budget is
  asserted, because the syscall dominates. `io.ReadAll` grows its
  buffer, so the count may rise against the `os.Root.ReadFile` form.
  Record the new number; do not assert one.

## Verification

Run the list below for commit A, then again for commit B, then again
for change two. The change-two-only items follow the shared list.

- `make verify` passes: gofmt, vet, tests under the race detector,
  the doc gate, the structure gate, the plan gate, the deps gate,
  the API gate, the Semgrep scan, and the probe suite.
- `go test -race ./workspace/... ./subagent/...` passes. Both
  packages change in both commits.
- Coverage for `workspace` reaches the 85 percent floor, and the
  total floor holds.
- `make api-update` runs, and the `api/workspace.txt` diff lands in
  the same commit as the code. Commit A's diff is one added `Close`
  line. Commit B's diff carries the `WriteFile` signature change and
  the new limit surface.
- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  `python3 scripts/check_prose.py`, and
  `python3 scripts/check_labels.py` pass.
- `policy/layers.json` already holds the row
  `"workspace": ["secretpath"]`. Change one uses no such import. The
  row is deliberately ahead of the code, and `check_deps.py` only
  rejects an undeclared import, so an unused declared edge stays
  gate-invisible. Neither commit A nor commit B edits that file.
- The read bound adds no import edge. `io` and `math` are standard
  library.
- `go.mod` needs no change. It declares `go 1.25.0`, and every
  `os.Root` method used here exists at that minimum: `WriteFile`,
  `MkdirAll`, `Stat`, `Open`, and `Close`. The behavior
  measurements in this plan come from the local go1.26.0 toolchain.
- `rootEscapeText` is a named constant for one reason: it gives the
  brittleness one named point. No Semgrep rule requires it. The rules
  `sdk.go.no-enum-string-literals` and
  `sdk.go.hash-prefix-centralized` cover enum values and hash
  prefixes, and neither applies here.
- `docs/packages/workspace.md` changes in the same commit as the code.
  Commit A: lines 36 to 41 state the deleted `resolveDeepestExisting`
  behavior as an invariant; replace them with the `os.Root` one, and
  add `Close` to the surface list. The runnable example at line 76
  gains `defer w.Close()`, so the program a reader copies shows the
  lifecycle the API now requires. Commit B: line 23 carries the
  `perm` parameter in the `WriteFile` signature, and line 81 passes
  `0o600` in the usage example; any claim that a read is unbounded is
  then false. Commit B adds `Options`, `OpenWith`, `ReadFileLimit`,
  `DefaultMaxReadBytes`, `Unbounded`, `ErrTooLarge`, and
  `ErrInvalidLimit` to the surface list.
- `AGENTS.md` describes each package's surface. Commit A names
  `Close` and the `os.Root` confinement in the `workspace` entry.
  Commit B names the read bound.
- `docs/plans/subagent.md`'s "Tracking the in-flight workspace
  migration" bullet describes commit B's edit as a follow-up. Commit B
  updates that bullet to state the edit is done.
- `Options.Validate` and `ReadFileLimit` both call the unexported
  `validateLimit` helper, so no comment states the limit rule alone.
  `effectiveLimit` calls `validateLimit` before it maps a passing
  value onto the value the read uses.

Change two only:

- `python3 scripts/check_deps.py` passes against the existing row
  `"workspace": ["secretpath"]`. Change two is the commit that first
  uses that edge. `policy/layers.json` needs no edit.
- `make api-update` runs, and `api/workspace.txt` gains exactly the
  `ErrSecretPath` line and the `Deny` field line.
- The `ErrSecretPath` doc comment states four things: the name check,
  the symlink-component refusal, the residual race, and the hard-link
  limit. The race sentence must say that a concurrent writer inside
  the root can swap a component between the check and the open, and
  that `os.Root` still confines the target to the root. The hard-link
  sentence must say that a hard link to a denied file is not detected.
- `docs/packages/workspace.md` changes in the same commit. It gains
  `ErrSecretPath` under "Failure modes", the `Deny` field under
  "Types", the `secretpath` edge under "Cross-references", and a
  residual-risk paragraph carrying the same race limit plus the
  case-insensitive-filesystem limit. Its "Cross-references" section
  today says `workspace` declares no internal import edge; that
  sentence becomes false.
- `AGENTS.md`'s `workspace` entry drops "A leaf package; no internal
  imports", names the `secretpath` import, and lists `ErrSecretPath`.
- `secretpath.Matcher.Matches` becomes nil-safe in the same change.
  See `docs/plans/secrets.md`. `api/secretpath.txt` does not change.
- Coverage for `workspace` and `secretpath` each stay at or above the
  85 percent floor.
- `workspace` holds a mutation-kill floor of 96, in
  `scripts/mutation_denylist/workspace.json`. Run `make mutation-gate`
  to check it.

## Correctness fix: Options.Validate admits a whitespace-only Root

Status: planned, not yet built.

### Fix goal

`Options.Validate`'s doc comment (`workspace/workspace.go:78-83`) says
"Root must not be blank." The code (`:82`) checks `o.Root == ""`
only. A `Root` of `" "` (a single space) is not empty, so it passes
`Validate` and reaches `OpenWith`. `dispatch/options.go`,
`tools/registry.go`, and `agentloop/options.go` already reject the
same shape for their own required-string fields, through
`strings.TrimSpace(name) == ""`. `Root` is a caller-supplied config
value with no other check ahead of it, so a padded value (a common
mistake from a YAML or JSON document) is exactly the case this
package's other blank checks already guard against elsewhere in the
module.

### Fix scope

Inside:

- `Options.Validate`, in `workspace/workspace.go`, changes its check
  from `if o.Root == ""` to `if strings.TrimSpace(o.Root) == ""`. The
  stored and later-resolved `Root` value stays exactly as the caller
  supplied it; only the blank test trims. `Open` and `OpenWith` do
  not rewrite `Root` and do not trim it before resolving it, so a
  `Root` with meaningful leading or trailing whitespace outside the
  all-whitespace case is unaffected by this fix.
- `workspace/workspace.go` gains a `"strings"` import.

Outside:

- Trimming `Root` before `filepath.Abs` or `os.OpenRoot`. This fix
  only widens the blank rejection; it does not change how a
  non-blank `Root` resolves.
- `MaxReadBytes` or `Deny`. Neither carries a "blank" claim.

### Fix API

No exported symbol changes. `make api-update` must produce no diff
for `api/workspace.txt`. No `policy/layers.json` change: `strings` is
standard library, not an internal package edge.

### Fix tests

In `workspace/workspace_test/workspace_test.go`:

- `TestOpenWithRejectsWhitespaceOnlyRoot` — `Options{Root: "
  "}.Validate()` returns an error whose text contains `Root is
  blank`, the same text `Options{Root: ""}.Validate()` already
  returns. Fails against today's code, where `Validate`
  returns nil for a single-space `Root`. Measured today: `Open(" ")`
  still fails, but only later and only by accident, with a raw
  `lstat: no such file or directory` error from `filepath.EvalSymlinks`
  once the process's current directory holds no file named `" "`, not
  with the deliberate blank-root rejection `Validate` is meant to
  give. A caller whose working directory happens to hold such a file
  would open it as the workspace root silently. This case pins the
  earlier, deliberate rejection instead of that accidental, environment-
  dependent one.
- Positive control, extending the existing `TestOpenRejectsBlankRoot`
  case or added beside it: `Open("")` and `OpenWith(Options{Root: ""})`
  keep returning the same blank-root error they return today. Proves
  the trim does not change the already-covered empty-string case.
- A second positive control: `OpenWith(Options{Root: root})`, where
  `root` is a real temporary directory `t.TempDir()` returns, still
  opens successfully. Proves the trim does not reject a normal path.

### Fix verification

- `make verify` passes; `workspace` holds the 85 coverage floor.
- `go test -race ./workspace/...` passes.
- `python3 scripts/check_api.py` passes with no `api/` diff.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass. No `policy/layers.json` change.
- `make mutation-gate` holds `workspace`'s existing mutation-kill
  floor of 96 in `scripts/mutation_denylist/workspace.json`.
- `docs/packages/workspace.md` line 35's `Options.Validate()` entry
  needs no wording change: "Root must not be blank" already matches
  the corrected code.
