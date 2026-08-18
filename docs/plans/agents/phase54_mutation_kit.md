# Phase 54: mutation kit

Status: rollout step 1 shipped. This plan passed plan review. Rollout
step 1 (the script, its probe mode, `make mutation`, and the floor
files for `envelope`, `machine`, and `ledger`) has landed. Rollout step
2 (a denylist and floor for each remaining package) is still open.
Depends on no unshipped phase. It adds no new top-level package.

## Why this phase exists

The coverage floor cannot tell a real test from a vacuous one. Four
surviving mutations escaped every suite during the agentrun review:
a deleted terminal filter, a deleted fallback rule, a reduced panel
union, and a deleted skip rule. All four held the 85 floor. Only
mutation probes caught them.

AGENTS.md lists mutation testing as future work. The 2026 consensus,
from Meta's mutation-guided LLM testing through every practitioner
guide, treats the mutation score as the quality gate for
agent-written tests. Coverage stays the floor; the kill rate becomes
the ceiling's proof.

## Goal

One stdlib-only script, `scripts/check_mutation.py`, applies simple
source mutations to a package, runs that package's tests per mutant,
and reports the kill rate against a floor.

## Scope

Inside:

- Text-level operator mutations over Go source: `==` against `!=`,
  `<` against `<=`, `&&` against `||`, a dropped `!`, and a deleted
  `continue` guard. One mutant per mutation site, applied alone.
- Site-finding tokenizes the source with Go's `go/scanner`, using
  token positions rather than a full AST or type-checking. This
  keeps the kit text-level, per the Outside constraint below, while
  still excluding matches inside string literals, rune literals, and
  comments, since the scanner classifies those tokens on its own.
- The dropped-`!` mutation excludes any `!` token immediately
  followed by a `=` token. Turning `!=` into `=` does not compile, so
  that site is never a candidate.
- A deterministic mutant list: sorted by file and offset. `--sample N`
  runs the first N mutants from that sorted list; the order is fixed,
  so the same command always exercises the same subset. No random or
  seeded sampling.
- A per-package kill floor, checked for the packages the run names.
  Each floor is the package's own observed kill rate at the commit
  that sets it, rounded down to the nearest whole percent. This
  mirrors the coverage floor's ratchet: a floor is measured, never
  picked low to pass trivially.
- The floor lives in a `floor` field inside that package's own
  `scripts/mutation_denylist/<pkg>.json` file, next to its denylist
  entries. This is the floor's one source of truth, the same way
  `COVERAGE_FLOOR := 85` lives once in the Makefile. When the CLI
  omits `--floor`, the kit reads this field. A `--floor` value typed
  on the command line overrides the file for that run only; it never
  writes the file and never sets a package's real floor. Ratcheting a
  floor means editing the `floor` field in the package's JSON file, in
  its own commit, the same way any other gate-floor change lands.
- A mutant that fails to build is discarded from the kill-rate
  denominator. The kit logs the discard and counts the mutant as
  neither killed nor surviving.
- A full run mutates a package's tracked `.go` file in place, one
  mutant at a time. Before mutating, the kit snapshots the file's
  original bytes in memory. It restores those bytes after the mutant's
  test run, and it restores them on interrupt or crash too, through a
  try/finally or equivalent guaranteed-cleanup mechanism. This gives
  the full-run mode the same restoration guarantee the probe fixtures
  already get from their temporary directory. The kit also installs a
  SIGTERM handler that re-raises as the same interrupt Python already
  routes through `finally`, since a bare SIGTERM skips `finally`
  blocks by default and a killed run would otherwise leave a mutated
  file on disk. SIGKILL stays unrecoverable; no process can catch it.
- Each mutant's test run has a fixed timeout. A mutant that times out
  counts as killed: a hung mutant (for example, one from a deleted
  `continue` guard) proves the suite would eventually fail or hang,
  which is a kill signal, not a pass.
- A denylist of documented equivalent mutants, one file per package,
  under `scripts/mutation_denylist/<pkg>.json`. The shared script
  loads the file for the package the run names. Each entry keys on
  the file path plus the exact matched source snippet, never a line
  number, since a line number drifts across edits. A run fails loudly
  if a denylisted key's snippet no longer appears verbatim in the
  named file, so a stale entry cannot rubber-stamp an unrelated site
  after a refactor.
- A denylisted snippet must match exactly one site in its named file.
  If the snippet matches more than one site, the kit fails loudly
  instead of applying the entry to either site. The fix is to widen
  the snippet with more surrounding context until it matches one site
  only.
- A full run skips any tracked `.go` file whose first non-blank line
  is a `//go:build` constraint. A tag-gated file (for example
  ledger's `sqlite_store.go` and `sqlite_schema.go`) never compiles
  into the default, untagged build the kit tests against, so mutating
  it would always "survive" and pollute the kill-rate denominator
  with meaningless results. This mirrors how the Makefile's default
  coverage block already excludes that same code, covering it
  separately through `make verify-ledger-sqlite`.
- Test invocation mirrors the Makefile's `verify` coverage block: for
  package `<pkg>`, the kit checks whether a `<pkg>/<pkg>_test`
  directory exists. If it does, the kit runs the mutant's test from
  that external test directory, the same way `verify` runs
  `go test ... ./ledger/ledger_test` instead of `./ledger` alone. If
  no such directory exists, the kit runs the test from `<pkg>` itself.
- A probe mode for the gate's own tests: one planted site the kit
  must generate, and one denylisted site it must skip. Following the
  `check_semgrep_probes.py` convention, the probe fixtures are not
  checked into the tree as `.go` files. `--probe` writes them to a
  temporary directory at run time, in Python, and removes the
  directory when the run ends. This keeps the fixtures out of `go vet
  ./...`, gofmt, `check_structure.py`, `check_docs.py`,
  `check_names.py`, and the Semgrep scan.
- `--probe` is fast: one planted fixture pair, no per-package test
  run over real source. It joins the probe tier inside `make verify`,
  the same tier `check_semgrep_probes.py` occupies. The Makefile's
  `verify` target gains `python3 scripts/check_mutation.py --probe`
  as a new line, directly after the existing
  `check_semgrep_probes.py` line inside the `verify` target.
- `make mutation` as a separate, on-demand target for a full
  per-package mutation run. It never runs inside `verify` or
  `verify-fast`, because a full run costs minutes, not seconds.
- Exit codes: 0 means the run is at or above its floor, `--probe`
  passed, or the run has no floor from either the CLI or the
  package's JSON file (an exploratory run, sampled or full, before a
  floor exists). 1 means the run is below its floor, or `--probe`
  failed. 2 means `--pkg` names a package the kit does not recognize.

Outside:

- Any third-party dependency, including go-mutesting and gremlins.
  The kit stays stdlib: Python's standard library plus the `go`
  tool.
- Any change to `make verify-fast`. `--probe` joins `make verify`
  only, at the point named above, next to the Semgrep probe suite.
  `verify-fast` stays free of both probe suites and of `make
  mutation`.
- AST-level or compiler-plugin mutations. Text-level scanning with
  `go/scanner` found the real escapes; deeper machinery adds cost,
  not findings.

## API

```sh
python3 scripts/check_mutation.py --pkg ledger --floor 85
python3 scripts/check_mutation.py --pkg flow --sample 40
python3 scripts/check_mutation.py --probe
```

`--pkg` names one package directory. `--floor` is optional and
overrides the package's stored floor for that run only; when omitted,
the kit reads the floor from the package's own
`scripts/mutation_denylist/<pkg>.json` file. `--sample` runs the first
N mutants from the sorted, deterministic mutant list. Neither `--sample`
nor a full run needs `--floor`: when a run has no floor from either
source, the kit prints the kill rate and exits 0, with no pass or fail
judgment, since a run with no floor set anywhere is not a gate — this
covers a `--sample` run and a full bootstrap run against a package
that has no floor yet, the case the Rollout section relies on to
measure a package's first floor. `--probe` proves the kit still
generates its planted site and honors the denylist.

## Tests

The kit's own checks live in `scripts/check_mutation.py --probe`.
`--probe` writes two fixtures to a temporary directory at run time,
the same way `check_semgrep_probes.py` writes its Semgrep fixtures:
one planted site with an `==` the kit must mutate, and one
denylisted site it must skip. Neither fixture is a committed `.go`
file. The probe fails when either behavior stops holding.

Additional cases the probe and the full-run tests must cover:

- A `!=` token never becomes a bare `=` mutant: the dropped-`!`
  mutation skips any `!` immediately followed by `=`.
- A match inside a string literal, a rune literal, or a comment never
  becomes a mutation site.
- A mutant that fails to build is discarded, logged, and excluded
  from the kill-rate denominator.
- A mutant whose test run exceeds the timeout counts as killed.
- A denylist entry whose snippet no longer matches the named file
  fails the run with a clear error, rather than silently passing.
- A denylist entry whose snippet matches two or more sites in the
  named file fails the run with a clear error, rather than applying
  to either site.
- A package with a `<pkg>/<pkg>_test` directory runs its mutant tests
  from that directory; a package without one runs them from `<pkg>`
  itself.
- A `//go:build` gated file never contributes a mutation site, even
  when it holds an obvious candidate.
- A run with no `--floor` reads the floor from the package's
  `scripts/mutation_denylist/<pkg>.json` file; a `--floor` value on
  the CLI overrides the file for that run only and never writes it.

## Rollout

Two steps, one commit each:

1. The script, the probe, `make mutation`, and the Makefile edit that
   adds `--probe` to `verify`, directly after the existing
   `check_semgrep_probes.py` line inside the `verify` target. Floors
   go into
   `scripts/mutation_denylist/envelope.json`,
   `scripts/mutation_denylist/machine.json`, and
   `scripts/mutation_denylist/ledger.json` (empty denylists where a
   package has no equivalent mutants yet), each with a `floor` field
   set from that commit's own measured kill rate for `envelope`,
   `machine`, and `ledger`. Those three hold the wire contract, the
   status model, and the task record; their mutants are small and
   their suites are fast. This step also adds `check_mutation` to
   `docs/architecture.md`'s Gate system list, alongside
   `check_semgrep_probes` and the other named gates.
2. One denylist file and one `floor` field per remaining package —
   `flow`, `agentrun`, `taskrun`, `a2a`, `identity`, `room`, and
   `tools` — each added in its own commit, after a full run names
   that package's surviving mutants and their tests land. One file
   per package keeps each rollout commit scoped to that package's
   own denylist and floor, with no shared file to collide on.

A surviving mutant is a test gap, not a script bug. Each rollout
step ends with every survivor either killed by a new test or
denylisted with its reason.

## Verification

- `make verify` passes; the kit changes no package code beyond the
  Makefile line named above.
- `python3 scripts/check_mutation.py --probe` passes as part of
  `make verify`, at the point named in Scope, not inside
  `verify-fast`.
- `make mutation` runs the full per-package mutation sweep on demand
  and reports each named package's kill rate against its floor.
- The first rollout run reports its per-package kill rates in the
  commit message.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
- `docs/architecture.md`'s Gate system list names `check_mutation`
  starting at rollout step one.
- AGENTS.md's enforcement ladder gains the kill-floor rule at
  rollout step two, not before.
