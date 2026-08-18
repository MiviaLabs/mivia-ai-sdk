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

Run `make verify` one final time yourself. Commit with
`type(scope): imperative subject`. The pre-commit hook re-runs every
gate; never bypass it.

## Stop rules

- Escalate after 3 failed rounds at any review stage.
- Escalate immediately if a gate failure looks like a gate bug (report
  it; do not patch the gate to make it pass).
- Escalate if the task conflicts with AGENTS.md.
