---
name: delivery
description: Drives a change through the full gated loop - plan, plan review, build, implementation review, verify - using the repo's subagents. Use for any non-trivial change (new package, API change, more than one file).
---

# Delivery loop

Drive the change through five stages in order. Never skip a stage.
Never let an agent grade its own work. The orchestrator (AGENTS.md)
clarifies the task with the user before Stage 1 and runs Stage 5
itself. If the task is still ambiguous when this skill runs, stop and
return to the user with proposals A/B/C instead of guessing.

## Stage 1: Plan

Dispatch the `planner` agent with the task text. It writes
`docs/plans/<pkg>.md`, the `policy/layers.json` row, and the expected
API surface. It validates with `check_plan.py` and `check_deps.py`.

## Stage 2: Plan review

Dispatch the `plan-reviewer` agent against the plan. It is hostile and
read-only.

- `APPROVE` → go to Stage 3.
- `REVISE` → send the findings back to the `planner`, then review
  again. After 3 REVISE rounds without approval, stop and escalate to
  the user with the unresolved findings.

## Stage 3: Build

Before dispatching, re-read `git log` and `git status`. Another
session may have committed the same fix or test while the plan waited.
Reconcile the planned tests against the files that now exist. A
duplicate test name is a build failure.

Dispatch the `builder` agent with the approved plan. It implements,
tests, and runs `make verify`. Its report must list changed files,
gate output, coverage, and deviations. A silent deviation invalidates
the build.

## Stage 4: Implementation review

Dispatch the `reviewer` agent against the diff and the plan. It
re-runs `make verify` itself and hunts confirmed bugs only.

- `SHIP` → go to Stage 5.
- `FIX` → send findings to the `builder`, then review again. After 3
  FIX rounds, stop and escalate to the user.

## Stage 5: Verify and commit

Stage 5 runs against a tree that may hold unrelated uncommitted work.
Read `git diff --cached` before committing: every hunk must belong to
this change. `git add <file>` sweeps in foreign edits that share the
file. When unrelated work blocks a full-tree `make verify`, verify the
staged tree in a throwaway worktree first.

Run `make verify` one final time yourself. Commit with
`type(scope): imperative subject`. The pre-commit hook re-runs every
gate; never bypass it.

## Stop rules

- Escalate after 3 failed rounds at any review stage.
- Escalate immediately if a gate failure looks like a gate bug (report
  it; do not patch the gate to make it pass).
- Escalate if the task conflicts with AGENTS.md.

## A gate firing on legitimate work

A gate can fire correctly on a change the plan explicitly requires: a
test-tampering heuristic cannot always tell a mandated rewrite from
real tampering, and a gate-infra rule can conflict with a doc-pairing
rule AGENTS.md itself mandates. This is a third case, distinct from a
gate bug and a real violation.

- The builder never decides this alone. It stops and reports the
  exact finding, the file:line the gate cites, and the plan text it
  believes justifies an override.
- The orchestrator verifies the builder's claim independently before
  acting on it: read the cited diff hunk and the plan section it
  points to, and confirm the plan genuinely mandates the change, not
  just that the builder says so.
- Only after that verification does the orchestrator use the
  documented override trailer (`Allow-Test-Change` or
  `Allow-Gate-Change`) with a reason that names the specific plan
  requirement, not a generic excuse. One trailer per finding.
- An override used to route around a gate the orchestrator has not
  personally verified is indistinguishable from bypassing the gate.
  Never authorize one on the builder's word alone.
