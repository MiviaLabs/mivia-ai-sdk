# Plan: ledger/ledgertest

Status: shipped.

## Goal

Run the durable ledger's claim-and-fence invariants as a conformance
kit. The kit moved here from the top-level `durablefence` package in
Phase 86. No production code may import it.

## Scope

The package stays a test kit built on `testing.TB`. A caller wires its
own claim, takeover, mutate, release, and fence-reading calls into a
`Scenario` literal and runs `RunAll`. The kit now lives beside the
`ledger` package it exercises, under a `test`-style suffix, so the
public package count stays small and the fixture cannot be mistaken
for product surface.

## API

The surface is unchanged from `durablefence`: `Scenario`, `Validate`,
`ErrIncompleteScenario`, the `Check*` functions, and `RunAll`. The
package name changes to `ledgertest`.

## Tests

The kit's own tests moved with it. They cover the check functions,
the negative paths, release on failure, and the reference scenario.

## Verification

`make verify` compiles and runs the kit through `ledger`'s test
targets. The orphan gate carries a `pending_wiring.json` entry, since
the kit's only callers are `_test` subdirectories.
