# Phase 13: agent execution loop

Status: future. Builds on phase 12. This phase runs the agent. The
loop walks the agent plan, fires each step, and waits on the ack gate.
It escalates when a step challenges or asks for a human.

## Goal

Run an agent start to finish. Each step becomes an envelope message.
A request waits for the confirmed ack. A challenge or an escalate
routes to a human. The run returns the final status.

## Scope

Inside: the run entry point, the step dispatch, the ack wait, and the
escalation path. Outside: the tool registry and the memory store.
Those bind in phases 14 and 15.

## API

- `(*Agent).Run(ctx context.Context) error`

`Run` walks the step plan in dependency order. It signs each message
with the agent identity. It sends through the a2a adapter or the
in-process runner. A step that waits on an ack blocks until the ack
confirms. An escalation returns to the caller for a human.

The semantic ack is the gate. No step acts on an unconfirmed request.
This is the core rule from `docs/protocol-design.md`.

## Tests

Test files live in `agent/agent_test/`:

- `run_test.go` — the red-green cases for `Run`. Start with
  the assertions. Confirm they fail on the empty phase. Implement and
  watch them pass.
- `run_integration_test.go` — run an agent over a two-step plan.
  Prove the ack confirms before the second step. Feed an escalate and
  prove the run routes to a human. Run under `go test -race`.
- `run_bench_test.go` — benchmark a two-step run with an ack
  round trip. Target under two milliseconds. State the allocation
  budget.

## Verification

`make verify` passes. The coverage floor for `agent` holds. The ack
gate projection in `docs/protocol-design.md` stays accurate. The tools
and the memory bind in the next phases.
