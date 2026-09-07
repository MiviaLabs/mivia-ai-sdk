# Plan: ledger/ledgertest

Status: shipped. Relocates `durablefence` into `ledger/ledgertest`.
See `docs/plans/durablefence.md`, marked superseded, for the original
design history.

## Goal

Give `ledger` its own conformance kit for claim, takeover, and fence
invariants. The kit proves these invariants against any claim-lease
implementation, including the concurrent case a sequential test
cannot reach.

## Scope

Inside: `Scenario`, `Scenario.Validate`, the seven `Check*` functions,
and `RunAll`. The package is test-only. No production code may import
it; it exists to run inside another package's `_test` subdirectory.

Outside: any claim-lease implementation itself. `ledgertest` takes an
implementation through `Scenario`'s function fields and asserts
invariants; it never implements a lease store.

## API

The exported surface mirrors `api/ledger/ledgertest.txt`: `Scenario`,
`Scenario.Validate`, `RunAll`, the seven `Check*` functions, and
`ErrIncompleteScenario`.

## Tests

`ledger/ledgertest/*_test.go` covers the package:
`TestScenarioValidateComplete`, `TestScenarioValidateMissingField`,
the `TestCheck*` happy-path set in `checks_test.go`, the
`TestCheck*CatchesBroken*` negative set in `checks_negative_test.go`,
the `TestCheck*PropagatesBackendErrors` set in `checks_error_test.go`,
`TestCheckReleasesHoldOnAssertionFailure`, and
`TestRunAllComposesOverOneScenario`.

`ledger/ledger_test/scenario_test.go` is the named production caller:
it wires a `Scenario` against `Ledger.Claim`, `Renew`, `Release`,
`Takeover`, and `State`.

## Verification

`make verify` passes for `ledger/ledgertest`: gofmt, vet, the python
gates, the Semgrep scan, and the coverage floor at 85.
