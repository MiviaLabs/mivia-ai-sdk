# Plan: a2aclient/a2atest

Status: shipped.

## Goal

Ship the gRPC A2A loopback test-server fixture beside the client it
serves. The fixture moved here from the top-level `a2aloopback`
package in Phase 86. No production code may import it.

## Scope

The fixture starts a real A2A server on a 127.0.0.1 port and returns
the address and a stop function. It keeps the `a2a-go` and `grpc`
third-party rows the client carries, scoped to the server-side
packages a production client never needs.

## API

The surface is unchanged from `a2aloopback`: `Loopback`, `Execute`,
and `Cancel`. The package name changes to `a2atest`.

## Tests

The fixture's own test moved with it.

## Verification

`a2aclient`'s integration tests exercise the fixture, and `make
verify` runs them.
