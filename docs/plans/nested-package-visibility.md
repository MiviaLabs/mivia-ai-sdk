# Plan: nested package visibility

## Goal

Make every structural gate see a package at any depth, not only at the
repo root. Today a package at `foo/bar/` is invisible to the deps,
plan, orphan, and API gates. This is a latent trapdoor, not a live
breach: `go list ./...` shows every non-test package in the tree is
top-level right now.

## Scope

Inside this change:

- `scripts/go_packages.py` — a new shared enumeration helper.
- `scripts/check_deps.py` — recursive enumeration, real import lists.
- `scripts/check_plan.py` — recursive enumeration.
- `scripts/check_orphan_packages.py` — recursive enumeration.
- `scripts/api_surface.go` — recursive package discovery.
- `scripts/check_api.py` — nested lock paths.
- `Makefile` — the `api-update` recipe and the new probe invocations.
- `AGENTS.md` — the matching enforcement-ladder entries.

Outside this change:

- No new Go package. No Go package code edit at all.
- No change to `policy/layers.json`. Every current row is a flat key
  with flat values, and nested keying needs no edit to reach them.
- No change to what a gate enforces for an existing flat package.
- No per-package third-party import rule. See
  `docs/plans/thirdparty.md`.
- No `scripts/check_mutation.py` change. See
  `docs/plans/mutation-nested-packages.md`.
- No change to `semgrep/`, `.githooks/`, or the coverage floor block.
- `docs/plans/gates.md` is history; do not edit it.
- No hardening of the surface stream against a fake `package ` header.
  An exported `const` holding a raw string with a newline can inject
  one, and the lock path follows the injected text. The former awk
  recipe had the same hole, so this change adds no new class. The
  attack needs a hostile committed Go file, which already implies code
  execution through `go run`. It needs its own plan.

### Confirmed faults

- `scripts/check_deps.py:12` — the import regex character class is
  `[a-z0-9_-]+` and a closing quote must follow. A nested import path
  matches nothing. Reproduced: the regex returns `[]` for
  `"github.com/MiviaLabs/mivia-ai-sdk/flow/engine"`.
- `scripts/check_deps.py:15` — `package_dirs` reads `root.iterdir()`
  and globs `*.go` at one level only.
- `scripts/check_plan.py:16` — the same top-level-only walk.
- `scripts/check_orphan_packages.py:16` — imports `IMPORT` and
  `package_dirs`, so it inherits both faults.
- `scripts/api_surface.go:51` — `os.ReadDir(".")` is one level only.
- The regex reads raw file text, so a module path inside a comment or
  a string literal counts as an import edge today. Reproduced: the
  regex returns `flow` for a comment line naming that path.

### Scripts that are already correct

- `check_docs.py`, `check_structure.py`, `check_names.py` use a
  recursive walk.
- `check_prose.py` walks `docs/` recursively.
- `check_labels.py` walks the tree recursively.
- The Makefile coverage block enumerates with `go list ./...`.
- The Semgrep scan reads the whole tree.

### One enumerator: go list

Every gate in this change enumerates through `go list -json`. No gate
enumerates through a filesystem walk. The constraint scan below walks
the tree to choose files, never to enumerate packages. Reasons:

- Go decides what a package is. A walk must copy Go's rules for
  `_`-prefixed directories and `testdata/`. This repo already relies
  on that rule: `docs/examples/_agentcomposition/main.go` is a real Go
  file that `go list` correctly ignores.
- A walk cannot resolve build constraints without a constraint parser.
  `ledger/sqlite_store.go` sits behind `ledger_sqlite`.
- `go list` returns resolved import lists. A path in a comment or a
  string literal is never an edge.
- `go list` separates `Imports` from `TestImports` and `XTestImports`.
  That matches the existing rule that test files are exempt.

Two enumerators would drift silently. `check_api.py` would then report
a missing or an orphan lock that `check_deps.py` disagrees with, and
no gate would catch the disagreement.

The Go toolchain is already a hard dependency of the Python gates:
`check_api.py:12` shells out to `go run`.

### The exclusion set

`go list -json ./...` already drops these:

- Any path segment that begins with `_` or `.`.
- Any `testdata` directory.
- Any file excluded by a build constraint for the requested tags.

Each caller then drops two more:

- `github.com/MiviaLabs/mivia-ai-sdk/scripts`. It is gate tooling.
- Any package whose relative path ends in `_test`. Those are the
  external test packages, and the coverage block drops them the same
  way.

A naive recursive `os.ReadDir` in `api_surface.go` would break the
tree. It would reach `docs/examples/_agentcomposition/main.go`, and
`surface()` at `scripts/api_surface.go:114` would hard-error with
"package main does not match directory name _agentcomposition".
`check_api.py` would exit 1 and `make verify` would fail.
`docs/examples/_agentcompositionsqlite/main.go` fails the same way,
and `build.Default.BuildTags` at line 30 guarantees no tag skip. Using
`go list` avoids that class of fault entirely.

The one-package-per-directory check in `surface()` stays. A future
directory holding `package main` under a normal name is a real error,
and the tool must name it, not crash.

The check's two sub-cases at `scripts/api_surface.go:109-111` do not
have equal weight after this change:

- The `len(pkgs) > 1` sub-case becomes unreachable, provided
  `api_surface.go` aborts when either `go list` run fails. An untagged
  package-clause clash fails both runs; a tag-split clash fails only
  the tagged run.
- The zero-package sub-case stays live. It guards the tag mismatch
  described under "Nested artifact paths". A directory whose files are
  all gated `//go:build !ledger_sqlite` appears in the default-tag run
  but matches no file under `build.Default.BuildTags`.

### scripts/go_packages.py

One new helper module, stdlib only. It exposes:

- `packages(root)` — a mapping from relative package path to its
  resolved internal imports.
- `package_paths(root)` — the sorted relative paths.

Rules the helper applies:

- Run `go list -json ./...` twice: once with default tags and once
  with `-tags ledger_sqlite`. Take the union of the imports per
  package.
- Apply the exclusion set above.
- Key a package by its relative import path: `flow` stays `flow`, and
  a nested package becomes `flow/engine`.
- Cache the subprocess result per process. Each gate calls it once.
- Exit non-zero with the `go list` stderr when `go list` fails. Never
  treat a toolchain failure as an empty package set.
- Scan every non-test `.go` file of every directory a filesystem walk
  finds. Exit non-zero and name the file when it carries a build tag
  outside `BUILD_TAGS`, or a `_GOOS`/`_GOARCH` filename suffix. The
  walk applies the same exclusion set. Reason below.

Why the union, stated honestly: the union is a superset of the
imports across both configurations. A union cannot model file removal;
a file gated `//go:build !ledger_sqlite` keeps its imports in both
runs. The superset is the right trade for each caller:

- `check_deps.py` becomes strict. It validates every edge that an
  enumerated configuration produces.
- `check_orphan_packages.py` becomes lenient. An importer that exists
  in one configuration only still counts as a real caller.

That leniency is deliberate and small. Only `ledger_sqlite` appears in
the tree, and no negated form of it exists, so no live case differs.

The enumerated configurations are the default one and `BUILD_TAGS`.
The old regex read raw file text, so it saw an import behind any
constraint. Two runs alone would lose that reach: a file gated
`//go:build windows`, or named `store_windows.go`, would contribute no
imports to either run. The constraint scan closes the hole. It fails
with the file name and the term, so a new tag needs a deliberate
`BUILD_TAGS` update, never a silent hole. Reproduced before the scan
existed: a package with a `//go:build windows` file importing `secret`
returned no edge from `check_deps.check`, where the old regex returned
`secret`.

The scan reads its directory list from a filesystem walk, not from the
enumerated set. `go list` exits zero and omits a directory whose files
are all constrained, so such a directory never reaches an enumeration
driven list. Reproduced: a directory holding one `//go:build windows`
file returned no package and no error. `go list` stays the sole
enumerator. The walk decides which files the scan reads, and every
constrained file it finds is a hard failure, so the two lists cannot
disagree. Confirmed against this repo: the walk returns the same 45
paths, and the scan reports nothing.

### policy/layers.json keying

- A nested package is keyed by its full relative path, `flow/engine`.
- Values under `allowed_imports` use the same full relative paths.
- Every existing key and every existing value is a flat name, so the
  file needs no edit in this change.

### Nested artifact paths

- Plan doc: `docs/plans/<relative path>.md`. A nested package needs
  `docs/plans/flow/engine.md`. `check_prose.py` already reaches it.
- API lock: `api/<relative path>.txt`. A nested package needs
  `api/flow/engine.txt`. Mirroring the directory avoids the collision
  that a flattened `flow_engine.txt` would allow.
- `check_api.py` keys locks by path relative to `api/`, walking
  recursively.
- `api_surface.go` prints the relative package path in its `package`
  header line. For a flat package that path equals the base name that
  line 42 emits today, so no existing `api/*.txt` file changes.
  Confirm with `make api-update` and an empty `git diff` on `api/`.
- `api_surface.go` runs `go list -json ./...` itself, through
  `os/exec`. It does not take the list on argv. Reason: `make
  api-update` runs `go run scripts/api_surface.go` alone, and the tool
  must work without a Python process in front of it.
- `api_surface.go` runs `go list` twice, once with default tags and
  once with `-tags ledger_sqlite`, and unions the directory list. The
  tag set must match `alwaysBuildTags` at
  `scripts/api_surface.go:27`.
- `api_surface.go` exits non-zero with the `go list` stderr when either
  run fails. This matches the `go_packages.py` rule above.

Why the second tag run is mandatory. `build.Default.BuildTags` at
`scripts/api_surface.go:30` already includes `ledger_sqlite`, and the
comment at lines 20 to 26 states the lock captures a tag-gated
symbol regardless of tag. A default-tag `go list` does not match that
rule. Reproduced on a package whose only file carries
`//go:build ledger_sqlite`: the default run lists `normal` alone, and
the tagged run lists `normal` and `tagonly`. Two consequences follow
if the second run is omitted:

- A fully gated package emits no `package` header, so
  `check_api.py:42-44` reports "lock exists but package is gone"
  against a valid lock.
- A partially gated package fails the other way. The directory list
  and `build.Default.BuildTags` disagree, and `surface()` can return
  "holds 0 packages". This is latent today only because `ledger` has
  untagged files and appears in both runs.

### The api-update recipe

The recipe at `Makefile:101` pipes the tool into awk. Awk cannot
create a directory, so the first nested package destroys the target.
Reproduced with a two-package stream, `flow/engine` first:

- awk exits 2 with `cannot open "api/flow/engine.txt" for output`.
- `api/` is left empty. Awk aborts before it writes the flat
  `room.txt` that follows in the stream.

Replace the awk one-liner with a shell `while read` loop. On each
`package ` header line the loop derives the lock path, runs `mkdir -p`
on its parent directory, and truncates the file. Every following line
appends to the current file. This matches awk's existing
truncate-once-then-append behavior, so a flat lock stays
byte-identical. Reproduced against the same two-package stream: the
loop exits 0 and writes both `api/flow/engine.txt` and `api/room.txt`
with the correct content.

Keep the current behavior of leaving a stale lock in place.
`check_api.py:42-44` already reports an orphan lock.

### Operational properties

Verified against a temporary module fixture:

- `go list -json` exits non-zero on a malformed import block or on a
  package-clause clash inside one directory. That is fail-closed.
- `go list -json` exits zero on a body-level syntax error and still
  returns the correct import list. It parses imports, not bodies.
  `go vet` and `go build` own the body error, so no gate loses reach.
- `go list -json` exits zero on an unresolved external dependency. It
  sets `"Incomplete": true` and fills `Imports` from source. That is
  exactly what a probe fixture needs.
- Probe fixtures must set `GOPROXY=off` and `GOFLAGS=-mod=mod`.
  Without them `go list` attempts network resolution and can hang CI.
- New operational property: `check_deps.py`, `check_plan.py`, and
  `check_orphan_packages.py` now stop working on a tree that does not
  parse. That is fail-closed and acceptable. `check_api.py:12` sets
  the precedent.

## API

No Go package gains, loses, or changes an exported symbol. Every file
under `api/` stays byte-identical. `make api-update` must produce an
empty diff.

New and changed non-Go surface:

- New file `scripts/go_packages.py`.
- New flag `--probe` on `check_deps.py`, `check_plan.py`, and
  `check_api.py`.
- Extended probe suite on `check_orphan_packages.py`.
- A rewritten `api-update` recipe in the Makefile.
- Three new probe invocations in the Makefile `verify` target.

## Tests

Gate work has no Go unit tests. Probes prove it, following the
`--probe` convention of `check_mutation.py` and
`check_orphan_packages.py`.

Each probe builds a throwaway module in a temp directory. The fixture
writes a `go.mod` with this repo's module path. Each probe runs with
`GOPROXY=off` and `GOFLAGS=-mod=mod`. Each gate must both fire on the
planted fault and stay silent on the clean fixture.

Enumeration probes, in `scripts/go_packages.py`:

- A package at `flow/engine/` appears, keyed `flow/engine`.
- A file under a `_`-prefixed directory yields no package.
- A file under `testdata/` yields no package.
- An external test package `flow/flow_test` is dropped.
- The `scripts` package is dropped.
- A file behind `//go:build ledger_sqlite` contributes its imports.
- A module path inside a comment is not an import.
- A module path inside a string literal is not an import.
- A malformed import block makes the helper exit non-zero.
- A file behind a build tag outside `BUILD_TAGS` makes the helper exit
  non-zero and names the file and the term.
- A file with a `_GOOS` filename suffix makes the helper exit non-zero
  and names the file.
- A directory whose every file is constrained makes the helper exit
  non-zero. `go list` omits that directory.

Deps probes, in `check_deps.py --probe`:

- A nested package absent from `allowed_imports` fails.
- A nested package listed in `allowed_imports` with a matching edge
  passes.
- A flat package importing a nested package fails when the row omits
  the nested path.

Plan probes, in `check_plan.py --probe`:

- A nested package with no `docs/plans/<path>.md` fails.
- The same package passes once the plan file exists with all sections.
- A plan file that lacks one required section still fails.

Orphan probes, added to `check_orphan_packages.py --probe`:

- A nested package with no caller reads as an orphan.
- A top-level package whose only non-test caller is nested does not
  read as an orphan. This is the false-orphan case today.
- The existing probes keep passing unchanged.

API probes, in `check_api.py --probe`:

- A nested package with no lock under `api/` fails.
- The same package passes once `api/flow/engine.txt` exists.
- A lock file with no package fails.
- A `package main` file inside a `_`-prefixed directory produces no
  header line and no error. This is the fault that broke a naive
  recursive walk.
- A directory whose package name differs from its base name produces
  a named error, not a crash.
- A package whose only file carries `//go:build ledger_sqlite`
  produces a `package` header. This proves the second tag run runs.

Recipe probe, run by the `check_api.py --probe` suite:

- `make api-update` against a fixture holding both a nested package
  and a flat package writes `api/flow/engine.txt`.
- The same run leaves every flat lock intact. The awk form destroyed
  them, so this half is the load-bearing half.

Real-tree probes:

- Every gate above runs against this repo and reports zero problems.
- The set of package paths the helper returns matches
  `go list ./...` minus `scripts` and minus the `_test` suffix.

## Verification

### What the corrected logic finds today

The corrected logic was run against the current tree before writing
this plan. It found nothing new.

- Packages seen: 45, the same set the current regex walk returns.
- Packages seen only by `go list`: none.
- Packages seen only by the regex walk: none.
- Import edges that differ between the two: none.
- Policy violations under the `go list` truth: none.
- Rows in `allowed_imports` naming no package: none.
- Packages with no plan doc: none.
- Packages with no `api/` lock: none.
- Locks with no package: none.
- Orphans: the same twelve packages, all already declared in
  `policy/pending_wiring.json`.
- Stale `pending_wiring.json` entries: none.

No follow-up change is needed. If a later run does find a violation,
report it and fix it in a separate change. Never widen a policy row to
silence it.

### Commands that must pass

- `python3 scripts/check_plan.py`
- `python3 scripts/check_deps.py`
- `python3 scripts/check_prose.py`
- `python3 scripts/check_labels.py`
- `python3 scripts/check_names.py`
- `make api-update` followed by an empty `git diff -- api/`
- `make verify`

### Test-tampering gate

`scripts/test_tampering_rules_infra.py` carries the infrastructure
rules. Expected behavior on this change:

- TT11 fires. The change edits `scripts/` and `Makefile`, which are
  gate infrastructure. It also edits `docs/` and `AGENTS.md`, which
  are not. TT11 fires on exactly that pairing. The firing is
  legitimate: AGENTS.md mandates the ladder update and the plan doc in
  the same change. This plan does not pre-authorize an override
  trailer. The builder must surface the finding and let a human
  decide, per `scripts/test_tampering_override.py`.
- TT12 stays silent. No file under `scripts/mutation_denylist/`
  changes.
- TT13 stays silent. `COVERAGE_FLOOR` is untouched, no stored mutation
  floor is lowered, and the change only adds gate invocations to the
  Makefile. TT13 fires on a removed invocation, never on an added one.
- TT14 stays silent. `_is_checker_source` matches
  `scripts/check_test_tampering.py` and `scripts/test_tampering_*.py`.
  This change touches neither, and it does not touch
  `.githooks/commit-msg`.

### AGENTS.md ladder updates

Update these entries so the documented reach matches the code:

- The `check_deps.py` entry states that the policy covers a package at
  any depth, keyed by its relative import path.
- The `check_plan.py` entry states the nested plan path.
- The `check_api.py` entry states the nested lock path.
- The `check_orphan_packages.py` entry states that a nested importer
  counts as a real caller.
- The gate-tiers section lists the three new probe invocations.

### Landing order

1. Land `scripts/go_packages.py` with its own probes.
2. Switch `check_deps.py` and add its probes.
3. Switch `check_plan.py` and add its probes.
4. Switch `check_orphan_packages.py` and extend its probes.
5. Rewrite the `api-update` recipe first, then switch
   `api_surface.go` and `check_api.py`. The recipe must handle a
   nested path before any tool can emit one. Confirm `api/` does not
   change.
6. Wire the new probes into `verify` and update AGENTS.md.
