# Plan: a2aclient/a2atest

Status: shipped. Relocates `a2aloopback` into `a2aclient/a2atest`.
See `docs/plans/a2aloopback.md`, marked superseded, for the original
design history.

## Goal

Give `a2aclient` its own gRPC A2A server test fixture, `Loopback`. It
lets `a2aclient`'s own tests and other packages' cross-package tests
run a full sign-send-poll-verify round trip against a live server,
with no live remote agent and no mock transport.

## Scope

Inside: `Loopback`, the unexported `loopbackExecutor` type and its
`Execute` and `Cancel` methods. The package is test-only. No
production code may import it; it exists to run inside another
package's own test files or nested `_test` directory, the same
convention `ledger/ledgertest` uses.

Outside: any part of `a2aclient`'s production client surface
(`Client`, `New`, `Close`, `Send`, `Status`, `Result`, `TaskHandle`,
`State`). `a2atest` never imports `a2aclient`'s production code from
its own non-test files; its tests import `a2aclient` to drive the
round trip.

## API

The exported surface mirrors `api/a2aclient/a2atest.txt`: `Loopback`,
plus the unexported `loopbackExecutor.Execute` and
`loopbackExecutor.Cancel` methods.

## Tests

`a2aclient/a2atest/loopback_test.go` covers the package:
`TestLoopbackRoundTrip`, `TestLoopbackRoundTripKeepsLargeIntegers`,
`TestLoopbackExecutorCancel`,
`TestLoopbackExecutorExecuteRejectsMissingContextID`,
`TestLoopbackRequestRejectsMalformedText`,
`TestLoopbackRejectsKeyGenerationFailure`, and
`TestLoopbackStopIsIdempotent`.

## Verification

`make verify` passes for `a2aclient/a2atest`: gofmt, vet, the python
gates, the Semgrep scan, and the coverage floor at 85.
