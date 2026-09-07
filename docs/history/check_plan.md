# Plan: check_plan

Copy of the template contract for the plan gate itself. The gate lives
at `scripts/check_plan.py`. It guards the plan documents, not a Go
package, so it owns no `api/` lock.

## Goal

Every Go package carries a plan at `docs/history/<pkg>.md` with the
template sections. The gate makes that structure non-optional. The
plan is the design contract an agent writes before or with the code.

## Scope

Inside: the section structure of every package plan, and the plan
status rule below. Outside: prose quality (`check_prose.py`), finding
labels (`check_labels.py`), and the api lock content
(`check_api.py`). Each concern has its own gate.

## API

The gate is a script, not a package. Its surface is the `check(root)`
function and the `--probe` flag. No `api/` lock exists for it.

### Status sections

A plan section whose status line matches `^Status: planned, not yet
built`, `^Status: planned, not shipped`, or `^Status: planned\.`
must name no exported symbol already locked in `api/<pkg>.txt`. The
section runs from the heading governing the status line to the next
heading of the same or higher level. The gate collects backticked
identifiers that look like exported Go symbols, strips a leading
lowercase dotted package prefix, and reports the first symbol present
in the lock.

Escape hatch: a section that extends an already-shipped symbol
renames its status to `Status: planned, extends <symbol>`. The gate
rejects the form when the named anchor is itself locked: a locked
anchor means the addition it names has shipped, so the section must
rename its status to `Status: shipped` instead.

## Tests

The probe suite in `check_plan.py --probe` covers the status rule:
a planned section naming a locked symbol fails with the file, the
line, and the symbol; a planned section naming no locked symbol
passes; the `planned, extends` escape form naming an unlocked symbol
passes; the same escape form naming a locked anchor fails. The existing probes keep covering the section structure and
the Tests cross-check.

## Verification

- `python3 scripts/check_plan.py` exits zero on a clean tree.
- `python3 scripts/check_plan.py --probe` exits zero; `make verify`
  runs it through the existing wiring.
