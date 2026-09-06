# Plan: doc drift batch and the plan-status gate

Two commits. Commit one fixes every stale sentence re-verified by grep
on 2026-09-06. Commit two adds the gate that would have caught the
first class. No code behavior changes. If a fix needs a code change,
leave the sentence and report it.

## Commit one: stale sentences

Re-run each grep before editing. Change each status line listed below
to `Status: shipped.` followed by the commit hash when
`git log -S<symbol>` finds the symbol in one call. Otherwise write
`Status: shipped.` alone. Do not rewrite the section body.

- `docs/plans/a2aclient.md:205` — ten exported sentinels, symbol
  `ErrNoBaseURL`.
- `docs/plans/a2aclient.md:640` — loopback extraction; the
  `a2aloopback` package exists.
- `docs/plans/a2aloopback.md:3` — the package itself.
- `docs/plans/dispatch.md:180` — client sentinels, symbol
  `ErrBadMethod`.
- `docs/plans/dispatch.md:274` — replay ladder, symbol
  `replaySentinels`.
- `docs/plans/longtermmemory.md:47` and `:791` — scope normalization,
  commit `201cec5`.
- `docs/plans/longtermmemory.md:624` — both-core merge guard, commit
  `9e02930`.
- `docs/plans/mcp.md:587` — `ErrNilProgressHandler`.
- `docs/plans/contextplan.md:872` and `docs/plans/agentloop.md:3128`
  — `Calibrated.Observe` pairing.
- `docs/plans/tools.md:469` — `ErrInvalidExecutionClass`.
- `docs/plans/tools.md:496` — `SchemaTool`.

### Stale no-op subscription sentences

Sentences claim `New` subscribes no-op bus handlers. The code
subscribes nothing; both plan addenda record the removal. Rewrite each
to: `Built when nil; no handler is subscribed. Callers add handlers
through Bus().Subscribe.` Sites:

- `agentrun/options.go:53`, `agentrun/wire.go:83-85`,
  `docs/plans/agentrun.md:16`, `docs/packages/agentrun.md:42`.
- `dispatch/options.go:33-34`, `docs/plans/dispatch.md:20-22` and
  `:71` and `:113-116`, `docs/packages/dispatch.md:18` and `:33-37`.
- `docs/examples/agentrun.md:12` and `:30-31`,
  `docs/examples/_agentrun/main.go:5-6`.
- The two addenda at `docs/plans/agentrun.md:364` and
  `docs/plans/dispatch.md:800` keep their meaning but rename the
  removed handlers `placeholder bus handlers`, so the exit grep
  below returns zero.

### Other drift, one line each

- `docs/architecture.md:22`: say forty-five packages. Add the missing
  mermaid edge `contextsummary --> provider`.
- `docs/plans/workspace.md:378-391` and
  `docs/packages/workspace.md:143`: name a removed caller. Add a
  status note under the plan Goal; rewrite the package-doc sentence.
  The toolbox removal is recorded in
  `policy/pending_wiring.json`.
- `docs/plans/subagent.md`: add the command vocabulary (the `Op`
  constants and the `*Command` types from `api/subagent.txt`) to the
  API section.
- `subagent/doc.go:3`: name the three constructor groups (spawn and
  join, mailbox, block wrappers) and point at
  `docs/packages/subagent.md`.
- `ledger/snapshot.go:36-39`: delete the stale `Restore` paragraph.
- `ledger/doc.go:10-11`: move the row-marshal phrase to
  `sqlite_store.go`.
- `docs/packages/dispatch.md:145-150`: add `ledger.ErrNotClaimed`
  with the race-window reason from `dispatch/ladder.go:24-35`.
- `flow/doc.go:4-9`: name every file. Drop the phase sentence.
- `docs/plans/a2aloopback.md:37-40`: reword. The import policy has no
  `a2aloopback` row in any importer, so the deps gate rejects a
  production import.
- `docs/plans/mcp.md:606-704`: rewrite the section to name
  `policy/thirdparty.json`, `scripts/check_thirdparty.py`, and the
  closure lock.
- `docs/plans/secretpath.md:9` and `:242`: add `Status: shipped.` and
  update the Blast radius section. `workspace` imports `secretpath`.
- `docs/plans/memory.md:11-12`: rewrite the Goal sentence to `A Put
  larger than the whole budget fails; a Put that fits evicts the
  oldest blobs first.`
- `docs/plans/toolcallctx.md:7-10`: add the settle-exactly-once
  contract from `batch.go:9-21` and the wake-once contract from
  `batch.go:72-73` to Scope.
- `durablefence/checks.go:17-22` and `:156-158`: reword to `a slow
  backend's busy-retry budget`; drop the private paths.
- `policy/pending_wiring.json`, `agentloop` reason: name the one open
  gap, injection-safe framing, plan
  `docs/plans/agents/phase82_injection_safe_framing.md`. The other
  four gaps are closed in code.
- `policy/pending_wiring.json`: set `workspace`, `envfile`,
  `longtermmemory`, and `mcp` to `permanent=true` with target
  `external application code`. For `longtermmemory`, name the
  intended SDK hook: an `agentloop` system-prompt hook rendering
  `CoreFrame` plus a `subagent` save/search tool.
- `policy/pending_wiring.json`, `secretpath` row: do not add one.
  `check_orphan_packages.py` counts `workspace` as a real caller, so
  a row would be a stale entry.
- `policy/pending_wiring.json`, `e2e` reason: name both callers,
  `e2e/e2e_test` and `subagent/subagent_test/helpers_test.go`.

Gates after commit one: `check_prose.py` and `check_labels.py` pass;
`make verify` exits zero.

## Commit two: the plan-status gate

Extend `scripts/check_plan.py` with one check. Keep one plan gate.

Rule: for each `docs/plans/<pkg>.md`, find every line matching
`^Status: planned, not yet built`. Take the section from its heading
to the next heading of the same or higher level. Collect every
backticked identifier in the section that looks like an exported Go
symbol. If one appears in `api/<pkg>.txt`, fail with the file, the
line, and the symbol. A section with no such identifier passes.

- `--probe` mode: a temp plan with a planned section naming a locked
  symbol must fail; one naming no locked symbol must pass. `make
  verify` runs the probe.
- Positive control: against commit `f2c0edf` the check reports all
  thirteen sites listed above. The plan-reviewer verifies this in a
  scratch worktree. Fewer than thirteen means the rule is too narrow.
- AGENTS.md gets one new enforcement-ladder prohibition naming the
  gate.
- `docs/plans/check_plan.md` gains the rule, the probe, and the
  escape: a section that must stay planned renames the status to
  `Status: planned, extends <symbol>`; the gate ignores that form.
  The script docstring documents the escape.

## API

No exported Go symbol changes. No `api/` lock edit.

## Tests

The gate probe lives in `check_plan.py --probe`, mirroring the other
gates. `TestCheckPlanStatus*` cases cover: fail on a locked symbol,
pass on a plain section, pass on the `planned, extends` escape form.
Wait: the existing probes are Python-side; the Go test set stays
untouched. Add probe cases, not Go tests.

## Verification

- `grep -rn "planned, not yet built" docs/plans/` returns zero after
  commit one.
- A case-insensitive grep over `agentrun`, `dispatch`, and `docs` for
  the three stale claim phrases (the removed-handler phrase, and the
  two `subscribed` pairing phrases) returns zero after commit one.
- `python3 scripts/check_prose.py` and
  `python3 scripts/check_labels.py` pass.
- `make verify` exits zero after each commit.
- The probe fails on a locked symbol and passes without one.
- The check run against `f2c0edf` in a scratch worktree reports
  thirteen sites.

## Positive control

The orchestrator ran the new check against commit `f2c0edf` in a
scratch worktree on 2026-09-06. It reported thirteen findings, one per
site listed in this plan: a2aclient 205 and 640, a2aloopback 3,
agentloop 3128, contextplan 872, dispatch 180 and 274,
longtermmemory 47, 624, and 791, mcp 587, tools 469 and 496. The rule
is not too narrow. The control exits 1 on that tree and exits 0 on the
fixed tree.

## Addendum: closing the extends escape hole

Status: shipped. A later architecture review found the escape this
plan documents had itself become the leak: seven package plans, listed
below, renamed a shipped section to
`Status: planned, extends <symbol>` where `<symbol>` was the very
addition that had shipped, instead of `Status: shipped`. The gate
never inspected the escape form's own claim, so it passed every one
of them. A further six locked-symbol sections evaded the gate under
status wordings the original regex never listed
(`plan, ready for plan review`, `approved`, a bare descriptive
sentence), because the rule was a denylist of known phrasings, not a
closed allowlist.

### Addendum fix

`_check_planned_status` now scans every `Status:` line whose text does
not start `shipped` or `superseded`; that is the only exemption. The
`planned, extends <symbol>` form still excuses its own named symbol
from the body scan, but the rule now also rejects the line outright
when that named symbol is itself locked: a locked anchor means the
addition it names has shipped, so the status must say `shipped`.

Sites corrected in the same change: `docs/plans/contextstate.md:718`,
`docs/plans/dispatch.md:668`, `docs/plans/envelope.md:393`,
`docs/plans/runconfig.md:436` and `:594`, `docs/plans/spool.md:788`,
`docs/plans/subagent.md:898` (the seven `extends` misuses), plus
`docs/plans/agentloop.md:439`, `:881`, `:1078`,
`docs/plans/agentrun.md:163`, `docs/plans/machine.md:185`, and
`docs/plans/flow.md:3` (locked symbols under a status wording the old
regex never matched).

### Addendum tests

`_probe_plan_status` gained cases for an escape naming a locked
anchor (must fail), an escape naming an unlocked anchor with another,
unrelated locked symbol still in the section body (must fail: the
exemption covers only the named anchor), an unfamiliar status wording
naming a locked symbol (must fail) and an unlocked one (must pass),
and `shipped`/`superseded` naming a locked symbol (must pass, the
only exemption).

### Addendum verification

`python3 scripts/check_plan.py --probe` passes. `python3
scripts/check_plan.py` against the live tree exits 0. Re-running the
positive control's script against a fresh `git archive f2c0edf`
checkout reports the locked-symbol class of finding at every site this
addendum names, confirming the widened rule still catches the
original evidence and more.
