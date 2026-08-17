# Phase 5: flow sequential runner

Status: ready to build. Builds on phase 4. This phase adds the
sequential runner. It executes the graph in topological order. The ack
confirms each step before the next runs. See
`docs/plans/agents/PHASES.md`.

## Goal

Run a step graph one step at a time. No step runs until the prior ack
confirms. The run returns the final status and the final record.

## Scope

Inside: `Run` for the sequential walk, the `Confirm` ack gate, and the
status result. Outside: the parallel panels, the error join, and the
chaining. Those belong to phases 6 and 7. This phase never imports
`envelope`. `flow` imports only `machine`.

## API

- `type Confirm func(ctx context.Context, step Step) error`
- `Run(ctx, d *Definition, m *machine.Definition, in machine.InOut, confirm Confirm) (machine.Status, machine.InOut, error)`

`Run` walks the graph in topological order. Ready steps run in
declaration order. `Run` keeps the current status. Each `Fire` call
moves it. One record threads through the walk. Each step reads the
record and writes the next.

Each step picks its transition by target status. `Run` takes the rows
`m.AllowedTransitions(cur)` returns. It keeps the row whose `To`
equals `machine.Status(step.To)`. It fires that row's trigger. Zero
matches fail the run. Many matches fail the run. A guard rejection
stops the run. Every failure names the failing step ID.

`Run` fires a step, then calls `confirm`. The next step waits for a
nil return from `confirm`. A nil return means full confirmation. `Run`
rejects a nil `confirm` at entry, before it touches the graph. A step
without a confirmed ack does not advance. The caller owns the ack
transport; `Run` never imports `envelope` in this phase.

`Run` also rejects a nil `d` and a nil `m` at entry, before it
dereferences either pointer. `d` and `m` are pointers, so a nil value
would panic inside `machine.Definition` methods. AGENTS.md bars a
panic path in a package. `Run` checks `d` first, then `m`. The
`d == nil` branch returns a zero `machine.Status` and never calls a
method on `m`, because `m` may be nil too. `machine.Definition.Initial`
has a value receiver, so calling it on a nil `m` still panics.

### Ready-step algorithm

`Run` recomputes the ready set on every iteration. It does not
precompute one topological order up front. Rescanning stays simple and
cheap at this step-graph scale. A precomputed order still needs a live
done-check per step, so it saves little work. The declaration-order
tie-break holds on every pass, because the scan always starts from the
top of `d.steps`.

The builder splits `Run` into three functions, so no single function
nears the 80-line cap and the pinned error wording stays stable under
gate pressure:

- `Run` — owns the nil checks, the loop, and the record threading.
- `nextReady(steps []Step, done map[string]bool) (Step, bool)` — the
  declaration-order scan. Returns the next ready step, or `false` when
  every step is done.
- `pickTransition(rows []machine.Transition, to machine.Status) (machine.Transition, error)` —
  filters `rows` to the one whose `To` equals `to`. Returns the zero-
  match and many-match errors named below.

```text
if d == nil:
    # m may also be nil here; do not call any m method.
    return machine.Status(""), in, error("flow: d must not be nil")
if m == nil:
    return machine.Status(""), in, error("flow: m must not be nil")
if confirm == nil:
    return m.Initial(), in, error("flow: confirm must not be nil")

done := empty set of step IDs
cur  := m.Initial()
rec  := in

loop until len(done) == len(d.steps):
    next, ok := nextReady(d.steps, done)
    if !ok:
        # unreachable when New() accepted the graph; New() already
        # proved the graph acyclic and every Needs entry resolvable.
        return cur, rec, error("flow: no ready step; graph stalled")

    rows := m.AllowedTransitions(cur)
    row, err := pickTransition(rows, machine.Status(next.To))
    if err != nil:
        return cur, rec, error("flow: step %q: %w", next.ID, err)

    cur, rec, err = m.Fire(ctx, cur, row.Trigger, rec)
    if err != nil:
        return cur, rec, error("flow: step %q: %w", next.ID, err)

    if err := confirm(ctx, next); err != nil:
        return cur, rec, error("flow: step %q: ack not confirmed: %w", next.ID, err)

    done.add(next.ID)

return cur, rec, nil
```

Every wrapped error carries the failing step ID through `%q`. The
prefix stays `"flow: step %q: "` for every failure that names a step.
`Run` reuses the package's existing `errorf` helper for the
step-less checks: the nil `d`, the nil `m`, and the nil `confirm`
rejections.

### Error messages

Pin the exact wording so tests can assert on it.

- Nil `d`: `flow: d must not be nil`.
- Nil `m`: `flow: m must not be nil`.
- Nil confirm: `flow: confirm must not be nil`.
- No transition: `flow: step %q: no transition to status %q from %q`.
- Ambiguous transition: `flow: step %q: ambiguous transition to
  status %q from %q (%d candidates)`.
- Fire failure: `flow: step %q: %w` wrapping the `machine.Fire` error.
  This covers a guard rejection.
- Unconfirmed ack: `flow: step %q: ack not confirmed: %w` wrapping the
  `confirm` error.

## Tests

Test files live in `flow/flow_test/`:

- `run_tdd_test.go` — the red-green cases for `Run`. Start with the
  assertions. Confirm they fail on the empty phase; record the red
  output in this file's leading comment or in the commit body. Cover
  the order rule, the transition pick, the nil `d` rejection, the nil
  `m` rejection, the nil `confirm` rejection, and the ambiguity
  failure. Add a case where both `d` and `m` are nil together. Assert
  the `d`-nil error wins and the call does not panic. Implement and
  watch them pass.
- `run_integration_test.go` — run a linear graph of three steps. Prove
  the order and the record threading. Run a diamond graph (one root,
  two mid steps that both need the root, one join step that needs
  both). Prove the declaration-order tie-break: the mid step declared
  first runs first. Add a two-root, asymmetric-depth case that
  separates "ready first" from "declared first". Declare steps `a`
  (needs `x`), `b` (needs `root`), `root`, and `x`, in that order.
  `root` finishes before `x`, so `b` becomes ready before `a`. Prove
  `b` runs before `a`, even though `a` is declared first.
  This proves the scan picks the first ready step, not the first
  declared step. Feed a gate failure through a `machine.Guard` and
  confirm the run stops before the failing step's ack. Prove an
  unconfirmed ack blocks the next step: a `confirm` that returns an
  error must stop the walk before the following step fires.
- `run_perf_test.go` — benchmark `Run` on the linear three-step graph
  with a no-op `confirm`. Target under one millisecond for three
  steps. State the allocation budget: no more than one allocation per
  step for the ready-step scan, plus the record copies `Fire` already
  makes. Record the measured baseline before the phase starts.
  Measured baseline after the build: 217.4 ns/op, 336 B/op, 6
  allocs/op (AMD Ryzen 9 9900X, `go test -bench`). The allocation
  budget test asserts at most 9 allocations, a 50% margin over the
  measured 6.

## Verification

`make verify` passes. The coverage floor for `flow` holds.
`policy/layers.json` gains the `flow` row: `flow` imports `machine`.
The envelope edge waits for phase 7. `api/flow.txt` gains `Run` and
`Confirm` via `make api-update`; commit the `api/` diff in the same
change as the code.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change. List `Run` and `Confirm` in the package
map and the package reference. Drop the "runner stays future" wording
from both files.

`AGENTS.md` updates its `flow/` layout bullet in the same change. The
bullet names `Run` and `Confirm` next to `Step`, `Panel`, and
`Definition`. The bullet drops the "the runner stays future" wording,
because the runner now exists. Parallel work stays out of this phase.
