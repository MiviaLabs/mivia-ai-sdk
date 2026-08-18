# Package reference: e2e

The e2e package is the end-to-end scenario suite. Each scenario
wires real high-level blocks together and drives one full run,
asserting the outputs every layer promised. Package suites prove
each block alone; e2e proves the handoffs. The harness below mirrors
`api/e2e.txt`.

## Harness

- `NewAgent(name, plan)` — builds an agent under a fresh identity
  and a one-capability card.
- `PrefixTool` — returns its prefix joined to the payload, so each
  step records a distinct, deterministic result.
- `EscalateTool` — fails with an error wrapping `agent.ErrEscalated`,
  so a wired `Ask` round trip can resolve it.
- `Recorder` — counts every observed event, in arrival order.
- `ThreadCapture` — confirms each step message and records the
  signed thread for `envelope.VerifyThread`.

## Scenarios

The scenarios live in `e2e/e2e_test/`, one file per composed
behavior:

- `pipeline_test.go` — one run through a sequential step, a panel
  wave, a sub-workflow, and a two-iteration loop.
- `thread_test.go` — thread integrity across hops, plus replay
  determinism on a fresh thread.
- `escalation_test.go` — an escalated step resolved by a human over
  the NDJSON transport, approved and declined.
- `taskrun_ceremony_test.go` — the ledger ceremony around one full
  pipeline run, with blocked and replayed tasks.

See [../plans/e2e.md](../plans/e2e.md) for the scenario map and the
growth backlog. The next scenarios land with their phases: two
agents over `dispatch` HTTP, a remote `a2aack`, MCP tools behind the
chain, and scheduled liveness.

## Invariants

- A scenario composes at least two high-level blocks. A single
  package's behavior belongs in its own suite.
- No scenario substitutes a stand-in for an SDK block. Tools,
  answers, and clocks may be scripted; blocks never.
- Every scenario fails when its wiring breaks. Each drop proves it
  once with a planted fault in a throwaway copy.
