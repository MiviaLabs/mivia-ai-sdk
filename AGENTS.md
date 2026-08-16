# Agent Instructions

PoC Go SDK for model-to-model communication. Module:
`github.com/MiviaLabs/mivia-ai-sdk`.

## Layout

- `envelope/` — the message envelope (Message, Ack, Sign,
  VerifyThread). One package per concern; new concerns get new
  subpackages, never root-level Go files.
- `room/` — standing groups: membership roster, roles, admission.
- `api/` — exported-surface locks; `scripts/check_api.py` diffs them.
- `policy/layers.json` — allowed internal imports per package.
- `docs/plans/` — one plan per package; `scripts/check_plan.py` gates it.
- `docs/` — design documents. `docs/protocol-design.md` is the rationale.
- `scripts/` — gates: check_docs, check_structure, check_deps,
  check_plan, check_api, api_surface (Go).
- `semgrep/sdk-standards.yml` — pattern rules: no panic/exit in
  packages, stdlib-only imports, no string literals where constants
  exist (enums, hash prefix), wire bytes only via Encode, signing only
  via Sign, no hardcoded secrets, no suppression annotations.
- `.githooks/pre-commit` — runs `make verify`.
- `.claude/agents/` — subagent roles: planner, plan-reviewer,
  builder, reviewer. `.claude/skills/delivery/` drives the loop.
- `.claude/settings.json` — PreToolUse hooks wired to
  `scripts/agent_hook_guard.py` (blocks hook bypass and manual edits
  to generated `api/` locks).
- Root: no Go code. Root holds go.mod, README, this file, Makefile.

## Subagent workflow

Non-trivial changes (new package, API change, more than one file) go
through the delivery loop in `.claude/skills/delivery/SKILL.md`:
planner → plan-reviewer (hostile, before code) → builder → reviewer
(adversarial, after code) → verify → commit. Never skip a review
stage. Never let an agent grade its own work. Three failed rounds at
any stage means stop and escalate to the user.

## Rules

- Run `make install-hooks` once per clone, `make verify` before you
  report done: gofmt, vet, tests, doc gate, structure gate.
- Never bypass Git hooks (no `--no-verify`, no skip env vars).
- The GitHub remote for this repo must be **private**. Never create a
  public remote or push to one.
- No third-party dependencies. Standard library only.
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
- No string literals where constants exist: enum values (Intent,
  Epistemic, AckStatus, Role), hash prefixes, wire serialization
  (Encode), signing (Sign). Enforced by `semgrep/sdk-standards.yml`.
  Tests may construct invalid values on purpose.
- Do not suppress Semgrep findings with inline annotations; fix the
  rule or the code.
- Tests table-driven where the case set grows. Test the invariants that
  `Validate` claims to enforce.
- The wire contract is pinned by conformance vectors in
  `envelope/testdata/vectors/`. Add a vector for every schema or rule
  change: `valid_`, `invalid_decode_`, or `invalid_sig_` prefix.
- Changes to message semantics must update `docs/protocol-design.md` in
  the same change.

## Enforcement ladder (all mechanical, all in make verify)

Rules below are phrased as prohibitions because that is what agents
follow reliably. Each has a gate behind it.

- Do not add or change an exported symbol without a deliberate lock
  update: `make api-update`, then commit the `api/` diff in the same
  change. Gate: `scripts/check_api.py`.
- Do not import another package of this module unless
  `policy/layers.json` allows the edge. A new package must declare its
  allowed imports there first. Gate: `scripts/check_deps.py`.
- Do not land a package without `docs/plans/<pkg>.md` following
  `docs/plans/TEMPLATE.md` (Goal, Scope, API, Tests, Verification).
  Gate: `scripts/check_plan.py`.
- Do not let total coverage fall below 85%. Gate: `make verify`
  coverage floor.
- Do not weaken a gate, raise a limit, or widen an exclusion to make
  your change pass. Change the design instead, or convince the user
  and record the exception in the gate file itself.
