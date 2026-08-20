# Phase 77: workspace list and stat tools return a JSON string result

## Goal

Fix a confirmed production bug. `runconfig.WorkspaceListKind` and
`runconfig.WorkspaceStatKind` cannot complete a real
`agentrun.Runner.Run`. Every run fails with `agentrun.ErrResultNotText`.
Make `subagent.WorkspaceListTool` and `subagent.WorkspaceStatTool`
return a JSON-encoded string in `tools.Out.Value`, matching every
other `subagent` tool's result convention.

## Why this phase exists

Two independent review passes confirmed the same bug with real
reproductions. `agentrun/wire.go`'s `chain` requires a tool's result,
`tools.Out.Value`, to be a `string`
(`agentrun/wire.go:134`). It returns `ErrResultNotText` otherwise.

`subagent.WorkspaceListTool.Run` returns `tools.Out{Value: out}`,
where `out` is `[]subagent.WorkspaceEntry`
(`subagent/workspacelisttool.go:68`).
`subagent.WorkspaceStatTool.Run` returns
`tools.Out{Value: WorkspaceFileInfo{...}}`
(`subagent/workspacestattool.go:72`). Neither is a string.

Every other tool `subagent` ships returns a string `Out.Value`:
`WorkspaceReadTool`, `WorkspaceWriteTool`, `DiffTool`, `AsTool`,
`FlowTool`, and the ten command-decoding internal tools all confirm
this by grep of every `tools.Out{Value: ...}` construction site in
the package. `WorkspaceListTool` and `WorkspaceStatTool` are the only
two outliers.

Blast radius: two of the six `Kind` constants phase 72 shipped
(`runconfig/blocks.go`) are unusable end to end through a real
`agentrun.Runner.Run`. `docs/plans/runconfig.md`'s phase 76 addendum
already records this as an open, expected failure pending a
follow-up fix. This phase is that follow-up.

### Fix direction: fix the two outlier tools, not chain

Two directions were on the table.

- Option A: fix `WorkspaceListTool.Run` and `WorkspaceStatTool.Run` to
  return a JSON-encoded string, matching every other `subagent` tool.
- Option B: teach `agentrun`'s `chain` to accept a non-string result,
  through a JSON fallback or a new opt-in tool capability interface.

This phase picks Option A.

`WorkspaceEntry` (`Name string`, `IsDir bool`) and `WorkspaceFileInfo`
(`Name string`, `Size int64`, `IsDir bool`, `ModTime time.Time`) are
plain structs. Every field is a string, an int, a bool, or
`time.Time`, which `encoding/json` marshals through its
`MarshalJSON` method. No field is a channel, a func, or an
unexported-only type. `json.Marshal` on either value cannot fail.
`WorkspaceListTool.Run` and `WorkspaceStatTool.Run` need no new error
path for the encode step.

No other caller in the module depends on the raw typed struct in
`Out.Value`. `subagent/subagent_test/filetools_test.go` is the only
site that type-asserts `out.Value.([]subagent.WorkspaceEntry)` or
`out.Value.(subagent.WorkspaceFileInfo)`, and this phase updates that
file in the same change. `WorkspaceEntry` and `WorkspaceFileInfo`
carry no other exported use in the repo (confirmed by a repo-wide
grep of both names). `subagent`'s `DecodeArguments` methods return
the argument struct, `WorkspaceListArgs` or `WorkspaceStatArgs`, in
`tools.InOut.Value`, on the input side; this phase changes only the
`Run` output side.

`agentloop.Loop.render`'s `renderValue` helper
(`agentloop/wire.go:44`) is a second real consumer of `Out.Value`.
It already branches on the value's type: a `string` passes through
unchanged; anything else falls back to `json.Marshal`. Before this
phase, `renderValue` hits the `json.Marshal` fallback branch for
`WorkspaceListTool` and `WorkspaceStatTool` results. After this
phase, it hits the `string` branch instead, because the tool now
encodes the JSON itself. `json.Marshal` on a `[]WorkspaceEntry` or a
`WorkspaceFileInfo` produces the same bytes whether `renderValue`
calls it or the tool's `Run` method calls it, so the rendered
`RoleTool` message content is byte-identical before and after this
phase. The Tests section adds a test proving this equivalence
directly, rather than asserting it by reasoning alone.

Option B was rejected. Widening `chain`'s accepted result type, even
behind an opt-in capability interface, changes a contract every
`agentrun` caller relies on: "a tool result is a string." A capability
interface would also need its own decode step somewhere downstream
(the model-facing render path, or `agentloop`), duplicating the
`encoding/json` call this phase already needs inside the tool. Option
A fixes the bug at its source, the two tools that break the existing
convention, with a smaller and more consistent change. It needs no
`agentrun` change and no new interface.

## Scope

In scope:

- `subagent.WorkspaceListTool.Run`: JSON-encode `[]WorkspaceEntry`
  into `Out.Value` as a string.
- `subagent.WorkspaceStatTool.Run`: JSON-encode `WorkspaceFileInfo`
  into `Out.Value` as a string.
- `subagent/subagent_test/filetools_test.go`: update
  `TestWorkspaceListTool` and `TestWorkspaceStatTool` to decode the
  JSON string result instead of type-asserting the typed struct.
- `runconfig/runconfig_test/runner_test.go`: two new end-to-end tests,
  `TestRunnerResolvesWorkspaceListReal` and
  `TestRunnerResolvesWorkspaceStatReal`, proving both Kinds now
  complete a real `Runner.Run`.
- `docs/plans/subagent.md`: addendum recording the JSON-string result
  shape for both tools.
- `docs/plans/runconfig.md`: addendum correcting the phase 76 note;
  all six Kinds are now confirmed end to end.
- `docs/packages/subagent.md:84-88`: update the `WorkspaceListTool`
  and `WorkspaceStatTool` bullets. Each must state the result is a
  JSON-encoded string of the named type (`[]WorkspaceEntry` or
  `WorkspaceFileInfo`), not the raw typed struct.

Out of scope:

- `agentrun/wire.go`'s `chain`. This phase changes no line in
  `agentrun`.
- `WorkspaceEntry` and `WorkspaceFileInfo`'s field shapes. Both types
  stay exported and unchanged; only the tool's `Run` method changes
  how it packages them into `Out.Value`.
- Any change to `WorkspaceListArgs` or `WorkspaceStatArgs`, the
  input-side argument structs. `DecodeArguments` is unaffected.

## API

No new exported symbol. `WorkspaceListTool` and `WorkspaceStatTool`
keep their existing signatures:

```go
func WorkspaceListTool(name string, ft *FileTools) tools.Tool
func WorkspaceStatTool(name string, ft *FileTools) tools.Tool
```

`WorkspaceEntry` and `WorkspaceFileInfo` keep their existing field
shapes and `json` tags, unchanged. `make api-update` produces no diff
in `api/subagent.txt`; this phase changes an unexported method body,
`workspaceListTool.Run` and `workspaceStatTool.Run`, not an exported
symbol's signature.

Diff for `subagent/workspacelisttool.go`:

```go
import (
	"context"
	"encoding/json"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Run lists the directory at args.Path, relative to t.ft's bound
// root, sorted the way ws.List's underlying os.ReadDir already
// sorts. The result is a JSON-encoded string of []WorkspaceEntry, so
// agentrun's chain accepts it as a tool result.
func (t *workspaceListTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(WorkspaceListArgs)
	if !ok {
		return tools.Out{}, badArguments(t.name)
	}
	entries, err := t.ft.ws.List(args.Path)
	if err != nil {
		return tools.Out{}, err
	}
	out := make([]WorkspaceEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, WorkspaceEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: string(encoded)}, nil
}
```

Diff for `subagent/workspacestattool.go`:

```go
import (
	"context"
	"encoding/json"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Run stats args.Path, relative to t.ft's bound root. The result is
// a JSON-encoded string of WorkspaceFileInfo, so agentrun's chain
// accepts it as a tool result.
func (t *workspaceStatTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(WorkspaceStatArgs)
	if !ok {
		return tools.Out{}, badArguments(t.name)
	}
	info, err := t.ft.ws.Stat(args.Path)
	if err != nil {
		return tools.Out{}, err
	}
	encoded, err := json.Marshal(WorkspaceFileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	})
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: string(encoded)}, nil
}
```

The `err` check after `json.Marshal` stays even though the field
shapes make it unreachable today. `Validate`-style defensive checks
follow the rest of the package's error-handling pattern, and a future
field addition to either struct keeps the check correct.

## Tests

- `subagent/subagent_test/filetools_test.go`:
  - `TestWorkspaceListTool`: decode `out.Value.(string)` with
    `json.Unmarshal` into `[]subagent.WorkspaceEntry`, then assert
    the same fixture rows the test already checks.
  - `TestWorkspaceStatTool`: decode `out.Value.(string)` with
    `json.Unmarshal` into `subagent.WorkspaceFileInfo`, then assert
    the same fields the test already checks.
- `runconfig/runconfig_test/runner_test.go`, new end-to-end tests
  mirroring `TestRunnerResolvesWorkspaceReadReal`,
  `TestRunnerResolvesWorkspaceWriteReal`, and
  `TestRunnerResolvesDiffReal`:
  - `TestRunnerResolvesWorkspaceListReal`: seeds a temp directory with
    one file and one subdirectory, binds a real `WorkspaceListTool` to
    `runconfig.WorkspaceListKind`, drives one step through a real
    `Runner.Run`, asserts status `done`, and asserts the recorded
    artifact JSON-decodes into the seeded `[]subagent.WorkspaceEntry`
    rows.
  - `TestRunnerResolvesWorkspaceStatReal`: seeds a temp file, binds a
    real `WorkspaceStatTool` to `runconfig.WorkspaceStatKind`, drives
    one step through a real `Runner.Run`, asserts status `done`, and
    asserts the recorded artifact JSON-decodes into a
    `subagent.WorkspaceFileInfo` matching the seeded file's name and
    size.
- Existing tests that already exercise `WorkspaceListTool` and
  `WorkspaceStatTool` through `tools.Registry.RunScoped` (scope and
  schema coverage in `filetools_test.go`) stay in place; only the
  result-shape assertion changes.
- `subagent/subagent_test/filetools_test.go`, a new agentloop-level
  test alongside `TestFileToolsThroughAgentloop`: drives
  `WorkspaceListTool` through one `agentloop.Loop.Run` tool call, then
  asserts the recorded `RoleTool` message content equals
  `json.Marshal` of the seeded `[]subagent.WorkspaceEntry` rows,
  computed independently in the test. This proves
  `agentloop.render`'s `renderValue` (`agentloop/wire.go:44`) renders
  the tool's new string result identically to the JSON-fallback
  branch it used before this phase.

## Verification

- `make verify` passes: gofmt, vet, tests, doc gate, structure gate,
  Semgrep scan, probes.
- `go test ./subagent/... ./runconfig/... ./agentrun/...` passes,
  including the two new end-to-end tests.
- `make api-update` produces no diff in `api/subagent.txt` or
  `api/runconfig.txt`; no exported symbol changes.
- `python3 scripts/check_deps.py` passes with no new
  `policy/layers.json` row. `subagent` already imports `encoding/json`
  for its command-decoding tools (`subagent/filetools.go`,
  `subagent/commands.go`); `encoding/json` is a standard-library
  import, not an internal-module edge, so `policy/layers.json` needs
  no change.
- `docs/plans/subagent.md`: addendum states
  `WorkspaceListTool.Run` and `WorkspaceStatTool.Run` return a
  JSON-encoded string, not the raw typed struct, correcting the
  "Result is a JSON-renderable" language in the tool descriptions.
- `docs/plans/runconfig.md`: addendum replaces the phase 76 note.
  States all six `Kind` constants, including `WorkspaceListKind` and
  `WorkspaceStatKind`, are confirmed end to end through a real
  `Runner.Run`, with a pointer to this phase's plan.
- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  `python3 scripts/check_prose.py`, and
  `python3 scripts/check_labels.py` all pass on this plan doc.
