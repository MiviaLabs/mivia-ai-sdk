# Phase 76: agentrun schema-tool argument decode

Status: shipped. Ships as changes to the shipped `agentrun` and
`runconfig` packages. No new top-level package. Depends on phase 72
(`runconfig` blocks, shipped as commit 0757dc8) and phase 71
(`subagent` file tools, folded into `docs/plans/subagent.md`).

## Goal

`agentrun.Runner.chain` must hand a bound tool the argument shape the
tool declares, not always a raw string. When the resolved tool
implements `tools.SchemaTool`, `chain` decodes the step's payload
through `DecodeArguments` before it calls `RunScoped`. When the tool
does not implement `SchemaTool`, `chain` keeps today's plain-string
behavior unchanged.

## Scope

### Why this phase exists

`agentrun/wire.go`'s `chain` drives every gated step like this:

```go
out, err := r.tools.RunScoped(ctx, name, tools.InOut{Value: msg.Payload}, r.scope)
```

`msg.Payload` is always a plain Go `string`, traced from
`flow.Step.Payload string` through `envelope.Message.Payload string`.
`chain` never checks whether the resolved tool implements
`tools.SchemaTool` (`tools/schema.go`), the interface a tool uses to
publish a parameter schema and decode raw argument bytes into its own
typed `tools.InOut`.

`runconfig/runner.go`'s `stepTool`, the wrapper `Definition.Runner`
registers under each step ID, passes this string straight through:

```go
func (s stepTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return s.inner.Run(ctx, in)
}
```

Phase 71 and phase 72 together shipped five `subagent` tools —
`WorkspaceReadTool`, `WorkspaceWriteTool`, `WorkspaceListTool`,
`WorkspaceStatTool`, `DiffTool` — each implementing `tools.SchemaTool`
and each type-asserting `in.Value` straight to its own typed args
struct inside `Run` (for example
`args, ok := in.Value.(WorkspaceReadArgs)`), with no string fallback.
Driven through `agentrun`'s `chain`, `in.Value` is always a string, so
the type assertion always fails. `Run` always returns
`subagent: <name>: subagent: bad arguments`, no matter what the
step's payload holds. `runconfig.WorkspaceReadKind`,
`WorkspaceWriteKind`, `WorkspaceListKind`, `WorkspaceStatKind`, and
`DiffKind` — five of the six `Kind` constants phase 72 shipped —
cannot drive a real end-to-end `Runner.Run` today.
`runconfig/runconfig_test/runner_test.go`'s
`TestRunnerResolvesWorkspaceReadReal` and
`runconfig/runconfig_test/load_integration_test.go`'s
`TestBudgetGatesDeclaredPayloadSize` both carry comments explaining
this gap and working around it: the first calls `DecodeArguments` and
`Run` directly instead of through `Runner.Run`; the second substitutes
`AsToolKind` for `WorkspaceReadKind` because `AsTool.Run` reads a
plain string and needs no schema decode. `AsToolKind`, the sixth Kind,
already works: `subagent.AsTool.Run` reads `in.Value` as a plain
string, matching `chain`'s current, unconditional behavior. The other
ten internal Kinds (`ledger`, `memory`, `room`, `scheduler`,
`heartbeat`, `discovery`, `trigger`, `channel`, `provider`,
`providerregistry`) also read `in.Value` as a plain string, most
self-parsing an internal JSON command out of it; none of them
implement `SchemaTool`.

A second, related gap sits in the same wrapper. `stepTool` also drops
`tools.ProfiledTool`, `tools.ResultBudgetTool`, and
`tools.PrivilegedTool` when it wraps `inner`: it declares no forwarding
methods for any of the four optional `tools.Tool` capability
interfaces. `tools.Registry.RunScoped` calls `ExecutionProfileOf(t)`
on the resolved tool to decide whether a call needs approval;
`tools.Scope.Allowed` calls `IsPrivileged(t)` to decide whether a
privileged tool needs an explicit allow entry. Both calls receive the
`stepTool` wrapper, not `inner`, from the registry's `Get`. A caller
that sets `agentrun.Options.Scope` with an approval threshold, or
binds a privileged internal tool through `runconfig`, gets silently
wrong gating: `ExecutionProfileOf` reports the zero profile and
`IsPrivileged` reports false, regardless of what `inner` actually
publishes. `spool/tool.go`'s `SpoolTool` wrapper already solves this
exact problem for its own wrapper, with a documented rule: "the
returned `tools.Tool` implements `tools.ProfiledTool`,
`tools.ResultBudgetTool`, `tools.PrivilegedTool`, and `tools.SchemaTool`
only when inner itself does, forwarding each call straight to inner."
This phase gives `stepTool` the same guarantee, using the same
capability-composition pattern, because fixing the `SchemaTool` gap
already means rebuilding `stepTool`'s construction path, and leaving
the other three capabilities half-fixed would repeat the same defect
class in three more places for zero extra design cost.

### In scope

- `agentrun/wire.go`'s `chain`: decode a `SchemaTool` tool's raw
  argument bytes from `msg.Payload` before calling `RunScoped`; keep
  the plain-string call unchanged for every other tool.
- `agentrun/options.go`: one new sentinel error for a decode failure,
  matching the existing sentinel-error and wrap style
  (`ErrResultNotText`'s precedent).
- `runconfig/runner.go` and a new `runconfig/steptool.go`: `stepTool`
  gains a constructor that returns a variant implementing exactly the
  optional `tools.Tool` interfaces `inner` implements —
  `tools.SchemaTool`, `tools.ProfiledTool`, `tools.ResultBudgetTool`,
  `tools.PrivilegedTool` — following `spool/tool.go`'s `SpoolTool`
  pattern (base struct plus per-capability wrapper structs plus a
  sixteen-variant composer). `Definition.Runner` calls the constructor
  instead of building a bare `stepTool{...}` literal.
- Test coverage proving a `SchemaTool`-implementing internal Kind
  drives end to end through a real `Runner.Run`, and proving a decode
  failure surfaces a clean, `errors.Is`-testable error, not a panic or
  an opaque `subagent: bad arguments` string.
- A short addendum to `docs/plans/runconfig.md`, added once the fix
  and its tests pass `make verify`, stating that the file-tool Kinds
  now drive through `Runner.Run`. A matching short note in
  `docs/plans/agents/phase72_runconfig_blocks.md`, appended after its
  existing content, records that this phase closed the gap; phase 72's
  own approved design and its existing prose stay unchanged.
- Simplifying or replacing the two workaround tests named above, once
  the real path works, so their comments no longer claim a limitation
  that no longer exists. Recommendation: replace
  `TestRunnerResolvesWorkspaceReadReal`'s direct-invoke body with a
  real `runner.Run(...)` call over a JSON-string-encoded payload
  matching `WorkspaceReadArgs`, and drop `AsToolKind`'s stand-in role
  in `TestBudgetGatesDeclaredPayloadSize` in favor of `WorkspaceReadKind`
  once the schema-decode path exists to drive it; the earlier
  `AsToolKind` version stays useful as a witness that a Kind needing no
  decode still runs unchanged, so keep it as a second subtest instead
  of deleting it.
- A new white-box test file `runconfig/steptool_internal_test.go`, in
  `package runconfig` (not the external `runconfig_test` package
  every other `runconfig` test file uses today). It calls the
  unexported `newStepTool` constructor directly to prove the
  capability-forwarding rule from `runconfig/steptool.go`'s doc
  comment. `newStepTool` stays unexported on purpose, unlike
  `spool.SpoolTool`, so `runconfig` has no black-box path to reach the
  constructor; this test file is the only way in.

### Out of scope

- `tools/registry.go`'s `Run` and `RunScoped`. They stay
  payload-shape-agnostic; the decode decision belongs to `chain`,
  agentrun's own caller of `RunScoped`, because only `agentrun` knows
  its payload source is a JSON document's string field, not an
  untrusted model's tool-call arguments.
- `agentloop/toolcall.go`'s `decodeAndRun`. It already does its own
  schema decode, plus schema *validation* against a pre-compiled
  `*schema.Compiled` `agentloop.Loop` builds at construction, because
  its caller is an untrusted model. `agentrun`'s caller is a JSON
  document loaded by `runconfig.Load`, which the document owner
  controls; `agentrun` decodes but does not build or run a compiled
  schema validator. The two paths stay separate on purpose; this phase
  does not consolidate them.
- Changing `flow.Step.Payload`'s type or `runconfig/loader.go`'s wire
  shape. `wireStep.Payload` is `json:"payload"` typed as a Go `string`
  today; a document author who wants to bind a `SchemaTool` step
  writes the argument JSON as a string value, with its quotes escaped,
  for example `"payload": "{\"path\":\"notes.txt\"}"`. `json.Unmarshal`
  accepts that value into the existing `string` field unchanged, and
  the resulting `flow.Step.Payload`, `envelope.Message.Payload`, and
  `msg.Payload` string carry the exact argument JSON bytes
  `DecodeArguments` needs. No `runconfig` wire-shape change is needed
  for this phase's fix to work.
- Widening `tools.Scope`'s approval-threshold or privilege behavior
  itself. This phase only makes `stepTool` report the truth about
  `inner`'s published capabilities; it changes no gating rule.

## API

### `agentrun` package

`agentrun/options.go`, added to the existing sentinel-error `var`
block:

```go
// ErrArgumentDecode is chain's error when the resolved tool's
// DecodeArguments rejects the step's payload bytes. Test with
// errors.Is.
ErrArgumentDecode = errors.New("agentrun: tool arguments failed to decode")
```

`agentrun/wire.go`'s `chain`, current body:

```go
out, err := r.tools.RunScoped(ctx, name, tools.InOut{Value: msg.Payload}, r.scope)
```

replaced with:

```go
in := tools.InOut{Value: msg.Payload}
if t, ok := r.tools.Get(name); ok {
	if st, ok := t.(tools.SchemaTool); ok {
		decoded, derr := st.DecodeArguments([]byte(msg.Payload))
		if derr != nil {
			return envelope.Ack{}, fmt.Errorf("agentrun: step %q: %w: %w", msg.ID, ErrArgumentDecode, derr)
		}
		in = decoded
	}
}
out, err := r.tools.RunScoped(ctx, name, in, r.scope)
```

`chain`'s doc comment gains one sentence stating the new branch: a
resolved tool implementing `tools.SchemaTool` decodes the step's
payload bytes through `DecodeArguments` before the tool runs; every
other tool keeps the plain-string payload unchanged. No other
`agentrun` exported symbol changes.

### `runconfig` package

`runconfig/runner.go`'s `stepTool` type and its two methods move,
unchanged, into a new `runconfig/steptool.go`. `runconfig/runner.go`'s
call site changes from a bare struct literal to a constructor call:

```go
// before
if err := reg.Add(stepTool{step: b.Step, inner: inner}); err != nil {

// after
if err := reg.Add(newStepTool(b.Step, inner)); err != nil {
```

`runconfig/steptool.go` adds the unexported constructor and the
capability-wrapper types, mirroring `spool/tool.go`'s `SpoolTool` and
`buildSpoolTool`:

```go
// newStepTool adapts inner to its step name and forwards exactly the
// optional tools.Tool interfaces inner implements: tools.SchemaTool,
// tools.ProfiledTool, tools.ResultBudgetTool, and tools.PrivilegedTool.
// A caller-set tools.Scope approval threshold, or a privileged inner
// tool, reads inner's own published capability, not a stripped
// default, once the wrapped tool sits in the registry chain drives.
func newStepTool(step string, inner tools.Tool) tools.Tool
```

No new exported symbol lands in `runconfig`; `stepTool` and
`newStepTool` stay unexported, matching today's shape. `make
api-update` runs after the change lands; `api/agentrun.txt` and
`api/runconfig.txt` are expected to show no diff beyond comment text,
since no exported symbol's name or signature changes.

### `policy/layers.json`

No new row and no new edge. `agentrun` already imports `tools`;
`runconfig` already imports `tools` and `agentrun`. Both edges already
exist in `policy/layers.json`.

## Tests

- `agentrun/agentrun_test/`: a new case proving `chain` decodes a
  `SchemaTool` tool's payload before calling `RunScoped`. Build a
  minimal stub tool implementing `tools.SchemaTool` whose
  `DecodeArguments` parses a small JSON object and whose `Run` asserts
  the decoded typed value, not a string; drive one gated step through
  `Runner.Run` with a JSON-string payload and assert the decoded value
  reached `Run`.
- `agentrun/agentrun_test/`: a negative case with a malformed-JSON
  payload for the same stub `SchemaTool`, asserting `Runner.Run`
  returns an error satisfying `errors.Is(err, agentrun.ErrArgumentDecode)`,
  not a panic and not the tool's own opaque bad-arguments error.
- `agentrun/agentrun_test/`: confirm an existing non-`SchemaTool` stub
  tool still receives the plain string payload unchanged; this proves
  the new branch never fires for a tool that does not implement
  `SchemaTool`.
- `runconfig/runconfig_test/runner_test.go`: replace
  `TestRunnerResolvesWorkspaceReadReal`'s direct `DecodeArguments`-then-
  `Run` body with a real `runner.Run(...)` call, the step's JSON
  document payload set to the JSON-string-encoded `WorkspaceReadArgs`
  form (`"payload": "{\"path\":\"notes.txt\"}"`), asserting the run
  reaches status `done` and the stored artifact equals the seeded file
  content. Update the test's doc comment to drop the "cannot drive a
  full `Runner.Run`" claim.
- `runconfig/runconfig_test/load_integration_test.go`:
  `TestBudgetGatesDeclaredPayloadSize`'s "kind binding composes with a
  permissive budget" subtest keeps its `AsToolKind` case as a witness
  that a no-decode Kind still runs unchanged, and gains a second
  subtest using `WorkspaceReadKind` with a real `FileTools`, over the
  same permissive budget, proving the budget check and the schema
  decode compose without conflict. Update the subtest's doc comment to
  drop the "WorkspaceReadKind cannot drive a full Runner.Run" claim.
- `runconfig/steptool_internal_test.go` (new file, `package
  runconfig`): a case proving `newStepTool` forwards
  `tools.SchemaTool`, `tools.ProfiledTool`, `tools.ResultBudgetTool`,
  and `tools.PrivilegedTool` exactly when `inner` implements them.
  Build a stub tool implementing all four, call `newStepTool(step,
  inner)` directly, and assert all four type assertions succeed on the
  returned value. Build a second stub implementing none of the four,
  call `newStepTool` on it, and assert all four type assertions fail
  on its returned value. This test needs no `Definition.Runner` call
  and no registry fetch-back; it exercises the constructor in
  isolation. `spool`'s capability-forwarding tests instead go through
  the exported `spool.SpoolTool` constructor, a black-box path;
  `runconfig` keeps `newStepTool` unexported and has no equivalent
  black-box path, so this in-package test is the only way to reach
  the constructor.
- `runconfig/runconfig_test/`: a case proving a `Scope` approval
  threshold set on `agentrun.Options.Scope`, when the run reaches a
  step bound to an internal Kind whose tool publishes
  `ExecutionClassWrite` through `ProfiledTool`, actually fires the
  scope's `approve` callback. This pins the `ExecutionProfileOf`
  forwarding fix at the level a real caller would notice.

## Verification

- `make verify-fast` and `make verify` both pass.
- `python3 scripts/check_api.py` passes after `make api-update`; the
  `api/agentrun.txt` and `api/runconfig.txt` diffs, if any, are
  reviewed and committed in the same change.
- `python3 scripts/check_deps.py` passes with no `policy/layers.json`
  change needed.
- `python3 scripts/check_plan.py` passes; `docs/plans/agentrun.md` and
  `docs/plans/runconfig.md` already exist and keep every required
  section.
- `python3 scripts/check_structure.py` passes; `runconfig/steptool.go`
  stays at or below 500 lines and every function at or below 80 lines,
  following `spool/tool.go`'s file layout as a size precedent.
- `go test -race ./agentrun/... ./runconfig/... ./subagent/...` passes.
- Coverage for `agentrun` and `runconfig` stays at or above the 85
  floor after the new tests land.
- `python3 scripts/check_labels.py` and `python3 scripts/check_prose.py`
  pass against this plan file and against the addenda this phase adds
  to `docs/plans/runconfig.md` and
  `docs/plans/agents/phase72_runconfig_blocks.md`.
