# Architecture

This document maps the modules, the message flow, the gate system, and
the invariants the architecture enforces. See
[protocol-design.md](protocol-design.md) for the wire rationale. See
[packages/envelope.md](packages/envelope.md),
[packages/room.md](packages/room.md),
[packages/machine.md](packages/machine.md),
[packages/flow.md](packages/flow.md),
[packages/identity.md](packages/identity.md), and
[packages/events.md](packages/events.md) for the exported API
references.

## Package map

- `envelope/` — the wire unit. It holds Message, Ack, Sign, and
  VerifyThread. One package per concern.
- `room/` — standing groups. It holds the roster, the roles, and
  message admission.
- `machine/` — the status model. It ships `Status`, `Trigger`,
  `Guard`, `Action`, `Transition`, `InOut`, `Definition`, `New`,
  `Initial`, `Transitions`, `AllowedTransitions`, `AllowedTriggers`,
  `Validate`, `Fire`, and the JSON wire form: `Encode`, `Decode`,
  `Registry`, `NewRegistry`, and `MoveEvent`. See
  [packages/machine.md](packages/machine.md) and
  [plans/machine.md](plans/machine.md).
- `flow/` — the step graph, the sequential runner, and the parallel
  panel waves. It ships `Step`, `Panel`, `Definition`, `New`, `Roots`,
  `Run`, and `Confirm`. `Step` carries an optional `Sub *Definition`
  for chaining. `Run` walks the graph in topological order. A step
  named in no panel runs alone and gates on a confirmed ack, as before.
  A step named in a panel runs as part of that panel's wave, in a
  goroutine, once every member is ready; the wave joins its members'
  errors with `errors.Join`. Chaining ships in phase 7. See
  [packages/flow.md](packages/flow.md) and
  [plans/flow.md](plans/flow.md).
- `events/` — the in-process reaction bus. It ships `Name`, `Event`,
  `Handler`, `Bus`, `New`, `Subscribe`, and `Emit`. The caller owns the
  bus; the module has no shared bus. Event names are typed `Name`
  constants owned by each domain. See
  [packages/events.md](packages/events.md) and
  [plans/events.md](plans/events.md).
- `identity/` — one agent key. It ships `Identity`, `New`, `Load`,
  `Validate`, `Sign`, `Signer`, and the sentinels `ErrKeyFormat` and
  `ErrKeyInvalid`. `Sign` wraps `envelope.Sign`; `Signer` derives the
  hex public key from the private key. See
  [packages/identity.md](packages/identity.md) and
  [plans/identity.md](plans/identity.md).
- `discovery/` — the capability card. It ships `Card`, `Parse`,
  `Validate`, and `Match`. `Parse` reads a card from JSON and validates
  it. `Validate` rejects a blank name, an empty capability list, and a
  duplicate capability. `Match` compares a capability request against
  the card, case-insensitive and exact. See
  [plans/discovery.md](plans/discovery.md).
- `agent/` — the composition layer. It ships `Agent`, `New`, `Name`,
  and `Capabilities`. `New` wires an `identity.Identity`, a
  `discovery.Card`, and a `flow.Definition` into one agent. It rejects
  a nil identity, an invalid card, and a nil plan, in that order. It
  also ships the envelope-to-events translator: `EmitMessageDelivered`,
  `EmitMessageAcked`, and `EmitThreadVerified`. Each function verifies
  an already-received `envelope.Message`, `envelope.Ack`, or message
  thread, then emits one typed `events.Event` onto a caller-owned
  `events.Bus`. It also ships `Run` and the `AckWait` function type:
  `Run` drives the agent's bound `flow.Definition` through `flow.Run`,
  in-process. For each step `flow.Run` gates behind `Confirm`, `Run`
  signs an `envelope.Message`, emits `MessageDeliveredEvent`, calls the
  caller-supplied `AckWait`, and emits `MessageAckedEvent` once the ack
  confirms. An `AckWait` that wraps `ErrEscalated` routes the step back
  to the caller. `agent` imports `envelope`, `events`, and `machine`;
  none of those three packages imports `agent` or either of the other
  two. See [plans/agent.md](plans/agent.md).
- `heartbeat/` — a leaf primitive. It ships `Monitor`, `New`, `Beat`,
  `Alive`, `Dead`, `Forget`, and the typed event name `MissedEvent`.
  `Monitor` tracks liveness by time: it records the last beat per id
  and reports which ids have gone silent past a fixed timeout. It has
  no caller in this repo yet; it is a plausible future building block
  for agent execution work, once that work is scoped, not yet named in
  any phase contract. It imports `events` only, for the `MissedEvent`
  constant. See [plans/heartbeat.md](plans/heartbeat.md).
- `a2a/` — a future package. It is planned in
  [plans/a2a.md](plans/a2a.md); no code exists yet.

The machine and flow packages compose. Flow imports machine for each
step's status transitions and for `Run`'s status walk. The machine
package imports events for its typed `MoveEvent` constant.
The events package imports nothing; it is a leaf.
The identity package imports envelope only; it wraps `envelope.Sign`.

The root holds no Go code. New concerns get new subpackages. The
import policy in `policy/layers.json` states which package may import
which; `scripts/check_deps.py` enforces it.

## Message flow

One step at a time. The wire form is the JSON bytes that Encode and
Decode handle.

1. **Sign.** `envelope/sign.go`, `Sign(key, m)`: sets Signer and
   Signature. The signature covers the canonical JSON of every field
   except itself.
2. **Encode.** `envelope/message.go`, `Message.Encode`: validates, then
   marshals to JSON. An invalid message cannot cross the wire.
3. **Transport.** Out of scope for this SDK. The wire form is the JSON
   bytes from Encode and to Decode.
4. **Decode.** `envelope/message.go`, `Decode(data)`: parses JSON, then
   validates. Unknown fields are ignored for forward compatibility.
5. **Verify.** `envelope/sign.go`, `Message.VerifySignature`: checks
   the ed25519 signature against the embedded Signer key.
6. **Room admission.** `room/room.go`, `Room.Accepts`: checks the room
   name, the signature, and the membership of the signer and the
   recipients.
7. **Ack.** `envelope/ack.go`, `NewAck`, `Ack.Confirm`, `Ack.Correct`:
   the semantic-ack flow. Only a confirmed Ack means the receiver may
   act.
8. **Thread chain.** `envelope/thread.go`, `VerifyThread`: checks the
   hash chain and rejects repeated message IDs.

## Gate system

The gates are mechanical. They run in `make verify` and, on a subset,
in the pre-commit hook.

- `scripts/` — the gates: check_docs, check_structure, check_deps,
  check_plan, check_prose, check_api, check_gomod,
  check_semgrepignore, check_labels, check_semgrep_probes, and
  api_surface (Go).
- `semgrep/` — the pattern rules: no panic or exit in packages,
  stdlib-only imports, centralized constants, no hardcoded secrets, no
  suppression markers, no drift markers.
- `.githooks/pre-commit` — runs `make verify-fast` on the staged
  snapshot. The worktree never leaks into the commit.

`make verify-fast` runs gofmt, vet, one test pass, the python gates,
the semgrep scan, and the suppression-marker scan. `make verify` runs
everything verify-fast runs, plus the coverage floor and the semgrep
probe suite.

## Invariants

The architecture enforces these rules:

- No root Go code. The root holds go.mod, the Makefile, and docs.
- One package per concern. New concerns get new subpackages.
- The import policy. `policy/layers.json` lists the allowed internal
  edges; `scripts/check_deps.py` enforces them.
- The API locks. The files in `api/` pin the exported surface;
  `scripts/check_api.py` diffs them.
- The plans gate. Every top-level package needs a plan;
  `scripts/check_plan.py` enforces it.
- The writing standard. Sentences stay at or below 25 words;
  `scripts/check_prose.py` scans the whole docs tree.
- The label ban. Audit-finding labels never appear in comments, docs,
  or plans; `scripts/check_labels.py` scans every file.
- The drift-marker ban and the suppression ban. The semgrep rules and
  the suppression-marker scan enforce them.
- The coverage floor. The total and every package reach 85; the
  coverage block in `make verify` enforces it.
