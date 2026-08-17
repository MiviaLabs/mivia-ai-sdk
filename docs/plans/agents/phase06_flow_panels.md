# Phase 6: flow parallel panels

Status: ready to build. Builds on phase 5. This phase adds the
parallel waves. Panels run in goroutines. The runner gathers the
results. See `docs/plans/agents/PHASES.md` for the contract.

## Goal

Run a panel of independent steps at once. Each wave holds steps with
no remaining dependency. The waves run in sequence. The steps inside a
wave run in parallel. Errors combine without a third-party library.

## Scope

Inside: the wave scheduler, the goroutine pool, the buffered channel,
`errors.Join`, the panel-homogeneity check, the panel-duplicate-ID
check, and the panel-independence check in `New`. Outside: the
chaining and the nested workflow. Those belong to phase 7. Outside:
merging more than one step's output record. A panel wave forwards one
record, chosen by a fixed rule below. Merging every member's record is
future work; no consumer needs it yet. Outside: detecting a
cross-panel scheduling deadlock. See the stall-branch note below the
independence check.

## API

No new exported symbol. `Run` gains parallel behavior for a panel. The
wave is internal state. The return stays `(Status, machine.InOut, error)`.
`New` gains three validation rules for panels; its signature and error
type do not change. `Run` gains one unexported helper, `markDone`; see
"Keeping Run under the line cap" below.

### Panel homogeneity and panel-membership checks, checked in `New`

A panel groups steps that fire together and land on one status. `New`
rejects a panel whose members do not all share one `To`. This keeps
`cur` well-defined after a wave. Every member fires from the same
`cur` and lands on the same `To`, so the wave has exactly one
resulting status, never several.

`validatePanels` also rejects a panel that names the same step ID
twice, such as `Panel{"A", "A"}`. Left unchecked, `runWave` would
spawn two goroutines for one step ID. Both goroutines would fire that
step's `Guard`, `OnExit`, and `OnEntry` at once. That duplicates side
effects; it is not merely wasted cycles.

`validatePanels` gains both checks next to its existing unknown-step
check. The homogeneity check reads each member's `To`, so
`validatePanels` needs the step slice, not only the ID-to-index map.
Its signature changes to:

`validatePanels(panels []Panel, steps []Step, ids map[string]int) error`

Within one panel, the check order is fixed. The unknown-step check
runs first, for every member, in declaration order. The duplicate-ID
check runs second, keeping a per-panel seen-set of resolved IDs,
populated as the unknown-step check passes each member. The
homogeneity check runs last, after every member passes both of the
first two checks. It reads a member's `To` through `steps[ids[id]]`,
which requires the member to already resolve to a known step.

A panel with both an unknown member and a `To` disagreement returns
the unknown-step message; the homogeneity check never runs for that
panel. A panel with both an unknown member and a duplicate member
returns the unknown-step message. The duplicate check only runs on an
ID the unknown-step check already resolved. Pin all three messages:

- `flow: panel %d names unknown step %q` — unchanged from phase 6's
  first draft and from the existing unexported check. `%d` is the
  panel index. `%q` is the unresolved step ID.
- `flow: panel %d names step %q twice` — `%d` is the panel index.
  `%q` is the step ID that appears more than once, named at its
  second occurrence in declaration order.
- `flow: panel %d: step %q and step %q disagree on To (%q vs %q)` —
  `%d` is the panel index. The two `%q` step pairs are the first step
  in the panel and the first step whose `To` differs, in declaration
  order. The trailing `%q` pair is their `To` values.

### Panel independence, checked in `New` after `findRoots`

A panel fires as one group: every member fires together, from one
shared `cur`, once every member is ready. A member whose `Needs`
closure contains another member of the same panel can never see that
condition. The dependency can only finish as part of the very group
firing it waits on. `New` rejects this shape before `Run` ever
deadlocks on it.

This check needs the full transitive `Needs` closure of every step,
not only the direct edges `validateSteps` already checked. Walking a
closure needs an acyclic graph, so this check runs after `findRoots`
proves the graph acyclic, in a new unexported function:

`validatePanelIndependence(steps []Step, panels []Panel, ids map[string]int) error`

`New`'s call order becomes: `validateSteps`, `validatePanels`,
`findRoots`, `validatePanelIndependence`. Pseudocode:

```text
validatePanelIndependence(steps, panels, ids):
    ancestors := memoized map[string]set[string]   # per step ID, computed
                                                     # once via DFS over
                                                     # Needs; safe because
                                                     # findRoots already
                                                     # proved the graph
                                                     # acyclic
    for i, p := range panels, in declaration order:
        for _, id := range p, in declaration order:
            for _, other := range p, in declaration order:
                if other == id:
                    continue
                if ancestors(id) contains other:
                    return errorf(
                        "panel %d: step %q needs step %q, a member of the same panel",
                        i, id, other,
                    )
    return nil
```

Pin the message:

- `flow: panel %d: step %q needs step %q, a member of the same panel`
  — `%d` is the panel index. The first `%q` is the first member, in
  declaration order, whose `Needs` closure reaches a fellow member.
  The second `%q` is the first such fellow member, in declaration
  order.

`New` still does not reject a step named in two panels. That stays
undefined behavior a future phase may close, since no consumer builds
that shape yet.

### The stall branch in `nextReadyGroup`: what stays reachable

Phase 5 called `nextReady`'s empty return "unreachable when `New`
accepted the graph." Phase 6 renames the scan `nextReadyGroup` and
must correct that claim, not repeat it. `validatePanelIndependence`
closes one path to the stall branch: a `Needs` cycle inside one panel.
It does not close every path.

A cross-panel scheduling deadlock stays reachable. A member of panel
A needs a member of panel B, and a member of panel B needs a member of
panel A. Neither shape puts a cycle in the `Needs` graph itself, and
neither violates a single panel's closure rule. `New` validates each
panel's own independence, not the scheduling feasibility of two panels
together. `Run`'s stall branch is the only place this shape surfaces,
as a runtime error, not a `New`-time rejection.

Update the code comment on this branch to say so exactly:

```text
// Unreachable for a same-panel Needs cycle: validatePanelIndependence
// rejects that shape in New. Still reachable for a cross-panel
// scheduling deadlock: a member of one panel needs a member of
// another panel, and vice versa, with no cycle in the Needs graph and
// no single panel's closure violation. New does not validate
// cross-panel scheduling feasibility; a future phase may close this
// gap.
```

### Wave grouping: `d.panels` drives it, not a recomputed readiness set

A wave is either one singleton step (the existing phase 5 path,
unchanged) or one whole panel, run together. A step belongs to a wave
group by membership in `d.panels`. A step named in no panel always
runs alone, exactly as phase 5 already runs it. `Run` never invents a
wave from steps that merely happen to be ready together; it only
groups steps the caller explicitly named in one `Panel`.

`nextReadyGroup(steps []Step, panels []Panel, done map[string]bool) ([]Step, bool)`
replaces `nextReady` as the scan `Run` calls each iteration:

```text
for s in steps, in declaration order:
    if done[s.ID]: continue
    if not every s.Needs in done: continue
    # s is ready
    p, found := the first panel in panels that contains s.ID
    if not found:
        return [s], true               # singleton wave, phase 5 path
    if every member of p is ready (not done, all Needs in done):
        return members of p, in p's declaration order, true
    # p is not fully ready yet; s waits, keep scanning for another
    # ready step so a partially-ready panel never blocks the graph
return nil, false
```

### Wave execution

`runWave` computes the shared transition row once, before it spawns
any goroutine. `validatePanels`'s homogeneity rule guarantees every
member shares one `To`, so `pickTransition(rows, to)` is a pure
function of `(rows, to)`. It cannot fail for one member and succeed
for a sibling. Resolving the row up front removes N redundant,
identical `pickTransition` calls and closes that impossible case
outright.

```text
type waveResult struct {
    step Step
    to   machine.Status
    rec  machine.InOut
    err  error
}

runWave(ctx, m, cur, rec, group) (machine.Status, machine.InOut, error):
    rows := m.AllowedTransitions(cur)
    to := machine.Status(group[0].To)   # homogeneous across group, by
                                         # New's panel-homogeneity rule
    row, err := pickTransition(rows, to)
    if err != nil:
        # the whole group fails before any goroutine spawns; no
        # member's Guard, OnExit, or OnEntry runs
        return cur, rec, errorf("panel: %w", err)

    results := make(chan waveResult, len(group))
    var wg sync.WaitGroup
    for _, s := range group:
        wg.Add(1)
        go func(s Step):
            defer wg.Done()
            recCopy := rec              # each goroutine gets its own
                                         # copy; none touches rec or
                                         # any other goroutine's copy
            to, out, err := m.Fire(ctx, cur, row.Trigger, recCopy)
            if err != nil:
                results <- waveResult{step: s, err: errorf("step %q: %w", s.ID, err)}
                return
            results <- waveResult{step: s, to: to, rec: out}
        (s)
    wg.Wait()
    close(results)

    var errs []error
    byID := map[string]waveResult{}   # first group member's result is
                                       # read back out by ID below, not
                                       # by channel arrival order, so the
                                       # forwarded record stays
                                       # deterministic under goroutine
                                       # scheduling
    for r := range results:
        byID[r.step.ID] = r
        if r.err != nil:
            errs = append(errs, r.err)
    if len(errs) > 0:
        return cur, rec, errors.Join(errs...)   # wave fails as one unit;
                                                  # cur and rec stay at
                                                  # their pre-wave values;
                                                  # no group member is
                                                  # marked done
    first := byID[group[0].ID]
    return first.to, first.rec, nil
```

Pin the whole-group failure messages, produced by wrapping
`pickTransition`'s error with `errorf("panel: %w", err)`:

- `flow: panel: no transition to status %q from %q`
- `flow: panel: ambiguous transition to status %q from %q (%d candidates)`

`Run`'s loop calls `nextReadyGroup` instead of `nextReady`. On a
singleton group it keeps calling `pickTransition` and `m.Fire`
directly, exactly as phase 5 does. The sequential path stays unchanged
code, not a one-member wave dressed up. On a multi-member group it
calls `runWave`. Either way, on success `Run` marks every group member
`done` in one pass. The single goroutine running the loop does this
marking, after the group's call returns, never inside a spawned
goroutine. So `done` never sees a concurrent write.

The forwarded record after a successful wave is the output of the
group's first member in declaration order. `Run` picks that record
after every member finished, not by channel arrival order. The other
members' `Fire` calls still ran and their guards, `OnExit`, and
`OnEntry` still fired; only their resulting records are discarded. A
caller whose panel members need their outputs merged must not rely on
this phase. This gap is real; a future phase closes it once a
consumer needs it.

A failed gate anywhere in the wave fails the whole run: `Run` returns
the joined error, the pre-wave `cur`, and the pre-wave `rec`. No group
member is marked `done`, and `Run` never starts the next wave. This
matches phase 5. A rejected step already stops the walk before its own
ack. A wave rejection stops the walk before any member's ack.

### One shared transition row, called concurrently

Every member of a group fires through the same `row`: one `Guard`,
one `OnExit`, one `OnEntry`. Each fires once per group member, at the
same time, from separate goroutines. Only each member's own `InOut`
record differs. A panel member's `Guard`, `OnExit`, and `OnEntry`
closures must be reentrant and safe for concurrent invocation. `flow`
calls them from N goroutines at once whenever a panel has N members.

### The copy is shallow; a shared reference-typed record still races

`recCopy := rec` copies the `InOut` struct, not the data an `any`
field points to. `Input` and `Output` are `any`. A caller's `Input` or
`Output` may hold a map, a slice, or a pointer. When two panel members
both mutate that value in place, through their own `OnEntry` or
`OnExit`, they still write the same underlying data. Each holds its
own `InOut` value, but the aliased data is shared. `flow` cannot
deep-copy an arbitrary `any` value; it does not know the concrete
type.

This is a caller contract, not a runtime check. A panel member's
`Input` and `Output` must be either an immutable value, or a value the
caller has already cloned per step. A step that needs a mutable,
step-local record must build that record fresh inside its own
`OnEntry`. It must not read that record from a pointer or map shared
with a sibling in the same panel. Document this contract on `Run`'s
doc comment when phase 6 lands.

This is a deliberate, recorded exception to the AGENTS.md rule that a
comment-stated invariant needs `Validate` enforcement. `flow` has no
runtime way to inspect an arbitrary `any` value's aliasing, so no
`Validate` check can enforce this rule.

### Keeping Run under the line cap

Phase 5 pre-split `Run` into three functions to stay under the 80-line
function cap. Phase 6 adds a singleton-versus-group branch and
multi-member done-marking; the builder splits the same way, naming the
split up front:

- `Run` — owns the nil checks, the loop, the singleton-versus-group
  branch, and the record threading. It calls `nextReadyGroup`, then
  either the phase 5 singleton path or `runWave`, then `markDone`.
- `nextReadyGroup` — the declaration-order scan described above.
- `pickTransition` — unchanged from phase 5.
- `runWave` — the group execution described above.
- `markDone(done map[string]bool, group []Step) ` — marks every member
  of `group` done in `done`, in one pass. `Run` calls it once per
  successful singleton (a one-element `group`) and once per successful
  wave, so the done-marking code exists once, not twice.

## Tests

Test files live in `flow/flow_test/`:

- `phase06_tdd_test.go` — the red-green cases. Start with the
  assertions; confirm they fail on the empty phase; implement and
  watch them pass. Cases:
  - `New` rejects a panel whose members disagree on `To`, with the
    pinned message.
  - `New` accepts a panel whose members share one `To`.
  - `New` rejects a panel that names the same step ID twice, with the
    pinned message.
  - `New` rejects a panel with both an unknown step and a duplicate
    step with the unknown-step message, proving the unknown-step
    check runs before the duplicate-ID check.
  - `New` rejects a panel where one member's `Needs` closure contains
    another member of the same panel, with the pinned message. Cover
    both a direct edge (member A needs member B) and a transitive
    edge (member A needs a step that needs member B).
  - `New` rejects a panel with both an unknown step and a `To`
    disagreement with the unknown-step message, proving the
    unknown-step check runs first.
  - `Run` returns the pinned stall error on a cross-panel scheduling
    deadlock. Panel A holds a member that needs a member of panel B.
    Panel B holds a member that needs a member of panel A. Neither
    panel repeats a member, so `validatePanelIndependence` accepts
    the definition. `Run` still stalls at the first wave, because
    neither panel ever becomes fully ready. Assert the returned error
    is `flow: no ready step; graph stalled`.
  - `nextReadyGroup` returns a singleton for a step in no panel.
  - `nextReadyGroup` returns a whole panel once every member is
    ready, and skips a partially-ready panel to return another ready
    step instead.
  - `runWave` returns the first group member's record, by
    declaration order, on an all-success wave.
  - `runWave` returns a joined error and the pre-wave `cur`/`rec` when
    one member's gate rejects it.
  - `runWave` returns a single, non-joined error, wrapped as
    `flow: panel: %w`, when `pickTransition` fails for the shared row,
    before any goroutine spawns. No member's `Fire` runs. Assert this
    with a `machine.Definition` whose rows never match the group's
    `To`, so a rejecting `Guard` would prove the miss if `Fire` ran.
  - A wave with two failing members returns an error that satisfies
    `errors.Is` and wraps both per-step messages. The test never
    asserts the full joined string, because `errors.Join`'s argument
    order follows goroutine completion order, not declaration order.
  - A `Guard` closure increments a shared `atomic.Int64` and checks
    the count. Run this case under `go test -race` with a four-member
    panel. It proves the shared row's `Guard` runs once per member,
    concurrently, without a race.
- `phase06_integration_test.go` — run a panel of four independent
  steps that share one `To`. Prove all four complete and that `done`
  advances past the whole panel in one step of `Run`'s loop. Feed one
  failing step, using a rejecting `Guard`. Confirm `errors.Join`
  reports it, `Run` returns the pre-wave status, and no sibling in
  that panel is marked done. Mix a panel with a singleton step that
  depends on the panel's output, and prove the singleton waits for the
  whole panel. Give each member an `Input` holding its own,
  independently allocated map. Run this case under `go test -race` to
  prove `flow` itself never writes `done` or a shared `rec`
  concurrently. Do not give two members the same underlying map in
  this test. That shape is a caller contract violation, not a `flow`
  bug. `-race` correctly catches it as user error, not as a scheduler
  defect.
- `phase06_perf_test.go` — before any phase 6 code lands, add a
  benchmark for a ten-step sequential chain on the phase 5 `Run` path.
  Record its ns/op, B/op, and allocs/op in this file's leading
  comment, per `PHASES.md`'s baseline-before-phase contract. No
  ten-step baseline exists yet; phase 5's own benchmark measured a
  three-step chain at 217.4 ns/op, which is not comparable at ten
  steps. After phase 6 lands, benchmark a ten-step panel (one panel,
  ten members, one shared `To`) and compare it against the freshly
  recorded ten-step sequential baseline. Report the ratio; the
  parallel run should track the slowest member, not the sum of all
  ten. Report the allocs/op ratio too; do not assert a budget on it.
  Goroutine, channel, and closure overhead raise the panel path's
  allocation count over the sequential baseline, so a fixed multiplier
  is not a meaningful pass/fail gate here. The fresh ten-step
  sequential baseline still must be measured and recorded before phase
  6 code lands, per `PHASES.md`'s baseline-before-phase contract.

## Verification

`make verify` passes. The coverage floor for `flow` holds. Run
`go test -race ./...` for the goroutine pool. Parallelism does not
trade correctness. `api/flow.txt` is unchanged; do not run
`make api-update` for this phase.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change, describing panel-driven waves. `AGENTS.md`
updates its `flow/` bullet if the panel behavior changes what it
describes about parallel panels staying future. `docs/plans/flow.md`
drops the "panels... stay future" wording from its Status line in the
same change, since phase 6 ships the panels.
