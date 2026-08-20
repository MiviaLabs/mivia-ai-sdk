# Plan: per-package third-party imports

## Goal

Make the stdlib-only rule mechanical per package. AGENTS.md names five
exceptions in prose. No gate checks which package imports which
third-party module.

## Scope

This plan is not scheduled. It lands after
`docs/plans/nested-package-visibility.md`, which supplies the
enumeration it needs.

Inside:

- `scripts/check_gomod.py` — the new per-package rule.
- `AGENTS.md` — a new enforcement-ladder entry.

Outside:

- `policy/layers.json`. That file owns internal layering.
  `check_gomod.py` already owns third-party truth, and a third home
  for one rule invites drift. Keep the allowlist beside
  `ALLOWED_MODULES` at `scripts/check_gomod.py:28`.

### The gap

`scripts/check_gomod.py:28` holds one flat union allowlist. It pins
`go.mod` and `go.sum` module paths. It carries no package attribution.
A package could import `modernc.org/sqlite` today and no gate would
object.

### Current truth

Measured with `go list -json`, default tags and `ledger_sqlite`:

- `a2aclient` imports `github.com/a2aproject/a2a-go` and
  `google.golang.org/grpc`.
- `a2aloopback` imports the same two modules.
- `mcp` imports `github.com/modelcontextprotocol/go-sdk`.
- `schema` imports `github.com/santhosh-tekuri/jsonschema/v6`.
- `ledger` imports `modernc.org/sqlite` under the tag only.

That set matches the AGENTS.md exception list exactly. The rule would
find zero violations today.

### Two design traps this plan must solve

Both traps come from the plan review of the enumeration change. A gate
that encodes a weaker rule than the prose is worse than no gate. It
turns "forbidden" into "checked and approved".

Trap one: build-tag scope. AGENTS.md permits `modernc.org/sqlite` in
`ledger` behind the `ledger_sqlite` tag only. An allowlist entry with
no tag field grants `ledger` an unconditional exception. An untagged
`ledger/store.go` importing that module would then pass. The schema
must carry the tag, and the check must run per configuration, never
over a union of configurations.

Trap two: test-package scope. The enumeration helper drops any package
whose path ends in `_test`. `ledger/ledger_test/` is a real dropped
package. It holds two files behind `//go:build ledger_sqlite` that
could import anything in `ALLOWED_MODULES` unseen. The AGENTS.md
exception for `a2aloopback` is scoped to a test fixture, so test
imports must be in scope. This rule therefore needs its own
enumeration pass that keeps the external test packages, and it must
read `Imports`, `TestImports`, and `XTestImports`.

### Rule shape

- Treat an import as third-party when it is not in `go list std` and
  does not start with the module path.
- Run one check per build configuration, not one check over a union.
- Match an allowlist entry by module path prefix, package path, and
  build tag.
- Fail when a package imports a third-party path that no entry covers.

## API

No Go symbol changes. No `api/` lock changes.

New non-Go surface:

- A new allowlist constant in `scripts/check_gomod.py`.
- A new `--probe` flag on `check_gomod.py`.

## Tests

Probes only, following the `--probe` convention. Each probe builds a
throwaway module with `GOPROXY=off` and `GOFLAGS=-mod=mod`.

- A package importing an uncovered third-party path fails.
- The same package passes once an entry covers it.
- A stdlib import never counts as third-party.
- An internal module import never counts as third-party.
- A tag-scoped entry does not cover an untagged import of the same
  module. This is trap one.
- A third-party import inside an external `_test` package is seen.
  This is trap two.
- The real tree reports zero problems.

## Verification

- `python3 scripts/check_gomod.py`
- `python3 scripts/check_gomod.py --probe`
- `make verify`

TT11 fires: the change edits `scripts/` and `AGENTS.md` together. That
firing is legitimate and needs a human decision at commit time.
