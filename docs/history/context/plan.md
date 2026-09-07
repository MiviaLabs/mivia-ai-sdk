# Plan: context/plan

Rename `contextplan` to `context/plan`. The Go package name is
`plan`, the last path element. Phase 86
(`docs/history/agents/phase86_package_consolidation.md`, merge 5c5239b)
already folded `contextsummary` into `contextplan`; this is a path
move only, with no merge steps and no symbol changes. It is the
largest of the three renames and carries the documentation and gate
surface for all of them: `context/ref` and `context/budget` too.

## Goal

Put the token-window planner under a parent that says what it is.
`context/plan` holds one concern: fitting a message history into a
bounded provider request. The window math, the structural
compaction, the calibrated estimator, and the folded summarizer stay
together, exactly as Phase 86 left them.

## Scope

Inside:

- The directory move `contextplan/` to `context/plan/` and the
  package clause change to `plan`.
- One `doc.go` rewrite. It states the layering in Phase 86's shape:
  `context/plan` consumes `context/ref` for ref minting;
  `agentloop` wires it above; the durable session contract and its
  store live in the `x/` sub-module
  (`github.com/MiviaLabs/mivia-ai-sdk/x`), module-private to that
  sub-module and not advertised by the core module. The three public
  context packages are `context/plan`, `context/ref`, and
  `context/budget`, and each path names its concern.
- The import-path rewrite in every importer, listed in the
  Documentation and gate surface section.

Outside:

- Any behavior change. No signature, sentinel, algorithm, or test
  change beyond import paths.
- Any error-string literal rewrite. Phase 86 preserved behavior
  including error text; the `contextplan: ` and `contextsummary: `
  literal prefixes stay verbatim. See the closure rule in the gate
  surface section.
- The `x/` sub-module's own packages. This rename does not move,
  edit, or re-export them. The x/ files that import the main
  module's `contextref` rewrite their import paths only, through the
  `replace` directive already in `x/go.mod`.
- Re-litigating Phase 86. The merge, the sentinel set
  (`ErrNoMessages` once, plus `ErrSummarySkipped`), and the two
  sentinels' literal texts are landed decisions.

## API

Unchanged. `make api-update` writes `api/context/plan.txt`, first
line `package plan`, mirroring the package path per
`scripts/check_api.py`'s rule. Confirm the generated file differs
from the current `api/contextplan.txt` only in the package line.

Two `policy/pending_symbols.json` rows are rekeyed in this plan
change and travel with the rename:

- `contextplan.ErrSummarySkipped` becomes
  `context/plan.ErrSummarySkipped`; the agentloop compaction
  addendum remains its named future caller.
- `contextplan.TokenEstimate` becomes `context/plan.TokenEstimate`;
  still no in-module consumer, still SDK surface.

The rekeyed keys must match the new lock paths exactly; the wiring
gate reads keys as `<api lock path relative to api/> . <symbol>`.

Import naming and aliasing, re-derived on this tip so no importer
invents its own rule:

- The new package names are `plan`, `ref`, and `budget`.
- `memory/store.go` declares a named return `ref` and calls the
  minter inside `Put`; it imports
  `github.com/MiviaLabs/mivia-ai-sdk/context/ref` under the alias
  `contextref`. `envelope/message.go` does the same, for one uniform
  rule; it calls `contextref.Mint` at `message.go:84` and loops over
  locals named `ref` in `Validate`.
- No production importer of `context/plan` declares a local `plan`;
  `agentrun`'s `plan *flow.Definition` parameters are a different
  type in a package that does not import this one. No alias needed.
- `agent.Run`'s `budget` parameter shadows `budget` inside `Run`,
  which makes no package-qualified call there; no alias needed.
  Alias a file only if the compiler reports a shadow.

## Tests

Move `contextplan/contextplan_test/` to `context/plan/plan_test/`
unchanged, then rewrite import paths and, where test bodies name the
old qualifier, the qualifier `contextplan.` becomes `plan.`. The
folded summary test files (`summary_test.go`, `render_test.go`,
`excerpt_test.go`, `summarizer_test.go`,
`token_estimate_test.go`) move in the same tree.

- The pure directory move passes the test-tampering gate without a
  trailer: `check_test_tampering.py` diffs with `-M` rename
  detection and matches removed test functions by
  whitespace-normalized body hash.
- In-body qualifier rewrites defeat that hash match, so TT01 can
  fire on a moved, edited test even though no test is lost. The
  authorization covers exactly two findings on one file:
  `TestWindowValidate` and `TestWindowBudget` in
  `contextplan/contextplan_test/window_test.go`, which moves to
  `context/plan/plan_test/window_test.go`. The commit carries two
  identical trailers for those two findings:
  `Allow-Test-Change: TT01 window_test.go moved to context/plan/plan_test; bodies changed only by the plan-mandated qualifier rewrite`.
  ANY other TT01 finding - in the ref tree, the budget tree, or a
  third finding in window_test.go - stops the build and escalates to
  the orchestrator for fresh verification. No trailer is
  pre-authorized anywhere else.
- No assertion changes. No test is deleted, skipped, or weakened;
  the trailers cover moved-and-rewritten bodies only.

## Verification

- `make verify` passes; the coverage floor holds for `context/plan`
  and the total.
- `(cd x && go build ./... && go test ./...)` passes: x/ imports the
  renamed paths through its `replace` directive, and no core gate
  builds x/, so this step is the only proof the rewrites compile.
- `make api-update` writes `api/context/plan.txt`; commit the diff in
  the same change.
- `go test -race ./context/plan/...` passes.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass.
- `verify-fast` still runs the `docs/examples/_agentloop*` positive
  controls, including `_agentloop_minimal`, against rewritten
  imports and unchanged output assertions.

## Documentation and gate surface

Every meta edit for all three renames, in the same change. New
package paths: `context/plan`, `context/ref`, `context/budget`. The
`x/` sub-module is a separate Go module with its own `go.mod`, so
`go list ./...` in the core module never enumerates it:
`check_deps.py` sees no x/ package, no `policy/layers.json` row is
needed or wanted for it, and its import rewrites are ordinary
cross-module path updates through the existing `replace` directive.

1. `policy/layers.json`. This plan change adds the three new rows.
   With the code change, delete the rows `contextplan`,
   `contextref`, and `contextbudget`, and update the importer rows:
   `agent` swaps `contextbudget` for `context/budget`; `agentloop`
   swaps `contextbudget` and `contextplan` for `context/budget` and
   `context/plan`; `agentrun` swaps `contextbudget` for
   `context/budget`; `envelope` and `memory` swap `contextref` for
   `context/ref`. `internal/e2e` keeps its row; its compaction test
   imports `contextplan` from test code, which the deps gate
   exempts.
2. `policy/pending_symbols.json`. Rekeyed in this plan change: the
   two `contextplan` rows take the `context/plan` prefix;
   `envelope.ContextRef` keeps its key and its reason gains the new
   citation path, `message.go:84`, and names `ref.Mint`.
3. `api/`. Delete `api/contextplan.txt`, `api/contextref.txt`, and
   `api/contextbudget.txt` with the code change. `make api-update`
   writes `api/context/plan.txt`, `api/context/ref.txt`, and
   `api/context/budget.txt`; the lock path mirrors the package path
   at any depth. `api/agentloop.txt`, `api/agent.txt`,
   `api/agentrun.txt`, and `api/memory.txt` regenerate with the
   renamed qualifiers.
4. `docs/architecture.md`. The count stays thirty: a rename changes
   no package count, and `go list ./...` on this tip already counts
   30 non-test packages, matching the spelled-out `thirty` in the
   opening paragraph. Update the package-map paragraph, the leaf
   list (`context/ref` and `context/budget` stay leaves;
   `context/plan` imports `context/ref` and `provider`), and the
   mermaid edges.
5. `docs/README.md`. Update the package-name list and the three
   `packages/context*.md` index entries to `packages/plan.md`,
   `packages/ref.md`, and `packages/budget.md`. Note that the x/
   packages are not indexed here.
6. `docs/packages/`. Move `contextplan.md` to `plan.md`,
   `contextref.md` to `ref.md`, `contextbudget.md` to `budget.md`,
   rewording package names. Delete `contextsummary.md`,
   `contextstate.md`, and `contextsession.md`; they are Phase 86
   leftovers describing folded or ejected packages. Update cross
   references in the other package pages that name the old paths.
7. `AGENTS.md`. Rewrite the layout line that names `contextplan/`
   and `contextref/` to name `context/` (plan, ref, budget) and keep
   the existing `x/` line as the home of state and session.
8. `semgrep/sdk-standards.yml`. The `HashPrefix` rule message cites
   `contextref/ref.go`; make it cite `context/ref/ref.go` so the
   citation stays true.
9. `scripts/mutation_denylist/`. Nothing moves. No floor row targets
   `contextplan`, `contextref`, or `contextbudget`, and the ejected
   `contextstate` floor left with the x/ package. The nested-package
   limitation in `check_mutation.py` still exists on this tip
   (`docs/history/mutation-nested-packages.md` is unscheduled); if a
   future floor for `context/plan` is wanted, that plan must land
   first.
10. Go import rewrites. Production code: `agentloop`
    (`adoption.go`, `compaction.go`, `loop.go`, `options.go`),
    `agent/run.go`, `agentrun` (`options.go`, `wire.go`),
    `envelope/message.go`, `memory/store.go`, `contextplan/wire.go`
    in the moved tree. Cross-module: every file under `x/` the grep
    in step 12 lists, including `x/contextstate/contracts.go`,
    `x/contextsession/planner.go`, `x/runconfig/loader.go`, and
    their test trees, importing `context/ref` from the main module
    path. Tests: the moved trees, `agentloop/agentloop_test/`,
    `agent/agent_test/`, `agentrun/agentrun_test/`,
    `agent/run_budget_internal_test.go`,
    `agentloop/capability_derivation_test.go`, and
    `internal/e2e/e2e_test/anthropic_compaction_test.go`. Examples:
    `docs/examples/_agentloop/main.go` and `_agentloop_adoption/`
    (both `contextplan` and `contextbudget`),
    `_agentloop_minimal/` (`contextplan` only).
11. `scripts/check_examples_sync.py` pairs
    `docs/examples/agentloop.md` fences with
    `docs/examples/_agentloop/main.go` byte for byte. Rewrite the
    markdown fences in the same commit as the example programs.
12. Carried doc text outside `docs/`, re-derived on this tip:
    - `README.md`, the confinement line: `contextplan` becomes
      `context/plan`. The same line still lists `longtermmemory`, an
      x/ package; that is Phase 86 drift the builder removes in
      passing.
    - `agent/doc.go`: `contextbudget.Limits` becomes
      `budget.Limits`.
    - `memory/doc.go`: `contextref.Mint` becomes `ref.Mint`.
    - `provider/reasoning.go`: no text change. The comment names the
      package qualifiers `contextstate` and `contextsession`, which
      did not change; the packages now live in `x/`.
    - `.agents/skills/test-review/SKILL.md`: the `contextbudget`
      mentions become `context/budget`. Its remaining-packages line
      also still lists `discovery`, merged into `flow` by Phase 86;
      drift the builder fixes in passing.
    - `.agents/memories/mutex_prevents_races_not_mispairing.md`:
      `contextplan` becomes `context/plan`. Reword the package path
      only, never the lesson.
13. Grep strategy and closure rule. Run
    `grep -rnE 'contextplan|contextsummary|contextstate|contextsession|contextref|contextbudget' .`
    over the worktree, excluding `.git` and `.claude/worktrees`,
    and split the hits into two classes.
    - Live package references: import paths, package qualifiers at
      use sites, doc citations of files or paths, and lock or policy
      keys. Every hit of this class naming the four retired names
      must be gone from the core module and the x/ sub-module.
      `contextstate` and `contextsession` hits stay where they name
      the x/ packages' qualifiers, which did not change.
    - Behavior-preserving bytes: Phase 86 explicitly preserved error
      text, so the string literals `contextplan: `, and
      `contextsummary: `, and any other wire- or error-visible
      bytes, stay verbatim. Grep-list them explicitly
      (`grep -rn '"contextplan:\|"contextsummary:' context/`) and
      treat that list as the exemption inventory, not as failures.
    - Hits inside `docs/history/` are historical records and stay;
      each old plan carries a superseded marker naming its
      successor.

## Addendum: Error sentinel sweep

Status: shipped

This pass classified every sentinel in `context/plan` as CONFIG
(construction-time argument check) or RUNTIME (reacts to live
message content or a completer call). It merged the CONFIG
sentinels behind one new `ErrInvalidOptions`, in `window.go`. Each
merged site now wraps `ErrInvalidOptions` with
`fmt.Errorf("%w: %s", ErrInvalidOptions, "<field>: <rule>")`.

`ErrMaxTokensNotPositive` stayed a named sentinel instead of
merging. `agentloop/agentloop_test/compaction_test.go` asserts on it
by name, and this pass does not own or edit `agentloop`. Treat this
as a deliberate, recorded exception, not an oversight.

| Sentinel | Classification | Disposition |
| --- | --- | --- |
| `ErrMaxTokensNotPositive` | CONFIG | Kept as its own sentinel. Exception: `agentloop/agentloop_test` asserts on it by name. |
| `ErrReserveNegative` | CONFIG | Deleted. Merged into `ErrInvalidOptions` with substring `Reserve`. |
| `ErrReserveTooLarge` | CONFIG | Deleted. Merged into `ErrInvalidOptions` with substring `Reserve`. |
| Compaction target-tokens-at-budget check (`window.go`, no prior name) | CONFIG | Now wraps `ErrInvalidOptions` with substring `TargetTokens`. |
| Compaction percent and target-tokens bounds (`compaction.go`, no prior name) | CONFIG | Now wraps `ErrInvalidOptions` with substring `TriggerPercent`, `TargetPercent`, `TargetTokens`, or `RecentTail`. |
| PreserveNames blank/duplicate checks (`compaction.go`, no prior name) | CONFIG | Now wrap `ErrInvalidOptions` with substring `PreserveNames`. |
| `ErrNilCompleter` | CONFIG | Deleted. Merged into `ErrInvalidOptions` with substring `Completer`. |
| `ErrNoMessages` | RUNTIME | Unchanged. Reacts to the message slice `Compact` receives. |
| `ErrEstimateFailed` | RUNTIME | Unchanged. Reacts to a live token-estimator call. |
| `ErrRetentionOverflow` | RUNTIME | Unchanged. Reacts to the retention set computed from the message slice. |
| `ErrNoObjective` | RUNTIME | Unchanged. Reacts to the message slice `Compact` receives. |
| `ErrNoMessagesToSummarize` | RUNTIME | Unchanged. Reacts to the message slice `Summarize` receives. |
| `ErrInvalidReply` | RUNTIME | Unchanged. Reacts to a live completer reply. |
| `ErrCallFailed` | RUNTIME | Unchanged. Reacts to a live completer call. |
| `ErrSummarySkipped` | RUNTIME | Unchanged. An adapter returns it at call time to decline summary injection. |

`summary.go` carries no named sentinels. Its field-validation errors
are plain, unwrapped `fmt.Errorf` calls, already funneled into the
RUNTIME `ErrInvalidReply` by `summarizer.go`'s `decodeReply`. This
pass left them as they were.
