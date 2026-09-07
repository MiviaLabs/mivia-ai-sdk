# Documentation

`mivia-ai-sdk` is a Go module of composable building blocks for
tool-calling agents. `agentloop` drives the tool-calling loop over a
model provider and a tool registry. The remaining blocks compose
around it: envelope, room, machine, flow, events, a2a, a2aclient,
dispatch, tools, context/budget, schema, mcp, ledger, memory,
provider, provider/anthropic, context/plan, context/ref, channel,
trace, scheduler, workflow, workflow/run, subagent, and workspace.
Each package covers one concern and composes through its exported
API. This doc tree covers the module map, the wire-protocol
rationale, every package's exported surface, and runnable-style
walkthroughs.

## Start here

- [architecture.md](architecture.md) — the single design reference:
  the module map, the message flow, why the envelope is shaped this
  way, the gate system, and the invariants.

## Agents and subagents

The composition stack, bottom to top:

- [packages/workflow.md](packages/workflow.md) — one identity, one card,
  one plan, driven through signed, acked, hash-chained messages.
- [packages/workflow/run.md](packages/workflow/run.md) — the config-struct
  layer over `workflow.Run`: tools, store, artifacts, ask, budget, and
  monitor wired by one `Options` value.
- [packages/subagent.md](packages/subagent.md) — the SDK's blocks as
  tools: a runner becomes a spawnable subagent, spawns run in
  parallel behind a depth guard, and a signed-message mailbox
  carries both directions.
- [packages/dispatch.md](packages/dispatch.md) and
  [packages/a2aclient.md](packages/a2aclient.md) — the receive and remote
  halves: an HTTP envelope endpoint, and a remote A2A task as one
  step's ack.

Internal tools an agent can be given, all optional, all registered
into a `tools.Registry` through `subagent`:

- `FlowTool` — run a flow plan, report the final status.
- `LedgerTool` — record one completed task through the taskrun
  ceremony, report key state.
- `MemoryTool` — store and fetch blobs under content-addressed refs.
- `RoomTool` — admit, remove, promote, list, and query membership.
- `SchedulerTool` — schedule and cancel one bound job.
- `HeartbeatTool` — beat, alive, and dead against a monitor.
- `DiscoveryTool` — match one capability card against a need.
- `ProviderTool` — one model turn through a caller's Completer.
- `TriggerTool` — fire a named trigger.
- `ChannelTool` — ask a human through a Notifier.
- `SendTool` and `InboxTool` — the mailbox plane's two ends.

## Package reference

- [packages/envelope.md](packages/envelope.md) — the wire unit: the message, its metadata types, the semantic ack, signing, and the agent key (`Identity`, `New`, `Load`).
- [packages/events.md](packages/events.md) — the in-process reaction bus and the hook registry: typed events, one subscription set, and vetoable lifecycle hooks.
- [packages/machine.md](packages/machine.md) — the state-machine building block: the status model and the move dispatch.
- [packages/room.md](packages/room.md) — standing groups for messages: the roster, the roles, and message admission.
- [packages/flow.md](packages/flow.md) — the declarative workflow building block: the step graph, the cycle check, the runner, the capability card, and the liveness monitor.
- [packages/a2a.md](packages/a2a.md) — the A2A v1.0 mapping: a message part shape, and the functions that map an envelope message onto it and back.
- [packages/a2aclient.md](packages/a2aclient.md) — the a2a-go client adapter plus the remote step ack: send a message as a remote task, and resolve one gated step through send, poll, result, verify, and ack.
- [packages/dispatch.md](packages/dispatch.md) — the NDJSON envelope endpoint: an `http.Handler` that runs the receive ladder per line and answers with confirmed acks, plus `Send`, the client-side counterpart.
- [packages/tools.md](packages/tools.md) — the tool registry: named actions a step can resolve and run by name, plus execution-risk markers, scoping, and approval gating.
- [packages/budget.md](packages/budget.md) — a pure, storage-agnostic budget check for one model call's context: a byte cap, an event-count cap, and `Fits`.
- [packages/ref.md](packages/ref.md) — the canonical content-reference minter and parser: `HashPrefix`, `Digest`, `Mint`, and `IsRef`.
- [packages/schema.md](packages/schema.md) — the JSON Schema compile/validate/corrective-message primitive: `Compile` admits and compiles a schema, `Validate` checks a payload against it, and `Corrective` renders a bounded, model-facing correction message.
- [packages/mcp.md](packages/mcp.md) — the MCP tool-calling client: connect to a server, list its tools, and call them, over stdio or streamable HTTP.
- [packages/ledger.md](packages/ledger.md) — the durable-task-admission primitive plus the one-task ceremony: idempotency-keyed admission, a leased claim with a fence, dependency blocking, and admit-claim-run-complete in one call.
- [packages/memory.md](packages/memory.md) — the content-addressed context store plus the principal-scoped spool: content refs, byte-budget eviction, grant-scoped oversized content, and tool wrappers.
- [packages/provider.md](packages/provider.md) — the model provider interface plus usage accounting and the routing registry: the `Completer` contract, per-session usage totals, and ordered named-provider fallback.
- [packages/provider/anthropic.md](packages/provider/anthropic.md) — the Anthropic Messages API adapter: concrete `Completer`, `ContextAccountant`, and `ReasoningPolicy` implementation.
- [packages/plan.md](packages/plan.md) — manages token budget windows and history compaction, plus the summarizer: a token `Window`, compaction thresholds, an EWMA-calibrated estimator, and one bounded summary call.
- [packages/channel.md](packages/channel.md) — the ask-and-wait shape: a `Question`, a typed `Answer`, and the caller-implemented `Notifier` that connects them.
- [packages/trace.md](packages/trace.md) — the structured-trace primitive: a `Span` records one named operation, a `Tracer` links spans through `ctx`, and `SpanFrom` reads the current span back.
- [packages/scheduler.md](packages/scheduler.md) — the invoke-on-schedule primitive plus the trigger registry: due-job firing, and the `Condition`/`Action` vocabulary that maps one name to a guarded call.
- [packages/workflow.md](packages/workflow.md) — the composition layer: one identity, one capability card, and one step plan, driven through signed, acked, hash-chained messages.
- [packages/workflow/run.md](packages/workflow/run.md) — the config-struct composition layer: one `Options` value validated and wired into a `Runner` that drives `workflow.Run`.
- [packages/subagent.md](packages/subagent.md) — the SDK's blocks as tools: a runner becomes a spawnable subagent, `RunAll` runs several at once, internal tools expose the blocks, and a signed-message mailbox carries both directions.
- [packages/workspace.md](packages/workspace.md) — filesystem confinement plus secret path matching: `Open` binds a handle to a root, rejects traversal or symlink escapes, and a `Matcher` flags configured secret paths.
- [packages/agentloop.md](packages/agentloop.md) — a tool-calling loop over a `provider.Completer` and a `tools.Registry`: offer tools, run model-requested calls, repeat until the model stops asking or a bound trips.

The core module also ships `x/`, a nested sub-module of orphaned
packages — `contextstate`, `contextsession`, `longtermmemory`,
`skills`, `envfile`, and `runconfig` — kept alive for the consumers
that import them but not advertised as SDK surface.

## Examples

- [examples/envelope-flow.md](examples/envelope-flow.md) — one message: create, sign, encode, decode, verify, then tamper.
- [examples/room-flow.md](examples/room-flow.md) — admission in a standing group: create, admit, send, accept, and a stranger's rejection.
- [examples/machine-flow.md](examples/machine-flow.md) — a three-status machine with a guarded transition.
- [examples/events-bus.md](examples/events-bus.md) — one bus, two subscribers, two event sources.
- [examples/heartbeat-liveness.md](examples/heartbeat-liveness.md) — two tracked ids, one going silent past the timeout.
- [examples/flow-runner.md](examples/flow-runner.md) — a step graph driven end to end through the runner.
- [examples/workflow-dispatch.md](examples/workflow-dispatch.md) — the full end-to-end walkthrough: an agent dispatching a plan through signed, acked messages.
- [examples/channel-ndjson-stdio.md](examples/channel-ndjson-stdio.md) — a `channel.Notifier` speaking newline-delimited JSON over stdin and stdout, a sibling consumer desktop app's own wire convention.
- [examples/flow-panel-concurrent.md](examples/flow-panel-concurrent.md) — one panel wave in depth: two steps firing the same transition row at the same time.
- [examples/flow-branch-routing.md](examples/flow-branch-routing.md) — a branch step's `Route` keeping one of two direct dependents at run time.
- [examples/flow-retry-policy.md](examples/flow-retry-policy.md) — a flaky step retried under a `RetryPolicy` until it succeeds.
- [examples/flow-loop-driving.md](examples/flow-loop-driving.md) — a step repeating its `Sub` child workflow under a `LoopPolicy` until a guard stops it.
- [examples/flow-fallback-admission.md](examples/flow-fallback-admission.md) — a failed step caught by an `AdmissionOnFailed` fallback instead of aborting the run.
- [examples/flow-checkpoint-resume.md](examples/flow-checkpoint-resume.md) — a run paused by canceling `ctx`, then resumed from a stored `Checkpoint`.
- [examples/tools-scope-approval.md](examples/tools-scope-approval.md) — a privileged tool denied by a `Scope`'s allowlist, then gated behind an `Approve` callback.
- [examples/memory-context-store.md](examples/memory-context-store.md) — three blobs put under a byte budget, the oldest evicted to make room for the third.
- [examples/provider-completer-turn.md](examples/provider-completer-turn.md) — one hand-written `Completer` driven through `RunTurn`'s sync and streamed dispatch paths.
- [examples/ledger-admission-lifecycle.md](examples/ledger-admission-lifecycle.md) — admit, claim, renew, a stale-lease takeover, complete as failed, and a blocked dependent.
- [examples/workflow-composition.md](examples/workflow-composition.md) — `workflow.Run` composed with `provider`, `tools`, `mcp`, `ledger`, and `memory`, shipped as both a Markdown fence and a committed, runnable package under `docs/examples/`.
- [examples/scheduler-recurring-jobs.md](examples/scheduler-recurring-jobs.md) — two recurring jobs on one `Scheduler`, one of them failing, observed through an `events.Bus`.
- [examples/trigger-condition-action.md](examples/trigger-condition-action.md) — a named `Condition`/`Action` pair on a `scheduler.Registry`, fired once unmet and once met.
- [examples/discovery-capability-match.md](examples/discovery-capability-match.md) — a parsed capability card checked against a matching and a non-matching request.
- [examples/identity-agent-key.md](examples/identity-agent-key.md) — a generated agent key signing an envelope message and matching its own hex signer.
- [examples/a2a-mapping-roundtrip.md](examples/a2a-mapping-roundtrip.md) — a signed message mapped to an A2A `Part` and back, verified bit-for-bit.
- [examples/workflow-run.md](examples/workflow-run.md) — a two-step plan run through the `workflow/run` composition layer with a tool, an artifact, and a store.
- [examples/taskrun.md](examples/taskrun.md) — the `taskrun` ledger ceremony: a successful build, a failed build, and a replay sentinel.
- [examples/a2aack.md](examples/a2aack.md) — one gated step resolved through a remote A2A task via `a2aclient.Wait`, confirmed by the caller's own key.
- [examples/dispatch.md](examples/dispatch.md) — one signed message posted to a live `dispatch.Endpoint`, admitted, handled, and confirmed over NDJSON.

## Internal records

`docs/plans/` holds internal development records: the change contract
behind each package. They are not part of this documentation.
