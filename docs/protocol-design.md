# Protocol design

**What this document is:** the wire-protocol rationale. It explains
why the message envelope is shaped this way.

Contents:

- [Problem statement](#problem-statement)
- [What the research says](#what-the-research-says)
- [Rejected alternatives](#rejected-alternatives)
- [Design: structured envelope, natural-language payload](#design-structured-envelope-natural-language-payload)
- [Enforced invariants](#enforced-invariants)
- [Semantic acknowledgment](#semantic-acknowledgment)
- [Group threads and the hash chain](#group-threads-and-the-hash-chain)
- [Deliberately omitted](#deliberately-omitted)
- [Known limits](#known-limits)
- [Validation plan](#validation-plan)
- [References](#references)

This document records the design decisions of the AI message protocol
that this SDK implements. Schema version: **v1**.

## Problem statement

Two language models that exchange plain natural language have four
recurring failure modes:

1. **No epistemic typing.** "The API returns JSON" and "I assume the
   API returns JSON" look identical. The receiver cannot tell a fact
   from a guess without re-deriving it.
2. **Silent misunderstanding.** There is no acknowledgment of meaning.
   The receiver can parse a request 15% wrong and the error only shows
   in the final output.
3. **Context is not addressable.** Each exchange re-transmits shared
   context, or assumes it and drifts.
4. **No provenance.** A claim from the model, from a tool result, and
   from an untrusted document arrive in the same register. This is a
   prompt-injection surface.

Bandwidth is not the bottleneck. Parsing natural language is not the
bottleneck. Both sides are trained on ambiguous human text.

## What the research says

Reviewed 2026-08. Full citations in the References section.

- **A2A (Google, now Linux Foundation)** standardizes capability
  discovery (Agent Cards at a well-known URI), a task lifecycle
  (submitted / working / input-required / completed / failed), and
  message transport (JSON-RPC, SSE, push). It has no epistemic typing
  and no semantic acknowledgment of meaning. A2A is routing and task
  management; this protocol is message semantics. They compose.
- **Governance gap analysis (arXiv 2606.31498).** Across MCP, A2A,
  ACP, ANP, and ERC-8004, voting, dissent preservation, and human
  escalation are universally absent. Audit, where it exists, is a
  substrate property (blockchain, session state), not a protocol
  primitive. This protocol takes three cheap primitives from that
  taxonomy: `challenge` (deliberation), `escalate` (human escalation),
  and a tamper-evident hash chain per thread (audit).
- **"Why do AI agents communicate in human language?"
  (arXiv 2506.02739).** Natural language between models causes
  cascading semantic loss: the internal-state-to-language mapping is
  lossy and non-invertible, so reconstruction error accumulates with
  every relay hop. The paper also documents "lost-in-conversation"
  (no task boundaries) and pseudo-execution (agents report done without
  doing). Consequences for this protocol: threads have explicit
  boundaries (`thread_id`), relays are capped (`max_hops`), and the
  semantic ack exists to detect reconstruction error early instead of
  after a cascade.

## Rejected alternatives

- **Pure formal language (logic, binary format).** Throws away the one
  thing models do best and adds translation errors at both ends.
- **Pure natural language.** No validation, no provenance, silent
  drift. This is the status quo and it fails on all four points above.
- **Activation/tensor-level exchange (arXiv 2501.14082).** Higher
  fidelity in theory, but requires same-family weights and gives up
  the human-readable debugging channel. Out of scope for a portable
  protocol.

## Design: structured envelope, natural-language payload

The protocol puts machine-checkable metadata around a natural-language
payload:

```json
{
  "version": "v1",
  "id": "msg-1",
  "room": "platform-team",
  "thread_id": "task-42",
  "to": ["agent-b", "agent-c"],
  "in_reply_to": "msg-0",
  "intent": "assert | query | request | challenge | retract | escalate",
  "epistemic": "verified | inferred | assumed | untrusted-input",
  "confidence": 0.85,
  "context_refs": ["sha256:..."],
  "prev_hash": "sha256:...",
  "provenance": {"source": "tool:grep", "chain": ["agent-a"],
    "evidence": ["sha256:..."]},
  "max_hops": 3,
  "cost_budget": 4000,
  "ack_required": true,
  "payload": "natural language content",
  "signer": "<hex ed25519 public key>",
  "signature": "<hex ed25519 signature>"
}
```

Decision by decision:

- **Natural-language payload.** Structure goes where structure pays:
  the metadata. The content stays in the format both sides parse best.
- **Epistemic label as a first-class field.** Errors in multi-agent
  systems come mostly from confidence laundering: a guess passes
  through two hops and comes out as a fact. A receiver that sees
  `inferred` at 0.6 confidence can decide to verify instead of
  inheriting the claim as ground truth. The strong label is pinned
  down mechanically: `verified` requires a named source plus evidence
  content refs, so "verified" points at artifacts a receiver can
  hash-check instead of trusting a bare claim.
- **Context by reference.** `ContextRef(content)` computes a
  `sha256:` address. Shared context is deduplicated and "do we talk
  about the same thing" becomes checkable. Context window is the one
  resource that is actually scarce.
- **Provenance chain.** Security, not bookkeeping. The
  `untrusted-input` label tells the receiver to hold the content at
  arm's length and not treat it as an instruction.
- **Thread boundary.** `thread_id` groups one conversation or task.
  Required, because unnamed threads are how agents lose the plot over
  long exchanges (lost-in-conversation).
- **Addressing: 1-to-1, multicast, rooms.** `signer` is the sender.
  `to` lists recipients: one entry is 1-to-1, several entries are
  multicast, empty is broadcast to the room. `room` names a standing
  group; threads live inside rooms. Membership is implemented in the
  `room` package: a moderator-gated roster with roles, and
  `Room.Accepts` gates a message on signer and recipient membership.
  The envelope carries the address; the room package carries the
  roster. `agent.Run` may stamp a caller-chosen room name onto each
  step message before signing, so a plan whose caller supplies one
  produces messages a `room.Room` can admit.
- **Tamper-evident audit.** `prev_hash` links each message to the
  `Hash()` of the previous message in the thread. Reordering,
  deletion, or insertion breaks the chain and is detectable. Cheap:
  one hash per message. `VerifyThread` also rejects a repeated message
  ID: `id` stays unique within its `thread_id`, so a replayed or
  duplicated message cannot enter the chain.
- **Hop cap.** `max_hops` limits how many relays a message may pass
  through (checked against the provenance chain length). Semantic
  error accumulates per hop; unbounded relay is unbounded drift.
- **Human escalation.** `escalate` routes a decision to a human or
  higher authority. Absent from every protocol surveyed above.
- **Cost budget.** Lets the sender cap reply cost so the receiver can
  pick a compression level.
- **Authentication.** `Sign`/`VerifySignature` (ed25519) authenticate
  a message: the signature covers the canonical JSON of every field
  except itself, so any post-signing change fails verification. The
  hash chain gives tamper-evidence for the thread; the signature gives
  authorship for each message. Trust policy (which signers to accept)
  stays with the caller.
- **Schema version.** `version` is validated against the one supported
  value. Unknown JSON fields are ignored on decode, so a newer sender
  can add fields without breaking an older receiver.

## Enforced invariants

`Message.Validate` enforces every rule a comment states:

- `version` equals the supported value.
- `id` is set and differs from `in_reply_to`.
- `thread_id` is set.
- `challenge` and `retract` require `in_reply_to`.
- `verified` requires `provenance.source` and at least one evidence
  ref.
- `confidence` is inside [0, 1].
- Context refs and `prev_hash` are canonical: `sha256:` plus 64
  lowercase hex chars. Canonical form keeps addresses comparable by
  string equality.
- `max_hops`, when set, is not exceeded by the provenance chain.
- Every evidence ref is a canonical sha256 address.
- `signer` and `signature` come as a pair, in canonical hex formats.
- `payload` is non-empty.

`VerifyThread` adds the thread-level rule: no `id` repeats within one
thread.

## Semantic acknowledgment

The one protocol rule that matters more than any field:

For any `request`, and for any message with `ack_required: true`, the
receiver must reply with a compressed **restatement** of what it
understood before it acts. The sender confirms or corrects.

This converts silent misunderstanding into a cheap two-round exchange.
It copies what careful human engineers do: "so you want X, not Y —
right?" It is also the direct counter to cascading semantic loss: the
reconstruction error is measured after one hop, not after ten.

Flow in code (three states: `pending | confirmed | corrected`):

```go
ack, _ := envelope.NewAck(msg, "agent-b", "You want X, not Y.") // pending; built by receiver
ack = ack.Confirm()                                              // sender accepts
ack = ack.Correct("Y is out of scope; only X.")                  // or sender fixes
```

Only a `confirmed` ack means the receiver may act.

In a group, each recipient sends its own ack and `from` tells them
apart. A request to a room is not actionable until every addressed
recipient has a confirmed ack — that rule belongs to the caller, not
the envelope.

## Group threads and the hash chain

`prev_hash` forms a linear chain, which assumes one writer appends to
a thread at a time. Two parties taking turns satisfy this. A busy room
does not: two agents can both append to the same parent, and the chain
forks. The rule is: a thread has serialized appends,
enforced by whoever owns the transport (last-hash-wins locking, a
sequencer, or a thread owner). A multi-parent DAG (`prev_hash` as a
list, git-style) is the known upgrade path if a use case needs
concurrent writers.

## Deliberately omitted

These belong to other layers. This protocol is the message envelope,
not the transport, registry, or session manager.

- **Capability discovery (A2A Agent Cards).** A registry concern. The
  `discovery` package defines its own minimal card shape instead of
  the A2A Agent Card format.
- **Streaming, push, task lifecycle (A2A).** Transport and session
  concerns. Task state ownership stays outside this protocol's scope.
  The `a2a` package maps an envelope message onto an A2A v1.0 message
  part and back, with no task-lifecycle or transport claim. The
  `a2aclient` package sends that mapped part to a remote agent as a
  task and polls its status, over `a2aproject/a2a-go`'s gRPC
  transport. It adds no message-semantics rule: `Result` re-verifies
  the signature this protocol already defines, and does not change
  what a valid envelope looks like.
- **Voting and dissent preservation.** Governance-layer primitives.
  `challenge` and `escalate` cover the two-party case; multi-party
  preference aggregation is out of scope.
- **Identity registries / DID resolution (ANP).** Signatures prove a
  message came from the holder of a key. Mapping that key to a
  organizational identity needs a registry decision this SDK does not
  make.

## Known limits

- **Epistemic labels are self-reported.** A model that hallucinates
  can also mislabel the hallucination as `verified`. The labels do not
  create truth. They create auditable claims about truth. Provenance
  fields can be checked mechanically (did the tool call happen?);
  confidence cannot.
- **Acks cost a round trip.** On trivial messages that is overhead for
  nothing. The `ack_required` threshold must be chosen with care.
- **Colluding blind spots.** Two identical models can agree on a
  shared misunderstanding faster than a human would catch it. A
  different model on the receiving side is arguably a protocol-level
  feature. `escalate` is the designed escape hatch.
- **Trust policy is out of band.** Signatures authenticate the key
  holder, but which signers a receiver accepts is the caller's
  decision. There is no revocation or key rotation story yet.
- **A status transition precedes its ack check.** The `flow` runner
  fires a step's status transition, then waits on the step's ack. A
  rejected or escalated ack halts the walk but does not roll the
  status or its record back to the pre-step value. `agent`'s `Run`
  signs each step's message, waits for a confirmed ack through a
  caller-supplied `AckWait`, and only advances the walk once the ack
  confirms.

## Validation plan

Do not validate by vibes. Run pairs of models on multi-step tasks
(plan / execute / review) over three conditions:

1. plain natural language,
2. structured-only messages,
3. this hybrid envelope.

Measure the downstream error rate: how often the final output contains
a mistake that traces back to a miscommunication between the two
models. Measure token cost. The hybrid wins if semantic acks catch
measurable misunderstandings at lower cost than full re-explanation.
If plain natural language matches it, the premise is wrong and models
do not need a special protocol.

## References

- A2A protocol, Linux Foundation: https://a2a-protocol.org/
- A. Ehtesham et al., "A Survey of Agent Interoperability Protocols:
  MCP, ACP, A2A, and ANP", arXiv:2505.02279.
- "Governance Gaps in Agent Interoperability Protocols: What MCP, A2A,
  and ACP Cannot Express", arXiv:2606.31498.
- P. Zhou et al., "Why do AI agents communicate in human language?",
  arXiv:2506.02739.
- "Communicating Activations Between Language Model Agents",
  arXiv:2501.14082.
