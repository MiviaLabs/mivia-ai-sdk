# Plan: gates

## Goal

Close the confirmed gate-layer holes from the 2026-08 adversarial audit.
Gate each commit on its staged snapshot, not on the worktree.
Split verification into a fast local tier and a full tier.

## Scope

Inside: the hook guard, the pre-commit hook, the api_surface tool, the
semgrep rules and ignore file, the Makefile gates, the doc updates.
Outside: package code fixes (see envelope.md and room.md) and the
review-only limit.

### A. Hook guard (scripts/agent_hook_guard.py)

- end the `git commit -n` pattern with `(?:\s|$)` so end-of-string matches.
- strip shell quotes and backslashes from the command before matching.
- block `core.hooksPath` overrides: `git -c core.hooksPath=...` and `git config core.hooksPath`. Exempt the one sanctioned assignment `git config core.hooksPath .githooks`; `make install-hooks` runs it and AGENTS.md mandates it.
- allow global options (`-C dir`, `-c k=v`) between `git` and `commit` in the pattern.
- scan Bash commands for writes to `api/*.txt` (redirect, tee, sed -i), not only Write/Edit paths.
- normpath and realpath Write/Edit targets; treat MultiEdit and NotebookEdit like the settings.json matcher does.
- reframe AGENTS.md honestly: the guard is best-effort against careless agents. No CI exists in this repo, so gates on the committed tree stay aspirational until CI exists. Do not invent CI.

### B. Staged snapshot (.githooks/pre-commit)

- Pass at once when nothing is staged (`git diff --cached --quiet`).
- Export the staged tree with `git archive $(git write-tree)` into a `mktemp -d` directory.
- Run `make -C <tmp> verify-fast` there; a trap removes the temp dir.
- Fail closed: any export failure (write-tree, archive, tar) or gate failure blocks the commit. Use `set -e` or explicit status checks.
- `git write-tree` exits 128 on an unmerged index. That is fail-closed-compatible: git commit refuses unmerged entries anyway.
- The copy builds: go.mod has no dependencies; GOPATH and GOCACHE come from the environment.
- Untracked files never enter the archive; the commit is gated on exactly its content.

### Gate tiers (Makefile)

- New target `verify-fast`: gofmt, vet, one `go test ./...` pass, the python gates, the semgrep scan, the new go.mod and semgrepignore gates, the nosemgrep scan.
- `verify` runs all of the above, plus the coverage floor block and the semgrep probe suite.
- The pre-commit hook runs `verify-fast` on the staged snapshot; local commits never run the test suite twice.
- The coverage re-run of tests is the dropped step; the audit showed it dominates local gate time.

### C. api_surface (scripts/api_surface.go)

- render exported vars: `var Name Type` with a declared type, `var Name` without one.
- render embedded struct fields as the type expression plus the tag.
- render valueless const specs as `const Name` so implicit values are locked.
- fail unless each dir holds exactly one package named after the dir.
- render alias types with `=`: `type T = X`.
- render generic type parameters on types and funcs.
- skip files excluded by build constraints (go/build MatchFile, stdlib).
- render exported methods on unexported receivers, keyed by receiver name.
- Regenerate both locks with `make api-update` in the same change; commit the api/ diff.

### D. Semgrep (semgrep/ plus new gates)

- commit a curated `.semgrepignore`. It lists only `.git/`; test files are scanned again. Add entries only when the repo gains trees to exclude.
- new gate `scripts/check_semgrepignore.py` pins the exact file content and prints a diff on mismatch. The hook guard blocks Write/Edit/Bash edits of `.semgrepignore` like the api/ locks.
- new gate `scripts/check_gomod.py` fails on any require, replace, exclude, or retract directive in go.mod.
- secrets rule: separator becomes `(:=|=|:)`; add bare `token` and `secret` keywords; match backtick values.
- Makefile gains a case-insensitive scan: `grep -riE '(//|#)\s*nosem[g]rep'` outside `.git/` and `semgrep/`. This grep is the enforcement for comment-form suppression. Semgrep self-suppresses findings on marker lines, so the semgrep rule keeps only its line-initial, non-comment reach (plus `(?i)`). Docs and probe code must never write the marker with a comment prefix.
- rule regex fixes: enum literals with a space or backticks; hash prefix backtick and `"sha256" + ":"` concat forms; MarshalIndent and NewEncoder().Encode(); method-form signing `priv.Sign(rand, msg, crypto.Hash(0))`; log.Fatalln and log.Panicln.
- import rule: keep the dot in the positive host regex so stdlib imports stay clean; make the positive and negative patterns case-insensitive to catch `GitHub.com/...`. The go.mod gate is the durable gate for dotless or mixed-case module paths; they cannot compile under Go modules anyway.
- drift rule: add the spaced marker variant and non-Go file scope. The rule gains `exclude: ["/semgrep/**"]` so semgrep/sdk-standards.yml does not self-match; this mirrors the suppression rule.
- new gate `scripts/check_semgrep_probes.py` proves each rule both ways. It writes violating and clean snippets to a temp dir, runs semgrep once, and asserts the expected rule IDs. Temp-dir probes keep the snippets out of gofmt, vet, and the repo scan. The script builds the nosemgrep marker from fragments so the suppression scan never matches it.
- probes, two special cases: the import-rule clean snippet carries slash-bearing stdlib imports (`"encoding/json"`) to prove no false positive. The suppression probe targets the suppression grep, not semgrep: a fixture file with the marker must flip the grep exit code.

### E. Coverage (Makefile)

- the coverage run keeps its output; a failing test aborts `make verify`. Assert the profile lists every package from `go list ./...` minus scripts.
- add a per-package floor: aggregate cover.out per package with awk; each package and the total must reach 85.
- document in AGENTS.md: assertion-free tests and test deletion game the floor; review catches them. Note mutation testing as future work. Add no machinery.
- Status: the mutation kit shipped. See `scripts/check_mutation.py` and `scripts/mutation_denylist/`.

### G. Doc truth (builder executes; outside planner boundaries)

- AGENTS.md: list the new gates and scripts, the `.semgrepignore` protection, the verify-fast versus verify tiers, the best-effort framing, the per-package floor, and the review-only limit.
- README.md: the hook runs verify-fast on the staged snapshot; make verify stays the full gate.
- docs/protocol-design.md lands with commit 1: VerifyThread now rejects duplicate message IDs; the `id` field stays unique within `thread_id`.
- check_structure.py gates Go files only; the new python scripts follow the existing scripts/ style (stdlib, header docstring).

## API

- No new Go packages; `policy/layers.json` is unchanged.
- No exported package symbol changes; the code fixes keep every signature.
- Lock impact of the api_surface fixes on current code: `api/room.txt` gains six lines (`var ErrAlreadyMember`, `var ErrLastModerator`, `var ErrNotMember`, `var ErrNotModerator`, `var ErrUnsigned`, `var ErrWrongRoom`).
- `api/envelope.txt` does not change. Verified by reading envelope/ and room/: no exported vars, embedded fields, implicit consts, aliases, generics, or build-tagged files exist today.
- New make target: verify-fast. New scripts: check_gomod.py, check_semgrepignore.py, check_semgrep_probes.py. New file: .semgrepignore.

## Tests

Gate work has no Go unit tests; probes and reproductions prove it.

- check_semgrep_probes.py: each semgrep rule fix fires on the plain violation and on the confirmed evasion, and stays silent on the clean snippet. The suppression probe asserts the grep exit code on a marker fixture. The plain scan in verify proves no false positives on current code.
- Hook guard: replay each hook-guard attack string; expect a block. Plain `git commit` and clean edits must pass. The reviewer replays this table.
- Hook: staging a semgrep violation must fail the commit; a clean staged change must pass; an empty stage must pass.
- Coverage gate: a failing package must fail verify; one package under 85 with a passing total must fail verify. Prove with temporary local edits, then revert.

## Verification

Order matters; gates must never block the change mid-way.

1. Commit 1 (code; see envelope.md and room.md): the code fixes, their tests, the new invalid_sig_ vector, and docs/protocol-design.md. It passes the existing `make verify` and leaves api/ untouched.
2. Commit 2 (gates): land `verify-fast` first, then api_surface with regenerated locks, then the semgrep rules with green probes, then `.semgrepignore` with its gate and guard entries, then the hook, then AGENTS.md and README.md.
3. A semgrep rule edit lands only when the plain scan passes on current code and the probe suite passes.
4. The hook references verify-fast only after the target exists in the same commit.

Final gates for this plan: `python3 scripts/check_plan.py`,
`python3 scripts/check_deps.py`, and `python3 scripts/check_prose.py`
pass. `make verify` passes on both commits.
