# Phase 68: file and text hygiene leaves

Status: plan only, not scheduled. Four small leaf packages, each a
straight port from the sibling repo. Each depends on nothing in this
module and builds independently of every other phase.

## Why this phase exists

Four packages in `mivia-agent` are small, dependency-free, and
generic. Nothing in them is mivia-specific. Every one serves any
caller that touches files or truncates text.

- `internal/envfile` loads KEY=VALUE dotenv files without printing
  secret values, about three hundred lines.
- `internal/secretpath` matches configured secret path patterns,
  about two hundred lines.
- `internal/workspace` confines filesystem access to a root
  directory, about eleven hundred lines.
- `internal/diff` produces bounded line diffs for tool output,
  about eight hundred lines.

This SDK has no filesystem story at all. `mcp` and `tools` callers
reach the filesystem with no confinement. Tool output that shows
file content has no bounded diff. Configuration loading that reads
secrets has no hygiene helpers.

## Goal

Four leaves give the SDK dotenv loading, secret-path matching,
filesystem confinement, and bounded diffs.

## Scope

Inside, one package per concern:

- `envfile`: `Load(path)` into a map, parsing quoted and unquoted
  values, never including values in errors.
- `secretpath`: `Matcher` over configured patterns, `Matches(path)`
  reporting whether a path is secret.
- `workspace`: `Open(root)` returning a confined handle; `ReadFile`,
  `WriteFile`, `List`, and `Stat` that reject paths escaping the
  root through symlink or traversal.
- `diff`: `Unified(a, b, maxLines)` returning a bounded line diff,
  failing closed when the input exceeds the bound.

Outside:

- Redaction policy. `mivia-agent`'s `internal/redact` stays
  app-side; its patterns are workspace-specific.
- Any process execution, git operations, or archive handling.
- Mivia's worktree manager; confinement is the only piece that
  ports.

## API

- `envfile.Load(path string) (map[string]string, error)`
- `secretpath.NewMatcher(patterns []string) (*Matcher, error)` with
  `Matches(path string) bool`
- `workspace.Open(root string) (*Workspace, error)` with the four
  file methods and `Root() string`
- `diff.Unified(name string, a, b []byte, maxLines int) (string, error)`
- Sentinels: `workspace.ErrEscape`, `diff.ErrTooLarge`.

## Tests

- Traversal and symlink escapes fail closed; each vector from the
  source repo's suite ports.
- Dotenv parsing covers quoting, comments, and blank lines; error
  text never contains values.
- Diffs stay inside the line bound and fail closed beyond it.
- Pattern matching covers globs, directories, and negation.

## Verification

- `make verify` passes; four empty rows land in
  `policy/layers.json`.
- One e2e case may compose `workspace` confinement behind an MCP
  tool; it is optional, not gating.
- One `docs/plans/<pkg>.md` per package lands with the code.
