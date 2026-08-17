# Documentation

The docs tree gives every file a clear role. Start with this index.
Read the wire rationale, then the architecture, then the package
references, then the examples. Read the plans before any code change.
Leave the research record for last.

## Reading order

1. [README.md](README.md) — this index: what each doc is and when to read it.
2. [protocol-design.md](protocol-design.md) — the wire-protocol rationale: why the envelope is shaped this way.
3. [architecture.md](architecture.md) — the module map and the message flow.
4. [packages/envelope.md](packages/envelope.md) — the envelope package reference.
5. [packages/room.md](packages/room.md) — the room package reference.
6. [packages/machine.md](packages/machine.md) — the machine package reference.
7. [examples/envelope-flow.md](examples/envelope-flow.md) — the envelope walkthrough.
8. [examples/room-flow.md](examples/room-flow.md) — the room walkthrough.
9. [plans/](plans/) — the change contracts; read them before code changes.
10. [research-a2a.md](research-a2a.md) — the A2A research record; read it last.
11. [research-agents.md](research-agents.md) — the building-block, agent, and A2A v1.0 assessment; read it last.
12. [research-state-machine.md](research-state-machine.md) — the workflow state primitive assessment; read it last.

## Change contracts

Every plan in `plans/` is a change contract. The plan gate
(`scripts/check_plan.py`) requires one plan per top-level Go package.
A plan declares the goal, scope, API, tests, and verification of a
change before the code lands.

- [plans/TEMPLATE.md](plans/TEMPLATE.md) — the required plan skeleton.
- [plans/envelope.md](plans/envelope.md) — the envelope package plan.
- [plans/room.md](plans/room.md) — the room package plan.
- [plans/flow.md](plans/flow.md) — the future flow step-runner plan.
- [plans/machine.md](plans/machine.md) — the machine state plan; Phase 1 code ships.
- [plans/agents/](plans/agents/) — the phased agent-block build: the
  PHASES framework plus one small plan per phase, each with its own
  integration, tdd, and perf test files.
- [plans/a2a.md](plans/a2a.md) — the future a2a package plan.
- [plans/gates.md](plans/gates.md) — the gate-hardening plan.
- [plans/labels.md](plans/labels.md) — the label-ban plan.
- [plans/docs.md](plans/docs.md) — the docs restructure plan.
