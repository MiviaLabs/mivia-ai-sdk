# Phase 14: tools registry

Status: future. Builds the tools block. A tool is a named action the
agent can call. The registry holds the tools and resolves a name to an
action. This phase owns the registry and the execution.

## Goal

Define and look up tools by name. A step names a tool by string. The
registry resolves the name and runs the action. An unknown name fails.

## Scope

Inside: the `Tool` interface, the `Registry`, and the execution.
Outside: the agent binding and the memory store. The agent binds the
registry in this phase. The memory bind follows in phase 15.

## API

- `type Tool interface { Name() string; Run(ctx context.Context, in InOut) (Out, error) }`
- `type Registry struct` holding the tools by name.
- `New() *Registry`
- `(*Registry).Add(t Tool) error`
- `(*Registry).Get(name string) (Tool, bool)`
- `(*Registry).Run(ctx context.Context, name string, in InOut) (Out, error)`

`Add` rejects a duplicate name. `Get` returns false for an unknown
name. `Run` wraps `Get` and the action. A tool never sees the agent.
The interface earns its place because tools are many and replaceable.

## Tests

Test files live in `tools/tools_test/`:

- `registry_test.go` — the red-green cases for `Add`, `Get`, and
  `Run`. Start with the assertions. Confirm they fail on the empty
  phase. Implement and watch them pass.
- `registry_integration_test.go` — register two tools, resolve them by
  name, and run one. Prove a duplicate fails add. Prove an unknown
  name fails run.
- `registry_bench_test.go` — benchmark `Run` on a registry of one
  hundred tools. Target under one microsecond. State the allocation
  budget.

## Verification

`make verify` passes. The coverage floor for `tools` holds. The tools
package declares its imports in `policy/layers.json`. `api/tools.txt`
lands via `make api-update`.
