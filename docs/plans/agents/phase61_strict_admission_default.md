# Phase 61: strict admission default

Status: shipped. It changes `flow` and `agentrun` semantics; no new
package. The matrix walk needed no change: it compares rules by
value and never modeled the tolerant default for non-fallback
steps. Five flow pins updated case by case; no test was deleted.

## Why this plan exists

A branch step's Route excludes the dependents it does not keep. The
exclusion prunes those direct dependents, but their own dependents
still admit by default: `AdmissionOnFinished`, the zero value,
treats a skipped need as finished. A step one hop below an excluded
branch runs even though its need never did. The parity scenarios hit
this live: on the no_bug path, the implement step ran after its
skipped fix_plan need, and only `AdmissionOnSucceeded` on every
branch descendant stopped it.

The sibling repo's engine has no such trap. A step there runs only
when a matched transition into it fired, so exclusion is transitive
by construction. Our tolerant zero value is the outlier, and every
composer must know to arm `AdmissionOnSucceeded` down each excluded
subtree to get the behavior the graph visibly shows.

## Goal

Make the zero-value admission rule strict: a step runs only when
every need ended `OutcomeSucceeded`. A skipped need skips the step.
Skip tolerance becomes an explicit opt-in.

## Scope

Inside:

- Reorder the `Admission` constants so `AdmissionOnSucceeded` is
  the zero value. Rename nothing; the two constants keep their
  names and meanings, only the default swaps.
- `AdmissionOnFinished` keeps its tolerant meaning for callers that
  want a step to run through skipped needs, such as join steps over
  optional branches.
- Every step that relied on the tolerant default and wants to keep
  it declares `When: flow.AdmissionOnFinished` explicitly. The audit
  walks the flow, agentrun, agent, and e2e suites; each newly
  skipping test is either a pin to update or evidence the old
  default was load-bearing, and each case is judged in review.
- `agentrun.ValidateMatrix`'s sequential walk swaps the same
  default, so the validator and the runner keep agreeing.
- The fallback rule `AdmissionOnFailed` is unchanged.

Outside:

- Any change to Route's pruning of direct dependents.
- Any change to panel admission or fallback semantics.
- Adding skip-cause tracking. The strict default removes the need
  to distinguish why a need skipped.

## API

No exported symbol is added or removed. The `Admission` constants
reorder, so the zero value changes meaning; `make api-update` lands
the reordered lock lines in the same change. The `When` doc comments
state the new default and the opt-in.

## Tests

- `flow` unit: a branch-excluded subtree skips transitively under
  the default, with no `When` set anywhere.
- `flow` unit: the same graph with `AdmissionOnFinished` on the
  join step keeps running through the skip.
- `agentrun` unit: the matrix walk agrees with both graphs.
- `e2e`: the parity scenarios drop their defensive
  `AdmissionOnSucceeded` clauses and stay green.

## Verification

- `make verify` passes, including the mutation probe tier.
- Every suite that changes behavior names the case in its diff; no
  test is deleted to make the flip pass.
- `AGENTS.md`'s flow section and `docs/packages/flow.md` state the
  strict default in the same change.
