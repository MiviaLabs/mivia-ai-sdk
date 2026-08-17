# Phase 16: system integration

Status: future. Builds on every prior phase. This phase binds all the
blocks into one exchange. Two agents talk end to end. The test runs
the whole stack, not a slice.

## Goal

Prove the blocks compose into a working agent pair. One agent sends a
signed request. The other admits, acks, runs, and replies. The thread
verifies after the exchange. The trust boundary is real, not mocked.

## Scope

Inside: the end-to-end test that wires identity, discovery, flow,
machine, a2a stubs, tools, and memory. Outside: multiple concurrent
rooms. A first version exercises one exchange.

## API

No new exported symbol. The phase adds no package. It confirms the
blocks work together as built.

## Tests

The test files live in `agent/agent_test/` under the system phase
name:

- `phase16_integration_test.go` — two agents on one plan exchange a
  signed request and a confirmed ack. The tool registry runs a step.
  The memory store holds the shared context. `VerifyThread` validates
  the thread after the exchange.
- `phase16_perf_test.go` — benchmark one full two-agent exchange.
  Target under ten milliseconds. State the allocation budget.
  The tdd file has no role here. The behavior is integration-only.

## Verification

`make verify` passes. Run `go test -race ./...` across the module.
The full exchange stays green. The agent work is complete when this
phase passes.
