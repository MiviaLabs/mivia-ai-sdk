# Plan: per-call run timeout backstop in `tools`

Change plan for the existing `tools` package. The package plan stays at
[docs/plans/tools.md](tools.md). This document follows the same five
sections.

Revision history:

- Revision 1 folds in nine findings from the first plan-reviewer
  round: the `stepTool` exemption was removed; `ledgerTool` and
  `schedulerTool` return to the protected group; the Scope
  contradiction is resolved; the abandonment contract states the true
  cost; the API-lock diff names the `New` signature change; the
  channel-safety test moved into the white-box file; the
  deadline-returning-tool case got a test and an ordering rule; the
  import claim is corrected; negative-value rules merged into one
  table.

## Goal

A caller-supplied tool can block forever. One blocking call stalls the
whole turn: `Registry.Run` and `Registry.RunScoped` call `t.Run(ctx,
in)` and nothing bounds the wait. `ExecutionProfile.Timeout` exists but
no function enforces it (`docs/packages/tools.md`, section "Published,
not enforced"). This change gives every registry-mediated run a
per-call deadline and starts enforcing the declared timeout.

Evidence:

- `tools/registry.go:126` and `tools/registry.go:147` and
  `tools/registry.go:162` are the only three places this package
  dispatches into a tool.
- `tools/execution_profile.go:44` declares `Timeout` today.
- `docs/plans/gaps-sentinel-errors.md` records the sentinel-error
  convention this plan reuses.

Failure class this closes: a tool that issues a blocking syscall or
network read and never selects on `ctx.Done()`. The mivia-agent repo
hit exactly this in `list_dir`; the fix there lives in one tool. The
same mistake in any next tool must stall nothing at the SDK layer.

## Scope

Inside the package:

- A deadline around each `t.Run` issued through `Registry.Run`,
  `Registry.RunScoped`, resolved per call.
- Precedence: the tool's declared `ExecutionProfile.Timeout` wins over
  the registry configuration. The registry configuration wins over
  the built-in default.
- Built-in default active from `New()`. The default value is 10
  minutes (`DefaultRunTimeout`). Nobody opts in to get protection;
  protection is the floor, not a feature.
- An explicit escape hatch, both directions:
  - A negative value in `ExecutionProfile.Timeout`, canonically
    `TimeoutNone`, exempts one tool fully.
  - `WithDefaultRunTimeout(TimeoutNone)` restores the current,
    unbounded behavior for one registry.
- New sentinel error `ErrRunTimeout`, returned wrapped with the tool
  name and the effective bound. Test with `errors.Is`.
- The deadline covers the `t.Run` call only. It never covers
  `Scope.Allowed` or `Approve`. A human approval that answers hours
  later must not consume the budget. In `RunScoped` the wrap sits
  after the approve branch, at the single shared helper below.

Outside the package:

- No wiring change anywhere. Every dispatcher keeps calling the same
  registry methods and inherits the backstop through them:
  `agentloop/toolcall.go` stays untouched because its error policy
  renders whatever `Run` returns, so `ErrRunTimeout` flows through
  `OnToolError` untouched and `ErrorPolicyReport` marks it for the
  model like any other tool error.
- The only edits outside `tools/` are profile declarations inside two
  files this SDK owns:
  - `subagent/subTool` (`astool.go`) and `subagent/flowTool`
    (`flowtool.go`) gain an `ExecutionProfile()` method returning
    exactly `tools.ExecutionProfile{Timeout: tools.TimeoutNone}`.
    Neither implements `ProfiledTool` today; this adds the method,
    not an overlay onto forwarding wrappers, so no variant-interface
    collision arises. `Class` stays unset at
    `ExecutionClassUnclassified`, the value their calls already
    ranked at, so scope approval behavior is unchanged and only the
    timeout declaration is new. These two genuinely host open-ended
    nested execution (whole runner turns, whole flow graphs); their
    embedded loops carry their own stop conditions and depth bounds,
    and an outer guess at their length would be fiction.
  - Every other registered tool in tree keeps the default:
    `ledgerTool` runs one bounded `taskrun` ceremony per call;
    `schedulerTool.add` enqueues and returns; mailbox, memory,
    trigger, room, channel, provider, discovery and friends are fast
    exchanges. The floor protects them like everything else.
  - `runconfig/stepTool` declares nothing new on purpose. Its sixteen
    variants forward exactly the optional interfaces `inner`
    implements; forwarded inner profiles already compose correctly
    under the resolver (inner declares nothing, the default applies;
    inner declares positive, enforced; inner declares none, exempt).
    One `stepTool.Run` delegates to `inner.Run` once, so there is no
    embedded loop to justify an overlay that would silently strip
    every variant's `ProfiledTool` status instead.
- Host adapters outside this module (the CLI converter
  `sdkadapter` in mivia-agent) do not publish profiles yet. They stay
  protected by the default. Wiring declared host capability timeouts
  through is a separate, host-side follow-up.

Semantics that must be documented, not hidden:

- Go cannot kill a goroutine. On expiry the call returns
  `ErrRunTimeout` while the abandoned tool goroutine keeps running
  until it observes the canceled context or leaks. This mirrors the
  standard library `context` contract. The real cost of expiry is
  that goroutine plus whatever resources it holds open. The agent
  loop itself continues the turn and recovers its worker capacity:
  in `agentloop.executeCalls`, a worker picks up its next index as
  soon as the expired call returns, so repeated stalls do not starve
  the pool.
- If the tool returns first, its result passes through unchanged,
  including cancellation-shaped errors it produced on its own. The
  backstop reclassifies nothing. This holds even when the tool
  returns its own `context.DeadlineExceeded` observed from the child
  context ahead of the timer: completion-first ordering passes it
  through as the tool's outcome.
- Tie-break, made deterministic: the timer firing does not trust
  `select` fairness among ready cases. When the deadline case wins
  the select, the wrap re-checks the parent context before
  synthesizing `ErrRunTimeout`; a parent already past done means the
  parent cause returns instead. Parent cause before local deadline,
  always resolvable, never coin-flipped.

Rejected alternatives, kept visible for review:

- Enforce in `agentloop` only. Rejected: `agentrun/wire.go` and any
  future composition path dispatch through the registry too. Also,
  the approve step runs inside `RunScoped`, so a caller cannot insert
  a deadline between approve and run from outside.
- A compose-time wrapper (`tools.WithBackstop(t)`). Rejected: opt-in
  helpers leave the default gap open. The `spool` package shows the
  cost of wrapper variants; sixteen structs forward four optional
  interfaces. Every author would have to remember, which is the bug
  class being closed.
- Struct-based `New(NewOptions{...})`. Rejected against variadic
  functional options because it breaks every existing call site;
  `New(opts ...Option)` is source-compatible with `New()`.
- Base-level `ExecutionProfile()` promotion on multi-variant wrappers
  (`stepTool`, `spoolTool`). Rejected: at equal embed depth Go drops
  colliding promoted methods from the method set, so every variant
  would stop implementing `ProfiledTool` and lose inner Class and
  Timeout silently. Overlaying values onto inner's forwarded profile
  would exempt config-bound tools wholesale, reopening the failure
  class in those paths.

## API

Added surface; `make api-update` refreshes `api/tools.txt` in the
same change. Six lock lines change there: five additions below plus
the existing `func New() (*Registry)` line becoming
`func New(opts ...Option) (*Registry)`.

Negative-value rules, one table, everywhere:

| Surface | Positive | Zero | Negative |
| --- | --- | --- | --- |
| `ExecutionProfile.Timeout` | Enforced verbatim | Undeclared; fall through | None; never cap this tool |
| `WithDefaultRunTimeout(d)` | Registry-wide bound | Use `DefaultRunTimeout` | None; restore unbounded |

A negative means "never cap" in both places; `TimeoutNone` names the
canonical constant, and the resolver treats any negative as none.

- `const DefaultRunTimeout time.Duration = 10 * time.Minute`. The
  bound a registry uses when the tool declares nothing and the caller
  configures nothing.
- `const TimeoutNone time.Duration = -1`.
- `type Option func(*Registry)`. Applied left to right by `New`.
- `func WithDefaultRunTimeout(d time.Duration) Option`. Any value the
  table allows is accepted. There is no invalid argument left to
  report.
- `var ErrRunTimeout = errors.New("tools: tool run exceeded its timeout")`.
  Wrapped with tool name and bound on expiry. Never naked.
- `func New(opts ...Option) *Registry`. Signature grows variadic;
  every existing `New()` call site compiles unchanged.

Unexported pieces:

- `runBounded(ctx context.Context, name string, t Tool, in InOut) (Out, error)`
  holds the wrap. All three dispatch sites call it. Effective bound
  derived once per call; non-positive effective bound skips the wrap
  entirely and calls `t.Run` inline. Otherwise: child context via
  `context.WithTimeout` handed to the tool; results collected over a
  one-buffered channel; deadline case re-checks the parent before
  synthesizing. Buffered channel guarantees the abandoned producer
  never blocks on send.
- `effectiveRunTimeout(t Tool, configured time.Duration) time.Duration`
  resolves precedence per the table. Pure function; unit-tested
  without wall clock.

Import policy: `tools` imports `context`, `errors`, `sort`, `sync`,
`strings` today. The wrap adds `fmt` for the wrapped error text and
`time` for durations. Both are standard library; `policy/layers.json`
gains no edge.

Behavioral invariants, restated as enforceable facts:

1. A registry built with no options bounds an undeclared tool by
   `DefaultRunTimeout`.
2. A declared positive profile timeout always wins, longer or
   shorter than the configured bound.
3. A negative profile timeout exempts that tool under any registry
   configuration.
4. A negative configuration exempts every undeclared tool.
5. The budget starts when `t.Run` starts, never before `Approve`
   returns.
6. Parent cancellation beats the local deadline. When they race, the
   wrap resolves deterministically toward the parent cause.
7. Results pass through byte-identical whenever the tool finishes
   first, whatever the result says, including self-declared timeout
   errors.
8. An expiry surfaces as `ErrRunTimeout` carrying the tool name.

## Tests

External package (`tools/tools_test`), following
`registry_concurrent_test.go` conventions. Determinism comes from
small real durations plus channel-blocking fakes; no clock injection,
matching the heartbeat package precedent of caller-supplied time.

White-box file `tools/run_timeout_internal_test.go` (package `tools`),
holding everything the exported surface cannot reach:

- `effectiveRunTimeout` table: undeclared tool and unset
  configuration yield `DefaultRunTimeout`; positive profile wins in
  both directions; negative profile beats everything; negative
  configuration yields none for undeclared tools.
- Channel safety driven directly against `runBounded`: after expiry,
  the late producer completes its buffered send; the test receives
  from the path the code itself uses, proving no send-block and no
  panic rather than asserting a vibe.

Wrap behavior, wall-clock fakes, external file:

- Blocking tool, declared 40 ms, blocked well past it: error identity
  via `errors.Is(err, ErrRunTimeout)`, message carries name and bound.
- Blocking tool, no declaration, registry via `WithDefaultRunTimeout(20ms)`:
  same identity proof; default path exercised end to end.
- Fast tool under tight default succeeds repeatedly; result equality
  invariant 7.
- Profile declares longer than the tight default; the longer bound is
  the one that fires (proves no silent min()).
- Negative profile under 20 ms default; 100 ms tool completes.
- Negative configuration via `WithDefaultRunTimeout(tools.TimeoutNone)`;
  150 ms tool completes; identical registry without the option behaves
  the same under its built-in ten-minute-only-bounds contract, asserted
  logically, never by sleeping toward it.
- Parent cancel mid-run: error satisfies `errors.Is(err,
  context.Canceled)` and not `ErrRunTimeout`; proves invariant 6.
- Tool returns its own `context.DeadlineExceeded` quickly, deadline
  longer than that: error identity stays the tool's
  `context.DeadlineExceeded`, not reclassified as `ErrRunTimeout`;
  proves invariant 7 against the indistinguishable-identity case.
- Slow approve (sleeps past a 20 ms default), fast tool after it:
  succeeds; proves invariant 5 through the true `RunScoped` approve
  branch.
- Unknown name through `Run` and `RunScoped` unchanged: `ErrUnknownName`,
  no wrap started; guards against eager resolution.
- Concurrent: several blocking calls time out together through one
  shared registry; each caller gets its own `ErrRunTimeout`; no
  cross-talk through shared derived contexts.

Integration honesty:

- `agentloop` needs no new test; nothing in its code changes. Existing
  suite keeps passing, asserted by `make verify`.

Prohibited shortcuts held to by review: no success-claim resting on
`time.Sleep` alone where a channel synchronize works; no test waiting
on the full ten-minute default; no deletion or weakening of existing
registry tests (`scripts/check_test_tampering.py` gates this); no test
in `tools_test` pretending to observe unexported internals.

## Verification

Commands, from the module root:

```
make api-update        # refresh api/tools.txt; commit the diff
make verify-fast       # format, vet, tests, python gates, semgrep
make verify            # full gate including coverage floors and probes
```

Named gate impacts:

- `api/tools.txt`: six lines change: five additions from the API
  section plus the `New()` signature line; no removals.
- `api/subagent.txt`: additions from the two new `ExecutionProfile`
  methods on `subTool` and `flowTool`; the same `make api-update`
  run covers it.
- `policy/layers.json`: unchanged.
- Coverage: new unexported functions live under `tools`, whose floor
  holds at 85 percent through the planned tests. No stored mutation
  floor gates this package today; if `check_mutation.py` grows one,
  the resolver and tie-break branches are the survival targets.
- `scripts/check_structure.py`: `registry.go` gains one delegate call
  per dispatch site; helpers live in new `registry_timeout.go` from
  the start, keeping both files far under the 500-line ceiling.
- Docs, updated in the same change:
  - `docs/packages/tools.md` section "Published, not enforced"
    shrinks to `ResourceKey` and `MaxResultBytes`. A new section, "Run
    timeout backstop", states precedence, the value table, expiry
    shape, approval exclusion, the deterministic tie-break, and the
    abandonment contract.
  - `docs/plans/tools.md` carries the same trio claim under its
    published-not-enforced heading; update it in the same change.
  - Tree grep for "Published, not enforced" catches stale repeats;
    both known sites are named here, grep confirms none beyond them.
- Prose gate `scripts/check_prose.py` governs this file: sentences at
  or below 25 words, imperative, no filler.

Commit shape, matching recent history (`fix(agentloop): ...`):
`fix(tools): bound tool runs with a per-call timeout backstop`.
Decide the final subject at commit time per repo rules.

Precedent this plan follows: [docs/plans/gaps-sentinel-errors.md](gaps-sentinel-errors.md)
is a cross-cutting change plan built through the same delivery loop,
including a `REVISE` round with the plan-reviewer before any code.
