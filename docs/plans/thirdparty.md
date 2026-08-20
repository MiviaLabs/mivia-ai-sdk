# Plan: third-party exception policy

## Goal

One policy file and one gate own the third-party dependency exception
rule. Four sites own it today. The four sites drift, and one of them
fails open.

## Scope

Inside this change:

- `policy/thirdparty.json` — new. It declares the direct exceptions
  only.
- `policy/thirdparty_closure.txt` — new. A generated lock of the module
  closure. Never hand-edited.
- `scripts/check_thirdparty.py` — new gate with a `--probe` mode.
- `scripts/go_packages.py` — two new enumeration functions.
- `semgrep/sdk-standards.yml` — reshape `sdk.go.stdlib-only-imports`.
  Delete the five scoped rules.
- `scripts/check_semgrep_probes.py` — delete the five scoped probe
  pairs. Keep the fixtures a surviving rule needs.
- `scripts/check_gomod.py` — delete.
- `Makefile` — swap the gate invocation. Add the probe invocation. Add
  the `thirdparty-update` target.
- `AGENTS.md` — point the rule at `policy/thirdparty.json`.
- `docs/architecture.md` — rename the gate in the gate list.

Outside this change:

- `policy/layers.json`. That file owns internal import edges. This
  change adds no Go package, so it needs no row.
- `api/` locks. No Go symbol changes.
- Every Go package. No package code changes.
- `docs/plans/gates.md`. That file is history. Do not edit it.
- Five further plan files that record how their own change edited
  `ALLOWED_MODULES`: `docs/plans/a2aclient.md`,
  `docs/plans/a2aloopback.md`, `docs/plans/mcp.md`,
  `docs/plans/schema.md`, and `docs/plans/agents/convergence.md`. Each
  is history, like `docs/plans/gates.md`. Leave every one unedited.
  This plan supersedes their procedure.

### The four sites today

Each site was read and confirmed.

- `AGENTS.md:112-120` — the prose list of five exceptions.
- `scripts/check_gomod.py:28-81` — `ALLOWED_MODULES`, a set of 52
  module paths kept by hand.
- `semgrep/sdk-standards.yml:46-52` — the blanket rule plus its
  five-directory `exclude` list.
- `semgrep/sdk-standards.yml:57-125` — five scoped rules, one per
  excepted package.
- `scripts/check_semgrep_probes.py:159-235` — the probe pairs for the
  five scoped rules.

### The three defects

Each defect was reproduced.

- Fail-open hole. A sixth excepted package needs an `exclude` entry and
  a scoped rule. An `exclude` entry alone leaves the package with no
  per-package rule. The package may then import any module already in
  `ALLOWED_MODULES`, silently.
- The build-tag clause is unenforced. `AGENTS.md:116` allows
  `modernc.org/sqlite` in `ledger` behind the `ledger_sqlite` tag only.
  `sdk.go.ledger-scoped-third-party-import` is a regex over import
  strings. It has no build-tag concept, so the word "only" binds
  nothing today.
- The docstring drifts. `scripts/check_gomod.py:2-4` says "four
  deliberate exceptions" and omits `a2aloopback`, which `AGENTS.md:114`
  names.

`ALLOWED_MODULES` itself does not drift today. It equals the `go.sum`
module set exactly, in both directions. Its cost is hand maintenance,
not present error.

### Measured ground truth

Measured with `go list -json`, over the default build and the
`ledger_sqlite` build, reading `Imports`, `TestImports`, and
`XTestImports`:

| package | third-party modules | build |
| --- | --- | --- |
| `a2aclient` | `github.com/a2aproject/a2a-go`, `google.golang.org/grpc` | both |
| `a2aloopback` | `github.com/a2aproject/a2a-go`, `google.golang.org/grpc` | both |
| `mcp` | `github.com/modelcontextprotocol/go-sdk` | both |
| `schema` | `github.com/santhosh-tekuri/jsonschema/v6` | both |
| `ledger` | `modernc.org/sqlite` | tagged only |

No other package imports a third-party module. The direct `require`
lines in `go.mod` are exactly those five module paths. The new gate
reports zero problems on the current tree.

### The open question: keep the blanket Semgrep rule

Keep it, and delete its `exclude` list. Move the five module paths into
global `pattern-not-regex` allowances.

Reason: an `exclude` list is the fail-open hole. A path allowance
cannot be. With module allowances only, a new excepted package needs
one edit in `policy/thirdparty.json` and one allowance line. Both
drift directions fail closed:

- Allowance line added, policy row missing: `check_thirdparty.py`
  rejects the import, because a package with no row gets no module.
- Policy row added, allowance line missing: the blanket rule fires on
  the import.

A zero-allowance blanket rule was rejected. The excepted packages'
legitimate imports would fire it, and `AGENTS.md` forbids inline
suppression.

The reshaped rule was run against the tree. It reports zero findings.

Keeping the rule also keeps a cheap smoke test that needs no compiling
tree. The new gate needs `go list`, which needs a compiling tree.
`make verify-fast` runs `go vet` and `go test` before the gates, so a
broken tree already fails earlier.

### The reach a global allowance gives up

A global allowance costs reach that the old `exclude` list kept. The
old `exclude` globs are single-level, so the blanket rule still covered
a nested or a tag-hidden file. A global allowance covers no file for
those five module paths.

Reproduced. This fixture hides two imports from the new gate:

```text
mcp/hidden_windows_test.go            -> import _ "google.golang.org/grpc"
ledger/ledger_test/fixture.go         -> //go:build windows, import _ "modernc.org/sqlite"
```

`go_packages._candidate_dirs` returned `['ledger', 'mcp']`, and
`go_packages._constraints` returned no problem. `scripts/go_packages.py:158`
drops every `_test` directory and `scripts`, and line 174 skips every
`_test.go` file. Both `go list` passes reported no imports for either
file.

The gate therefore needs a second scan. See check two below. Without
it the change would weaken a gate, which `AGENTS.md` forbids.

### Why this is strictly stronger

Site by site:

- Per-package scope. The five regex rules match import strings in one
  directory glob. The gate reads resolved imports from `go list`, over
  every package at any depth, including external `_test` packages. It
  covers test imports, which `ALLOWED_MODULES` never attributed to a
  package.
- Unattributable files. The residual scan reads every `.go` file that
  no `go list` pass claims. That set includes tag-hidden files,
  underscore directories, and `testdata`. The old rules reached those
  files only by accident of glob depth.
- Build tag. Unenforced today. The gate runs one pass per build
  configuration and rejects a tagged module found in an untagged pass.
- Module closure. `ALLOWED_MODULES` is a hand-kept union of 52 paths.
  `policy/thirdparty_closure.txt` holds the same set, generated. A
  closure change becomes a reviewable diff instead of a hand edit.
- Directives. The gate keeps the `replace`, `exclude`, and `retract`
  rejection unchanged.

The closure lock is not optional. Direct-require equality plus
`go mod tidy -diff` pins only the five direct modules. A version bump
of `a2a-go` that pulls a new indirect module leaves both checks at exit
zero. `ALLOWED_MODULES` fails on that bump today. The lock restores
that behavior.

Nothing the four sites reject today becomes allowed.

## API

No Go symbols change. No `api/` lock changes.

### `policy/thirdparty.json`

It mirrors the `policy/layers.json` shape: one comment, one keyed
table. The key is the package path relative to the module root.

```json
{
  "comment": "Third-party import exceptions. The SDK is standard library only. A package absent from this table may import no third-party module. See docs/plans/thirdparty.md.",
  "exceptions": {
    "a2aclient": {
      "modules": ["github.com/a2aproject/a2a-go", "google.golang.org/grpc"],
      "tag": ""
    },
    "a2aloopback": {
      "modules": ["github.com/a2aproject/a2a-go", "google.golang.org/grpc"],
      "tag": ""
    },
    "mcp": {
      "modules": ["github.com/modelcontextprotocol/go-sdk"],
      "tag": ""
    },
    "ledger": {
      "modules": ["modernc.org/sqlite"],
      "tag": "ledger_sqlite"
    },
    "schema": {
      "modules": ["github.com/santhosh-tekuri/jsonschema/v6"],
      "tag": ""
    }
  }
}
```

Field rules:

- `modules` holds module paths. An import matches a module when the
  import path equals the module path or starts with the module path
  plus a slash.
- `tag` names the one build tag the exception needs. An empty string
  means the exception holds in every build.
- A `tag` value outside `go_packages.BUILD_TAGS` fails the gate.
- An unknown field or a missing field fails the gate.
- A key must name a package the enumeration reports. A key naming no
  package fails the gate, matching `scripts/check_deps.py:30`.
- An external test package is its own key. `foo/foo_test` does not
  inherit the row of `foo`, and needs its own row to import anything.
  No such row exists today, and none is needed.

### `policy/thirdparty_closure.txt`

One module path per line, sorted, newline terminated. It is generated,
never hand-edited. `make thirdparty-update` writes it from the `go.sum`
module set. The gate requires equality with that set. The file carries
no comment, so the diff stays readable.

### `scripts/go_packages.py`

Two new functions beside `packages`:

```python
def third_party_imports(root: Path, tags: str | None,
                        env_extra: dict | None = None) -> dict[str, set[str]]:

def attributed_go_files(root: Path, tags: str | None,
                        env_extra: dict | None = None) -> set[Path]:
```

`third_party_imports` maps every relative package path to its
third-party import paths, for one build configuration.

- It reuses `_go_list` and `_decode_stream`. Do not reimplement either.
- It keeps every package `go list ./...` reports. It does not drop
  `_test` packages, and it does not drop `scripts`. `_relative` drops
  both, so this function needs its own relative-path helper.
- It reads `Imports`, `TestImports`, and `XTestImports`.
- It treats an import as third-party when `go list std` does not hold
  it, it is not `C`, and it is not the module path or a path below it.

`attributed_go_files` returns the absolute paths of every file the same
`go list` pass claims. It reads `GoFiles`, `CgoFiles`, `TestGoFiles`,
and `XTestGoFiles`. The gate needs it for the residual scan.

Both sides of the subtraction must call `Path.resolve()`. `go list`
reports a fully resolved `Dir`. A walk rooted at a symbolic link or at
a relative path yields keys that never match, the subtraction removes
nothing, and every attributed file turns residual. The pass case below
catches that failure.

Both functions take one build configuration. The gate iterates the same
configuration list `packages` uses at `scripts/go_packages.py:191`:
`None`, then `",".join(BUILD_TAGS)` as one combined pass. Do not
iterate the tags one at a time. The two forms agree with one tag and
diverge with two.

### `scripts/check_thirdparty.py`

```
python3 scripts/check_thirdparty.py
python3 scripts/check_thirdparty.py --probe
```

The flag set matches `check_deps.py`, `check_plan.py`,
`check_api.py`, and `check_orphan_packages.py`. The gate exposes
`check(root, env_extra)` returning problem strings, and `run_probe()`
returning a bool, like `check_deps.py`.

The gate runs seven checks.

1. Per-package imports. For each build configuration, call
   `third_party_imports`. For each package and each third-party import,
   find the policy row. No row, or no matching module, is a problem. A
   row whose `tag` is not in the current configuration is a problem.
2. Residual files. Resolve the root once, with `Path.resolve()`. Walk
   it with `pathlib.Path.rglob`, not `os.walk`. For each `.go` file,
   compute `path.resolve().relative_to(root)`. Apply the dot test to
   that relative path only, never to the absolute path. Skip a path
   whose relative segments hold one starting with a dot. Keep a path
   whose relative segments hold one starting with an underscore,
   because those directories are the point. The rule mirrors
   `scripts/go_packages.py:156` minus its underscore clause. Subtract
   the union of `attributed_go_files` over every build configuration.
   Scan each remaining file as raw text. A quoted occurrence of any
   policy module path is a problem. The scan covers the union of every
   row's modules, not the row of the directory the file sits under.
3. Policy shape. Every row has exactly the `modules` and `tag` fields.
   Every `tag` is empty or a member of `go_packages.BUILD_TAGS`. Every
   key names a package that `third_party_imports` reports, unioned over
   both build configurations. Do not validate keys against
   `go_packages.packages`. That function drops `_test` packages and
   `scripts` at `scripts/go_packages.py:93-96`, so a valid external
   test package row could never pass. A package that neither
   configuration reports can hold no row, by design. It needs none:
   check two forbids every policy module path in a residual file
   outright, with no row to escape through.
4. Direct requires. The `require` lines in `go.mod` without an
   `// indirect` marker must equal the union of every `modules` list.
5. Closure lock. The `go.sum` module set must equal
   `policy/thirdparty_closure.txt`. A mismatch reports both differences
   and names `make thirdparty-update`.
6. Tidy identity. `go mod tidy -diff` must exit zero. On a non-zero
   exit, print the `go` stderr verbatim beside the gate's own message.
   A cold module cache and an untidy tree both exit non-zero, and the
   stderr is what separates them.
7. Directives. A `replace`, `exclude`, or `retract` directive in
   `go.mod` is a problem. This is `check_gomod.py`'s existing rule,
   moved unchanged.

The gate also calls `go_packages.packages(root, env_extra)` once. That
call runs the build-constraint scan, which fails closed on a file
behind an unlisted constraint. That scan covers non-test files in
enumerated directories only. Check two covers the rest.

Check two is complete over the walked set, with exactly one named
exclusion: dot-prefixed segments. That exclusion is required, not
cosmetic. Measured on the current worktree:

| set | count | kind |
| --- | --- | --- |
| every `.go` path under the root | 12002 | one worktree, environment-dependent |
| under a dot-prefixed directory | 11303 | one worktree, environment-dependent |
| dot-directory files quoting a policy module path | 351 | one worktree, environment-dependent |
| walked, after the dot exclusion | 699 | property of the tracked tree |
| residual, after subtraction | 3 | property of the tracked tree |

The first three rows count local agent worktrees and vary by machine.
No gate asserts any of these numbers.

The 351 hits are agent worktrees under `.claude/`. A walk without the
dot exclusion reports every one of them. The walked count equals
`git ls-files '*.go'`, but the gate must not use that command:
`.githooks/pre-commit:17-20` runs `make verify-fast` on a `git archive`
extraction that holds no `.git` directory.

The exclusion is not a weakening. Go itself never builds a
dot-directory, and Semgrep skips one by default, so no rule reaches
that region today either.

The relative-path wording is required, and a dot test on the absolute
path silently disables the whole scan. This repo holds 17 registered
worktrees under `.claude/worktrees/`, each a full checkout of about 694
Go files. `.agents/skills/delivery/SKILL.md:38,45,56` has the builder
and the reviewer run `make verify`, and line 57 names a throwaway
worktree. Run from such a root, every absolute path holds a dot
segment, so an absolute-path test skips every file. The gate then
reports zero problems while enforcing nothing.

Measured on one of those worktrees: the absolute path holds a dot
segment, the path relative to the root does not.

`os.walk` carries the same hazard by another route. Its first `dirpath`
is the string `"."`, whose first segment is a dot. Use `pathlib`, whose
`Path("./foo.go").parts` is `("foo.go",)`.

The three residual files are `docs/examples/_agentcomposition/main.go`,
`docs/examples/_agentcompositionsqlite/main.go`, and
`docs/examples/_agentrun/main.go`. None of them hits.

A residual file may not name a policy module path anywhere, including
inside a comment or a plain string. Check two reads raw text and has no
escape hatch. A doc example that needs such an import must move under a
directory `go list` attributes.

### `semgrep/sdk-standards.yml`

`sdk.go.stdlib-only-imports` keeps its id, message, and
`pattern-regex`. Its `paths.exclude` list goes away. It gains one
`pattern-not-regex` line per module path in `policy/thirdparty.json`,
beside the existing module-self allowance. The five
`sdk.go.<pkg>-scoped-third-party-import` rules are deleted.
`sdk.go.no-a2aloopback-import` at `semgrep/sdk-standards.yml:81` is a
different rule. Leave it alone.

## Tests

Probes only, following the `--probe` convention of `check_deps.py`.
Each fixture is a throwaway module built with
`go_packages.probe_env()`, so `go list` stays off the network.

Adversarial cases, each of which must fail the gate:

- Wrong package. A package with a policy row imports a module its row
  does not list. Use `mcp` importing `modernc.org/sqlite`.
- Unscoped package. A package with no policy row imports a module that
  another row lists. This is the fail-open case. The old design allowed
  it.
- Excepted-but-unscoped package. A package that the deleted `exclude`
  list would have covered, with no policy row, importing its module.
  This pins the fail-open hole shut.
- Missing tag. A package imports its tag-gated module from a file with
  no build tag. The untagged pass must report it.
- Foreign module in `go.mod`. A `require` line for a module path
  outside the union of the `modules` lists.
- Closure drift. A `go.sum` module path absent from
  `policy/thirdparty_closure.txt`.
- Closure drift, other direction. A lock line whose module path is
  absent from `go.sum`.
- Untidy state. A stray `require` line makes `go mod tidy -diff` exit
  non-zero. Confirmed against a copy of this repo.
- A `replace` directive. Repeat for `exclude` and `retract`.
- Bad policy row. A `tag` value outside `go_packages.BUILD_TAGS`.
- Bad policy row. An unknown field name.
- Stale policy row. A key naming no enumerated package.

Residual-scan cases, one probe per file class. Each hides an import of
a policy module path, and each must fail the gate:

- A `_test.go` file behind a filename constraint, as in
  `mcp/hidden_windows_test.go`.
- A file in an external `_test` directory behind a build tag, as in
  `ledger/ledger_test/fixture.go`.
- A file under an underscore-prefixed directory, as in
  `docs/examples/_agentcomposition/`.
- A file under `testdata/`.
- A file under `scripts/`, behind a `//go:build windows` constraint.
  The constraint is required. `scripts` is a real package in
  `go list ./...`, so a plain `scripts/*.go` file is attributed and is
  never residual. Without the constraint the fixture passes without
  reaching the residual branch.
- A root path that itself holds a dot segment. Build the fixture module
  at `<tmp>/.agentwork/<module>` and plant a residual violation inside
  it. Correct code reports the problem. An implementation that dot-tests
  the absolute path reports nothing.

That last case is the one control that separates a working residual
scan from a disabled one. Every other fixture sits under
`tempfile.TemporaryDirectory`, which holds no dot segment, so an
absolute-path implementation passes the rest of the suite. Do not drop
it.

Each residual fixture must sit in a directory `_candidate_dirs` skips.
The gate calls `go_packages.packages`, which calls `sys.exit(1)` on a
constrained file it can see. A fixture at `room/hidden_windows.go`
would exit the process before check two runs. The probe would then fail
for the wrong reason. The five classes above are all safe:
`_candidate_dirs` skips `scripts`, `testdata`, underscore directories,
`_test` directories, and `_test.go` files.

Cases that must pass:

- Tagged import in the scoped package. The tagged pass allows
  `modernc.org/sqlite` in `ledger`.
- Sub-path import. `github.com/a2aproject/a2a-go/a2asrv/eventqueue`
  matches the `github.com/a2aproject/a2a-go` module entry.
- A standard library import is never third-party.
- An import of this module is never third-party.
- An attributed file is never residual, so a legitimate import never
  fires check two. This case also catches a broken path
  normalization, which turns every attributed file residual.
- A dot-prefixed directory holding a policy module path is skipped.
  Build the fixture under `.worktree/a2aclient/grpc.go`, mirroring the
  351 real files under `.claude/`.
- The real tree reports zero problems.

Test-package reach, both directions:

- A third-party import inside an external `_test` package directory is
  seen, and needs that directory's own policy row.
- A third-party import inside an in-package `_test.go` file is seen
  through `TestImports`.

Semgrep probe changes in `scripts/check_semgrep_probes.py`:

- Delete the five scoped-rule probe pairs and their fixture writes.
- Keep `a2aclient_dir` and the file
  `clean_a2aloopback_caller_import_test.go` written into it at
  `scripts/check_semgrep_probes.py:203-205`. That file is the clean
  fixture of `sdk.go.no-a2aloopback-import`, which survives this
  change. Its directory is created by the a2aclient scoped block at
  line 165, so the block's deletion must not take the directory with
  it. The name must keep its `_test.go` suffix and its `a2aclient/`
  parent, or the rule's `exclude` stops matching.
- Delete every assertion that the blanket rule stays silent inside an
  excluded directory. Those directories are no longer excluded.
- Add a pair proving the blanket rule stays silent on an allowed module
  path and fires on any other domain-shaped import, in the same
  directory.
- Register every kept or added fixture in `expected`. An unregistered
  fixture that fires trips the unlisted-probe assertion at
  `scripts/check_semgrep_probes.py:400`. Every basename must stay
  unique across the whole fixture tree, or the collision check at
  lines 245-254 raises before Semgrep runs.

## Verification

Commands that must pass:

- `python3 scripts/check_thirdparty.py`
- `python3 scripts/check_thirdparty.py --probe`
- `python3 scripts/check_plan.py`
- `python3 scripts/check_deps.py`
- `python3 scripts/check_prose.py`
- `python3 scripts/check_labels.py`
- `python3 scripts/check_semgrep_probes.py`
- `make verify`

Gate wiring:

- In `verify-fast`, replace the `python3 scripts/check_gomod.py` line
  with `python3 scripts/check_thirdparty.py`.
- In `verify`, add `python3 scripts/check_thirdparty.py --probe` beside
  the other `--probe` lines.
- Add a `thirdparty-update` target. It writes
  `policy/thirdparty_closure.txt` from the `go.sum` module set. It
  mirrors `api-update`: generate the lock, then commit its diff in the
  same change.

Docs to update in the same change:

- `AGENTS.md:112-120`. Replace the exception list with one sentence
  naming `policy/thirdparty.json` as the source of truth.
- `AGENTS.md` enforcement ladder. Add two entries. The first: do not
  import a third-party module unless `policy/thirdparty.json` grants
  the package that module. Name the gate.
- The second ladder entry mirrors the wording of the api-lock entry.
  Do not change the dependency closure without a deliberate lock
  update: `make thirdparty-update`, then commit the
  `policy/thirdparty_closure.txt` diff in the same change. The lock is
  generated and never hand-edited. Its diff is the review surface for
  a new indirect module. Name the gate.
- `docs/architecture.md:903`. Replace `check_gomod` with
  `check_thirdparty`.
- `docs/plans/third-party-per-package.md`. Marked superseded by this
  plan.
- `docs/plans/nested-package-visibility.md:30-31`. Points at this plan.

Commit staging. Land `docs/architecture.md` in its own commit, before
or after the rest. That file is the only non-companion, non-infra file
in the change. `_is_doc_companion` at
`scripts/test_tampering_rules_infra.py:30` exempts `AGENTS.md` and
`docs/plans/`, and `_is_gate_infra` at line 24 covers the `Makefile`,
`scripts/`, `semgrep/`, and `policy/`. With that split, the
self-reference finding stays silent.

The main commit still removes a gate invocation line from
`verify-fast`. `_recipe_invocations` at
`scripts/test_tampering_rules_infra.py:93` takes a set difference, so
the added replacement does not cancel the removal. That commit needs
one `Allow-Gate-Change` trailer with a real reason. See
`docs/plans/test-tampering.md`, "Detections and finding IDs". Do not
bypass the hook.
