# Phase 76: agentloop mutation survivors

Status: plan, ready for plan review. This phase closes the 11
`agentloop` survivors that phase 75's gap 2 measured and deferred.
`docs/plans/agents/phase75_mutation_kit_hardening.md`'s "Gap 2
survivors" section lists them. This phase re-measures, classifies,
and closes them, the same way phase 75's part C closed `schema`'s.

## Goal

Kill every killable `agentloop` mutation survivor with a genuine
test. `agentloop` runs the model tool-calling loop. It validates
every model-chosen tool argument and enforces every budget bound. A
missed boundary in that code is a security-relevant gap, not routine
cleanup. Follow phase 75's proof discipline: plant each mutation by
hand, confirm the test fails, revert, confirm the test passes.

## Re-measurement

`python3 scripts/check_mutation.py --pkg agentloop` ran once during
planning. It reports `killed=114 survived=11 discarded=4
rate=91.20%`, matching phase 75's Gap 2 result table exactly. `git
status --short` showed no mutated file after the sweep. `pgrep -fl
'_test\.test'` found no orphan process. The sweep is honest and the
11 survivors below are current.

The 11 survivors, with file, line, and swap:

| File:line | Swap | Classification |
| --- | --- | --- |
| `compaction.go:84` | `>` to `>=` | killable |
| `compaction.go:137` | `+` to `-` | equivalent |
| `compaction.go:153` | `+` to `-` | equivalent |
| `run.go:93` | `>` to `>=` | killable |
| `run.go:176` | `>` to `>=` | equivalent |
| `run.go:185` | `+` to `-` | killable |
| `run.go:186` | `+` to `-` | killable |
| `run.go:187` | `+` to `-` | killable |
| `run.go:188` | `+` to `-` | killable |
| `toolcall.go:25` | `<` to `<=` | killable |
| `wire.go:36` | `>` to `>=` | killable |

No survivor in this list is a production bug. Every killable site
enforces its documented boundary correctly today; the survivor exists
because no test exercises the exact boundary input. This finding
still needs a second look from the plan-reviewer, since a boundary
bug in budget enforcement is a security-relevant class of finding and
this plan's author is not the last word on it.

## Scope

Inside:

- The 8 killable survivors listed above: one test per site, each
  naming its exact boundary input and its expected observable
  failure, in the Tests section below.
- The 3 equivalent-mutant proofs listed above, written into this plan
  so a future reviewer does not re-open them.
- A re-sweep of `agentloop` after the tests land, and a re-lock of
  `scripts/mutation_denylist/agentloop.json` at the new measured
  rate, following phase 75's margin rule.

Outside:

- Any other package's survivor list. `schema`'s is already closed.
  `envelope`, `ledger`, `workspace`, `subagent`, `dispatch`,
  `a2aclient`, and `contextstate` stay open follow-up work with their
  own future plans.
- Any change to the mutation kit itself: `scripts/check_mutation.py`,
  `scripts/mutation_tokenize.py`, `scripts/mutation_process.py`, and
  the operator set stay as phase 75 shipped them.
- Any floor already locked in phase 75, except `agentloop`'s own
  floor, which this phase re-locks after its fixes land.
- Any production-code change beyond what a genuine-kill test forces.
  This plan finds no bug, so it proposes no code fix beyond tests.

## Classification detail

### compaction.go:84 — killable

`checkCompactedBudget` reads:

```go
if est > w.Budget() {
    return fmt.Errorf(...ErrCompactionFailed...ErrRetentionOverflow)
}
```

The boundary is `est == w.Budget()` exactly. The original code passes
a rebuilt history estimated at exactly the budget; the mutant (`>=`)
fails it. Both `est` and `w.Budget()` are deterministic under the
existing `scaleEstimator{div: 1}` fixture, which prices one token per
content byte. The builder picks `Window.MaxTokens` and `Reserve` so
`Budget()` is a small fixed number, then sizes the post-compaction
`Kept` and summary content so the re-estimate lands on that number
exactly. Boundary input: a compaction whose rebuilt history estimates
to exactly `w.Budget()` tokens. Expected observable output: `Run`
returns `nil` error and reaches `Completer.Chat`, not
`ErrCompactionFailed`. A paired case one byte over the same budget
asserts `errors.Is(err, contextplan.ErrRetentionOverflow)`, pinning
the boundary from both sides.

### compaction.go:137 and compaction.go:153 — equivalent

Both sites are `make([]provider.Message, 0, len(msgs)+1)`, the
capacity argument of a `make` call, inside `injectAfterSystem` and
`injectNotice`. The mutant changes only the third `make` argument,
capacity, to `len(msgs)-1`.

Capacity is not length. Every following `append` call in both
functions grows the slice through Go's normal append semantics,
which reallocates past a full backing array. No code in `agentloop`
or its tests calls `cap()` on the result, benchmarks its allocation
count, or otherwise observes capacity. The two forms produce
byte-identical output slices for every input.

The only way `len(msgs)-1` could differ observably is a `make` panic
on a negative capacity, which needs `len(msgs) == 0`. Both call sites
are unreachable with an empty `msgs`: `injectAfterSystem`'s own doc
comment states `msgs is never empty: Compact always keeps the
mandatory retention set, which holds at least the user objective`,
and `injectNotice` is called from `compactHistory` on the same
`rebuilt` value that already carries this guarantee. No reachable
input distinguishes the mutant from the original. These are
equivalent mutants; no test closes them.

### run.go:93 — killable

`run`'s cap check reads:

```go
if l.maxTotalTokens > 0 && runningTokens > l.maxTotalTokens {
    return ...ErrTokenBudgetExceeded
}
```

The API doc in `docs/plans/agentloop.md` states `MaxTotalTokens` caps
cumulative spend and "a total over the cap fails the run" — over,
not at. The boundary is `runningTokens == l.maxTotalTokens` exactly.
Boundary input: two scripted responses with
`Usage.TotalTokens` summing to exactly `MaxTotalTokens` (for example
50 and 50 against a cap of 100). Expected observable output: `Run`
returns `nil` error, following the loop to its normal stop, not
`ErrTokenBudgetExceeded`. A paired case with the same two responses
summing to `MaxTotalTokens + 1` (51 and 50) asserts
`errors.Is(err, agentloop.ErrTokenBudgetExceeded)`, matching the
existing `TestRunMaxTotalTokens` shape but pinned to the boundary
value instead of a value well past it.

### run.go:176 — equivalent

`billedTokens` reads:

```go
if u.TotalTokens > sum {
    return u.TotalTokens
}
return sum
```

This is the same max-idiom shape phase 75 proved equivalent in
`schema.go` at offsets 5271 and 5401 (`if d > max { max = d }`). The
boundary is `u.TotalTokens == sum` exactly. On that input the
original code falls through and returns `sum`; the mutant (`>=`)
takes the branch and returns `u.TotalTokens`. Because the two values
are equal at this input, both forms return the identical number. No
reachable input distinguishes the mutant from the original. This is
an equivalent mutant.

### run.go:185, 186, 187, 188 — killable

`sumUsage` adds `a`'s and `b`'s four `provider.Usage` fields:

```go
return provider.Usage{
    PromptTokens:     a.PromptTokens + b.PromptTokens,
    CompletionTokens: a.CompletionTokens + b.CompletionTokens,
    TotalTokens:      a.TotalTokens + b.TotalTokens,
    CachedTokens:     a.CachedTokens + b.CachedTokens,
}
```

`sumUsage` is unexported, but its result becomes `run`'s
`totalUsage`, which lands in `Result.Usage`, an exported field. No
current test in `agentloop_test` asserts `Result.Usage`'s exact field
values after more than one iteration; the closest,
`TestRunMaxTotalTokens`, checks only the `usage.Accumulator` total,
which sums `Completer`-reported `Usage` independently of `sumUsage`.
No test today sets or reads `CachedTokens` at all.

Boundary input, one test covering all four fields at once: two
scripted responses whose `Usage` values set every field to distinct,
nonzero, easily-summed numbers (for example response one
`{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30,
CachedTokens: 5}`, response two `{PromptTokens: 7, CompletionTokens:
3, TotalTokens: 10, CachedTokens: 2}`), run to `StopNoToolCalls` with
no bound tripped. Expected observable output: `res.Usage ==
provider.Usage{PromptTokens: 17, CompletionTokens: 23, TotalTokens:
40, CachedTokens: 7}`, asserted field by field. Flipping any one `+`
to `-` in `sumUsage` changes exactly one field's expected sum, so
this single test kills all four survivors: each mutant produces a
different wrong value in its own field, and the assertion checks
every field.

### toolcall.go:25 — killable

`runToolCalls` sorts model-requested calls:

```go
sort.Slice(ordered, func(i, j int) bool { return ordered[i].Index < ordered[j].Index })
```

`provider.ToolCall` carries no `Validate` method and no documented
uniqueness rule on `Index`. `provider.mergeToolCallDelta` happens to
deduplicate by `Index` when a streaming `Completer` builds a
`Response`, but a hand-built or vendor `Completer` can return two
`ToolCall` values with the same `Index`; nothing in this SDK rejects
that shape before it reaches `runToolCalls`. Duplicate `Index` is
therefore a reachable input, not merely a theoretical one.

Go's `sort.Slice` uses insertion sort for a 2-element slice: it
swaps element 1 before element 0 exactly when `Less(1, 0)` is true.
With the original `<`, two calls sharing one `Index` give `Less(1,
0) == false` (not strictly less), so the input order is kept. With
the mutant `<=`, `Less(1, 0) == true` (equal counts as `<=`), so the
call at index 1 is swapped ahead of index 0, reversing the order.

Boundary input: two `provider.ToolCall` values with the same `Index`
(for example both `Index: 0`), different `ID`s and different tool
names, in a single response. Expected observable output: the
resulting `RoleTool` messages in `Result.History` appear in the same
`ID` order the response supplied, not reversed. The test records
call order through a fake tool that appends its own name to a shared
slice, then asserts that slice equals the input order.

### wire.go:36 — killable

`render`'s truncation guard reads:

```go
if budget, ok := tools.ResultBudgetOf(t); ok && budget > 0 && len(content) > budget {
    content = truncateContent(content, budget)
}
```

The boundary is `len(content) == budget` exactly. The original code
leaves content untouched when it exactly fits the budget; the mutant
(`>=`) truncates it anyway, dropping legitimate content and
appending `truncationMarker` at the boundary. Boundary input: a tool
whose `tools.ResultBudgetOf` reports a fixed positive bound and whose
rendered result is exactly that many bytes. Expected observable
output: the `RoleTool` message's `Content` equals the untruncated
result exactly, with no `truncationMarker` suffix. A paired case one
byte over the same budget asserts `Content` ends with
`truncationMarker` and is at most `budget` bytes long, pinning the
boundary from both sides.

## Proof method

For every claimed kill above, the builder plants the mutation by
hand in the target file, runs the new test, and confirms it fails
with a message distinguishing the wrong behavior. The builder then
reverts the plant and confirms the test passes. This matches phase
75's Gap 1 and Gap 2 proof method exactly: a claimed kill with no
planted-mutation proof is not accepted.

For every claimed equivalent mutant above, the builder does not plant
the mutation as a proof step; the proof is the code trace in this
plan's Classification detail section. The plan-reviewer re-derives
each equivalence from the code independently before accepting it, the
same review depth phase 75's `schema` equivalents received.

## Expected new floor

The re-sweep after the fixes land can only move `survived` down and
`killed` up; `discarded` (the 4 build-failure `+` sites) and the
total mutant count (129) do not change, since no production code
changes. Closing 8 of 11 survivors and leaving 3 equivalent moves the
count to `killed=122 survived=3 discarded=4`, a computed rate of
`122 / (129 - 4) = 97.60%`.

`agentloop` is on phase 75's margin list (its test suite drives
goroutines against the code under test), so its floor locks at the
measured rate rounded down, minus one point: `96`. This is the
expected outcome if every killable survivor closes and every
equivalent-mutant proof holds. The builder confirms this by running
the actual re-sweep after the tests land; if the measured rate
differs from 97.60 (for example a planted-then-reverted test leaves
an unrelated site newly killed or newly discarded), the builder
records the actual number and computes the floor from it using the
same margin rule, not the number in this paragraph.

`scripts/mutation_denylist/agentloop.json`'s `floor` value updates to
the re-sweep's result. No other package's denylist file changes.

## Measured result

The re-sweep after the eight tests landed reports `killed=122
survived=3 discarded=4 rate=97.60%`, matching the projection above
exactly. The three survivors are:

- `compaction.go:5292` (`compaction.go:137`), `+` to `-`: the
  equivalent mutant this plan classified above.
- `compaction.go:5900` (`compaction.go:153`), `+` to `-`: the
  equivalent mutant this plan classified above.
- `run.go:6455` (`run.go:176`), `>` to `>=`: the equivalent mutant
  this plan classified above.

No previously-killable site appears in the survivor list. `git status
--short` after the sweep shows no mutated production file, only the
new and edited test files this phase adds. `scripts/mutation_denylist/
agentloop.json`'s `floor` updates from 90 to 96: the measured rate
rounded down (97) minus agentloop's one-point margin (96), a five-
point rise.

Each of the eight killable survivors was proven by hand: the builder
planted the exact mutation, ran the matching new test, confirmed a
failure with a message distinguishing the wrong behavior, reverted,
and confirmed the test passed again. `compaction.go:84`,
`run.go:93`, all four `run.go:185-188` sites, `toolcall.go:25`, and
`wire.go:36` each confirmed this way.

## API

No new exported Go symbol. This phase adds test cases only, all
inside the existing `agentloop/agentloop_test` external test package.
No new `agentloop`, `provider`, or `tools` symbol is needed: every
claimed kill above observes state already exported (`Result.Usage`,
`Result.History`, the returned `error`) or already reachable through
existing test fixtures (`scaleEstimator`, `scriptedCompleter`,
`schemaEchoTool`, `toolCallResponse`). `api/agentloop.txt` needs no
`make api-update` run for this phase.

## Tests

Table-driven where the case set grows; each boundary case above
states its own exact input and expected output, so a single
assertion table is unnecessary for the arithmetic-boundary cases
that share one shape (the paired at-boundary / over-boundary cases
read more clearly as two named tests each).

- `compaction_test.go` gains `TestCheckCompactedBudgetAtBudgetPasses`
  and `TestCheckCompactedBudgetOverBudgetFails`, covering
  `compaction.go:84`.
- `loop_bounds_tokens_test.go` gains
  `TestRunMaxTotalTokensAtCapPasses` and reuses the existing
  over-cap shape, adjusted to trip at exactly `MaxTotalTokens + 1`,
  covering `run.go:93`.
- A new file, `agentloop/agentloop_test/usage_sum_test.go`, gains
  `TestRunUsageSumsAllFourFields`, covering `run.go:185-188`.
- `toolcall.go:25`'s test lands in a new file,
  `agentloop/agentloop_test/toolcall_order_test.go`, as
  `TestRunToolCallsPreservesOrderOnDuplicateIndex`.
- `wire.go:36`'s tests land in `render_test.go`, as
  `TestRenderExactBudgetNoTruncation` and
  `TestRenderOneByteOverBudgetTruncates`.

Every new test file stays under the 500-line structure gate; the
builder splits further only if a file would exceed it.

## Verification

- `python3 scripts/check_plan.py` passes on this plan.
- `python3 scripts/check_prose.py` passes on this plan.
- `go test -race ./agentloop/...` passes, including every new test.
- `make verify` passes.
- `python3 scripts/check_mutation.py --pkg agentloop` re-sweep, run
  once after the tests land. The builder records the actual
  `killed`/`survived`/`discarded`/`rate` numbers in this plan's
  Expected new floor section, replacing the projected numbers with
  the measured ones.
- `scripts/mutation_denylist/agentloop.json` updates to the re-swept
  floor, and `make mutation-gate` passes against it.
- `git status --short` and `pgrep -fl '_test\.test'` both come back
  clean after the re-sweep, matching phase 75's Gap 1 proof that the
  process-group fix holds under a real sweep.
- The module coverage floor (85 percent, `agentloop` and the total)
  stays met; the new tests only add coverage.
