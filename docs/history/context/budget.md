# Plan: context/budget

Rename `contextbudget` to `context/budget`. The Go package name is
`budget`, the last path element. Phase 86 kept this package split;
this is a path move only. The shared gate-surface enumeration lives
in `docs/history/context/plan.md`.

## Goal

Give the byte-cap and event-cap check a path that says what it is: a
budget block under the `context` parent, distinct from the token
math in `context/plan`.

## Scope

Inside:

- The directory move `contextbudget/` to `context/budget/` and the
  package clause change to `budget`.
- The import-path rewrite in every importer, listed below.

Outside:

- Any exported symbol or behavior change. `Limits`, `Limits.Fits`,
  and `Limits.Validate` move as they are.
- Any token accounting. `context/budget` counts bytes and events;
  `context/plan` counts tokens. The split stays.
- No `x/` file imports this package; the rewrite stays in the core
  module, its tests, and the examples.

## API

Unchanged. `make api-update` writes `api/context/budget.txt`, first
line `package budget`, mirroring the package path. Confirm the
generated file differs from the current `api/contextbudget.txt` only
in the package line.

## Tests

Move `contextbudget/contextbudget_test.go` with the package, then
rewrite import paths and, where the body names the old qualifier,
`contextbudget.` becomes `budget.`. No test is deleted or weakened.
The TT01 trailer for this tree is not pre-authorized; if the body
rewrites trip TT01, take it to the orchestrator. See
`docs/history/context/plan.md`'s Tests section.

Aliasing note, re-derived on this tip: the `budget` parameter and
field of `agent.Run` and `agentloop` shadow the new package name in
those scopes, and neither makes a package-qualified call there, so
the plain package name compiles everywhere. Do not alias
preemptively; alias a file only if the compiler reports a shadow.

## Verification

- `make verify` passes; the coverage floor holds for
  `context/budget`.
- `make api-update` produces `api/context/budget.txt`; commit the
  diff in the same change.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass.

Importer set, verified on this tip:

- Core module, production: `agent/run.go`, `agentloop`
  (`loop.go`, `options.go`), `agentrun` (`options.go`, `wire.go`).
- Core module, tests: `agent/agent_test/`,
  `agentloop/agentloop_test/`, `agentrun/agentrun_test/`,
  `agent/run_budget_internal_test.go`, and the package's own file.
- Examples: `docs/examples/_agentloop/main.go` and
  `_agentloop_adoption/main.go`; `_agentloop_minimal/` imports
  `contextplan` only.
- `internal/e2e`'s compaction test imports `contextplan` only, not
  this package.

## Addendum: Error sentinel sweep

Status: shipped

`context/budget` had no named `Err*` sentinel before this pass. Both
checks in `Limits.Validate` returned a plain `errors.New` literal.
This pass introduced one sentinel, `ErrInvalidOptions`, and rewrote
both checks to wrap it with `fmt.Errorf("%w: %s", ErrInvalidOptions,
"<field>: <rule>")`. Both checks are construction-time argument
checks, so both classify as CONFIG.

| Sentinel | Classification | Disposition |
| --- | --- | --- |
| MaxBytes negative check (no prior name) | CONFIG | Now wraps `ErrInvalidOptions` with substring `MaxBytes`. |
| MaxEvents negative check (no prior name) | CONFIG | Now wraps `ErrInvalidOptions` with substring `MaxEvents`. |

No sentinel in this package classifies as RUNTIME. `Limits.Fits`
returns a bool, not an error, and carries no sentinel of its own.
