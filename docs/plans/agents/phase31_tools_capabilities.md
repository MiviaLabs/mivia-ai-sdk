# Phase 31: tools execution profile markers

Status: future. Depends on phase 14 (shipped). Extends the `tools`
package with optional execution-risk markers. It adds no new package.

## Precedent for shipping with no caller yet

This phase ships with no caller, the same way phase 14 itself shipped.
`AGENTS.md`'s `tools/` layout bullet states the precedent plainly: the
package ships "a leaf package; no internal imports. No caller yet; the
agent binding is a later phase." Phase 14 landed on that basis and
`make verify` accepted it. Phase 31 is an additive-only extension of
that same, already-precedented package. It adds no new package and no
new import edge; it only adds optional interfaces and a new method
next to the ones phase 14 already shipped with no caller. The eventual
caller is the same one phase 14 named: a future agent-binding phase
that wires a `Registry` into `agent.Run` and chooses which tools an
agent step may call. That phase is not committed to a number yet.
Until it lands, `RunScoped`, `ExecutionProfileOf`, and `Scope` sit
beside `Run` the way `Registry.Remove` sat beside `Registry.Add`
before any caller used it: read, tested, and locked, waiting on the
`agent/` composition-layer bullet in `docs/architecture.md`'s Package
map.

## Goal

Let a caller classify a tool's execution risk and bound its result
size, without changing the shipped `Tool` or `Registry` shape. Let a
caller narrow which tools a run may invoke, through a scope that can
only remove tools, never add them back.

## Scope

Inside: an `ExecutionClass` enum, an `ExecutionProfile` struct, an
optional `ProfiledTool` interface, an optional `ResultBudgetTool`
interface, an optional `PrivilegedTool` interface, and a `Scope` type
built from `ScopeOptions`. Inside: a `Registry.RunScoped` method that
applies a `Scope` before it runs a tool. Outside: any change to
`Tool`, `Registry.Add`, `Registry.Get`, `Registry.Remove`, or
`Registry.Run`. Outside: any mivia-specific field on `ExecutionProfile`
or `Scope`; the shape stays generic so any caller in this module or a
future one can reuse it.

This phase touches only `tools/`. `policy/layers.json` keeps the
`tools` row at `[]`. The new types use only `context`, `time`, and
`errors` from the standard library, the same as the shipped package.
No internal import is added.

A tool that does not implement `ProfiledTool` is unclassified.
Callers treat an unclassified tool as `ExecutionClassUnclassified`,
the zero value. This keeps every existing shipped tool valid with no
change.

`Registry.Run` keeps its four-argument signature and its behavior.
Execution-profile checks are opt-in through a new method, `RunScoped`,
not through a hidden check inside `Run`. Two reasons drive this
choice. First, `Run`'s signature is a locked API from phase 14; adding
a silent check inside it would change behavior for every existing
caller without a signature change to flag the shift. Second, scope
narrowing needs a `Scope` value the caller must build and pass; `Run`
has no parameter for it, and adding one would break the lock. A new
method keeps the old path unchanged and makes scoped calls explicit
at the call site.

`Scope` narrows only. Built once from `ScopeOptions{Allowlist,
ExtraDenylist}` through `NewScope`. `ExtraDenylist` always removes a
name from the allowed set. `Allowlist`, when non-empty, keeps only
the named tools; when empty, every tool not denied and not privileged
is allowed. A tool that implements `PrivilegedTool` and reports true
is denied unless its name appears in `Allowlist`. No operation on a
built `Scope` can re-add a name `ExtraDenylist` removed.

### Naming: `ExecutionProfile`, not `Capability`

`discovery.Card.Capabilities` already owns the word "capability" for
what an agent can do: a discovery card lists capability names another
agent matches against. This phase's struct means something different:
execution-risk metadata for one tool call — its class, its dedup
resource key, and its timeout. Reusing "capability" for both concepts
in packages the roadmap composes (`discovery` and `tools` both feed a
future agent-binding phase) would collide two meanings under one word.
This phase names its struct `ExecutionProfile` and its marker
interface `ProfiledTool` instead, so the word "capability" stays
`discovery`'s alone.

### Out-of-enum `Class` values: permissive by design

`ExecutionProfileOf` and `RunScoped` do not call `ExecutionClass.
Validate`. A `ProfiledTool` publishes whatever `Class` it wants;
`ExecutionProfileOf` returns it unchanged, including a value outside
the four constants. `Validate` exists for a caller that builds an
`ExecutionProfile` by hand and wants to check it before it registers
the tool, the same way a caller may call `envelope.Message.Validate`
before it signs. `RunScoped` does not reject an unvalidated or
out-of-enum `Class`, because `Scope.Allowed` never reads `Class`; it
reads only `PrivilegedTool`, `Allowlist`, and `ExtraDenylist`. An
out-of-enum `Class` cannot change whether a tool runs. This plan
states this choice once, here, instead of leaving it implicit: an
untrusted or malformed `Class` value passes through `ExecutionProfileOf`
unchanged and never blocks `RunScoped`.

### Distinct from the future `contextbudget.Limits` budget

`ResultBudgetTool.MaxResultBytes` bounds one tool call's output size.
`docs/plans/agents/phase32_context_budget.md` plans a `Limits` type
that bounds a whole model call's context: total byte and event count
across many messages. The two are orthogonal. `MaxResultBytes` caps
what one `Run` or `RunScoped` call returns before that result ever
reaches a context budget's accounting. Neither type composes with the
other in this phase; `contextbudget` does not import `tools`, and
`tools` does not import `contextbudget`.

## API

- `type ExecutionClass string` — the enum. Validate enforces the set.
- `const ExecutionClassUnclassified ExecutionClass = ""` — the zero
  value; the default for a tool with no `ExecutionProfile`.
- `const ExecutionClassRead ExecutionClass = "read"`
- `const ExecutionClassWrite ExecutionClass = "write"`
- `const ExecutionClassExternal ExecutionClass = "external"`
- `type ExecutionProfile struct { Class ExecutionClass; ResourceKey string; Timeout time.Duration }`
- `type ProfiledTool interface { ExecutionProfile() ExecutionProfile }`
  — optional; a `Tool` implements it to publish its class, its
  per-turn dedup key, and its timeout.
- `type ResultBudgetTool interface { MaxResultBytes() int }` —
  optional; a `Tool` implements it to bound its output size.
- `type PrivilegedTool interface { Privileged() bool }` — optional; a
  `Tool` implements it to mark itself as needing explicit allowlisting.
- `func ExecutionProfileOf(t Tool) ExecutionProfile` — returns
  `t.ExecutionProfile()` when `t` implements `ProfiledTool`; else
  returns the zero `ExecutionProfile`, whose `Class` is
  `ExecutionClassUnclassified`. Never calls `Validate`; see "Out-of-enum
  `Class` values" above.
- `func ResultBudgetOf(t Tool) (int, bool)` — returns
  `t.MaxResultBytes()` and true when `t` implements `ResultBudgetTool`;
  else returns `0, false`.
- `func IsPrivileged(t Tool) bool` — returns `t.Privileged()` when `t`
  implements `PrivilegedTool`; else returns false.
- `type ScopeOptions struct { Allowlist []string; ExtraDenylist []string }`
- `type Scope struct` — built only through `NewScope`; holds the
  resolved allow and deny sets.
- `func NewScope(opts ScopeOptions) *Scope`
- `(*Scope).Allowed(name string, t Tool) bool` — true when `name`
  passes the denylist, the privileged check, and the allowlist.
- `(*Registry).RunScoped(ctx context.Context, name string, in InOut, scope *Scope) (Out, error)`
  — resolves `name` through `Get`, checks `scope.Allowed`, then calls
  `Run`. Returns `ErrUnknownName` for an unresolved name and
  `ErrScopeDenied` for a name the scope excludes. A nil `scope` allows
  every resolved tool, matching `Run`'s current behavior.
- `var ErrScopeDenied = errors.New(...)` — `RunScoped`'s error when
  `scope.Allowed` returns false.

`Validate` on `ExecutionClass` lives beside the enum: it rejects any
value outside the four constants. A caller that builds an
`ExecutionProfile` by hand may call it before it registers a
`ProfiledTool`, but nothing in this package calls it, per the
"Out-of-enum" note above.

### Semgrep: `ExecutionClass` joins the no-string-literal enum rule

`semgrep/sdk-standards.yml`'s `sdk.go.no-enum-string-literals` rule is
a fixed regex alternation over `Intent|Epistemic|AckStatus|Role`. This
phase adds `ExecutionClass` to that alternation in the same change, so
`ExecutionClass("read")` and `.Class = "read"` fail the rule the same
way an ad hoc `Intent("request")` does today. This phase also adds one
probe pair to `scripts/check_semgrep_probes.py`'s `PROBES` list, in the
same `viol_enum.go`/`clean_enum.go` shape the existing `sdk.go.
no-enum-string-literals` probe uses: a violation snippet that builds
`ExecutionClass("x")` and a clean snippet that uses the declared
constant. The existing probe pair may extend to cover
`ExecutionClass` in the same two files rather than adding new ones, as
long as both the `Intent` and the `ExecutionClass` violations still
fire and both clean snippets still stay silent.

## Tests

Test files live in `tools/tools_test/`, beside the phase 14 suite:

- `execution_profile_test.go` — red-green cases for
  `ExecutionProfileOf`, `ResultBudgetOf`, and `IsPrivileged`. A tool
  implementing `ProfiledTool` returns its published `ExecutionProfile`
  unchanged. A tool that does not implement `ProfiledTool` returns the
  zero `ExecutionProfile` with `Class == ExecutionClassUnclassified`.
  A tool implementing `ResultBudgetTool` returns its bound and true; a
  tool that does not returns zero and false. `ExecutionClass.Validate`
  rejects a value outside the four constants. One case registers a
  `ProfiledTool` that publishes an out-of-enum `Class` (a string none
  of the four constants match) and proves `ExecutionProfileOf` returns
  it unchanged and `RunScoped` still runs the tool when the scope
  otherwise allows it, proving the permissive-by-design choice above.
- `scope_test.go` — red-green cases for `NewScope` and
  `Scope.Allowed`. An empty `ScopeOptions` allows any non-privileged
  tool. A name in `ExtraDenylist` is denied even when `Allowlist` also
  names it, proving denylist wins. A name absent from a non-empty
  `Allowlist` is denied. A privileged tool is denied when its name is
  absent from `Allowlist`, and allowed when present.
- `registry_run_scoped_test.go` — red-green cases for `RunScoped`. An
  unknown name returns `ErrUnknownName`. A denied name returns
  `ErrScopeDenied` and never calls the tool's `Run`. An allowed name
  runs and returns the tool's result. A nil `Scope` behaves like
  `Run`.
- `execution_profile_integration_test.go` — register a read-class tool
  and a write-class tool implementing `ProfiledTool` in one `Registry`.
  Build a `Scope` that allowlists only the read tool. Prove `RunScoped`
  runs the read tool and denies the write tool with `ErrScopeDenied`.
  Prove `Registry.Run`, unscoped, still runs both, showing the phase 14
  path is unchanged.
- `registry_run_scoped_concurrent_test.go` — modeled on phase 14's
  `registry_concurrent_test.go` pattern, required by
  `docs/plans/tools.md` for every method that touches the tools map.
  A tool is registered. N goroutines call `RunScoped` for its name
  under an allowing `Scope`, racing against N goroutines calling
  `Remove` for the same name, all under `go test -race`. Sub-case one
  uses an allowing `Scope`. Every `RunScoped` call returns either the
  tool's result or `ErrUnknownName` (removed before `Get` resolved
  it), never `ErrScopeDenied`. A second sub-case adds a denying
  `Scope` racing the same `Remove` goroutines and asserts every call
  returns either `ErrScopeDenied` or `ErrUnknownName`. No call may
  panic.
- `registry_run_scoped_bench_test.go` — benchmark `RunScoped` on a
  registry of one hundred tools behind a `Scope` with a fifty-name
  allowlist. State the allocation budget next to phase 14's `Run`
  benchmark.

## Verification

`make verify` passes. The coverage floor for `tools` holds, including
the new files. `api/tools.txt` gains the new symbols via
`make api-update`; every symbol already in the lock (`Add`, `Get`,
`Remove`, `Run`, `New`, `InOut`, `Out`, `Registry`, `Tool`,
`ErrBlankName`, `ErrDuplicateName`, `ErrNilTool`, `ErrUnknownName`)
stays unchanged. `policy/layers.json`'s `tools` row stays `[]`.
`go test -race ./tools/...` passes, covering
`registry_run_scoped_concurrent_test.go`.

`semgrep/sdk-standards.yml`'s `sdk.go.no-enum-string-literals` rule
gains `ExecutionClass` in its regex alternation, in the same change as
the code, and `python3 scripts/check_semgrep_probes.py` passes with
the extended or added probe pair proving the rule fires on an
`ExecutionClass("x")` violation and stays silent on the declared
constants. `make verify`'s Semgrep probe suite covers this in the same
run as the existing `Intent` probe.

`docs/plans/tools.md` and `docs/packages/tools.md` — the landed
contract `scripts/check_plan.py` gates and its package reference — are
amended in the same change to add `ExecutionClass`, `ExecutionProfile`,
`ProfiledTool`, `ResultBudgetTool`, `PrivilegedTool`, `Scope`,
`ScopeOptions`, `NewScope`, `ExecutionProfileOf`, `ResultBudgetOf`,
`IsPrivileged`, `RunScoped`, and `ErrScopeDenied`, and the
concurrency contract stated above. This matches how phase 14's
roadmap plan was folded into `docs/plans/tools.md` when phase 14
landed: the roadmap file in `docs/plans/agents/` records the design
history, and the package plan `scripts/check_plan.py` reads records
the current contract.
