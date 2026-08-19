# secrets plan

## Goal

Connect the three secret-handling pieces into one path a caller can
take. `secretpath` decides which paths hold secrets. `workspace`
enforces that decision at the filesystem boundary. `envfile` parses
the bytes `workspace` hands back. Today the three ship as leaves with
no non-test consumer anywhere in the module.

This is a composition change. It adds one import edge, one `Options`
field, one sentinel, one nil guard in `secretpath`, and one `envfile`
function. It builds no tool wrapper.

## Status

Both halves are shipped, so nothing in this plan is deferred.

The `workspace` half shipped first: `Options.Deny`, `ErrSecretPath`,
the two-stage deny check, and the `secretpath` nil guard. See
`docs/plans/workspace.md` change two.

The `envfile` half shipped next: `LoadBytes`, its `api/envfile.txt`
line, its `docs/packages/envfile.md` and `docs/plans/envfile.md`
entries, its `AGENTS.md` entry, the `envfile/envfile_test/` rows, and
`workspace/workspace_test/secret_integration_test.go`, which calls
`LoadBytes`.

## Scope

Inside:

- `workspace` consults an optional `*secretpath.Matcher` before the
  filesystem call. The check lives at four sites: `ReadFileLimit`,
  `WriteFile`, `List`, and `Stat`. `ReadFile` is
  `ReadFileLimit(path, 0)` and holds no check of its own, so it
  inherits one. A fifth copy would drift from the other four. The
  surface is the new `Options.Deny` field and `ErrSecretPath`.
  `docs/plans/workspace.md` holds the full `workspace` surface, and
  its change one already landed `Options`, `Validate`, and `OpenWith`
  for the read bound.
- The rule itself lives in one unexported method,
  `(*Workspace) denied(resolved, path string) error` in
  `confine.go`. It holds the nil test, the `Matches` call, the
  symlink walk, and the `ErrSecretPath` wrap. Each of the four sites
  calls it in one line. `docs/plans/workspace.md` specifies it.
- The deny check runs before the read bound, so a denied path over
  the bound reports `ErrSecretPath`. See `docs/plans/workspace.md`.
- The denial runs after the lexical confinement check and before the
  `os.Root` call. That order has three consequences, and this plan
  states all three.
- A path that lexically escapes the root reports `ErrEscape`. The
  lexical check runs first, so the matcher never sees such a path.
- A root-local symlink whose own name is denied reports
  `ErrSecretPath`, even when the link target lies outside the root.
  The name check runs before `os.Root` resolves the link. An escaping
  path can therefore report `ErrSecretPath`.
- A symlink with a permitted name that resolves outside the root
  reports `ErrEscape` when `Deny` is nil. It passes the matcher and
  fails inside `os.Root`. With `Deny` set it reports `ErrSecretPath`
  instead, because the symlink walk below refuses it first.
- **Scope increase: refuse a symlink component when `Deny` is set.**
  A name policy alone fails open through aliasing, and the earlier
  plan's mitigation for that was measured false. So `denied` walks
  every prefix of the resolved path with `os.Root.Lstat`, including
  the final component, and returns `ErrSecretPath` when any component
  is a symlink. The walk runs only when `Deny` is non-nil, and only
  after the name check permits the path.
  `docs/plans/workspace.md` owns the full specification: the prefix
  set, the `Lstat` error rule, the O(depth) cost, and the residual
  race.
- The consequence for a caller: a workspace with a `Deny` policy
  refuses every symlink path, denied or not. That is a deliberate
  tightening. A caller who needs symlinks inside the root sets no
  `Deny` and enforces the policy itself.
- A nil `Deny` matcher denies nothing, and `Open(root)` leaves it nil,
  so no existing caller changes.
- `secretpath.Matcher.Matches` becomes nil-safe: `if m == nil {
  return false }` at the top of the method. A nil `Matcher` matching
  nothing is exactly what a nil `Options.Deny` already means. Today a
  call on a nil `*Matcher` reads `m.patterns` and panics, and the
  Semgrep rule `sdk.go.no-panic-in-packages` cannot see that, because
  it matches a literal `panic(` and not a nil dereference. The guard
  removes the failure class instead of relying on every caller to
  remember it.
- The two fixes compose. `denied` still tests `w.deny` for nil, now to
  skip the symlink walk rather than to avoid a panic.
- `envfile.LoadBytes(data []byte) (map[string]string, error)` parses
  an already-read dotenv body. `Load` reads the file and calls
  `LoadBytes`, so the two share one parser and one error set.
- `LoadBytes` gives `envfile` a caller shape that composes with
  `workspace`: read the bytes through the confined, secret-aware
  workspace, then parse them. No import edge is needed in either
  direction.

Outside:

- Any `tools.Tool` wrapper over `workspace`, `envfile`, or `diff`.
  Phase 71 owns that; see "Phase 71 owns the tool surface" below.
- A non-test consumer for `envfile` and `secretpath`. State this
  plainly: after this change those two still have no caller inside
  the module, except that `workspace` now imports `secretpath`.
  `subagent` already wraps `workspace` and `diff` as file tools, so
  the deny policy reaches a real caller through `workspace`.
- The reason that was accepted: the policy edge belongs inside the
  confinement boundary, not in the wrapper that comes later. Building
  the edge first keeps the wrapper from owning the safety rule. The
  cost is uncalled surface until phase 71 lands, and the contract
  below is what pays that cost back.
- A `Secret` string type, a redacting type, or value zeroing in
  `envfile`. A Go string is immutable, and the garbage collector may
  copy it, so a `Zero` method cannot promise the bytes are gone. A
  type that promises what it cannot deliver is worse than a plain
  map. `envfile` already refuses to put a parsed value in any error
  message, which is the guarantee it can actually keep.
- A built-in secret pattern list. `secretpath` ships none, and this
  change adds none. The caller supplies the patterns.
- Exposing `os.Root.FS()` from `workspace`. A raw `fs.FS` reaches the
  root without passing the `Deny` matcher, so it would open a second
  policy path. If a future caller needs an `fs.FS`, it must be a
  wrapper that applies `Deny`, and it needs its own plan.
- Reading the filesystem from `secretpath`. `Matches` stays a pure
  string decision.
- Resolving a symlink and re-running the matcher on its target. This
  is the obvious alternative to blanket refusal, and it is worse.
  `os.Root.Readlink` returns one hop, so a chain needs its own loop
  and its own bound. Each extra read adds a second check-then-use
  window between the resolution and the open. It still misses hard
  links, which carry no link target to read. Recorded here so phase 71
  does not relitigate it.

## Why the write side is denied too

Denying reads is obvious. Denying writes needs its reason written
down, because the objection is real: an agent may legitimately want to
write its own `.env`.

- The threat is plant-and-redirect. An agent that can write a denied
  path can overwrite `.env` with an attacker-chosen endpoint or key,
  and the next turn reads it back and uses it. Read-only denial leaves
  that path open, so a read-only policy protects the current secret
  and loses the next one.
- The same argument covers a denied config file, a denied credentials
  file, and a denied private key.
- The workaround is in the policy, not in the API. A caller that wants
  one writable path adds a negation pattern, such as `!config/app.env`
  after `config/`, or omits that path from the deny list. `secretpath`
  already supports negation, and the last matching pattern decides.
- No write-only or read-only mode is added to `Options`. One matcher,
  one meaning. A second mode would double the policy surface for one
  caller that does not exist yet.

Residual risk, stated plainly:

- The `Deny` matcher is a name policy, not a content policy. A file
  named `notes.txt` holding an API key is not denied.
- The symlink walk is check-then-use. It closes the planted-link case.
  It does not close a concurrent attacker who already holds write
  access to the root and swaps a component between the walk and the
  open. `os.Root` still confines the target to the root, so the limit
  is on secrecy, not on confinement.
- A hard link is the same aliasing class, and the walk does not close
  it. A hard link carries no distinguishing mode bit, so `Lstat`
  reports a regular file. Measured with a `secrets/` deny list:
  `os.Link` from `secrets/key.pem` to `innocent.txt` gives
  `symlink=false`, the walk permits, and `ReadFile("innocent.txt")`
  returns the secret with a nil error.
- The hard-link case ranks below the symlink case for three reasons.
  `workspace` exposes no `Link`, so this package cannot create one.
  Git carries no hard link, so a checkout cannot plant one. Creating
  one needs write access through another channel.
- The case is still real, because tar and zip archives carry hard
  links, and archive extraction is inside the planted-checkout threat
  the walk names. Closing it needs inode identity, which is a
  different mechanism and a different plan. The limit goes in the
  `ErrSecretPath` doc comment, not only here.
- The matcher is byte-exact, because `path.Match` is. On a
  case-insensitive filesystem, the default on darwin and windows,
  `SECRETS/key.pem` opens the file that `secrets/` denies, and the
  matcher reports false. A Unicode-normalizing filesystem does the
  same across NFC and NFD spellings of one name. This change adds no
  case folding and no normalization: that behavior belongs to
  `secretpath` and needs its own plan.
- `List` returns the names of denied files inside a permitted
  directory. It denies the listing of a denied directory, not the
  existence of a denied name.

## Why the edge belongs in `workspace`

Three placements were considered.

- Option A, recommended: `workspace` imports `secretpath` and the
  check runs inside the confinement boundary. Reason: a caller that
  forgets to check is exactly the failure mode this package already
  refuses to allow for path traversal, and the same argument applies
  to secret paths.
- Option B: a new wrapper package over `workspace` plus `secretpath`.
  Rejected. It adds a fourth package with no caller to fix three
  packages with no caller. `AGENTS.md` forbids abstraction without a
  caller.
- Option C: leave both leaves alone and let the phase-71 tool wrapper
  compose them. Rejected as the only measure, kept as a complement.
  A tool wrapper is one caller among several, and the policy would
  live outside the boundary it protects. `workspace` would stay a
  package whose safety depends on the caller reading its doc.

The wrapper form was checked against the precedent in `spool`.
`spool.SpoolTool` forwards three optional interfaces and needs an
eight-case switch at `spool/tool.go:157` to do it. A fourth optional
interface makes that switch sixteen cases. That cost is a reason to
put policy in the block, not in another wrapper layer.

`workspace` importing `secretpath` keeps the direction inward.
`secretpath` is a leaf with an empty row and stays one. No cycle is
possible.

## API

Every entry lands in `api/workspace.txt` and `api/envfile.txt`
through `make api-update`.

`workspace` delta:

- `var ErrSecretPath = errors.New("workspace: path is a secret path")`
- `Options` gains one field: `Deny *secretpath.Matcher`

`Options`, `Validate`, and `OpenWith` are not part of this delta.
Change one of `docs/plans/workspace.md` lands all three, because the
read bound needs an open-time option. This change adds the third
`Options` field and the sentinel only.

`ErrSecretPath` wraps the same way `ErrEscape` does. The name check
uses `fmt.Errorf("%w: %s", ErrSecretPath, path)`, matching
`workspace/confine.go:29`. It echoes the caller's raw path, not the
resolved one, so the two sentinels read alike.

The symlink walk uses the same sentinel and a different text:
`fmt.Errorf("%w: %s: symlink component", ErrSecretPath, path)`. One
message for both refusals would lie, because a permitted link to a
permitted file is a symlink and not a secret path.
`docs/plans/workspace.md` states why a second sentinel was rejected.

`envfile` delta:

- `func LoadBytes(data []byte) (map[string]string, error)`

`secretpath` delta: no exported-surface change, so `api/secretpath.txt`
does not move. The package does change: `Matches` gains a nil-receiver
guard. A behavior change with no lock diff needs a test instead of a
lock, and one is listed under "Tests".

`policy/layers.json` rows, exact:

- `"workspace": ["secretpath"]`
- `"envfile": []`, unchanged
- `"secretpath": []`, unchanged

Reasoning behind the shape:

- `Options` plus `Validate` follows the repo's existing option
  pattern. Its invariants live in `Validate`, not in a comment. This
  change adds no invariant: `Deny` may be nil.
- `Deny` is a `*secretpath.Matcher`, not a `[]string`. Compiling a
  pattern list can fail, and `Open` should not own that failure. The
  caller compiles once with `secretpath.NewMatcher` and handles the
  bad pattern where it wrote it.
- `LoadBytes` takes bytes, not an `fs.FS` and not a reader. Bytes are
  what `workspace.ReadFile` returns, and the dotenv parser already
  reads the whole file at once.

## Tests

All new `workspace` cases live in
`workspace/workspace_test/secret_test.go`. Each one below names what
it kills.

- `TestDenyAcrossMethods`, table-driven over the four methods that
  hold the check: `ReadFileLimit`, `WriteFile`, `List`, and `Stat`.
  The table drives `ReadFileLimit` directly, not only through
  `ReadFile`, because `ReadFileLimit` is where the check lives. One
  extra row calls `ReadFile` and asserts the same result, pinning the
  inheritance. A `Deny` matcher for `.env` and `secrets/` denies
  `.env`, `secrets/api.json`, and `secrets`. It permits `notes.txt`
  and `data/notes.txt`. Every denial matches `ErrSecretPath` under
  `errors.Is`. Kills: a check missing from any one of the four sites.
- `TestDeniedWriteTouchesNothing`: a denied `WriteFile` leaves the
  target file unchanged on disk and creates no parent directory.
  Kills: a check placed after `os.Root.MkdirAll`.
- `TestEscapeBeatsDeny`: a lexically escaping path that also matches
  the matcher, such as `../.env`, returns `ErrEscape`, not
  `ErrSecretPath`. Kills: a check placed before `resolve`.
- `TestDeniedNameLinkOutOfRoot`: a link inside the root named `.env`
  points at a file outside the root. `ReadFile(".env")` returns
  `ErrSecretPath`, not `ErrEscape`. Kills: a check placed after the
  `os.Root` call.
- `TestPermittedNameLinkOutOfRoot`, two rows on one fixture: a link
  inside the root named `notes.txt` points at a file outside the root.
  With the `Deny` matcher set, `ReadFile("notes.txt")` returns
  `ErrSecretPath`, because the walk refuses the link before `os.Root`
  sees it. With a nil `Deny`, the same call returns `ErrEscape`. The
  pair pins both the walk's precedence over `ErrEscape` and the
  unchanged no-policy behavior.
- `TestAbsoluteCallerPathDenied`:
  `ReadFile(filepath.Join(w.Root(), "secrets", "api.json"))` returns
  `ErrSecretPath`. `resolve` returns `secrets/api.json`, which the
  pattern matches; the raw caller string carries the root's absolute
  prefix, which it does not. Kills: passing the raw string to
  `Matches` instead of `resolve`'s output. This is the one vector that
  separates the two candidate inputs.
- `TestNilDenyPermitsEverything`: a nil `Deny` permits every path,
  through all four methods, proving `Open` keeps its old behavior and
  that no `Lstat` walk runs. Kills: a walk that runs unconditionally.
- `TestOptionsValidate`: blank `Root` fails; set `Root` with nil
  `Deny` passes; set `Root` with a compiled matcher passes.

Symlink-walk cases, all in the same file, all skipped on Windows where
symlink creation needs a privilege. Each one is `ErrSecretPath` under
the walk; each one was a silent success before it.

- `TestFileSymlinkToDeniedFile`: `Deny` is `secrets/`. Create
  `secrets/key.pem` and a file symlink `innocent.txt` pointing at it.
  `ReadFile("innocent.txt")` returns `ErrSecretPath`. Kills: a walk
  that skips the final component. This case read the secret with a nil
  error before the walk.
- `TestDirectorySymlinkToDeniedDirectory`: `Deny` is `secrets/`.
  Create a directory symlink `sdir` pointing at `secrets`.
  `ReadFile("sdir/key.pem")` returns `ErrSecretPath`. Kills: a walk
  that tests the final component only. This is the case that falsified
  the earlier plan's "deny the directory, not the file" advice, and
  not pinning it is how the wrong sentence survived.
- `TestWriteThroughSymlinkDenied`: with the same `innocent.txt` link,
  `WriteFile("innocent.txt", data)` returns `ErrSecretPath`, and
  `secrets/key.pem` still holds its original bytes. Kills: a walk
  wired into the read path only. Before the walk this write
  overwrote the secret through the link.
- `TestDeniedAndMissing`: `ReadFile(".env")` on a workspace with a
  `.env` deny pattern and no `.env` on disk returns `ErrSecretPath`,
  and does not match `fs.ErrNotExist`. Kills: a check placed after the
  open. It also pins the property that a denial leaks the policy and
  never the filesystem.
- `TestSymlinkPermittedWithoutDeny`: the same `innocent.txt` fixture
  on a workspace with a nil `Deny` reads the target. Kills: a walk
  that ignores the nil test, and pins that no existing caller changes.
- `TestDanglingSymlinkDenied`, two rows on one fixture: a symlink
  `dangle` inside the root points at a missing in-root name. With
  `Deny` set, `ReadFile("dangle")` returns `ErrSecretPath`. With a nil
  `Deny`, it returns an error matching `fs.ErrNotExist`. Kills: a walk
  that treats a broken link as absent. It reaches the same shape as
  `TestDeniedAndMissing` through a different mechanism, so both stay.
- `TestWalkErrorNamesSymlink`: the error from
  `ReadFile("innocent.txt")` matches `ErrSecretPath` and its text
  contains `symlink component`, while the error from
  `ReadFile(".env")` matches `ErrSecretPath` and its text does not.
  Kills: one shared message for the two refusals, which misattributes
  the walk's refusal to the deny patterns.
- `secret_integration_test.go`: the composed path end to end. Open a
  workspace with a `Deny` matcher for `secrets/`. Write
  `config/app.env` through the workspace. Read it back through
  `ReadFile`. Parse it with `envfile.LoadBytes` and assert the key
  and value. Then assert `ReadFile("secrets/prod.env")` returns
  `ErrSecretPath`, and assert the returned error text contains no
  value from the parsed file.

`envfile/envfile_test/`:

- `load_test.go` gains `LoadBytes` cases. The existing table moves to
  drive `LoadBytes` directly, and `Load` keeps one case proving it
  reads the file and delegates. The missing-file case stays on
  `Load` and still matches `os.ErrNotExist`.
- A `LoadBytes(nil)` case returns an empty map and no error.
- The existing error-message case still asserts no error text
  contains a parsed value, now driven through `LoadBytes`.

`secretpath/secretpath_test/`:

- One new row in the existing `Matches` table, or one small test
  beside it: `var m *secretpath.Matcher` and
  `m.Matches("secrets/key.pem")` returns false and does not panic.
  Kills: dropping the nil guard. Without the guard this row panics on
  the `m.patterns` read.

## Verification

- `make verify` passes: gofmt, vet, tests under the race detector,
  the doc gate, the structure gate, the plan gate, the deps gate,
  the API gate, the Semgrep scan, and the probe suite.
- Coverage for `workspace`, `envfile`, and `secretpath` each reach the
  85 percent floor, and the total floor holds.
- `make api-update` runs, and the `api/workspace.txt` and
  `api/envfile.txt` diffs land in the same change as the code. The
  lock delta is exactly one new `workspace` symbol, one new
  `Options` field line, and the one `envfile` symbol listed under
  "API". `api_surface` prints one line per exported struct field, so
  the `Deny` field shows as its own line. See `api/dispatch.txt` for
  that rendering. `api/secretpath.txt` does not change.
- The `ErrSecretPath` doc comment names three things: the name check,
  the symlink-component refusal, and the residual race. The race
  sentence says that a concurrent writer inside the root can swap a
  component between the check and the open, and that `os.Root` still
  confines the target to the root. A limit no caller reads is not
  documented, so it lives in the doc comment and in
  `docs/packages/workspace.md`, not only in this plan.
- `docs/packages/workspace.md` also gains the case-insensitive and
  Unicode-normalizing filesystem limit, under residual risk.
- `docs/packages/secretpath.md` states that `Matches` on a nil
  `*Matcher` returns false.
- `docs/packages/envfile.md` gains a `LoadBytes` entry under
  "Functions", and it lands in the same commit. The entry states that
  `Load` reads the file and delegates to `LoadBytes`, and that
  `LoadBytes(nil)` returns an empty map. The doc's surface list must
  match `api/envfile.txt`.
- `docs/plans/envfile.md` says `Load` is the only exported symbol.
  That sentence becomes false, so the same commit corrects it. The
  plan gate checks section presence, not truth.
- This change is what falsifies three more documents, so the same
  commit corrects all three. `docs/packages/workspace.md` claims
  `workspace` is a leaf with no internal import edge; replace that
  with the `secretpath` edge, and add `Close`, `OpenWith`, `Options`,
  and `ErrSecretPath` to its surface list.
  `docs/packages/secretpath.md` says `secretpath` has no caller
  inside this module; its "Cross-references" section names
  `workspace` as the caller instead.
- `AGENTS.md` is the contract every agent reads first, so its layout
  list must not carry a stale edge. The `workspace` entry drops "A
  leaf package; no internal imports", states that it imports
  `secretpath`, and lists `Close`, `OpenWith`, `Options`, and
  `ErrSecretPath`. The `envfile` entry names `LoadBytes` beside
  `Load`.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass against the `"workspace": ["secretpath"]` row.
- No third-party import is added. Both packages stay stdlib-only.
- This change adds no gate and weakens none.

## Build order

1. The `workspace` confinement, file-mode, and read-bound change,
   from `docs/plans/workspace.md`. **Shipped**, in commits `f3adff3`,
   `3bf7496`, `78e1934`, and `f8ec0fb`. It landed its own
   `api/workspace.txt` diff.
2. This change. It adds one line to each of the four method bodies,
   adds `denied` to `confine.go`, adds the nil guard to
   `secretpath.Matches`, and lands the second `api/workspace.txt` diff
   plus the `api/envfile.txt` diff.

Neither change depends on phase 69 or phase 70. Neither touches
`agentloop`, `tools`, `subagent`, or `spool`. Both may land before,
during, or after phase 69.

## Phase 71 owns the tool surface

`workspace`, `envfile`, `secretpath`, and `diff` still have no
non-test consumer after this change, and this change does not claim
to fix that. It makes the four safe to expose. Phase 71 exposes them.

Phase 71's plan file, `docs/plans/agents/phase71_filetools.md`, is
now the source of truth for its contract; the corrected terms live
there and in `docs/plans/subagent.md`'s "File tools addendum," not
here. The paragraph below records why the original terms changed,
now that real shipped code exists to check them against.

The original terms, written before phase 71's code shipped, listed
nine checkable items. Two needed correction against the code an
independent audit read line by line, both recorded in
`docs/plans/agents/phase71_filetools.md`:

- The phrase "one constructor" undercounted the shape phase 71
  actually needs. `subagent` ships five separate per-tool
  constructors, one per file, following its own established
  per-tool-file convention (`RoomTool`, `HeartbeatTool`, and the
  rest each get their own file). The enforcing constructor phase 71
  adds is a sixth symbol, `OpenFileTools`, that the five now depend
  on for their second argument; it is not a replacement for the
  five, and was never meant to be.
- The `envfile.LoadBytes` term is dropped, not met. `envfile` still
  gets no tool and no new caller. A parsed dotenv map is a
  credential-exfiltration primitive with no legitimate model-facing
  use this SDK's callers have named yet; adding a caller only to
  satisfy this contract line would repeat the exact "uncalled
  surface" problem this whole plan exists to fix. `envfile` stays a
  caller-side helper, wired before a subagent runs.

Every other original term stands, restated precisely in
`docs/plans/subagent.md`'s "File tools addendum": the mandatory
`Deny`, its `Validate` rejection of a nil `Deny`, the symlink-walk
consequence, `OpenWith` as the open path, `Close` ownership, and no
sixth optional interface added to `tools`.

If phase 71 does not land, the three symbols this change adds stay
uncalled, and the four packages keep test-only callers.
That is the accepted risk of this plan, recorded here on purpose.
