# Research: A2A integration and workflows

Date: 2026-08. Question: how should this SDK compose with the A2A
protocol for delegation, and does A2A give us workflows?

Update: A2A reached v1.0 in 2026-03. The facts below still hold for
version negotiation, discovery, tasks, and streaming. The v1.0 part
model changed: one Part type carries text, raw, url, or data. The old
kind field and the separate part classes are gone. A future plan must
embed the envelope in Part data, not in a DataPart. A2A v1.0 added
contextId, which maps to our thread_id. The official Go SDK is
a2aproject/a2a-go. See [research-agents.md](research-agents.md) for the
full v1.0 assessment and the agent plan.

## What A2A provides

Verified against the A2A specification and current analyses:

- Discovery through Agent Cards (JSON at a well-known URI).
- Task delegation with a lifecycle: submitted, working,
  input-required, completed, failed, canceled.
- Messages with typed parts (text, data, file).
- Artifacts as task outputs.
- Streaming (SSE) and push notifications for long tasks.
- An extension mechanism since v1.0.1.

## What A2A does not provide

- Epistemic typing. A2A messages carry content, not certainty.
- Semantic acknowledgment. A2A confirms receipt, not understanding.
- Drift control. No hop caps, no content-addressed context.
- Governance primitives. No challenge, no escalation, no audit chain.
- **Workflows.** A2A delegates single tasks. It has no multi-step
  orchestration primitive.

## Industry consensus on layering

The 2026 landscape settled into three layers:

1. MCP connects agents to tools (vertical).
2. A2A connects agents to agents across vendors (horizontal).
3. An orchestrator (LangGraph and similar) runs workflows in process.

Sources: [IoT Digital Twin PLM layer analysis](https://iotdigitaltwinplm.com/multi-agent-orchestration-mcp-a2a-langgraph-2026/),
[Apigene decision framework](https://apigene.ai/blog/mcp-vs-a2a-when-to-use-each-protocol),
[governance gap analysis, arXiv 2606.31498](https://arxiv.org/html/2606.31498).

## Concept mapping

| This SDK | A2A |
|---|---|
| `thread_id` | Task ID + contextId |
| `room` | no equivalent (roster stays ours) |
| Message payload | Message with parts |
| envelope (full struct) | carried in a DataPart |
| semantic Ack | no equivalent; rides in the DataPart |
| task lifecycle | Task states |

## Decision

Compose, do not compete. The envelope keeps its semantics. A2A
becomes one transport for cross-service delegation. See
docs/plans/a2a.md.

## Workflows without an engine

A2A has no workflows, and building a Temporal-style engine would be
overengineering for this SDK. The minimal design:

- A workflow is a declarative list of steps with dependencies.
- Each step is one envelope request to an agent (in process) or one
  A2A task (remote).
- Steps run in topological levels. Independent steps may run in
  parallel later; sequential first.
- The semantic ack gates execution: no step runs unconfirmed.
- The thread hash chain gives the audit trail for free.

Explicitly out of scope for a first version: retries, compensation,
scheduling, persistence. See docs/plans/flow.md.
