<p align="center">
  <img src="docs/mivia-logo.png" alt="mivia" width="120">
</p>

<h1 align="center">mivia-ai-sdk</h1>

<p align="center">Go SDK for building AI agents and workflows. Composable blocks, not a monolith.</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8.svg" alt="Go 1.25+">
</p>

Go SDK for building AI agents and workflows. A set of composable
building blocks, not a monolith. Most packages use the standard
library only. The `go.mod` file lists the small set of third-party
dependencies used by `a2aclient`, `a2aloopback`, `mcp`, and `ledger`
(the latter only when the `ledger_sqlite` build tag is set).

## Features

- **envelope** — AI message protocol: typed intents, epistemic labels,
  ed25519 signatures, semantic acks, thread audit chains
- **room** — standing groups: roster, roles, moderator-gated admission
- **machine** — state machines: typed statuses, guard-gated transitions,
  entry and exit actions, JSON wire form
- **flow** — step graphs: sequential steps, concurrent panels, branch
  routing, failure fallback, bounded retry, loop-driving repeat,
  checkpoint pause and resume
- **events** — in-process reaction bus: typed names, subscribe, emit
- **identity** — agent keys: ed25519 key wrap, key-file load, hex signer
- **heartbeat** — liveness tracking: per-id beats, alive/dead checks
- **discovery** — capability cards: parse, validate, match
- **a2a** / **a2aclient** — A2A v1.0 envelope mapping and a client
  adapter over `a2a-go`'s gRPC transport
- **a2aack** — a remote A2A task round trip as one step's ack wait
- **dispatch** — an HTTP envelope endpoint: NDJSON lines in, the
  receive ladder per line, ack or error lines out, plus a Send client
- **tools** — a named-action registry: execution-risk markers, scoped
  allow/deny lists, synchronous approval gating
- **contextbudget** — a byte and event-count budget check for one
  model call's context
- **mcp** — an MCP tool-calling client over stdio or streamable HTTP
- **ledger** — durable task admission: idempotency keys, leased
  ownership, fenced takeover, dependency blocking, an optional SQLite
  store
- **durablefence** — a test-only conformance kit proving claim, lease,
  and fence invariants
- **memory** — a content-addressed context store with a byte-budget
  eviction policy
- **provider** — a model-provider interface: one chat-completion turn,
  sync or streamed
- **channel** — an ask-and-wait shape for approval, escalation, and
  query flows, with an NDJSON-over-stdio reference transport
- **scheduler** — generic invoke-on-schedule: interval and one-shot
  jobs against a caller-supplied bus
- **trigger** — the shared "condition fired, so run this" vocabulary
- **agent** — the composition layer: wires blocks into an agent
- **agentrun** — the config-struct composition layer: one `Options`
  value validates and wires tools, store, artifacts, ask, budget,
  and monitor behind a plan-versus-machine matrix check
- **taskrun** — the ledger ceremony as one call: admit, claim, run
  the work, complete, with replay and blocked sentinels
- **subagent** — the SDK's blocks as tools: a runner spawns as a
  subagent tool, spawns run in parallel behind a depth guard, and a
  signed-message mailbox carries both directions between
  orchestrators, subagents, and humans
- **e2e** — the end-to-end scenario suite: real blocks wired
  together, one full run per scenario, outputs asserted across the
  handoffs

The GitHub remote for this repo is private.

## Why

Natural language is the right payload format between AI agents. Both
sides parse it well. The failures in agent exchange are not in the
payload. They are in the missing metadata:

- A guess and a checked fact look the same in prose.
- A misunderstanding is silent until it shows up in the output.
- Shared context is re-sent in full, or assumed and then it drifts.
- Trusted and untrusted content arrive in the same register.
- Nothing proves who sent a message, or that a thread was not edited.

The envelope block fixes those points. The remaining blocks compose
around it. See [docs/architecture.md](docs/architecture.md) for the
wire rationale and the module map. See [docs/README.md](docs/README.md)
for the doc index and the reading order.

## The blocks

Each block is one top-level package with one concern. A block is
replaceable and testable on its own. Compose blocks through their
public API. All twenty-seven ship.

- **envelope** — the wire unit: Message, Ack, Sign, VerifyThread.
- **room** — standing groups: roster, roles, message admission.
- **machine** — the status model: Status, Trigger, Guard, Transition,
  Fire, and the JSON wire form.
- **flow** — the step graph and the runner: Step, Panel, Definition,
  Run, Confirm, Outcome, Admission, Route, Failure, RetryPolicy,
  LoopPolicy, Checkpoint, Resume.
- **events** — the in-process reaction bus. Caller-owned; no shared
  bus.
- **identity** — the agent key wrap: Identity, New, Load, Sign,
  Signer.
- **heartbeat** — liveness tracking: Monitor, Beat, Alive, Dead.
- **discovery** — capability cards: Card, Parse, Validate, Match.
- **a2a** — A2A v1.0 envelope mapping. The a2a-go client lives in
  `a2aclient`.
- **a2aclient** — the a2a-go client adapter: send a task, poll its
  status, fetch its result over gRPC.
- **a2aloopback** — a gRPC A2A loopback test fixture: Loopback. No
  production package may import it.
- **a2aack** — the remote step ack: a Wait func over one A2A task.
- **dispatch** — the envelope endpoint: Handler, New, Send, and the
  per-line receive ladder.
- **tools** — the named-action registry: Tool, Registry, ExecutionProfile,
  Scope, RunScoped.
- **contextbudget** — a pure budget check: Limits, Fits.
- **mcp** — the MCP tool-calling client: Client, Connect, ListTools,
  CallTool.
- **ledger** — durable task admission: Ledger, Admit, Claim, Renew,
  Takeover, Complete.
- **durablefence** — a test-only conformance kit: Scenario, RunAll.
- **memory** — the content-addressed context store: Store, Put, Get.
- **provider** — the model provider interface: Completer, RunTurn.
- **channel** — the ask-and-wait shape: Question, Answer, Notifier.
- **scheduler** — invoke-on-schedule: Job, Schedule, Scheduler.
- **trigger** — condition-fired dispatch: Condition, Action, Registry.
- **agent** — the composition layer. An agent wires the blocks; a
  block never imports the agent.
- **agentrun** — the config-struct layer over `agent.Run`: Options,
  New, Runner, ValidateMatrix, Artifacts, PayloadOf.
- **taskrun** — the ledger ceremony as one call: Run, Options, Task.
- **subagent** — the blocks as tools: AsTool, RunAll, ten internal
  tools, Mailbox, SendTool, InboxTool.
- **e2e** — the scenario harness: NewAgent, PrefixTool, EscalateTool,
  Recorder, ThreadCapture.

## Install

```bash
go get github.com/MiviaLabs/mivia-ai-sdk
```

## Quick start

Compose a pipeline from an identity, a capability card, a two-step
plan, and one tool per step. One `agentrun.Options` literal wires it
all: `New` validates the plan-versus-machine transition matrix and
the tool names, subscribes the bus, and builds the ack chain. `Run`
signs each step as an envelope message, chains it into one
verifiable thread, runs the step's tool, and records the result.

```go
package main

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// prefixTool returns its prefix joined to the step's payload, so each
// step records a distinct, deterministic result.
type prefixTool struct {
	name   string
	prefix string
}

func (t prefixTool) Name() string { return t.name }

func (t prefixTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: t.prefix + s}, nil
}

func main() {
	artifacts := &agentrun.Artifacts{}
	plan, err := flow.New([]flow.Step{
		{ID: "review", To: "reviewed", Payload: "invoice 42"},
		{ID: "ship", To: "shipped", Needs: []string{"review"},
			PayloadFrom: agentrun.PayloadOf("review", artifacts)},
	}, nil)
	if err != nil {
		panic(err)
	}

	id, err := identity.New()
	if err != nil {
		panic(err)
	}
	a, err := agent.New(id, discovery.Card{
		Name: "invoice-agent", Capabilities: []string{"invoice.review"},
	}, plan)
	if err != nil {
		panic(err)
	}

	reg := tools.New()
	_ = reg.Add(prefixTool{name: "review", prefix: "reviewed: "})
	_ = reg.Add(prefixTool{name: "ship", prefix: "shipped: "})

	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "reviewed", Trigger: "run"},
		machine.Transition{From: "reviewed", To: "shipped", Trigger: "run"},
	)
	if err != nil {
		panic(err)
	}

	// One Options literal wires everything. New validates the
	// plan-versus-machine matrix and the tool names, subscribes the
	// bus, and builds the ack chain that runs each step's tool.
	runner, err := agentrun.New(agentrun.Options{
		Agent: a, Machine: m, Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		panic(err)
	}

	status, _, err := runner.Run(context.Background(), "thread-1", machine.InOut{})
	if err != nil {
		panic(err)
	}
	ship, _ := artifacts.Get("ship")
	fmt.Println("status:", status)      // prints "status: shipped"
	fmt.Println("ship artifact:", ship) // prints "shipped: reviewed: invoice 42"
}
```

Step two reads step one's result through `PayloadOf`, so the two
tools chain without shared variables. From here the stack widens:

- A model in the loop: swap the hand-written tool for
  `subagent.ProviderTool` over a caller-supplied `Completer`; see
  [docs/examples/provider-completer-turn.md](docs/examples/provider-completer-turn.md).
- A subordinate agent as a step: wrap another runner with
  `subagent.AsTool` and register it like any tool; see
  [docs/packages/subagent.md](docs/packages/subagent.md).
- A second agent answering over HTTP: the `dispatch` endpoint and
  its `Send` client carry the same envelope messages across a
  process boundary; see [docs/packages/dispatch.md](docs/packages/dispatch.md).
- The raw exchange, with rooms and hand-written acks:
  [docs/examples/agent-dispatch.md](docs/examples/agent-dispatch.md)
  keeps the full walkthrough the quick start replaced, and
  [docs/examples/envelope-flow.md](docs/examples/envelope-flow.md)
  and [docs/examples/room-flow.md](docs/examples/room-flow.md) cover
  the envelope and room blocks alone.

## Concepts

### Wire concepts

These concepts belong to the envelope block.

- **Intent** — what the message does: `assert`, `query`, `request`,
  `challenge`, `retract`, `escalate` (route to a human).
- **Epistemic label** — how the sender knows: `verified`, `inferred`,
  `assumed`, `untrusted-input`. `verified` requires `provenance.source`
  plus at least one `provenance.evidence` content ref, so the strong
  label is pinned to checkable artifacts, not just claimed.
- **Confidence** — self-reported certainty, 0.0 to 1.0.
- **Thread** — `thread_id` groups one conversation or task. Required;
  unnamed threads are how agents lose the plot.
- **Addressing** — `room` names a standing group, `to` lists
  recipients: one entry is 1-to-1, several is multicast, empty is
  broadcast to the room. `signer` is the sender.
- **Membership** — the `room` package holds the roster: moderator-gated
  `Admit`/`Remove`/`Promote`, `Leave`, and `Room.Accepts` which admits
  a message only when signer and recipients are members. Group acks
  are attributed: every `Ack` carries `from`.
- **Context refs** — shared context addressed by canonical content hash
  (`sha256:` + 64 lowercase hex), not re-sent.
- **Audit chain** — `prev_hash` links each message to `Hash()` of the
  previous one in the thread; tampering is detectable.
- **Authentication** — `Sign`/`VerifySignature` (ed25519) prove who
  sent a message and that no field changed after signing.
- **Provenance** — where the content comes from, with a hop chain.
- **Hop cap** — `max_hops` bounds relays, because semantic error
  accumulates per hop.
- **Semantic ack** — the receiver restates the message in compressed
  form (`pending`); the sender confirms or corrects before the receiver
  acts. Required for every `request`, optional for other intents through
  `AckRequired`.

All wire invariants are enforced in code by `Message.Validate` and
`Ack.Validate`, never only documented. The wire contract is pinned by
conformance vectors in `envelope/testdata/vectors/`.

### Composition concepts

These concepts belong to the layers above the envelope.

- **Pipeline** — one `agentrun.Options` literal composes an agent,
  a machine, tools, and an artifact bag. `New` validates the
  plan-versus-machine transition matrix and every gated step's tool
  name before anything runs.
- **Ack chain** — each gated step runs its tool by step ID, stores
  the string result under a content ref, and confirms a signed ack.
  A rejected ack fails the run; an escalated tool routes to one
  human ask.
- **Orchestrator and subagent** — `subagent.AsTool` wraps a built
  runner as a step tool. Spawned runs get fresh threads; a
  context-carried depth bound, default three, stops recursive
  spawns in code.
- **Message plane** — a bounded `Mailbox` carries signed messages
  between orchestrators, subagents, and humans. `Deliver` validates
  every message before it lands; `Take` drains in delivery order.
- **Durable task** — ledger records move through `pending`,
  `claimed`, `completed`, `failed`, and `blocked`. A monotonic fence
  token makes a dispossessed owner's late write fail; a failed
  dependency blocks its dependents at completion and at admission.
- **Receive ladder** — a `dispatch` endpoint runs each NDJSON line
  through decode, verify, admit, resolve, and handle, failing fast
  per line. One bad line answers an error object; later lines still
  process.

The composition invariants are enforced in code too: ledger's
`TaskState.Validate` and compare-and-swap, subagent's depth check,
dispatch's fixed ladder order, and agentrun's matrix walk.

## Layout

```text
envelope/            message envelope — one package per concern
  doc.go             package map: what lives where
  message.go         Message, Intent, Epistemic, validation, Encode/Decode
  ack.go             Ack, the semantic-ack flow
  sign.go            ed25519 Sign / VerifySignature
  thread.go          VerifyThread, the hash-chain check for an ordered thread
  testdata/vectors/  conformance vectors pinning the wire contract
room/                standing groups: roster, roles, message admission
machine/             status model: triggers, guards, transitions, wire form
flow/                step graph and runner: panels, routing, retry, loop, checkpoint
events/              in-process reaction bus: typed names, Subscribe, Emit
identity/            agent key wrap: ed25519 pair, Load, Sign, Signer
heartbeat/           liveness tracking: Monitor, Beat, Alive, Dead
discovery/           capability cards: Card, Parse, Match
a2a/                 A2A v1.0 envelope mapping: ToPart, FromPart
a2aclient/           the a2a-go client adapter: Send, Status, Result
a2aloopback/         gRPC A2A loopback test fixture: Loopback
a2aack/              remote step ack: a Wait func over one A2A task
dispatch/            envelope endpoint: NDJSON ladder, Send client
tools/               named-action registry: profiles, scope, approval gating
contextbudget/       a byte and event-count budget check: Limits, Fits
mcp/                 MCP tool-calling client: Connect, ListTools, CallTool
ledger/              durable task admission: Admit, Claim, Renew, Takeover, Complete
durablefence/        test-only conformance kit: Scenario, RunAll
memory/              content-addressed context store: Put, Get
provider/            model provider interface: Completer, RunTurn
channel/             ask-and-wait shape: Question, Answer, Notifier
scheduler/           invoke-on-schedule: Job, Schedule, Scheduler
trigger/             condition-fired dispatch: Condition, Action, Registry
agent/               composition layer: wires blocks into an agent
agentrun/            config-struct composition: Options, Runner, ValidateMatrix
taskrun/             ledger ceremony as one call: Run around a work func
subagent/            blocks as tools: AsTool, RunAll, internal tools, mailbox
e2e/                 end-to-end scenario harness and suite
docs/                index + architecture + package docs + examples
api/                 exported-surface locks; check_api diffs them
policy/              layers.json: the allowed internal import edges
scripts/             gates: docs, structure, deps, plan, api, semgrep
semgrep/             pattern rules for the Semgrep scan
.semgrepignore       Semgrep ignore list; test files are scanned again
.githooks/           pre-commit runs make verify-fast on the staged snapshot
Makefile             make verify, make verify-fast, make verify-ledger-sqlite,
                     make bench, make api-update, make install-hooks
AGENTS.md            contribution rules for AI and human agents
```

Root holds no Go code. New concerns get new subpackages.

## Development

```bash
make install-hooks   # once per clone; sets core.hooksPath to .githooks
make verify-fast     # fast tier: fmt, vet, test, gates, semgrep scan
make verify          # full tier: verify-fast, coverage floor, semgrep
                     # probes, and the SQLiteStore tier over its build tag
```

The pre-commit hook runs `make verify-fast` on the staged snapshot.
It never runs the full suite twice.

Contribution rules (comment style, layout, limits, no dependencies)
live in [AGENTS.md](AGENTS.md).

## Author & Contributors

- **Maciej (Mac) Lisowski** — *Author / Lead Architect* ([@mac-lisowski](https://github.com/mac-lisowski))

Contributions are welcome! See [AGENTS.md](AGENTS.md) for contribution rules.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.

