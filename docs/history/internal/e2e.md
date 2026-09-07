# Plan: internal/e2e

Status: shipped.

## Goal

Keep the end-to-end scenario harness and suite out of the public
package surface. The package moved here from the top-level `e2e`
directory in Phase 86. Only test code imports it.

## Scope

Each scenario wires real high-level blocks together and asserts one
full run's outputs. The package name stays `e2e`; only its path
gains the `internal/` prefix, so the Go toolchain itself blocks
outside importers.

## API

The surface is unchanged. The pending_wiring entry and the symbol
locks re-path to `internal/e2e`.

## Tests

The scenario suite is the package's own test directory. It moves
unchanged.

## Verification

`make verify` runs the suite, and the SQLite ceremony target names
`./internal/e2e/...` in the Makefile.
