# Agent Instructions

Go SDK for building AI agents. Module:
`github.com/MiviaLabs/mivia-ai-sdk`.

## Layout

The SDK is composed of single-concern packages. See [docs/README.md](docs/README.md) and [docs/architecture.md](docs/architecture.md) for full package references and architecture diagrams.

- `envelope/` — AI message protocol (Message, Ack, Sign, VerifyThread).
- `room/` — standing groups: membership roster, roles, admission.
- `machine/` — status model: triggers, guards, transitions, wire form.
- `flow/` — step graphs: sequential steps, panels, routing, retry, loops, checkpoint/resume.
- `events/` — in-process reaction bus.
- `tools/` — tool registry: execution profiles, scopes, approval gating.
- `provider/` — model provider interface and reasoning vocabulary.
- `agent/` — composition layer wiring blocks into an agent.
- `agentrun/` — config-struct runner composition over agent.Run.
- `subagent/` — blocks as tools, concurrent spawns, and mailboxes.
- `runconfig/` — JSON-document loader binding a step graph to agentrun.Options.
- `ledger/` — durable task admission, leased claims, fenced takeover.
- `workspace/` — filesystem confinement via `os.Root` and secret denial.
- `mcp/` — Model Context Protocol client over stdio/HTTP.
- `a2a/` / `a2aclient/` / `a2aack/` — A2A v1.0 protocol integration.
- `dispatch/` — NDJSON envelope HTTP endpoint.
- `contextplan/` / `contextstate/` / `contextsummary/` — context window management & compaction.
- `longtermmemory/` / `memory/` — tiered long-term and content-addressed memory.
- `channel/` / `scheduler/` / `trigger/` / `heartbeat/` / `discovery/` — supporting primitives.
- `policy/` — `layers.json` allowed imports; `pending_wiring.json`.
- `api/` — exported-surface locks checked by `scripts/check_api.py`.
- `docs/` — design reference, package docs, examples, and change plans.
- `scripts/` — gate validation scripts.
- `.agents/memories/` — team-shared operational memory; read every file
  here at the start of a task.

Root holds no Go code. New concerns get new subpackages, never root Go files.

## Trigger words

The user's vocabulary is a contract. The `review` and `audit` triggers,
formerly defined here, now live in the `review` skill at
`.agents/skills/review/SKILL.md`. Invoke it for a deep review or a
gate-audit pass.

## Orchestrator role

The agent the user talks to is the orchestrator. It drives everything
else.

- Clarify first. Never start the delivery loop with ambiguity. Ask
  questions with proposals A, B, C. Mark the recommended option and
  say why in one sentence. Wait when the choice changes the design.
  Decide alone only when options are equivalent.
- Simplicity over complexity. Prefer the smallest change that works.
  Three files beat a framework. Reject planner output that adds
  abstraction without a caller. No speculative generality.
  `scripts/check_orphan_packages.py` enforces the no-caller rule
  mechanically against `policy/pending_wiring.json`.
- Drive the loop below and consolidate the reports. The user gets one
  answer, not four.

## Building blocks

The SDK is a set of composable blocks, not a monolith. Every package
decision follows this rule.

- A package is a building block with one concern. A new concern gets a
  new top-level package, never a root file.
- Compose packages through their public API. Never copy a type into
  another package to dodge an import. Use the exported type.
- The import policy in `policy/layers.json` pins every edge. Direction
  flows inward: leaf blocks first, the composition last. The deps gate
  enforces it.
- An agent is the composition layer. It wires blocks: a transport
  adapter, a workflow runner, and the message plane. The agent imports
  the blocks; a block never imports the agent.
- Do not split a working package for purity alone. Split a package only
  when a real consumer needs the concern by itself. Keep cohesion. The
  building-block rule is about composing behaviors, not about tearing
  one cohesive struct into many packages: `envelope` holds the
  message, the ack, the signing, and the thread chain in one package
  because the four concerns share one struct and cannot split without
  artificial layering.
- A block stays replaceable and testable on its own. Do not entangle it
  with a caller.

## Subagent workflow

Non-trivial changes (new package, API change, more than one file) go
through the delivery loop in `.agents/skills/delivery/SKILL.md`:
planner → plan-reviewer (hostile, before code) → builder → reviewer
(adversarial, after code) → verify → commit. Never skip a review
stage. Never let an agent grade its own work. Three failed rounds at
any stage means stop and escalate to the user.

## Rules

- **Writing standard (critical):** all agent-authored prose (plans,
  docs, comments, commit messages, reports) uses ASD-STE100-style
  Simplified Technical English. One idea per sentence. Sentences stay
  at or below 25 words. Instructions use the imperative mood. Same
  thing, same word — no synonym drift. No filler words ("simply",
  "just", "seamless", "robust"). Gate: `scripts/check_prose.py`
  enforces sentence length in `docs/plans/`.
- Run `make install-hooks` once per clone, `make verify` before you
  report done. `make verify` is the full gate: gofmt, vet, tests,
  doc gate, structure gate, Semgrep scan, and probes.
- Never bypass Git hooks (no `--no-verify`, no skip env vars).
- The GitHub remote for this repo must be **private**. Never create a
  public remote or push to one.
- No third-party dependencies. Standard library only.
  Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
  `google.golang.org/grpc`; `a2aloopback` may import the same two
  modules, scoped to its own gRPC test-server fixture; `mcp` may import
  `github.com/modelcontextprotocol/go-sdk`; `ledger` may import
  `modernc.org/sqlite`, behind the `ledger_sqlite` build tag only;
  `schema` may import `github.com/santhosh-tekuri/jsonschema/v6`; no
  other package may add a third-party import without its own plan
  review.
- Comments are a machine-read API surface. Keep them short: one line of
  what, plus invariants and cross-references (`See X`, file names) where
  they exist. No prose paragraphs, no restating the signature.
- Every exported symbol needs a doc comment starting with the symbol
  name. Enforced by `scripts/check_docs.py`.
- Files stay at or below 500 lines, functions at or below 80 lines.
  Enforced by `scripts/check_structure.py`. Split code; do not raise
  the limits.
- Invariants live in `Validate` methods, not in comments alone. If a
  comment states a rule, `Validate` must enforce it.
- File and function names must describe the feature, not the development
  process. Forbidden in names: phase, tdd, perf, wip, draft, scratch,
  tmp, old, backup, version suffixes (v2, v3). Use descriptive names
  like `panel_test.go`, `chain_bench_test.go`. Gate:
  `scripts/check_names.py`; Semgrep:
  `sdk.go.no-phase-tdd-perf-names`. Plan documents in
  `docs/plans/agents/` may use phase numbers as plan identifiers.
- No string literals where constants exist: enum values (Intent,
  Epistemic, AckStatus, Role), hash prefixes, wire serialization
  (Encode), signing (Sign). Enforced by `semgrep/sdk-standards.yml`.
  Tests may construct invalid values on purpose.
- Do not suppress Semgrep findings with inline annotations; fix the
  rule or the code.
- No audit-finding labels in comments, docs, or plans. A label is a
  letter A through G followed by a digit. Gate:
  `scripts/check_labels.py`.
- Tests table-driven where the case set grows. Test the invariants that
  `Validate` claims to enforce.
- The wire contract is pinned by conformance vectors in
  `envelope/testdata/vectors/`. Add a vector for every schema or rule
  change: `valid_`, `invalid_decode_`, or `invalid_sig_` prefix.
- Changes to message semantics must update `docs/architecture.md`'s
  "Why the envelope is shaped this way" section in the same change.

## Enforcement ladder (all mechanical, all in make verify)

Rules below are phrased as prohibitions because that is what agents
follow reliably. Each has a gate behind it.

- Do not add or change an exported symbol without a deliberate lock
  update: `make api-update`, then commit the `api/` diff in the same
  change. Gate: `scripts/check_api.py`.
- Do not import another package of this module unless
  `policy/layers.json` allows the edge. A new package must declare its
  allowed imports there first. Gate: `scripts/check_deps.py`.
- Do not copy an exported type into another package to reuse it. Import
  the source package; the import policy already allows the edge. A
  copied type forks on the next change. Gate: review catches the copy.
- Do not let a package see its own caller. Dependency direction flows
  inward; the import policy declares each edge, so a cycle or a caller
  import cannot compile. Gate: `scripts/check_deps.py`.
- Do not land a package without `docs/plans/<pkg>.md` following
  `docs/plans/TEMPLATE.md` (Goal, Scope, API, Tests, Verification).
  Gate: `scripts/check_plan.py`.
- Do not leave a package with zero internal callers undeclared. List
  it in `policy/pending_wiring.json` with a reason and a target. Gate:
  `scripts/check_orphan_packages.py`.
- Do not let coverage fall below 85%. The total and every package each
  need the floor. Gate: `make verify` coverage block. Assertion-free
  tests and deleted tests game the floor; review catches them.
  Mutation testing is future work.
- Do not write an audit-finding label in comments, docs, or plans: a
  letter A through G followed by a digit. Gate:
  `scripts/check_labels.py`.
- Do not weaken a gate, raise a limit, or widen an exclusion to make
  your change pass. Change the design instead, or convince the user
  and record the exception in the gate file itself.

## Gate tiers

Two tiers guard the tree. `make verify-fast` runs the fast local
checks: gofmt, vet, tests, the python gates, the Semgrep scan, and the
suppression-marker scan. The pre-commit hook runs `make verify-fast` on
the staged snapshot.

`make verify` runs `verify-fast`, the coverage floor block, and the
Semgrep probe suite. The probes prove every Semgrep rule fires on a
violation and stays silent on clean code. The coverage block asserts
the profile lists every package and that the total and each package
reach 85.

The hook guard and the pre-commit hook are best-effort against
careless agents. They are not a security boundary. GitHub Actions CI
now runs `make verify` on every push and pull request to `main`. No
branch protection rule exists yet, so CI stays informational only: a
failing check does not block a merge or a direct push.
