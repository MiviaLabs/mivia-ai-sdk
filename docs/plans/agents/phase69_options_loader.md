# Phase 69: declarative options loader

Status: plan only, not scheduled. One new composition package. It
depends on the shipped `agentrun` and `subagent` surfaces and on no
unshipped phase. It makes no third-party import request.

## Why this phase exists

Every composition in this SDK is code. A caller builds
`agentrun.Options` field by field in Go. That is right for libraries
and tests, and wrong for deployments: an operator cannot define a
runner without recompiling.

The sibling repo solved deployment the other way.
`mivia-agent`'s workflow files are TOML documents compiled into
runs. Its `internal/config` resolves provider settings, worktrees,
and stacking from the same documents. The engine reads definitions;
nobody recompiles to change a workflow.

The gap between the two is the loader. The SDK owns the types a
definition must resolve into: `agentrun.Options`, the tool set, the
machine rows, the plan steps. A JSON loader mapping declarative data
onto those types gives every deployment the sibling repo's shape
without the SDK taking a TOML dependency.

The encoding decision is deliberate. `encoding/json` is stdlib. TOML
needs a third-party exception or a hand-rolled parser. A sibling
consumer keeps its TOML front end and translates to this JSON form;
the SDK stays stdlib-only.

## Goal

One package loads a JSON document into a validated `agentrun`
runner and its tool set, so a deployment defines runners as data.

## Scope

Inside:

- A `runconfig` package importing `agentrun`, `flow`, `machine`,
  `subagent`, and `tools`. `policy/layers.json` gains exactly that
  row.
- A JSON schema for the document: the machine's rows, the plan's
  steps and panels, the options, and the tool set's names.
- `Load(data []byte)` resolving into a validated structure; the
  structure builds a `*agentrun.Runner` through the existing
  `New`.
- Every validation the typed constructors enforce runs again on the
  loaded form: unknown tool names, duplicate step IDs, bad
  transitions, admission rules. `flow.New`, `machine.New`, and
  `agentrun.New` stay the only validators; the loader feeds them.
- Internal tool references by name: `flow`, `ledger`, `memory`,
  `room`, `scheduler`, `heartbeat`, `discovery`, `trigger`,
  `channel`, `provider`, and `providerregistry`. The loader wires
  the named internal tool onto its step.

Outside:

- TOML parsing. A caller translates.
- Any provider client construction. A provider entry names a
  registered completer; registration stays code.
- Secrets. `envfile` and `secretpath` from phase 68 compose at the
  caller; the loader never reads the environment.
- Mivia's workflow semantics: loops with named identities, repair
  budgets, delivery policy. The loader maps what `flow.Step`
  already expresses; richer semantics stay app-side translations.
- `dispatch` endpoint configuration and HTTP wiring.

## API

- `func Load(data []byte) (*Definition, error)`
- `type Definition` holding the resolved machine, plan, options,
  and tool set, all exported for inspection.
- `func (d *Definition) Runner() (*agentrun.Runner, error)`
- Sentinels: `ErrUnknownTool`, `ErrUnknownInternal`,
  `ErrBadDocument`.

## Tests

- Table-driven over the document grammar: every step field, panel,
  machine row, and option round-trips into the built runner.
- Every rejection: unknown tool, duplicate step, missing row,
  invalid admission, malformed JSON.
- A golden document loads into a runner whose `Run` completes,
  proving the whole path.
- The subagent wiring: a document naming `subagent` tools builds
  spawns.

## Verification

- `make verify` passes; `policy/layers.json` gains the row.
- One e2e case loads a golden document and runs it end to end.
- `docs/plans/runconfig.md` lands with the code.
