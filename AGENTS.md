# Agent Instructions

PoC Go SDK for model-to-model communication. Module:
`github.com/MiviaLabs/mivia-ai-sdk`.

## Layout

- `envelope/` — the message envelope (Message, Ack, Sign,
  VerifyThread). One package per concern; new concerns get new
  subpackages, never root-level Go files.
- `room/` — standing groups: membership roster, roles, admission.
- `docs/` — design documents. `docs/protocol-design.md` is the rationale.
- `scripts/check_docs.py` — doc-comment gate.
- `scripts/check_structure.py` — file/function size gate.
- `semgrep/sdk-standards.yml` — pattern rules: no panic/exit in
  packages, stdlib-only imports, no string literals where constants
  exist (enums, hash prefix), wire bytes only via Encode, signing only
  via Sign, no hardcoded secrets, no nosemgrep suppression.
- `.githooks/pre-commit` — runs `make verify`.
- Root: no Go code. Root holds go.mod, README, this file, Makefile.

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
