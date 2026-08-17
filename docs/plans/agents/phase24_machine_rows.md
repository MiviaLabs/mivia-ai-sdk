# Phase 24: machine row accessors allocate once

Status: done. This phase fixes an allocation defect that
flow's phase 7 benchmark exposed. The machine block plan lives in
`docs/plans/machine.md`. See `docs/plans/agents/PHASES.md` for the
contract.

## Goal

Make `AllowedTransitions` and `AllowedTriggers` allocate exactly one
slice per call. Drop the chained benchmark to the flat baseline alloc
count. Remove the zero-margin allocation budget in flow's phase 7
benchmark.

## Scope

Inside: the two accessor bodies and the `AllowedTriggers` doc comment
in `machine/definition.go`, the alloc budget test in
`machine/machine_test/status_bench_test.go`, the leading comment of
`flow/flow_test/chain_bench_test.go`, and one paragraph in
`docs/plans/machine.md`.

Outside: caching, exported-symbol changes, wire changes, flow code,
`policy/layers.json`, and `docs/protocol-design.md`. The lock file
`api/machine.txt` stays byte-identical.

## API

No exported symbol changes and no signature changes. Both accessors
keep their value receivers; the `Decode` result in `machine/wire.go`
is not addressable and needs them. Run `make api-update`; it must
produce no diff on `api/machine.txt`. That absence is the proof of no
surface change.

Behavior stays identical. Both accessors return rows in declaration
order. Both return an empty slice when no rows match. Both keep the
fresh-copy contract that `docs/plans/machine.md` states. The fix adds
no state to `Definition`.

### Defect

`AllowedTransitions` grows an empty slice through `append`. A status
with two outgoing rows allocates twice: one slice at capacity one,
then a larger slice at capacity two. The chained runner calls
`AllowedTransitions` four times on a two-row status. The chained
benchmark reports twelve allocs per op against the flat baseline's
eight. That count equals the 1.5x budget with zero margin. See the
leading comment in `flow/flow_test/chain_bench_test.go`.

`AllowedTriggers` has the same append growth plus a `seen` map. Its
cost depends on escape analysis. At an escaping call site, a call with
two matching rows allocates three times: the map, then two slice
growths. At a call site that discards the result, the map and the
growths stack-allocate, so the call allocates zero times.

### Design: two passes, one exact-size slice

Each accessor makes two passes over `d.transitions`. The first pass
counts the rows whose `From` equals the argument. The second pass
allocates one slice of that exact size and fills it in declaration
order.

`AllowedTransitions` appends each matching row into the exact-size
slice. An append into free capacity does not allocate again.

`AllowedTriggers` drops the `seen` map and emits `t.Trigger` for each
matching row. No dedup scan replaces the map. Distinctness is an
invariant, not a runtime filter: `Validate` rejects two rows that
share `From` and `Trigger`. Both `New` and `Decode` run `Validate`, so
every constructible definition has distinct triggers per status. The
doc comment on `AllowedTriggers` cross-references `Validate` for that
rule.

The build sketch proposed a linear-scan dedup over the collected
triggers. No reachable input can exercise the duplicate arm of that
scan, because `Validate` rejects the rows it exists to filter. A dead
arm would drop machine coverage below 100 percent. Dropping the scan
keeps every statement reachable and changes no observable output.

### Rejected: caching

A map from status to rows, built in `New`, would silently miss the
`Decode` path. `Decode` in `machine/wire.go` constructs `Definition`
directly, so the cache would stay empty for decoded definitions.

A lazy cache cannot exist. Both accessors use a value receiver, and a
value receiver cannot store state for the next call.

Flow's panel waves call the machine from parallel goroutines. A cached
map would need locking, or concurrent calls would race on it.

### docs/plans/machine.md

Append this paragraph at the end of the API section:

> Both row accessors allocate exactly one slice per call. The first
> pass counts matching rows; the second pass fills one slice of that
> exact size.

## Tests

The two accessors already sit under the status concern, so their perf
cases join `machine/machine_test/status_bench_test.go`. That file holds
the benchmarks and the allocation budgets for the status concern.

- `TestAllowedRowsAllocBudget` — a table-driven alloc budget test. Use
  `testing.AllocsPerRun` with one thousand runs, as
  `TestFireAllocBudget` does. Reuse the `threeStep` helper from
  `status_test.go`. Cases:
  - Allowed transitions from `running`, two rows: budget one.
  - Allowed transitions from `absent`, no rows: budget zero.
  - Allowed triggers from `running`, two triggers: budget one.
  - Allowed triggers from `absent`, no rows: budget zero.

Red step: only the AllowedTransitions `running` case fails before the
fix; it allocates 2.0 times today. The triggers cases pass before and
after on this toolchain, because their map and growths stack-allocate
when the test discards the result. The triggers budget still pins the
post-fix bound of one slice per call. The `absent` cases pass before
and after; a zero-size `make` does not count as an allocation.

`TestAllowedTransitions` and `TestAllowedTriggers` in
`status_test.go` stay unchanged and green. They keep pinning
declaration order, empty results, and the fresh copy.

`flow/flow_test/chain_bench_test.go` changes in its leading comment
only:

- Re-measure both benchmarks after the machine change with
  `go test -bench -benchmem -count=3` on the flow tests.
- Record the new ns/op, B/op, and allocs per op for both benchmarks.
  Expect flat and chained at equal allocs per op, ratio 1.0x. The
  chained count drops from twelve to eight.
- Replace the zero-margin warning with a margin statement: the chained
  count now holds four allocations of margin against the budget of
  twelve.
- Keep the toolchain note. The benchmark bodies and
  `TestChainedPerfBudget` stay byte-identical.

## Verification

Precondition: build on a green tree. The in-flight flow events wiring
must settle first, because the current working tree does not compile.
The baseline at the last green commit was flat 8 allocs per op and
chained 12.

- `make verify` passes.
- `make api-update` produces no diff on `api/machine.txt`. That
  absence is the proof of no surface change.
- Machine coverage stays at 100 percent. The existing behavior tests
  plus the new alloc test run every statement of the two-pass bodies.
- `policy/layers.json` gains no row; the change imports nothing new.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass.
- No conformance vector changes, because no schema or rule changed.
  `docs/protocol-design.md` stays untouched for the same reason.
