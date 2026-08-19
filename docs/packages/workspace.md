# Package reference: workspace

`workspace` confines all filesystem access to one root directory, so
a tool or agent that reads and writes files cannot escape its
sandbox through traversal or a symlink. It is a leaf package. The
exported surface below mirrors `api/workspace.txt`.

## Types

- `Workspace` — a handle bound to one resolved root directory. Fields
  are unexported; build one with `Open`.

## Functions and methods

- `Open(root string) (*Workspace, error)` — resolves `root` to an
  absolute, symlink-free real path and returns a handle bound to it.
  `root` must exist and be a directory.
- `Workspace.Root() string` — returns the resolved root path.
- `Workspace.ReadFile(path string) ([]byte, error)` — reads a file
  named relative to the root. Unbounded by design; a caller that
  needs a byte cap applies its own after the read, the division of
  labor `contextbudget` already uses for a model call's context.
- `Workspace.WriteFile(path string, data []byte, perm os.FileMode) error`
  — writes a file relative to the root, creating missing parent
  directories under the root as needed.
- `Workspace.List(path string) ([]os.DirEntry, error)` — lists a
  directory relative to the root.
- `Workspace.Stat(path string) (os.FileInfo, error)` — stats a path
  relative to the root.

## Invariants

- Every method confines `path`: join it onto the root, clean the
  result, and reject it when the cleaned path is not the root or
  under it. This rejects `..` traversal and an absolute path outside
  the root.
- Before use, every method resolves any symlink components in the
  joined path and rejects the result if it escapes the root. It
  resolves the deepest existing ancestor when the final component
  does not yet exist, the `WriteFile` case, and it resolves a
  symlink at the final path component itself, not only an
  intermediate one.

## Failure modes

Use `errors.Is` to test the escape case.

- `ErrEscape` ("workspace: path escapes root") — every one of
  `ReadFile`, `WriteFile`, `List`, and `Stat` returns it, wrapped
  with the offending relative path, when `path` fails the
  confinement check through lexical traversal or a symlink. Pinned
  by `workspace/workspace_test/workspace_test.go`.

## Cross-references

None. `workspace` declares no internal import edge and has no caller
inside this module yet.

## Wire contract

`workspace` reads and writes plain files; it defines no wire format
and carries no conformance vector.

## Usage

```go
package main

import (
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

func main() {
    w, err := workspace.Open(".")
    if err != nil {
        panic(err)
    }

    if err := w.WriteFile("notes/plan.txt", []byte("plan the release\n"), 0o600); err != nil {
        panic(err)
    }

    data, err := w.ReadFile("notes/plan.txt")
    if err != nil {
        panic(err)
    }
    fmt.Print(string(data))
}
```

### What the program shows

`Open` binds a handle to the current directory. `WriteFile` creates
the missing `notes` directory under the root and writes the file.
`ReadFile` reads it back. A path like `../outside` or a symlink
pointing outside the root would fail both calls with `ErrEscape`.
