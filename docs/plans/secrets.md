# secrets plan

## Goal

Connect the three secret-handling pieces into one path a caller can
take. `secretpath` decides which paths hold secrets. `workspace`
enforces that decision at the filesystem boundary. `envfile` parses
the bytes `workspace` hands back. Today the three ship as leaves with
no non-test consumer anywhere in the module.

This is a composition change. It adds one import edge, one `Options`
field, one sentinel, and one `envfile` function. It builds no tool
wrapper.

## Scope

Inside:

- `workspace` consults an optional `*secretpath.Matcher` before every
  filesystem call. `ReadFile`, `ReadFileLimit`, `WriteFile`, `List`,
  and `Stat` each check the cleaned root-relative path against the
  matcher and return `ErrSecretPath` on a match. The surface is the
  new `Options.Deny` field and `ErrSecretPath`.
  `docs/plans/workspace.md` holds the full `workspace` surface, and
  its change one already lands `Options`, `Validate`, and `OpenWith`
  for the read bound.
- The deny check runs before the read bound, so a denied path over
  the bound reports `ErrSecretPath`. See `docs/plans/workspace.md`.
- The denial runs after the lexical confinement check and before the
  `os.Root` call. That order has two consequences, and this plan
  states both.
- A path that lexically escapes the root reports `ErrEscape`. The
  lexical check runs first, so the matcher never sees such a path.
- A root-local symlink whose own name is denied reports
  `ErrSecretPath`, even when the link target lies outside the root.
  The name check runs before `os.Root` resolves the link. An escaping
  path can therefore report `ErrSecretPath`.
- A symlink with a permitted name that resolves outside the root
  reports `ErrEscape`. It passes the matcher and fails inside
  `os.Root`.
- A nil `Deny` matcher denies nothing. Each method tests its `deny`
  field for nil before it calls `Matches`. The nil test is required,
  not stylistic: `secretpath.Matcher.Matches` reads `m.patterns`, so a
  call on a nil `*Matcher` panics, and the Semgrep rule
  `sdk.go.no-panic-in-packages` bans a panic in a package.
  `Open(root)` leaves `deny` nil, so no existing caller changes.
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
- A non-test consumer for the four packages. State this plainly:
  after this change, `workspace`, `envfile`, `secretpath`, and `diff`
  still have no caller inside the module. The five symbols this
  change adds ship ahead of their caller.
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

Residual risk, stated plainly:

- The `Deny` matcher is a name policy, not a content policy. A file
  named `notes.txt` holding an API key is not denied.
- A symlink inside the root that points at a denied in-root path
  bypasses the name check, because `os.Root` follows an in-root
  symlink and the matcher only sees the name the caller typed. A
  caller that needs content-level assurance denies the directory,
  not the file.
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

`envfile` delta:

- `func LoadBytes(data []byte) (map[string]string, error)`

`secretpath` delta: none. The package is used as it is.

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

`workspace/workspace_test/`:

- `secret_test.go`, table-driven over the four methods. A `Deny`
  matcher for `.env` and `secrets/` denies `.env`,
  `secrets/api.json`, and `secrets`. It permits `notes.txt` and
  `data/notes.txt`. Every denial matches `ErrSecretPath` under
  `errors.Is`.
- A denied `WriteFile` leaves the target file unchanged on disk, and
  creates no parent directory.
- A lexically escaping path that also matches the matcher, such as
  `../.env`, returns `ErrEscape`, not `ErrSecretPath`.
- A symlink case, using `os.Symlink`. A link inside the root named
  `.env` points at a file outside the root. `ReadFile(".env")`
  returns `ErrSecretPath`, not `ErrEscape`, pinning the stated order.
- A second symlink case. A link inside the root named `notes.txt`
  points at a file outside the root. `ReadFile("notes.txt")` returns
  `ErrEscape`, because the matcher permits the name.
- An absolute caller path inside the root:
  `ReadFile(filepath.Join(w.Root(), "secrets", "api.json"))` returns
  `ErrSecretPath`. This is the one vector that separates the two
  candidate inputs. `resolve` returns `secrets/api.json`, which the
  pattern matches. The raw caller string starts with the root's
  absolute prefix, which the pattern does not match. A builder who
  passes the raw string to `Matches` fails only this case, so it is
  the pin for "the matcher sees `resolve`'s output".
- A nil `Deny` permits every path, proving `Open` keeps its old
  behavior. The same case calls all four methods, proving no method
  calls `Matches` on a nil `*Matcher`.
- `Options.Validate`: blank `Root` fails; set `Root` with nil `Deny`
  passes; set `Root` with a compiled matcher passes.
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

`secretpath/secretpath_test/`: no change. The package's behavior is
unchanged.

## Verification

- `make verify` passes: gofmt, vet, tests under the race detector,
  the doc gate, the structure gate, the plan gate, the deps gate,
  the API gate, the Semgrep scan, and the probe suite.
- Coverage for `workspace` and `envfile` each reach the 85 percent
  floor, and the total floor holds.
- `make api-update` runs, and the `api/workspace.txt` and
  `api/envfile.txt` diffs land in the same change as the code. The
  lock delta is exactly one new `workspace` symbol, one new
  `Options` field line, and the one `envfile` symbol listed under
  "API". `api_surface` prints one line per exported struct field, so
  the `Deny` field shows as its own line. See `api/dispatch.txt` for
  that rendering.
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
   from `docs/plans/workspace.md`. It rewrites every method body and
   adds `Options`, so it goes first and lands its own
   `api/workspace.txt` diff.
2. This change. It edits the same method bodies to add one check
   each, and lands the second `api/workspace.txt` diff plus the
   `api/envfile.txt` diff.

Neither change depends on phase 69 or phase 70. Neither touches
`agentloop`, `tools`, `subagent`, or `spool`. Both may land before,
during, or after phase 69.

## Phase 71 owns the tool surface

`workspace`, `envfile`, `secretpath`, and `diff` still have no
non-test consumer after this change, and this change does not claim
to fix that. It makes the four safe to expose. Phase 71 exposes them.

Phase 71 has no plan file yet. The terms below are its contract. Each
term is checkable in the phase-71 plan review. No term is checkable
in `make verify`. `scripts/check_plan.py` gates `docs/plans/<pkg>.md`
for a directory holding `*.go`. `docs/plans/agents/` is outside that
set.

- Its plan file is `docs/plans/agents/phase71_filetools.md`, and it
  carries all five `TEMPLATE.md` sections.
- It wraps `workspace` and `diff` as `tools.Tool` values in
  `subagent`, the module's one "blocks as tools" package. It adds no
  new top-level package for the wrapping.
- Its `api/subagent.txt` diff adds one constructor that returns a
  `tools.Tool`, whose options carry a `*secretpath.Matcher`. Its tool
  maps `workspace.ErrSecretPath` onto a tool error. Phase 71 also
  calls `envfile.LoadBytes` on bytes that `workspace.ReadFile`
  returned. Naming the symbols is what checks that each new symbol
  got a caller. A constructor taking `*workspace.Workspace` alone
  checks nothing, because `api/workspace.txt` already lists that type.
- Every file tool it adds implements `tools.SchemaTool` from phase
  69, so `agentloop.Definitions` offers the tool to the model. This
  one term depends on phase 69. It is unenforceable if phase 69 never
  lands; the other terms stand on their own.
- It owns the enforcing constructor for the secret policy. Its file
  tool options carry a `Deny *secretpath.Matcher`, and their
  `Validate` returns an error on a nil `Deny`. A phase-71 test
  asserts that rejection.
- This plan does not enforce that rule, and does not state it as one.
  `Options.Validate` here accepts a nil `Deny` on purpose, because
  `Open` must keep its current behavior. The "no secret policy"
  restriction is a phase-71 caller-side invariant, enforced by the
  phase-71 `Validate` named above.
- It builds its workspace through `workspace.OpenWith`.
- It calls `Registry.RunScoped`, never `Registry.Run`, matching the
  rule phase 69 already states for a model-chosen call.
- It closes the workspace it opens. `Workspace.Close` releases a file
  descriptor.
- It adds no fifth optional interface to `tools`. `spool`'s
  forwarding switch already reaches sixteen cases with
  `tools.SchemaTool`, and fixing that explosion is phase 70's work,
  not phase 71's.

If phase 71 does not land, the five symbols this change adds stay
uncalled, and the four packages stay leaves with test-only callers.
That is the accepted risk of this plan, recorded here on purpose.
