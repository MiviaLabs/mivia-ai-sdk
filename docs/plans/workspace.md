# workspace plan

## Goal

`workspace` confines all filesystem access to one root directory, so
a tool or agent that reads and writes files cannot escape its
sandbox through traversal or a symlink.

## Scope

Inside:

- `Open(root string) (*Workspace, error)`: resolves `root` to an
  absolute, symlink-free real path and returns a handle bound to it.
  `root` must exist and be a directory.
- `(*Workspace) Root() string`: returns the resolved root path.
- `(*Workspace) ReadFile(path string) ([]byte, error)`: reads a file
  named relative to the root.
- `(*Workspace) WriteFile(path string, data []byte, perm os.FileMode) error`:
  writes a file relative to the root, creating parent directories
  under the root as needed.
- `(*Workspace) List(path string) ([]os.DirEntry, error)`: lists a
  directory relative to the root.
- `(*Workspace) Stat(path string) (os.FileInfo, error)`: stats a path
  relative to the root.
- Confinement check, shared by all four methods: join `path` onto the
  root, clean the result, and reject it if the cleaned path is not
  the root or under it (rejects `..` traversal). Before use, resolve
  any symlink components in the joined path and reject the result if
  the resolved path escapes the root (rejects symlink indirection),
  using `filepath.EvalSymlinks` on the deepest existing ancestor when
  the final component does not yet exist (the `WriteFile` case).
- Sentinel: `ErrEscape`, returned wrapped with the offending relative
  path whenever a path fails the confinement check.
- File layout, to stay under the 500-line file cap: `workspace.go`
  holds the `Workspace` type, `Open`, and `Root`; `confine.go` holds
  the shared path-resolution and symlink-check logic every method
  calls; `read.go` and `write.go` each hold one of `ReadFile` and
  `WriteFile`; `list.go` and `stat.go` each hold one of `List` and
  `Stat`.
- `ReadFile` has no size bound; it is unbounded by design. A caller
  that needs a byte cap pairs it with a check of its own, the same
  division of labor `contextbudget` already uses for a model call's
  context.

Outside:

- Process execution, git operations, or archive handling, per the
  phase doc.
- Any caching, locking, or concurrent-write coordination beyond what
  the OS filesystem already gives.
- Path patterns or secret detection; that composes in through
  `secretpath` at the call site, not inside this package.

## API

- `var ErrEscape = errors.New("workspace: path escapes root")`
- `type Workspace struct { ... }` (fields unexported)
- `func Open(root string) (*Workspace, error)`
- `func (w *Workspace) Root() string`
- `func (w *Workspace) ReadFile(path string) ([]byte, error)`
- `func (w *Workspace) WriteFile(path string, data []byte, perm os.FileMode) error`
- `func (w *Workspace) List(path string) ([]os.DirEntry, error)`
- `func (w *Workspace) Stat(path string) (os.FileInfo, error)`

## Tests

- `Open` on a missing root, on a file instead of a directory, and on
  a valid directory (the happy path, asserting `Root()` returns the
  resolved absolute path).
- Traversal-escape vectors for each of the four methods: `../secret`,
  `a/../../secret`, and an absolute path outside the root; each
  returns an error wrapping `ErrEscape`, checked with `errors.Is`.
- Symlink-escape vectors: a symlink inside the root pointing outside
  it, exercised through each of `ReadFile`, `WriteFile`, `List`, and
  `Stat`; a symlink pointing to an absolute path outside the root as
  an intermediate directory component, exercised through `ReadFile`,
  `List`, and `Stat` at minimum; a symlink pointing outside the root
  as the final path component itself, not just an intermediate one,
  exercised through `ReadFile`, `List`, and `Stat` at minimum; all
  fail closed with `ErrEscape`.
- Happy-path round trip: `WriteFile` then `ReadFile` the same relative
  path returns the written bytes; `List` on a directory with two
  files returns both entries; `Stat` on a written file reports its
  size.
- `WriteFile` creates a missing parent directory under the root and
  still rejects a parent path that would escape the root.

## Verification

- `make verify` passes: gofmt, vet, tests, doc gate, structure gate,
  Semgrep scan, and probes.
- Coverage for `workspace` reaches the 85 percent floor.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass against this plan and the new `policy/layers.json` row.
- One e2e case may compose `workspace` confinement behind an MCP
  tool; optional, not gating, per the phase doc.
