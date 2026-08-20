# Plan: nested packages in the mutation gate

## Goal

Make `scripts/check_mutation.py` and the `mutation-gate` recipe work
for a package at any depth. Both are top-level only today.

## Scope

This plan is not scheduled. It lands after two other changes:

- `docs/plans/nested-package-visibility.md`, which supplies the
  enumeration helper.
- The concurrent change already in flight against
  `scripts/check_mutation.py`. That change touches the subprocess and
  timeout region at lines 166 to 205 and adds argparse flags at lines
  409 to 412. This plan touches lines 125 to 133 and lines 213 to 223.
  The two overlap only in `main()`, so this plan lands second.

Inside:

- `scripts/check_mutation.py` — four faults, listed below.
- `Makefile` — the `mutation-gate` recipe.

Outside:

- No stored floor changes. No denylist entry changes. Both would fire
  TT12 or TT13.

### Four confirmed faults

- `recognized_packages` at `scripts/check_mutation.py:213-223` reads
  `ROOT.iterdir()` and globs one level. A nested package is
  unrecognized, so `--pkg flow/engine` is rejected at line 421.
- `test_target` at lines 125 to 133 builds `./<pkg>/<pkg>_test`. For
  `flow/engine` that yields `flow/engine/flow/engine_test`, which does
  not exist. The fix uses the path base name.
- `load_denylist` at lines 41 to 43 builds
  `denylist_dir / f"{pkg}.json"`. For `flow/engine` that names
  `scripts/mutation_denylist/flow/engine.json`, inside a subdirectory
  nothing creates. `collect_sites` at line 112 and `resolve_floor` at
  line 205 both route through it. A nested package therefore gets a
  silent floor of `None`, and its stored floor stays unreachable.
- The `mutation-gate` recipe globs `scripts/mutation_denylist/*.json`
  and takes `basename $f .json`. A nested floor file is invisible to
  the glob. The base name also yields `engine`, not `flow/engine`. The
  fix uses a recursive find and a path relative to
  `scripts/mutation_denylist/`.

### Why this is separate

`check_mutation.py` does not run inside `verify` except as `--probe`.
Its enumeration fault is not one of the gates the enumeration change
names. Carving it out also removes a merge conflict with the
concurrent change.

## API

No Go symbol changes. No `api/` lock changes. No new script.

## Tests

Probes only, added to the existing `check_mutation.py --probe` suite.

- `recognized_packages` includes a planted nested package.
- `test_target` for `flow/engine` resolves `flow/engine/engine_test`.
- `load_denylist` reads a floor from
  `scripts/mutation_denylist/flow/engine.json`.
- `resolve_floor` returns that stored floor, not `None`.
- The `mutation-gate` package list includes a nested floor file, keyed
  by its relative path.
- The real tree reports the same package set and the same floors as
  before the change.

## Verification

- `python3 scripts/check_mutation.py --probe`
- `make verify`
- `make mutation-gate` lists the same packages as before the change.

TT12 and TT13 must stay silent. No denylist file and no stored floor
changes. TT11 stays silent too: this plan's own doc
(`docs/plans/mutation-nested-packages.md`) is a doc companion under
the TT11 doc-companion exemption, so pairing it with `scripts/` and
`Makefile` alone does not fire. See `docs/plans/test-tampering.md`'s
"TT11 doc-companion probes" addendum.
