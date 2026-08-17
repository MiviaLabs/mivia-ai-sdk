# Plan: docs

## Goal

Restructure the docs tree so every file has a clear role. Give the
tree an index, an architecture doc, package references, and examples.
Answer what docs/protocol-design.md is: the wire-protocol rationale.

## Scope

Inside: one new plan file, six new docs, and four edits. Outside:
every rejected item at the bottom of this section.

### Extension: public-docs completion and language purge

The SDK grew past the original six shipped packages. Three packages —
discovery, heartbeat, agent — shipped with no package reference or
example. Every public doc still carried internal development-process
language: phase numbers, `docs/plans/` cross-links, "ships"/"future"
status wording. This extension closes both gaps in one pass.

New files: `docs/packages/discovery.md`, `docs/packages/heartbeat.md`,
`docs/packages/agent.md`, `docs/examples/machine-flow.md`,
`docs/examples/events-bus.md`, `docs/examples/heartbeat-liveness.md`,
`docs/examples/flow-runner.md`, `docs/examples/agent-dispatch.md` (the
full end-to-end walkthrough).

Edited files: `docs/README.md` and `docs/architecture.md` (full
rewrite: an index and a module map with no `docs/plans/` links or
process language), `docs/protocol-design.md`, `docs/packages/machine.md`,
`docs/packages/flow.md`, `docs/packages/identity.md` (each purged of
phase numbers, `docs/plans/` links, and the "ships" status idiom).

Rule going forward: no file outside `docs/plans/` links to
`docs/plans/`, names a phase number, or describes an unshipped
package as part of the reading path. `docs/plans/` and
`docs/research-*.md` stay internal; `docs/README.md`'s "Internal
records" section names them collectively, not per file. AGENTS.md is
exempt — it is the contribution contract, not public documentation,
and correctly describes the `docs/plans/` convention.

### New files

- `docs/README.md` — the table of contents.
- `docs/architecture.md` — the module map and data flow.
- `docs/packages/envelope.md` — the envelope package reference.
- `docs/packages/room.md` — the room package reference.
- `docs/examples/envelope-flow.md` — the envelope walkthrough.
- `docs/examples/room-flow.md` — the room walkthrough.

### Edited files

- `docs/protocol-design.md` — add the role paragraph and the section
  TOC.
- `AGENTS.md` — rewrite the docs layout bullet.
- `README.md` — rewrite one layout line; add one docs link.
- `scripts/check_prose.py` — scan all docs, not only the plans.

### The index (docs/README.md)

Map every file under docs/. Give each file a one-line role. Give a
reading order. List the plans with the collective role "change
contracts, gate-mandated". Give docs/protocol-design.md the role
line: "the wire-protocol rationale: why the envelope is shaped this
way". List this file too. Every listed path must resolve to a real
file. The reviewer checks this by hand; no new gate.

The reading order: the index, the wire rationale, the architecture
doc, the package references, the examples, the plans before code
changes, the research record last.

### The architecture doc (docs/architecture.md)

Sections, in order:

1. The package map. envelope holds the wire unit. room holds the
   roster. flow and a2a are future packages, planned in
   docs/plans/flow.md and docs/plans/a2a.md.
2. The message flow, one step at a time: sign, encode, transport,
   decode, verify, room admission, ack, thread chain. Name the file
   and function for each step. The transport is out of scope; the
   wire form is the JSON bytes that Encode and Decode handle.
3. The gate system: the scripts/ gates, the semgrep/ rules, and the
   .githooks/ pre-commit. The pre-commit runs make verify-fast on
   the staged snapshot. State what verify-fast and verify each run.
4. The invariants the architecture enforces: no root Go code, one
   package per concern, the import policy, the API locks, the plans
   gate, the writing standard, the label ban, the drift-marker ban,
   the suppression ban, the coverage floor.

Cross-link to docs/protocol-design.md and to both package references.

### The package references (docs/packages/*.md)

Both files follow the same skeleton:

- One-line purpose.
- Exported API grouped by concern. Use api/envelope.txt and
  api/room.txt as the source of truth. Do not invent symbols.
- The invariants that Validate and Accepts enforce. Read the code.
- Wire contract notes: JSON tags, omitted fields, conformance
  vectors, version handling.
- A short usage snippet in a code fence.

envelope.md covers: Message, Intent, Epistemic, Provenance, Ack,
AckStatus, Version; Sign, VerifySignature, Encode, Decode, Hash,
ContextRef; Validate on both types; NewAck, Confirm, Correct,
RequiresAck, DecodeAck; VerifyThread. The invariants come from
message.go and ack.go. The vectors live in
envelope/testdata/vectors/.

room.md covers: Room, New, Role, the roster operations, Accepts, and
the six sentinel errors. The invariants come from room.go.

### The walkthroughs (docs/examples/*.md)

Each file is a prose walkthrough with one complete Go program in a
code fence. The code is illustrative only. It is not buildable. Root
Go code is forbidden. A runnable examples package would need a
policy/layers.json row, an api/ lock, a plan, and API churn;
record that as future work inside each file.

envelope-flow.md shows: create, sign, encode, decode, verify, then
tamper with a field value (for example, change the payload) so the
JSON stays valid. The tampered message still decodes but fails
VerifySignature.

room-flow.md shows: the founder creates a room, a moderator admits a
member, the member sends a signed message, and Room.Accepts checks
the signer and the recipients. Show a stranger failing admission.

### The protocol-design edit

Add two blocks right after the H1, before "Problem statement":

1. A "What this document is" paragraph. It states the role: the
   wire-protocol rationale. Keep it to two sentences.
2. A small TOC of the existing sections. One link per "## " heading.
   Derive each anchor the standard way: lowercase, spaces to hyphens,
   punctuation dropped.

Keep every existing statement. Add no other content.

### The AGENTS.md edit

Rewrite the docs layout bullet. The new text, one bullet:

- `docs/` — README.md is the index; architecture.md the module map;
  packages/ the package references; examples/ the walkthroughs;
  plans/ the change contracts; protocol-design.md the wire rationale.

Keep the separate docs/plans/ bullet as it is.

### The README.md edit

Two changes only.

1. In the Layout code block, the docs/ line becomes "index +
   architecture + package docs + examples". Keep the column
   alignment.
2. In the Why section, after the protocol-design link, add one link
   to docs/README.md. One sentence: start there for the index and
   the reading order.

### The check_prose.py edit

Two changes only.

1. The scan root becomes the whole docs tree. Change
   `(root / "docs" / "plans").glob("*.md")` to
   `(root / "docs").rglob("*.md")`.
2. The header docstring says the gate now scans docs/**/*.md.

The sentence check itself stays unchanged. Headings, list lines, and
code fences stay exempt. The dry run on the current tree passes; the
existing docs already fit the standard. New docs must not use
abbreviation traps such as "e.g.". STE forbids them anyway.

### Constraints

- No Go code anywhere.
- No policy/layers.json change.
- No api/ lock change.
- No new top-level directories. docs/ subdirs only.
- Every new and edited doc is STE: sentences at most 25 words, one
  idea per sentence, no filler words.
- No audit-finding label tokens anywhere. A label is a letter A
  through G followed by a digit. Describe rules without examples.
- No literal unresolved-work marker tokens. The drift rule scans all
  files.
- No comment-form suppression markers.
- Every doc stays at or below 500 lines.

### Rejected and future work

- Runnable examples package: rejected. Root Go code is forbidden.
  A runnable package would need a policy row, an api/ lock, a plan,
  and API churn. Future work if the SDK gains an examples
  convention.
- Renaming docs/protocol-design.md: rejected. AGENTS.md, README.md,
  and the plans reference the name. The name is accurate.
- Touching docs/plans/*.md content: rejected. The change contracts
  stay as they are.
- Any Go code or api/ lock change: rejected.

## API

No Go API changes. No exported symbol changes. The api/ locks stay
untouched. The policy stays untouched. The deliverable surface is
the docs tree above plus the two edited non-doc files.

## Tests

- `python3 scripts/check_prose.py` passes on every doc, new and old.
  The extended scan covers the index, the architecture doc, the
  package references, the examples, and this plan.
- `python3 scripts/check_labels.py` passes on the full tree.
- `python3 scripts/check_plan.py` passes. No new Go package exists,
  so no new package plan is required.
- `python3 scripts/check_deps.py` passes. The import policy does not
  change.
- The reviewer checks index coverage by hand: every file under
  docs/architecture.md, docs/protocol-design.md, docs/packages/, and
  docs/examples/ appears in docs/README.md, and every entry resolves.
  docs/plans/ and docs/research-*.md are referenced collectively, not
  per file, per docs/README.md's "Internal records" section. No new
  gate.
- The drift rule, the label gate, and the suppression scan stay clean
  on the new docs. The builder runs the full scan.

## Verification

1. `python3 scripts/check_plan.py`
2. `python3 scripts/check_deps.py`
3. `python3 scripts/check_prose.py`
4. `make verify`

All must pass on the final tree. The pre-commit hook re-runs
verify-fast on the staged snapshot.
