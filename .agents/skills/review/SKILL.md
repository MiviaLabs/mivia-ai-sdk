---
name: review
description: >-
  Run the deep adversarial review of code in this repo. Trigger whenever
  the user types "review", "audit", or asks to deep-review, adversarially
  review, or verify that a change is correct and its gates are intact.
  This skill owns the full pass: make verify, race, fuzz, the reviewer
  agent, and the gate-integrity audit. Use it instead of a skim. It
  covers gate integrity, doc truth, and plan drift.
---

# Review

A review is adversarial, deep, and evidence-backed. It is never a skim.
Run the mechanical gates, dispatch the independent reviewer agent, and
audit the gates when the change touched them. Hunt confirmed bugs only.
Fix nothing during the review. Report first.

This skill owns the `review` and `audit` triggers that once lived in
AGENTS.md. Invoke it for either word.

## The workflow

Do these in order. Stop nowhere. Consolidate into one report at the end.

### 1. Mechanical gates

Run the full suite. Record every result. A single failure is a finding.

- `make verify` — gofmt, vet, tests, the python gates, the semgrep
  scan, the coverage floor, and the probes.
- `go test -race ./...` — data races and the race detector.
- A short fuzz pass. Run each `Fuzz*` target for a fixed time.
  `go test -fuzz=<name> -fuzztime=15s ./<pkg>/`

### 2. The reviewer agent

Dispatch the `reviewer` subagent against the change. Follow its
instruction in `.agents/agents/reviewer.md`. The reviewer is
independent; never let it grade its own work. It is read-only and
returns SHIP or FIX. Require its reproduction for every finding.

When the change touched a plan, `policy/layers.json`, `api/`,
`scripts/`, `semgrep/`, the `Makefile`, or `.githooks/`, also run a
hostile audit of the gates. Prove each gate can still catch a planted
violation. Report any bypass.

### 3. The four coverage axes

Cover all of these in the report. Each is a finding source.

- **Gate integrity** — did the change weaken a gate, raise a limit, or
  widen an exclusion? Check `scripts/`, `semgrep/`, the `Makefile`, and
  `.githooks/` line by line against the change.
- **Doc truth** — do the doc comments, README, plan, and
  `docs/architecture.md` describe the code as it now is? A stale
  claim is a finding.
- **Plan drift** — does the change match its plan
  (`docs/plans/<pkg>.md`)? Scope creep is a finding. A dropped Scope
  item is a finding. Every exported symbol must match `api/<pkg>.txt`.
- **Test adequacy** — do the tests fail when the code is broken? Pick
  an invariant and mentally mutate the code. If no test catches it,
  that is a finding. For test-quality depth, use the `test-review`
  skill; this pass covers the basic adequacy check.

## Finding discipline

Confirmed bugs only. Every finding needs these four:

1. A reproduction. A command the reviewer runs, or a scratch test under
   `/tmp` with a replace directive. No reproduction, no finding.
2. A severity: low, medium, or high.
3. A file:line.
4. A minimal fix.

Guesses are not findings. Style preferences are not findings. Never
pad the report.

## The audit variant

When the user asks for an `audit` instead of a `review`, do all of the
above plus the evasion probes:

- **Semgrep probes** — run the probe suite in `make verify` and add a
  planted violation. Prove the rule fires on it and stays silent on
  clean code.
- **Hook probes** — prove the pre-commit hook still blocks a planted
  violation on the staged snapshot. Never bypass the hook to test.
- **Lock probes** — prove the API lock and `check_api.py` still catch a
  deliberate, undocumented surface change.
- **Ignore probes** — prove `check_semgrepignore.py` still pins the
  exact `.semgrepignore` content and labels can be planted and caught.

Report which probe caught the planted violation. An evasion is a
finding.

## Output format

Write one consolidated report in this order.

1. The verdict for each coverage axis: holds or fails.
2. Evidence: the gate results you collected.
3. Findings: each with a reproduction, a severity, a file:line, and a
   minimal fix.
4. The recommendation. Pick the smallest change that fixes the
   confirmed findings, or "no change, the limit is documented".

## Constraints

- Never change code during the review. Report first.
- Do not weaken a gate or raise a limit to make a pass. Change the
  design instead, or convince the user and record the exception in the
  gate file itself.
- Follow the STE writing standard. One idea per sentence. Sentences stay
  at or below 25 words.
- No audit-finding labels. A label is a letter A through G followed by a
  digit. Describe rules with words.
- Never bypass Git hooks. Run `make verify` before you finish.

## After the review

When the user asks for the fix, route it through the delivery loop in
`.agents/skills/delivery/SKILL.md`: planner, plan-reviewer, builder,
reviewer, verify, commit. This skill reviews. It does not build.
