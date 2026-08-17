# Documentation

`mivia-ai-sdk` is a Go module of composable building blocks for
agent-to-agent messaging: envelope, room, machine, flow, events,
heartbeat, identity, discovery, and agent. Each package covers one
concern and composes through its exported API. This doc tree covers
the wire protocol, the module map, every package's exported surface,
and runnable-style walkthroughs.

## Start here

- [architecture.md](architecture.md) — the module map and the message flow.
- [protocol-design.md](protocol-design.md) — the wire-protocol rationale: why the envelope is shaped this way.

## Package reference

- [packages/envelope.md](packages/envelope.md) — the wire unit: the message, its metadata types, the semantic ack, and signing.
- [packages/events.md](packages/events.md) — the in-process reaction bus. A caller emits a typed event; a subscriber runs one callback per event.
- [packages/machine.md](packages/machine.md) — the state-machine building block: the status model, the move dispatch, and the JSON wire form.
- [packages/identity.md](packages/identity.md) — one agent key: an ed25519 pair, the key-file load, the invariant check, and the hex signer string.
- [packages/discovery.md](packages/discovery.md) — the capability card: a name, an optional description, and a capability list.
- [packages/heartbeat.md](packages/heartbeat.md) — liveness tracking by time: the last beat per id, and which ids have gone silent.
- [packages/room.md](packages/room.md) — standing groups for messages: the roster, the roles, and message admission.
- [packages/flow.md](packages/flow.md) — the declarative workflow building block: the step graph, the cycle check, and the runner.
- [packages/agent.md](packages/agent.md) — the composition layer: one identity, one capability card, and one step plan, driven through signed, acked, hash-chained messages.

## Examples

- [examples/envelope-flow.md](examples/envelope-flow.md) — one message: create, sign, encode, decode, verify, then tamper.
- [examples/room-flow.md](examples/room-flow.md) — admission in a standing group: create, admit, send, accept, and a stranger's rejection.
- [examples/machine-flow.md](examples/machine-flow.md) — a three-status machine with a guarded transition.
- [examples/events-bus.md](examples/events-bus.md) — one bus, two subscribers, two event sources.
- [examples/heartbeat-liveness.md](examples/heartbeat-liveness.md) — two tracked ids, one going silent past the timeout.
- [examples/flow-runner.md](examples/flow-runner.md) — a step graph driven end to end through the runner.
- [examples/agent-dispatch.md](examples/agent-dispatch.md) — the full end-to-end walkthrough: an agent dispatching a plan through signed, acked messages.

## Internal records

`docs/plans/` holds internal development records: the change contract
behind each package. They are not part of this documentation.

`docs/research-*.md` files are internal research notes. They are not
part of the public reading path, and this index does not link them
individually.
