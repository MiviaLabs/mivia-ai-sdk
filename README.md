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
dependencies used by `a2aclient`, `mcp`, and `ledger` (the latter
only when the `ledger_sqlite` build tag is set).

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

Compose an agent from an identity, a capability card, and a one-step
plan, then run it. The step's content comes from a model provider, so
swap `stubCompleter` for a real one to change what it asks. `Run`
signs the step as an envelope message, a room checks the signer is a
member, and the receiver acks it back.

```go
package main

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// stubCompleter stands in for a real model client (OpenAI, Anthropic,
// a local model). Swap it for one that calls a real API.
type stubCompleter struct{}

func (stubCompleter) Name() string { return "stub" }

func (stubCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{
		Message: provider.Message{
			Role:    provider.RoleAssistant,
			Content: "Summarize the release notes in 3 bullets.",
		},
	}, nil
}

func (stubCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, fmt.Errorf("stub: streaming not implemented")
}

func main() {
	id, err := identity.New() // this agent's key pair
	if err != nil {
		panic(err)
	}
	receiver, err := identity.New() // the agent it talks to
	if err != nil {
		panic(err)
	}

	// Generate the step's content through a model provider, before the
	// plan builds. RunTurn dispatches to Chat or ChatStream; a real
	// Completer wraps a hosted API, a stub wraps a canned response.
	turn, err := provider.RunTurn(context.Background(), stubCompleter{}, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "Draft a one-line task for summarizing release notes."},
		},
	})
	if err != nil {
		panic(err)
	}

	card := discovery.Card{Name: "summarizer", Capabilities: []string{"summarize"}}
	plan, err := flow.New([]flow.Step{
		{ID: "ask", To: "asked", Payload: turn.Message.Content},
	}, nil)
	if err != nil {
		panic(err)
	}

	a, err := agent.New(id, card, plan) // compose: identity + capability + plan
	if err != nil {
		panic(err)
	}

	rm, err := room.New("standup", receiver.Signer())
	if err != nil {
		panic(err)
	}
	if err := rm.Admit(id.Signer(), receiver.Signer()); err != nil {
		panic(err)
	}

	// wait plays the receiving agent: check membership, then build and
	// confirm an ack.
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		if err := rm.Accepts(msg); err != nil {
			return envelope.Ack{}, err
		}
		ack, err := envelope.NewAck(msg, receiver.Signer(), "got it: "+msg.Payload)
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}

	m, err := machine.New("queued", machine.Transition{From: "queued", To: "asked", Trigger: "send"})
	if err != nil {
		panic(err)
	}

	// Run emits to bus; a caller must subscribe before it fires.
	bus := events.New()
	noop := func(context.Context, events.Event) error { return nil }
	_ = bus.Subscribe(agent.MessageDeliveredEvent, noop)
	_ = bus.Subscribe(agent.MessageAckedEvent, noop)
	_ = bus.Subscribe(agent.ThreadVerifiedEvent, noop)

	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, rm.ID(), nil)
	if err != nil {
		panic(err)
	}
	fmt.Println("status:", status) // prints "status: asked"
}
```

See [docs/examples/agent-dispatch.md](docs/examples/agent-dispatch.md)
for the full walkthrough, including a heartbeat monitor and captured
events. See [docs/examples/envelope-flow.md](docs/examples/envelope-flow.md)
and [docs/examples/room-flow.md](docs/examples/room-flow.md) for the
envelope and room blocks on their own.

## Concepts

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

All invariants are enforced in code by `Message.Validate` and
`Ack.Validate`, never only documented. The wire contract is pinned by
conformance vectors in `envelope/testdata/vectors/`.

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

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.
