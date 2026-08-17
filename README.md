# mivia-ai-sdk

Proof-of-concept Go SDK for an agent stack. It is a set of composable
building blocks, not a monolith. The core block is a message envelope:
natural-language payloads inside machine-checkable, authenticatable
metadata. The other blocks add standing groups, a status model, a step
graph, and an event bus.

Status: **PoC**. The API can change at any time. The GitHub remote for
this repo is private. Licensed under the [MIT License](LICENSE).

## Why

Natural language is the right payload format between two models. Both
sides parse it well. The failures in model-to-model exchange are not in
the payload. They are in the missing metadata:

- A guess and a checked fact look the same in prose.
- A misunderstanding is silent until it shows up in the output.
- Shared context is re-sent in full, or assumed and then it drifts.
- Trusted and untrusted content arrive in the same register.
- Nothing proves who sent a message, or that a thread was not edited.

The envelope block fixes those points. The remaining blocks compose
around it. See [docs/protocol-design.md](docs/protocol-design.md) for
the wire rationale. See [docs/README.md](docs/README.md) for the doc
index and the reading order.

## The blocks

Each block is one top-level package with one concern. A block is
replaceable and testable on its own. Compose blocks through their
public API.

- **envelope** — the wire unit: Message, Ack, Sign, VerifyThread. Ships.
- **room** — standing groups: roster, roles, message admission. Ships.
- **machine** — the status model: Status, Trigger, Guard, Transition,
  Fire, and the JSON wire form. Ships.
- **flow** — the step graph: Step, Panel, Definition. The runner stays
  future.
- **events** — the in-process reaction bus. Caller-owned; no shared
  bus. Ships.
- **a2a** — a future block. The plan lives in
  [docs/plans/a2a.md](docs/plans/a2a.md).
- **agent** — the composition layer. Future. An agent wires the blocks;
  a block never imports the agent.

## Roadmap

The build runs in twenty phases. Each phase is the smallest unit that
ships. [docs/plans/agents/PHASES.md](docs/plans/agents/PHASES.md) is
the framework. One small plan per phase lives in
[docs/plans/agents/](docs/plans/agents/).

- Phases 1 to 7, foundation: the machine status model and wire form,
  then the flow graph, runner, panels, and chaining. Phases 1 to 4
  ship: the machine block and the flow graph.
- Phases 8 to 11, transport and identity: identity key wrap, a2a
  mapping, a2a client, discovery card. Future.
- Phases 12 to 15, composition: agent definition, run loop, tools,
  memory. Future.
- Phase 16, system: the end-to-end two-agent exchange. Future.
- Phases 17 to 20, reaction and delivery: the events bus and the block
  emissions. Phases 17 and 18 ship: the bus and the machine emissions.

## Install

```bash
go get github.com/MiviaLabs/mivia-ai-sdk
```

## Quick start

Two blocks compose here. The envelope signs and verifies the message.
The room admits it because the signer is a member.

```go
package main

import (
	"crypto/ed25519"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

func main() {
	_, key, _ := ed25519.GenerateKey(nil)

	msg := envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "task-42",
		Room:       "standup",
		Intent:     envelope.IntentRequest,
		Epistemic:  envelope.EpistemicVerified,
		Confidence: 0.9,
		Provenance: envelope.Provenance{
			Source:   "tool:grep",
			Evidence: []string{envelope.ContextRef("grep output")},
		},
		Payload: "Summarize the config loading path in 5 bullets.",
	}
	msg, err := envelope.Sign(key, msg) // stamps Signer, signs every field
	if err != nil {
		panic(err)
	}
	data, err := msg.Encode() // validates, then serializes to JSON
	if err != nil {
		panic(err)
	}

	// The receiving side:
	got, err := envelope.Decode(data)
	if err != nil {
		panic(err)
	}
	if err := got.VerifySignature(); err != nil {
		panic(err) // forged or tampered
	}

	// A room admits a message only when signer and recipients are members.
	r, err := room.New(got.Room, got.Signer) // the founder is the first member
	if err != nil {
		panic(err)
	}
	if err := r.Accepts(got); err != nil {
		panic(err)
	}
	fmt.Println("admitted to room:", r.ID())
}
```

For the semantic ack, the receiver answers with `envelope.NewAck`; the
sender confirms or corrects before the receiver acts. See
[docs/examples/envelope-flow.md](docs/examples/envelope-flow.md) and
[docs/examples/room-flow.md](docs/examples/room-flow.md) for the full
walkthroughs.

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
flow/                step graph: Step, Panel, Definition; runner is future
events/              in-process reaction bus: typed names, Subscribe, Emit
docs/                index + architecture + package docs + examples
api/                 exported-surface locks; check_api diffs them
policy/              layers.json: the allowed internal import edges
scripts/             gates: docs, structure, deps, plan, api, semgrep
semgrep/             pattern rules for the Semgrep scan
.semgrepignore       Semgrep ignore list; test files are scanned again
.githooks/           pre-commit runs make verify-fast on the staged snapshot
Makefile             make verify, make verify-fast, make bench, make api-update,
                     make install-hooks
AGENTS.md            contribution rules for AI and human agents
```

Root holds no Go code. New concerns get new subpackages.

## Development

```bash
make install-hooks   # once per clone; sets core.hooksPath to .githooks
make verify-fast     # fast tier: fmt, vet, test, gates, semgrep scan
make verify          # full tier: verify-fast, coverage floor, semgrep probes
```

The pre-commit hook runs `make verify-fast` on the staged snapshot.
It never runs the full suite twice.

Contribution rules (comment style, layout, limits, no dependencies)
live in [AGENTS.md](AGENTS.md).
