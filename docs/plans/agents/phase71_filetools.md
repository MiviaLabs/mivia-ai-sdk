# Phase 71: file tools, gated by a mandatory secret policy

Status: plan, ready for plan review. Corrects a confirmed gap in
commit 718d79b, which shipped the five file tools this phase names
below with no enforced secret policy.

## Why this phase exists

Commit 718d79b shipped `WorkspaceReadTool`, `WorkspaceWriteTool`,
`WorkspaceListTool`, `WorkspaceStatTool`, and `DiffTool` in
`subagent`. Each constructor took an already-open
`*workspace.Workspace` directly. `docs/plans/secrets.md` built
`workspace.Options.Deny` and `secretpath.Matcher` for exactly one
reason: to refuse a model-facing tool a path that holds a secret.
Nothing in the shipped constructors called that machinery.

An independent code audit confirmed the gap by reading the shipped
files line by line:

- Every constructor's second parameter is `ws *workspace.Workspace`,
  for example `subagent/workspacereadtool.go:24`.
- No `Options` type, no `Deny *secretpath.Matcher` field, and no
  `Validate` call exist anywhere in `subagent`.
- No call to `workspace.OpenWith` exists anywhere in `subagent`'s
  non-test code; only `workspace.Open`, only in the test file.
- No call to `Workspace.Close` exists anywhere in `subagent`'s
  non-test code.
- `workspace.ErrEscape` and `workspace.ErrSecretPath` already
  propagate to the model unchanged; see "Error mapping" below for why
  that part of the shipped design stays.
- `secretpath` and `envfile` still have zero non-test consumers in
  the module.

The practical consequence: whoever opens the `Workspace` and passes
it to these constructors controls, silently, whether `.env` and
credential files are denied to a model. The tool itself enforces
nothing, and nothing in the type system stops a caller from wiring
an unrestricted workspace straight into a model-facing tool.

`tools.SchemaTool` is correctly implemented on all five tools, and
they correctly route through `agentloop`'s `Registry.RunScoped`.
Neither part of the shipped design needs a fix; both are inherited
correctly from `agentloop`'s own enforcement, not built by
`subagent`.

## Scope

Inside:

- One new file, `subagent/filetoolset.go`, holding `FileToolOptions`,
  `FileToolOptions.Validate`, `FileTools`, `OpenFileTools`,
  `(*FileTools) Close`, and `ErrDenyRequired`.
- A breaking signature change on the five existing tool constructors:
  their second parameter changes from `ws *workspace.Workspace` to
  `ft *FileTools`.
- The error-mapping decision: no new translation layer.
  `workspace.ErrSecretPath` and `workspace.ErrEscape` keep
  propagating to the model unchanged.
- Rewriting `subagent/subagent_test/filetools_test.go` to build a
  `*FileTools` through `OpenFileTools` instead of a bare
  `*workspace.Workspace` through `workspace.Open`, and adding a new
  file, `subagent/subagent_test/filetoolset_test.go`, holding the
  options-validation, close-idempotence, secret-path-denial,
  symlink-refusal, and concurrency test cases. See "Test migration"
  below for the reason the new cases need a second file.
- The `policy/layers.json` `subagent` row gains `secretpath`.
- `docs/plans/subagent.md`'s "File tools addendum" is rewritten to
  match this design; that document is the declarative API contract.
  This document is the narrative: why the change, what it corrects,
  and the sequencing.
- `docs/plans/secrets.md`'s "Phase 71 owns the tool surface" section
  is corrected: the `envfile.LoadBytes` term is marked dropped, and
  the "one constructor" phrase is corrected to name the actual shape
  (five tool constructors plus one enforcing constructor).

Outside:

- Any change to `workspace`, `secretpath`, or `diff`. All three
  already ship the exact primitives this phase composes; an
  independent audit read `workspace/workspace.go`,
  `workspace/confine.go`, and `secretpath/matcher.go` and found each
  correct for this phase's purpose. This phase adds one new consumer
  in `subagent`, and no new symbol in any of the three.
  `workspace.Options.Deny` stays optional at the `workspace` level;
  `subagent` adds its own, stricter requirement on top, in its own
  `FileToolOptions.Validate`.
- `envfile.LoadBytes` as a tool, or as any new caller. See "Envfile"
  below.
- Any narrowing of the symlink walk. `workspace.denied` refuses every
  symlink component whenever `Deny` is non-nil, with no narrower
  option. A mandatory `Deny` means this refusal now always applies to
  a `FileTools`-bound tool. This phase accepts that refusal; adding a
  narrower walk is `workspace`'s change to make, on a real caller
  need, not this phase's.
- A backward-compatible shim for the five old signatures. See
  "Breaking change, not a shim" below.

## Design decisions

### Where the enforcing constructor lives

Three shapes were possible: (a) each of the five constructors takes
its own `Options` struct with a `Deny` field; (b) one shared options
type all five constructors accept directly, alongside the raw
workspace; (c) enforcement moves up a level, into how the
`*workspace.Workspace` itself gets built and handed to `subagent`.

This phase picks (c), sharpened into one owning type. `OpenFileTools`
is the one place these five tools accept a workspace from.
`FileToolOptions.Validate` rejects a nil `Deny` before any filesystem
call runs, so a caller cannot construct a usable `*FileTools` without
naming a secret policy, even an empty one
(`secretpath.NewMatcher(nil)`). Shape (a) or (b) alone would still
let a caller build the five tools from a raw `*workspace.Workspace`
that some other code path opened with no `Deny`, since neither shape
removes that parameter. Only removing the raw `*workspace.Workspace`
parameter closes the hole; shape (c) is the only one of the three
that does.

`FileTools` bundles one opened `*workspace.Workspace` so the five
tools share one open root and one owner for `Close`, instead of each
tool opening its own `os.Root` on the same directory. This follows
`subagent.Mailbox`'s existing precedent: one caller-owned value,
opened once, several tool constructors reading it.

This breaks all five existing constructors' signatures. See
"Breaking change, not a shim."

### Old constructors: removed, not deprecated

The five old signatures are replaced in place, not kept alongside the
new ones. AGENTS.md forbids a backwards-compatibility shim for
removed code. A shim here is worse than the general case: it would
keep the exact hole this phase closes open for any caller that keeps
using the old signature, since the old signature took the
enforcement-free `*workspace.Workspace` directly. Any real caller of
718d79b's constructors updates its call sites: build a `*FileTools`
through `OpenFileTools`, then pass it where the old
`*workspace.Workspace` argument was.

### Error mapping: no new translation layer

`workspace.ErrSecretPath` and `workspace.ErrEscape` already carry
distinct text: "path is a secret path" (plus "symlink component" for
the walk-triggered case) against "path escapes root." Under
`agentloop`'s default `ErrorPolicyReport`, `err.Error()` reaches the
model verbatim, `ToolErrorPrefix`-marked. A model reading that
message already learns which rule it hit, with no added mapping
layer needed.

Neither sentinel leaks anything the model did not already supply.
Both echo back only the path argument from the model's own failed
call, never file content and never a different path. `DiffTool`'s
doc comment already states "any other `ws.ReadFile` error, including
`workspace.ErrEscape`, propagates unchanged"; this phase extends the
same rule to `ErrSecretPath` and applies it to all five tools.

A generic, single refusal message was considered and rejected: it
would discard information a model can act on for free (stop retrying
a traversal attempt against a hard sandbox boundary, versus stop
asking for a specific denied file), for no privacy gain, since the
path text is the model's own input either way.

Distinguishable sentinels also let a model probe candidate paths and
learn, from which sentinel comes back, whether a guessed name exists
and whether it is secret-designated: `ErrSecretPath` for a denied
name, `fs.ErrNotExist` for an absent one, success for a permitted
one. This is an existence-plus-classification oracle, distinct from
the no-content-leak argument above: a model that guesses `.env`,
`id_rsa`, and `credentials.json` learns one bit per guess about which
names this deployment treats as secret, with no file bytes crossing
the boundary. This phase accepts that trade-off. The information gain
per guess is bounded to one bit and never includes content, and the
same ergonomic value that justifies unmapped sentinels for the
traversal case, above, applies here: a model that cannot distinguish
"denied" from "absent" cannot tell a hard sandbox boundary from a
typo, and keeps probing instead of moving on.

### Envfile

`docs/plans/secrets.md`'s original phase-71 contract read: "Phase 71
also calls `envfile.LoadBytes` on bytes that `workspace.ReadFile`
returned." This phase drops that term.

`envfile.Load` was already refused as a tool in `docs/plans/subagent.md`'s
shipped addendum, with the reasoning: a parsed dotenv map is close to
a credential-exfiltration primitive, and a model reading it gains no
legitimate benefit `envfile` does not already give the caller's own
process at startup. Adding a `LoadBytes` call inside a file tool just
to satisfy the original contract line, with no real caller asking for
it, repeats the exact "uncalled surface" problem `docs/plans/secrets.md`
exists to fix: AGENTS.md rejects added abstraction with no caller. If
a real caller later names a reviewed use for a parsed dotenv result,
that need gets its own tool and its own plan review; this phase does
not speculate one into existence.

### Close ownership

`OpenFileTools`'s caller owns the matching `Close`, mirroring
`workspace.Open`'s own existing contract. `(*FileTools) Close` closes
the workspace `OpenFileTools` opened; a `defer ft.Close()` right
after a successful `OpenFileTools` call is the expected shape.
Skipping `Close` leaks the `os.Root` file descriptor `OpenFileTools`
opened, for the life of the process, the same leak a
`workspace.Open` caller already risks today by skipping its own
`Close`. No component inside `subagent` calls `Close` on the
caller's behalf, since nothing inside `subagent` outlives the
caller's own `*FileTools` value.

`FileTools` needs no mutex of its own for concurrent `Run` calls.
`os.Root`'s documented contract states its methods are safe for
concurrent use from multiple goroutines, and `FileTools` adds no
further mutable state on top of the `*workspace.Workspace` it wraps.
Concurrent `Run` calls against one shared `FileTools` are a real
pattern this SDK supports elsewhere: a `flow` panel runs its member
steps concurrently in goroutines, so a panel that shares one
`FileTools` across two file-tool steps relies on this guarantee.

### Test migration

`subagent/subagent_test/filetools_test.go` is rewritten, not merely
patched. Its `openWorkspace` helper, which calls `workspace.Open`
directly, is replaced by `openFileTools(t, deny []string) *FileTools`,
which calls `subagent.OpenFileTools` with a `secretpath.NewMatcher`
built from the given patterns and registers `Close` on `t.Cleanup`.
Every table row's `build` func changes its parameter from
`ws *workspace.Workspace` to `ft *subagent.FileTools`. The rewrite is
forced regardless of intent: the old helper does not compile against
the new constructor signatures.

The shipped `filetools_test.go` already sits at 481 lines before this
phase's changes. The new `FileTools`-level cases (options validation,
`Close` idempotence, secret-path denial, the mandatory symlink
refusal, and the concurrency case in finding four below) do not fit
under the 500-line file cap alongside the existing tool-behavior
tests. This phase adds a second file,
`subagent/subagent_test/filetoolset_test.go`, package `subagent_test`,
holding only the `FileTools`-level cases. `filetools_test.go` keeps
the `openFileTools` helper and every tool-behavior test
(`TestWorkspaceReadTool` through
`TestFileToolsThroughAgentloopScopeDeniesWrite`), rewritten in place
for the `*FileTools` parameter type but otherwise unmoved. See
"Tests" below for the exact split.

## API

```go
type FileToolOptions struct {
	Root         string
	Deny         *secretpath.Matcher
	MaxReadBytes int64
}
func (o FileToolOptions) Validate() error

type FileTools struct{ /* unexported */ }
func OpenFileTools(opts FileToolOptions) (*FileTools, error)
func (f *FileTools) Close() error

var ErrDenyRequired error

func WorkspaceReadTool(name string, ft *FileTools, maxResultBytes int) tools.Tool
func WorkspaceWriteTool(name string, ft *FileTools) tools.Tool
func WorkspaceListTool(name string, ft *FileTools) tools.Tool
func WorkspaceStatTool(name string, ft *FileTools) tools.Tool
func DiffTool(name string, ft *FileTools, maxLines int) tools.Tool
```

The argument and result types (`WorkspaceReadArgs`,
`WorkspaceWriteArgs`, `WorkspaceListArgs`, `WorkspaceStatArgs`,
`DiffArgs`, `WorkspaceEntry`, `WorkspaceFileInfo`) and the existing
`ErrBadArguments` sentinel are unchanged. `docs/plans/subagent.md`'s
"File tools addendum" is the full declarative contract; this section
restates only the new and changed symbols.

`policy/layers.json`'s `subagent` row gains `secretpath`, alphabetized
alongside its existing `diff` and `workspace` entries. `diff` and
`workspace` already carry rows for `subagent`; neither changes.

## Sequencing

This phase has no dependency on phase 69 or 70's code; `agentloop`
already exists as a caller through `tools.Registry.RunScoped`, and
`tools.SchemaTool` already ships. This phase depends only on the two
already-shipped halves of `docs/plans/secrets.md`: `workspace.Options.Deny`
and `secretpath.Matcher`, both confirmed correct by the audit that
motivated this plan. It ships as one change: the `subagent` code, the
`docs/plans/subagent.md` addendum rewrite, this file, the
`docs/plans/secrets.md` correction, and the `policy/layers.json` row.

## Tests

`docs/plans/subagent.md`'s "File tools addendum" Tests section is the
full list. Named here for the phase-plan record, split across two
files to hold the 500-line file cap: the existing
`subagent/subagent_test/filetools_test.go` keeps the tool-behavior
cases, and a new `subagent/subagent_test/filetoolset_test.go` holds
the `FileTools`-level cases. Both files, package `subagent_test`.

In `subagent/subagent_test/filetoolset_test.go`:

- `TestOpenFileToolsValidatesOptions` — red-green. Table over a blank
  `Root`, a nil `Deny`, both invalid together, and a valid case.
  Asserts `errors.Is(err, subagent.ErrDenyRequired)` on the nil-`Deny`
  row and a non-nil `*FileTools` plus nil error on the valid row.
  This is the direct pin for the gap this phase closes: a caller could
  previously build the five tools from a raw `*workspace.Workspace`
  opened with no `Deny` at all; this row is red against the shipped
  718d79b constructors and green once `OpenFileTools` and its
  mandatory `Deny` land.
- `TestFileToolsCloseIsIdempotent` — red-green. Two `Close` calls on
  one opened `*FileTools` both return `nil`.
- `TestFileToolsDeniesSecretPath` — integration, table-driven over
  all five tools against `openFileTools(t, []string{"secret.env"})`.
  Asserts `errors.Is(err, workspace.ErrSecretPath)` against the
  denied path and no such error against a permitted sibling path.
  This confirms the tool layer forwards the already-shipped
  workspace-level denial without swallowing it; denial-when-a-pattern-
  matches is a `workspace` property, shipped and tested before this
  phase existed. It is a real property this phase must not break, not
  the headline gap this phase closes.
- `TestOpenFileToolsSymlinkRefused` — integration. A symlink to a
  permitted file, under `openFileTools(t, nil)` (an empty-pattern,
  non-nil `Deny`), still returns `errors.Is(err, workspace.ErrSecretPath)`
  through `WorkspaceReadTool`, pinning the unconditional symlink
  refusal.
- `TestFileToolsConcurrentReadsSafeUnderClose` — race-covered. Several
  goroutines call `WorkspaceReadTool.Run` against one shared
  `*FileTools` while it stays open, joined before `Close` runs once.
  Asserts every call returns its expected result or error with no
  race, pinning the "no mutex needed" claim in "Close ownership."

In `subagent/subagent_test/filetools_test.go`:

- `TestWorkspaceReadTool`, `TestWorkspaceWriteTool`,
  `TestWorkspaceListTool`, `TestWorkspaceStatTool`, `TestDiffTool` —
  integration, rewritten to build through `openFileTools`, otherwise
  unchanged in intent from the shipped suite.
- `TestFileToolsEscape` — integration, table-driven over all five
  tools, `..`-traversal and absolute-path rows, asserting
  `errors.Is(err, workspace.ErrEscape)`.
- `TestFileToolsBadArguments`, `TestFileToolsSchemaCompile`,
  `TestFileToolsThroughAgentloop`,
  `TestFileToolsThroughAgentloopScopeDeniesWrite` — unchanged in
  intent from the shipped suite; rewritten only for the `*FileTools`
  parameter type.

## Verification

- `make verify` passes; `subagent` and the module total hold the 85
  floor.
- `go test -race ./subagent/... ./agentloop/...` passes.
- `make api-update` lands the `api/subagent.txt` diff: the five
  changed constructor signatures, `FileToolOptions`, `FileTools`,
  `OpenFileTools`, `(*FileTools) Close`, and `ErrDenyRequired`, in the
  same change as the code.
- `python3 scripts/check_deps.py` passes against the new `secretpath`
  edge on the `subagent` row.
- `python3 scripts/check_plan.py`, `check_prose.py`, and
  `check_labels.py` pass against this file and the
  `docs/plans/subagent.md` and `docs/plans/secrets.md` edits.
- `docs/packages/subagent.md` and `AGENTS.md`'s `subagent/` entry gain
  `FileToolOptions`, `FileTools`, `OpenFileTools`, `ErrDenyRequired`,
  and the five tools' new signatures in the same change as the code.
- This phase adds no gate and weakens none.
