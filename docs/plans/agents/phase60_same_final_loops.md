# Phase 60: same-final loop re-entry

Status: shipped. Depends on no unshipped phase. It changes `flow`
and `agentrun` semantics; no new package.

## Why this plan exists

The mivia-agent parity scenarios in `docs/plans/e2e.md` proved one
representability gap. A loop child that ends every iteration on one
status cannot re-enter. The parent's re-entry fires from the previous
child final, so the second iteration needs a self-row, and
`machine.New` forbids a `From` equal to its `To`. Today composers
hide the gap with a parity branch that alternates two finals, as
`pipeline_test.go` and the parity scenarios do. That workaround is a
secret every new composer must rediscover.

The sibling repo's repair loops re-enter the same steps up to
`max_iterations` times with no status alternation. `bug-fix.toml`
bounds five repair cycles per evidence gate. The parity workaround
expresses at most a fail-pass pair per loop parent without extra
branch plumbing, which the `delivery_repair_test.go` budget case
pins: a stubborn host outlasts only a one-repair budget before the
missing same-final row kills the run.

## Goal

Make a loop whose child ends on one status representable. A parent
whose standing already equals the child's final re-enters without a
transition row.

## Scope

Inside:

- `flow.fireFromChild` treats `cur == child` as a satisfied
  transition: no row lookup, no fire, status and record unchanged.
  The rule covers both the looped and the plain chained-step paths.
- The skipped no-op fires no step event; the parent was already at
  the target status, so no observable state changes.
- `agentrun.ValidateMatrix` accepts the same-final re-entry pair and
  demands no row for it. The sequential walk keeps every other row
  requirement.
- One e2e scenario drops its parity alternation and re-pins the
  walk: a stubborn host with a three-repair budget settles terminal
  on the fourth iteration through the missing budget error, not a
  missing row.

Outside:

- Allowing self-rows in `machine.New`. The global invariant stays;
  the loop re-entry path stops needing the row.
- Any change to ack fatality, admission rules, or checkpoint hooks.
  Those contracts stay as the parity scenarios pin them.

## API

No exported symbol changes. `fireFromChild` and the matrix walk are
internal. The behavioral contract lands in the `Loop` and
`LoopPolicy` doc comments: a child may end on one status across
iterations; the parent re-enters without a row when its standing
already matches the child final.

## Tests

- `flow` unit: a two-iteration loop whose child has one final, run
  against a machine with no self-row, completes with the parent's
  confirm firing once.
- `flow` unit: a plain chained step whose standing equals the child
  final also skips the row.
- `agentrun` unit: `ValidateMatrix` accepts the same-final loop
  machine and still rejects a genuinely missing row.
- `e2e`: the delivery budget case re-pins with budget three.

## Verification

- `make verify` passes, including the mutation probe tier.
- The parity scenarios and `pipeline_test.go` stay green unchanged:
  alternating finals remain valid, only no longer required.
- `docs/plans/e2e.md` drops the same-final bullet from its disclosed
  limits in the same change.
