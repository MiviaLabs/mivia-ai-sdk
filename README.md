# mivia-ai-sdk

Proof-of-concept Go SDK for model-to-model communication. The core is a
message envelope: natural-language payloads inside machine-checkable,
authenticatable metadata.

Status: **PoC**. The API can change at any time. The GitHub remote for
this repo is private.

## Why

Natural language is the right payload format between two models. Both
sides parse it well. The failures in model-to-model exchange are not in
the payload. They are in the missing metadata:

- A guess and a checked fact look the same in prose.
- A misunderstanding is silent until it shows up in the output.
- Shared context is re-sent in full, or assumed and then it drifts.
- Trusted and untrusted content arrive in the same register.
- Nothing proves who sent a message, or that a thread was not edited.

This SDK wraps the payload in an envelope that fixes those points.
See [docs/protocol-design.md](docs/protocol-design.md) for the full
rationale and the research behind it. Start at
[docs/README.md](docs/README.md) for the index and the reading order.

## Install

```bash
go get github.com/MiviaLabs/mivia-ai-sdk/envelope
```

## Quick start

```go
package main

import (
	"crypto/ed25519"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

func main() {
	msg := envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "task-42",
		Intent:     envelope.IntentRequest,
		Epistemic:  envelope.EpistemicVerified,
		Confidence: 0.9,
		Provenance: envelope.Provenance{
			Source:   "tool:grep",
			Evidence: []string{envelope.ContextRef("grep output")},
		},
		Payload: "Summarize the config loading path in 5 bullets.",
	}

	_, key, _ := ed25519.GenerateKey(nil)
	msg, err := envelope.Sign(key, msg) // authenticate
	if err != nil {
		panic(err)
	}

	data, err := msg.Encode() // validates, then serializes to JSON
	if err != nil {
		panic(err)
	}

	// On the other side:
	got, err := envelope.Decode(data)
	if err != nil {
		panic(err)
	}
	if err := got.VerifySignature(); err != nil {
		panic(err) // forged or tampered
	}
	if got.RequiresAck() {
		ack, err := envelope.NewAck(got, "agent-b", "You want a 5-bullet summary of config loading.")
		if err != nil {
			panic(err)
		}
		ack = ack.Confirm() // sender side; or ack.Correct("fix")
		fmt.Println(ack.Status)
	}
}
```

## Concepts

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
docs/                index + architecture + package docs + examples
scripts/             gates: docs, structure, deps, plan, api, semgrep
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
