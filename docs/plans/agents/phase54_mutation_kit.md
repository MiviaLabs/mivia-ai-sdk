# Phase 54: mutation kit

Status: future. Plan-only; it has not gone through plan review yet.
Depends on no unshipped phase. It adds no new top-level package.

## Why this phase exists

The coverage floor cannot tell a real test from a vacuous one. Four
surviving mutations escaped every suite during the agentrun review:
a deleted terminal filter, a deleted fallback rule, a reduced panel
union, and a deleted skip rule. All four held the 85 floor. Only
mutation probes caught them.

AGENTS.md lists mutation testing as future work. The 2026 consensus,
from Meta's mutation-guided LLM testing through every practitioner
guide, treats the mutation score as the quality gate for
agent-written tests. Coverage stays the floor; the kill rate becomes
the ceiling's proof.

## Goal

One stdlib-only script, `scripts/check_mutation.py`, applies simple
source mutations to a package, runs that package's tests per mutant,
and reports the kill rate against a floor.

## Scope

Inside:

- Text-level operator mutations over Go source: `==` against `!=`,
  `<` against `<=`, `&&` against `||`, a dropped `!`, and a deleted
  `continue` guard. One mutant per mutation site, applied alone.
- A deterministic mutant list: sorted by file and offset, seeded
  sampling when the caller asks for a subset.
- A per-package kill floor, checked for the packages the run names.
- A denylist of documented equivalent mutants, each with its reason,
  kept in the script.
- A probe mode for the gate's own tests: one planted site the kit
  must generate, and one denylisted site it must skip.
- `make mutation` as an on-demand target. The kit never joins
  `verify-fast`, because a full run costs minutes, not seconds.

Outside:

- Any third-party dependency, including go-mutesting and gremlins.
  The kit stays stdlib: Python's standard library plus the `go`
  tool.
- Any change to `make verify` or `verify-fast`. The kit starts as a
  separate tier, like the Semgrep probes.
- AST-level or compiler-plugin mutations. Text operators found the
  real escapes; deeper machinery adds cost, not findings.

## API

```sh
python3 scripts/check_mutation.py --pkg ledger --floor 85
python3 scripts/check_mutation.py --pkg flow --sample 40
python3 scripts/check_mutation.py --probe
```

`--pkg` names one package directory. `--floor` sets the kill-rate
floor for that run. `--sample` runs the first N mutants in sorted
order. `--probe` proves the kit still generates its planted site and
honors the denylist.

## Tests

The kit's own checks live in `scripts/check_mutation.py --probe` and
in a planted violation under `scripts/probes/mutation/`: one Go file
whose `==` the kit must mutate, and one denylisted site it must
skip. The probe fails when either stops holding.

## Rollout

Two steps, one commit each:

1. The script, the probe, and `make mutation`, with floors set for
   `envelope`, `machine`, and `ledger`. Those three hold the wire
   contract, the status model, and the task record; their mutants
   are small and their suites are fast.
2. Floors for `flow`, `agentrun`, `taskrun`, `a2a`, `identity`,
   `room`, and `tools`, after a full run names each package's
   surviving mutants and their tests land.

A surviving mutant is a test gap, not a script bug. Each rollout
step ends with every survivor either killed by a new test or
denylisted with its reason.

## Verification

- `make verify` passes; the kit changes no package code.
- `python3 scripts/check_mutation.py --probe` passes and joins
  `make verify` as a probe-tier check.
- The first rollout run reports its per-package kill rates in the
  commit message.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
- AGENTS.md's enforcement ladder gains the kill-floor rule at
  rollout step two, not before.
