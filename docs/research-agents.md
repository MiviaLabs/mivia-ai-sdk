# Research: building blocks, agents, and A2A integration

Date: 2026-08. Question: is the package split correct? How does A2A
fit now? How do future agents map onto the code? This report answers
all three. It records the assessment and the plan.

## Package breakdown assessment

The current split is correct. Two top-level packages exist: envelope and
room. Keep both. Do not refactor them into smaller packages.

The envelope package holds four concerns: the message, the ack, the
signing, and the thread chain. These four concerns share one struct.
They cannot live in separate packages without artificial layering.

A buildable split would add import edges, api locks, and plans. It
would add churn to a proof-of-concept with no consumer. The internal
file split already gives one concern per file. The structure gates keep
every file small.

The building-block principle is about composing behaviors. It is not
about tearing one cohesive struct into many. An agent composes blocks
at a higher level. The blocks stay coarse and cohesive.

Splitting envelope is wrong. Adding new top-level blocks is right. The
assessment: the current split is a solid foundation. Add new packages,
do not break the existing ones.

## The building-block rule

Each package is a building block. A block declares its internal imports
in policy/layers.json. A block never imports across the declared layer.
A new block lands as a new top-level package. It never lands as a root
file. The deps gate enforces the edges. The plan gate forces a reason
for each block.

Blocks combine by dependency, never by merge. Two blocks that need the
same data use the shared exported type. They do not copy the type into
a combined package. This keeps every block replaceable and testable.

An agent is the composition layer. It wires a transport block, a
workflow block, and the message blocks. The agent does not fork a block.
It imports a block as its public API. A block cannot see an agent.

Rules to embed in AGENTS.md:

- A package is a building block with one concern.
- Add a new top-level package for each new concern.
- Do not split a working package for purity alone.
- Split a package only when a real consumer needs the concern alone.
- Declare each internal import in policy/layers.json.
- Compose packages through their public API.
- Never copy a type into another package to avoid an import.

## A2A integration, A2A v1.0

A2A reached v1.0 in 2026-03. The Linux Foundation hosts the project. The
Agentic AI Foundation umbrella covers it. An official Go SDK exists:
a2aproject/a2a-go. The SDK needs Go 1.25 or newer.

A2A v1.0 changed the part model. The old kind field is gone. The
separate part classes are gone. One part type carries text, raw, url,
or data. A custom envelope rides in part data or in message metadata.

A2A v1.0 added contextId. It groups related tasks and messages. This
maps well to our thread_id. The old plan line about a DataPart is stale.
The plan must use part data and metadata instead.

A2A still has no workflow primitive. It delegates a single task. The
three-layer model still holds. MCP connects tools. A2A connects agents.
An orchestrator runs steps in process. Our flow package owns that role.

A2A has no epistemic typing. It has no semantic ack. It has no hop
caps. Our envelope semantics still add value across the wire.
Compose, do not compete. That decision from research-a2a.md stands.

## Agent definitions and the code map

The SDK has no agent runtime yet. It has the message plane. An agent
definition binds each concern to one block. The table below maps a
concern to its current block and its future block.

| Agent concern | Current block | Future block |
|---|---|---|
| Identity | envelope.Sign | identity |
| Capability | none | discovery |
| Membership | room | room |
| Wire | envelope | envelope |
| Semantic ack | envelope.Ack | envelope |
| Audit chain | envelope.VerifyThread | envelope |
| Multi-step work | none | flow |
| Remote delegation | none | a2a |
| Execution loop | none | agent |
| Tools | none | tools |
| Context memory | envelope.ContextRef | memory |

An agent definition is declarative. It states an identity, a key. It
states the capabilities. It states the room roles. It states the
transport. It states the workflow steps. Each statement binds to one
block in the table.

The a2a block is the transport adapter. It uses the official Go SDK.
The a2a-go SDK requirement decides the stdlib-only rule question. A
future plan must grant one exception. This report records that question,
it does not answer it.

## Recommendations

Keep the envelope and room split. Add the building-block rule to
AGENTS.md. Update the a2a plan to use part data. Route the agent
package through the delivery loop. Start the agent plan from the table
above. Update research-a2a.md with the v1.0 facts.

The phased build lives in docs/plans/agents/. Read the PHASES
framework there before any phase. Each phase has its own integration,
tdd, and perf test files.

## Open questions

- Should a2a-go break the stdlib-only rule? The plan review must decide.
- Which block owns the execution loop? The agent block or the flow?
- Does discovery reuse the A2A AgentCard shape? Or define its own?
- Should the identity block wrap a key file? Or stay stateless?

## References

- A2A specification: https://a2a-protocol.org/latest/specification/
- A2A Go SDK: https://github.com/a2aproject/a2a-go
- A2A project governance: https://github.com/a2aproject/A2A

See also docs/research-a2a.md and docs/plans/a2a.md for the older A2A
record. See docs/plans/flow.md for the workflow plan. See AGENTS.md for
the building-block rule.
