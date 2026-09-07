# Plan: a2a/a2atest

Status: shipped. Relocates `a2aclient/a2atest` into `a2a/a2atest`
after the `a2aclient` package merged into `a2a`. See
`docs/history/a2aclient/a2atest.md`, marked superseded, for the
original design history.

## Goal

Give `a2a` a gRPC A2A server test fixture, `Loopback`. It lets `a2a`'s
own tests run a full sign-send-poll-verify round trip against a live
server, with no live remote agent and no mock transport.

## Scope

Inside: the loopback server, its executor, and its stop function.

Outside: any part of `a2a`'s production client surface. `a2atest`
never imports `a2a` from its own non-test files. Its tests import
`a2a` to drive the fixture.

## API

The exported surface mirrors `api/a2a/a2atest.txt`: `Loopback`,
`Execute`, and `Cancel`. The shape is unchanged from the superseded
plan.

## Tests

`a2a/a2atest/loopback_test.go` covers the package. It starts the
server, runs a signed round trip through `a2a.Client`, and checks the
text carrier keeps a large integer byte-exact.

## Verification

`make verify` passes for `a2a/a2atest`: gofmt, vet, the python gates,
the Semgrep scan, and the coverage floor at 85.

`policy/layers.json` holds the row `"a2a/a2atest": ["a2a",
"envelope"]`. `policy/thirdparty.json` holds its `a2a-go` and `grpc`
exception. `semgrep/sdk-standards.yml`'s
`sdk.go.no-a2atest-import` rule allows only `a2atest`'s own files and
`a2a`'s test files to import this package.
