# Phase 12: agent definition

Status: future. Builds the agent block. An agent composes the blocks.
This phase defines the agent shape. It holds an identity, a capability
set, a transport, and a set of steps. It does not run yet.

## Goal

Define one agent declaratively. The agent states who it is, what it
can do, where it talks, and what it runs. The definition is data. The
tool registry and the memory store bind to it later.

## Scope

Inside: the `Agent` type, `New`, and the binding of the identity and
the discovery card. Outside: the execution loop and the tools. Those
belong to phase 13 and phase 14.

## API

- `type Agent struct` holding the identity, the card, and the step
  set.
- `Name() string` returns the agent name.
- `Capabilities() []string` returns the card capability list.
- `New(id identity.Identity, card discovery.Card, plan flow.Definition) (*Agent, error)`

`New` rejects an agent with no name on the card. It rejects a step
plan with a cycle. The agent imports the blocks; no block imports the
agent. The agent is the composition layer.

## Tests

Test files live in `agent/agent_test/`:

- `definition_test.go` — the red-green cases for `New`. Start with
  the assertions. Confirm they fail on the empty phase. Implement and
  watch them pass.
- `definition_integration_test.go` — build an agent from an identity, a
  card, and a plan. Prove the name and the capabilities resolve. Feed
  a bad plan and confirm `New` rejects it.
- `definition_bench_test.go` — benchmark `New` on a small plan. Target
  under one millisecond. State the allocation budget.

## Verification

`make verify` passes. The coverage floor for `agent` holds. The agent
package declares its imports of identity, discovery, and flow in
`policy/layers.json`. `api/agent.txt` lands via `make api-update`.
The execution stays out of this phase.
