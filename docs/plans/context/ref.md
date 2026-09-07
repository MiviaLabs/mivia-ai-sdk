# Plan: context/ref

Rename `contextref` to `context/ref`. The Go package name is `ref`,
the last path element. Phase 86 kept this package split; this is a
path move only. The shared gate-surface enumeration lives in
`docs/plans/context/plan.md`.

## Goal

Give the canonical content-reference minter a path that says what it
is: a small block under the `context` parent, one of the three public
context packages.

## Scope

Inside:

- The directory move `contextref/` to `context/ref/` and the package
  clause change to `ref`.
- The import-path rewrite in every importer, in the core module and
  across the `x/` module boundary through the existing `replace`
  directive in `x/go.mod`.

Outside:

- Any exported symbol, sentinel, conformance vector, or behavior
  change. `HashPrefix`, `Digest`, `IsLowerHex`, `IsRef`, and `Mint`
  move as they are.
- Any new consumer. The importer set is fixed, listed below.
- The `contextsummary: ` and `contextplan: ` error-literal exemption
  does not apply here; this package defines no such literal. The
  `sha256:` literal stays in its definition only, per the Semgrep
  rule.

## API

Unchanged. `make api-update` writes `api/context/ref.txt`, first
line `package ref`, mirroring the package path. Confirm the
generated file differs from the current `api/contextref.txt` only in
the package line.

## Tests

Move `contextref/contextref_test/` to `context/ref/ref_test/`
unchanged, then rewrite import paths and, where test bodies name the
old qualifier, `contextref.` becomes `ref.`. Run seeded smoke tests
only; never run `go test -fuzz` at default parallelism. No TT01
trailer is pre-authorized for this tree: any TT01 finding here stops
the build and escalates to the orchestrator. See
`docs/plans/context/plan.md`'s Tests section for the authorization
scope and the hash-match caveat.

## Verification

- `make verify` passes; the coverage floor holds for `context/ref`.
- `(cd x && go build ./... && go test ./...)` passes: x/ consumes
  `context/ref` through its `replace` directive, and no core gate
  builds x/.
- `make api-update` produces `api/context/ref.txt`; commit the diff
  in the same change.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass.
- `python3 scripts/check_orphan_packages.py` passes: the importer set
  below keeps `context/ref` off the orphan list.

Importer set, verified on this tip, so no consumer is missed:

- Core module, production: `envelope/message.go` (Mint at
  `message.go:84`, `HashPrefix` alias at `:22`, `IsLowerHex` at
  `:190`), `memory/store.go`, and the renamed `context/plan`
  (`wire.go`, for `Compact`'s idempotency key).
- Core module, tests: `context/ref`'s own tree.
- x/ sub-module, production: `x/contextstate/contracts.go`,
  `x/contextsession/planner.go`, `x/runconfig/loader.go`.
- x/ sub-module, tests: the `x/contextsession` and
  `x/contextstate` and `x/runconfig` test files the grep in
  `docs/plans/context/plan.md` step 12 lists.
- Aliasing: `memory/store.go` and `envelope/message.go` import under
  the alias `contextref`; both declare locals named `ref` in scopes
  that call the minter.
