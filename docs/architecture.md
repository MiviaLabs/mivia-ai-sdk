# Architecture

This document maps the modules, the message flow, the gate system, and
the invariants the architecture enforces. See
[protocol-design.md](protocol-design.md) for the wire rationale. See
[packages/envelope.md](packages/envelope.md),
[packages/room.md](packages/room.md), and
[packages/machine.md](packages/machine.md) for the exported API
references.

## Package map

- `envelope/` — the wire unit. It holds Message, Ack, Sign, and
  VerifyThread. One package per concern.
- `room/` — standing groups. It holds the roster, the roles, and
  message admission.
- `machine/` — the status model. Phases 1 and 2 ship `Status`,
  `Trigger`, `Guard`, `Action`, `Transition`, `InOut`, `Definition`,
  `New`, `Validate`, and the `Fire` dispatch. The wire form lands in a
  later phase. See [packages/machine.md](packages/machine.md) and
  [plans/machine.md](plans/machine.md).
- `flow/` — a future package. It owns the step graph, panels, parallel
  execution, and chaining. It is planned in
  [plans/flow.md](plans/flow.md); no code exists yet.
- `a2a/` — a future package. It is planned in
  [plans/a2a.md](plans/a2a.md); no code exists yet.

The machine and flow packages compose. Flow imports machine for each
step's status transitions. The import edge lands when the code lands.

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
