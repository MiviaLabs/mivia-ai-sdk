# Package reference: workspace

`workspace` confines all filesystem access to one root directory, so
a tool or agent that reads and writes files cannot escape its
sandbox through traversal or a symlink. It is a leaf package. The
exported surface below mirrors `api/workspace.txt`.

## Types

- `Workspace` — a handle bound to one resolved root directory. It
  holds an open `os.Root`, so it owns a file descriptor. Fields are
  unexported; build one with `Open` or `OpenWith`, and release it with
  `Close`.
- `Options` — the open-time configuration: `Root string` and
  `MaxReadBytes int64`. See `OpenWith`.

## Constants

- `DefaultMaxReadBytes int64 = 10 << 20` — the read bound a
  `Workspace` uses when the caller sets no `MaxReadBytes`.
- `Unbounded int64 = -1` — the explicit opt-out from the read bound.

## Functions and methods

- `Open(root string) (*Workspace, error)` — resolves `root` to an
  absolute, symlink-free real path, opens it with `os.OpenRoot`, and
  returns a handle bound to the open root. `root` must exist and be a
  directory. `Open` is `OpenWith(Options{Root: root})`, so its reads
  carry `DefaultMaxReadBytes`. Close the result.
- `OpenWith(opts Options) (*Workspace, error)` — the same open under
  the read bound `opts` names. It calls `opts.Validate` first and
  returns that error unchanged.
- `Options.Validate() error` — `Root` must not be blank.
  `MaxReadBytes` must be `Unbounded`, zero, or a positive value at or
  under `math.MaxInt64 - 1`.
- `Workspace.Root() string` — returns the resolved root path.
- `Workspace.Close() error` — closes the open root. `Close` is
  idempotent. Every method returns an error matching `fs.ErrClosed`
  after `Close`, except `ReadFileLimit` with an invalid limit, which
  returns `ErrInvalidLimit` before it touches the root. `Close` on a
  nil handle returns nil, so a deferred `Close` before the error check
  is safe.
- `Workspace.ReadFile(path string) ([]byte, error)` — reads a file
  named relative to the root, under the workspace's own read bound. It
  is `ReadFileLimit(path, 0)`.
- `Workspace.ReadFileLimit(path string, limit int64) ([]byte, error)`
  — the same read under a per-call bound. A zero `limit` uses the
  workspace's `MaxReadBytes`, a positive `limit` replaces it, up or
  down, and `Unbounded` removes it for this call only.
- `Workspace.WriteFile(path string, data []byte) error` — writes a
  file relative to the root, creating missing parent directories under
  the root as needed. It takes no `os.FileMode`.
- `Workspace.List(path string) ([]os.DirEntry, error)` — lists a
  directory relative to the root, sorted by filename.
- `Workspace.Stat(path string) (os.FileInfo, error)` — stats a path
  relative to the root.

## Invariants

- Every method confines `path`: join it onto the root, clean the
  result, and reject it when the cleaned path is not the root or
  under it. This rejects `..` traversal and an absolute path outside
  the root.
- Every symlink decision belongs to the open `os.Root`, at the
  syscall. The confinement check does no filesystem work, so no
  window exists between the check and the syscall for a concurrent
  rename or symlink swap to exploit. An absolute symlink is refused,
  even when its target lies inside the root.
- A read is bounded. `Options{}` yields `DefaultMaxReadBytes`, so an
  unset field bounds the read instead of removing the bound. Only
  `Unbounded` removes it.
- `WriteFile` creates a new file with mode `0o600` and a new parent
  directory with mode `0o700`. The mode applies at create only, so
  `WriteFile` does not tighten an existing file.
- A `Workspace` is not safe against a hostile root directory.
  `os.Root` does not prohibit traversal of filesystem boundaries,
  bind mounts, `/proc` special files, or device files.

## Failure modes

Use `errors.Is` to test the escape case.

- `ErrEscape` ("workspace: path escapes root") — every one of
  `ReadFile`, `WriteFile`, `List`, and `Stat` returns it, wrapped
  with the offending relative path, when `path` fails the
  confinement check through lexical traversal or a symlink. Pinned
  by `workspace/workspace_test/workspace_test.go`. A refusal that
  arrives while an attacker swaps a path component may report the raw
  syscall error instead. It still fails closed; only the error class
  is unlabelled.
- `ErrTooLarge` ("workspace: file exceeds read limit") — the file is
  longer than the read's effective bound. It wraps no filesystem
  error, because it is this package's own policy refusal.
- `ErrInvalidLimit` ("workspace: invalid read limit") — the bound is
  neither `Unbounded`, nor zero, nor a positive value at or under
  `math.MaxInt64 - 1`. `Options.Validate` and `ReadFileLimit` both
  return it. `ReadFileLimit` opens no file in that case.

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
    defer w.Close()

    if err := w.WriteFile("notes/plan.txt", []byte("plan the release\n")); err != nil {
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

`Open` binds a handle to the current directory and `Close` releases
its descriptor. `WriteFile` creates
the missing `notes` directory under the root and writes the file with
mode `0o600`. `ReadFile` reads it back under `DefaultMaxReadBytes`. A
path like `../outside` or a symlink pointing outside the root would
fail both calls with `ErrEscape`. A file over the bound would fail the
read with `ErrTooLarge`.
