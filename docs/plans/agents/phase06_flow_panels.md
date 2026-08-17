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
`errors.Join`, and the panel-homogeneity check in `New`. Outside: the
chaining and the nested workflow. Those belong to phase 7. Outside:
merging more than one step's output record. A panel wave forwards one
record, chosen by a fixed rule below. Merging every member's record is
future work; no consumer needs it yet.

## API

No new exported symbol. `Run` gains parallel behavior for a panel. The
wave is internal state. The return stays `(Status, machine.InOut, error)`.
`New` gains a validation rule for panels; its signature and error type
do not change.

### Panel homogeneity, checked in `New`

A panel groups steps that fire together and land on one status. `New`
rejects a panel whose members do not all share one `To`. This keeps
`cur` well-defined after a wave: every member fires from the same
`cur` and lands on the same `To`, so the wave has exactly one
resulting status, never several. `validatePanels` gains this check
next to its existing unknown-step check. Pin the message:

- `flow: panel %d: step %q and step %q disagree on To (%q vs %q)`
  — `%d` is the panel index, the two `%q` step pairs are the first
  step in the panel and the first step whose `To` differs, in
  declaration order, and the trailing `%q` pair is their `To` values.

### Wave grouping: `d.panels` drives it, not a recomputed readiness set

A wave is either one singleton step (the existing phase 5 path,
unchanged) or one whole panel, run together. A step belongs to a wave
group by membership in `d.panels`; a step named in no panel always
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

`New` already rejects a panel naming an unknown step
(`validatePanels`). Phase 6 adds one more rule there: it does not
reject a step in two panels; that stays undefined behavior a future
phase may close, since no consumer builds that shape yet.

### Wave execution

```text
type waveResult struct {
    step Step
    to   machine.Status
    rec  machine.InOut
    err  error
}

runWave(ctx, m, cur, rec, group) (machine.Status, machine.InOut, error):
    rows := m.AllowedTransitions(cur)
    results := make(chan waveResult, len(group))
    var wg sync.WaitGroup
    for _, s := range group:
        wg.Add(1)
        go func(s Step):
            defer wg.Done()
            recCopy := rec              # each goroutine gets its own
                                         # copy; none touches rec or
                                         # any other goroutine's copy
            row, err := pickTransition(rows, machine.Status(s.To))
            if err != nil:
                results <- waveResult{step: s, err: errorf("step %q: %w", s.ID, err)}
                return
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

`Run`'s loop calls `nextReadyGroup` instead of `nextReady`. On a
singleton group it keeps calling `pickTransition` and `m.Fire`
directly, exactly as phase 5 does, so the sequential path is
unchanged code, not a one-member wave dressed up. On a multi-member
group it calls `runWave`. Either way, on success `Run` marks every
group member `done` in one pass, from the single goroutine running
the loop, after the group's call returns — never inside a spawned
goroutine, so `done` never sees a concurrent write.

The forwarded record after a successful wave is the output of the
group's first member in declaration order, chosen after every member
finished, not by channel arrival order. The other members' `Fire`
calls still ran and their guards, `OnExit`, and `OnEntry` still fired;
only their resulting records are discarded. A caller whose panel
members need their outputs merged must not rely on this phase; it
names a real gap for a future phase to close, once a consumer needs
it.

A failed gate anywhere in the wave fails the whole run: `Run` returns
the joined error, the pre-wave `cur`, and the pre-wave `rec`. No
group member is marked `done`, and `Run` never starts the next wave.
This matches phase 5: a rejected step already stops the walk before
its own ack, and a wave rejection stops the walk before any member's
ack.

### The copy is shallow; a shared reference-typed record still races

`recCopy := rec` copies the `InOut` struct, not the data an `any`
field points to. `Input` and `Output` are `any`. When a caller's
`Input` or `Output` holds a map, a slice, or a pointer, two panel
members that both mutate it in place through their own `OnEntry` or
`OnExit` still write the same underlying data, even though each holds
its own `InOut` value. `flow` cannot deep-copy an arbitrary `any`
value; it does not know the concrete type. This is a caller contract,
not a runtime check: a panel member's `Input` and `Output` must be
either an immutable value or a value the caller has already cloned
per step. A step that needs a mutable, step-local record must build
that record fresh inside its own `OnEntry`, not read it from a
pointer or map shared with a sibling in the same panel. Document this
contract on `Run`'s doc comment when phase 6 lands.

## Tests

Test files live in `flow/flow_test/`:

- `phase06_tdd_test.go` — the red-green cases. Start with the
  assertions; confirm they fail on the empty phase; implement and
  watch them pass. Cases:
  - `New` rejects a panel whose members disagree on `To`, with the
    pinned message.
  - `New` accepts a panel whose members share one `To`.
  - `nextReadyGroup` returns a singleton for a step in no panel.
  - `nextReadyGroup` returns a whole panel once every member is
    ready, and skips a partially-ready panel to return another ready
    step instead.
  - `runWave` returns the first group member's record, by
    declaration order, on an all-success wave.
  - `runWave` returns a joined error and the pre-wave `cur`/`rec` when
    one member's gate rejects it.
  - `runWave` returns a joined error when `pickTransition` fails for
    one member (no matching or ambiguous row), leaving the other
    members' successful `Fire` calls discarded.
  - A wave with two failing members returns an error that satisfies
    `errors.Is`/wraps both per-step messages; the test never asserts
    the full joined string, because `errors.Join`'s argument order
    follows goroutine completion order, not declaration order.
- `phase06_integration_test.go` — run a panel of four independent
  steps that share one `To`; prove all four complete and `done`
  advances past the whole panel in one step of `Run`'s loop. Feed one
  failing step (a rejecting `Guard`) and confirm `errors.Join` reports
  it, `Run` returns the pre-wave status, and no sibling in that panel
  is marked done. Mix a panel with a singleton step that depends on
  the panel's output, and prove the singleton waits for the whole
  panel. Give each member an `Input` holding its own, independently
  allocated map, and run under `go test -race` to prove `flow` itself
  never writes `done` or a shared `rec` concurrently. Do not give two
  members the same underlying map in this test; that shape is a
  caller contract violation, not a `flow` bug, and `-race` would
  correctly catch it as user error, not as a scheduler defect.
- `phase06_perf_test.go` — benchmark a ten-step panel (one panel, ten
  members, one shared `To`) against a ten-step sequential chain from
  phase 5's benchmark. Record the sequential baseline first, measured
  before this phase's code lands. Then measure the panel and report
  the ratio; the parallel run should track the slowest member, not
  the sum of all ten.

## Verification

`make verify` passes. The coverage floor for `flow` holds. Run
`go test -race ./...` for the goroutine pool. Parallelism does not
trade correctness.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change, describing panel-driven waves. `AGENTS.md`
updates its `flow/` bullet if the panel behavior changes what it
describes about parallel panels staying future.
