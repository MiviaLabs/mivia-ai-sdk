# Phase 10: a2a client adapter

Status: future. Builds on phase 9. This phase adds the a2a-go client
adapter. It sends a task, reads the status, and fetches the result.
This is the first external network call. See `docs/research-agents.md`
for the Go SDK decision.

## Goal

Delegate a task to a remote agent through a2a-go. `Send` creates the
task. `Status` reads it. `Result` fetches the output. The envelope
arrives at the remote side intact.

## Scope

Inside: the client wrapper around a2aproject/a2a-go, and the task
lifecycle. Outside: the Agent Card discovery and the server. We remain
a client of a2a, not a server, in this version.

## API

- `type Client struct` wrapping the a2a-go client.
- `New(opts ...Option) (*Client, error)`
- `(*Client).Send(ctx context.Context, msg envelope.Message) (TaskHandle, error)`
- `(*Client).Status(ctx context.Context, h TaskHandle) (State, error)`
- `(*Client).Result(ctx context.Context, h TaskHandle) (envelope.Message, error)`
- `type TaskHandle struct` identifying the remote task.

The adapter imports the a2aproject/a2a-go module. Plan review must
accept the one exception to the stdlib-only rule. The signature verifies
again after every remote hop.

## Tests

Test files live in `a2a/a2a_test/`:

- `phase10_tdd_test.go` — the red-green cases for the adapter against a
  recorded transcript. Start with the assertions. Confirm they fail on
  the empty phase. Implement and watch them pass.
- `phase10_integration_test.go` — run a contract test against a
  recorded a2a server transcript. Send a signed message, poll the
  status, fetch the result. Verify the signature after the hop.
- `phase10_perf_test.go` — benchmark a full send-status-result cycle
  against the recorded transcript. Target under ten milliseconds.
  State the allocation budget.

## Verification

`make verify` passes. The coverage floor for `a2a` holds. The
`go.mod` gate in `scripts/check_gomod.py` rejects the new dependency,
so the plan must record the exception. See `docs/plans/a2a.md`.
