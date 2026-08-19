# Package reference: spool

The spool package stores oversized content under a principal-scoped
grant and hands the caller a bounded view plus a reference. `SpoolTool`
gives any `tools.Tool` this behavior. The exported surface below
mirrors `api/spool.txt`.

## Types

- `ContentStore` — the storage interface `Spool` writes to and reads
  from: `Put(content []byte) (ref string, err error)` and
  `Get(ref string) ([]byte, error)`. `memory.Store` satisfies it with
  no import needed on either side.
- `Spool` — a grant store keyed by reference and principal. Safe for
  concurrent use. The zero value is not usable; create one with
  `NewSpool`.

## Functions and methods

- `NewSpool(store, maxGrantBytes)` — creates a `Spool` backed by
  `store`, tracking grants under a `maxGrantBytes` budget.
- `Spool.Spool(ctx, principal, data)` — writes `data` to `store`,
  grants `principal` the right to read it back, and returns a bounded
  view of `data` plus its reference.
- `Spool.Load(ctx, principal, ref)` — returns the full bytes stored
  under `ref` for the granted `principal`.
- `WithPrincipal(ctx, principal)` — returns a context carrying
  `principal` for a later `SpoolTool` call to read.
- `PrincipalFrom(ctx)` — reads the principal `WithPrincipal` attached
  to `ctx`.
- `SpoolTool(name, maxBytes, store, inner)` — wraps `inner` so a
  string result over `maxBytes` spools to `store` under the `ctx`
  principal instead of returning in full.

## Failure modes

Use `errors.Is` to test these.

- `ErrUnknownRef` ("spool: unknown ref") — `Load` wraps it when no
  live grant matches `ref`, and also when a live grant's
  `ContentStore.Get` fails: a live grant whose bytes are gone reads as
  an unknown ref.
- `ErrWrongPrincipal` ("spool: wrong principal") — `Load` wraps it
  when `principal` does not match the grant's recorded principal.
- `ErrNoPrincipal` ("spool: no principal in context") — `SpoolTool`'s
  wrapped `Run` returns it when `ctx` carries no principal and the
  inner result needs a grant.
- `ErrNoBudget` ("spool: maxGrantBytes must be positive") — `NewSpool`
  wraps it for a non-positive `maxGrantBytes`.
- `ErrGrantTooLarge` ("spool: content exceeds grant budget") —
  `Spool.Spool` wraps it when `data` alone is longer than
  `maxGrantBytes`: no eviction could ever make room for it, so `Spool`
  rejects the call before writing to `store`.
- `ErrPrincipalConflict` ("spool: ref already granted to a different
  principal") — `Spool.Spool` wraps it when `store` returns a `ref`
  that already has a live grant for a different principal. A
  content-addressed `ContentStore` returns the same `ref` for
  identical bytes regardless of caller; without this check, a second
  principal spooling the same content would silently take over the
  first principal's grant.

## Invariants

- `NewSpool` rejects a non-positive `maxGrantBytes` with `ErrNoBudget`.
- `Spool.Spool` rejects `data` longer than `maxGrantBytes` with
  `ErrGrantTooLarge`, before it calls `store.Put`.
- `Spool.Spool` rejects a `ref` collision across principals with
  `ErrPrincipalConflict`; the earlier grant is left unchanged.
- `Spool.Spool` evicts the oldest grants, by insertion order, until
  the new grant fits `maxGrantBytes`. A dropped grant's `Load` fails,
  even when the underlying `ContentStore` still holds the bytes.
- `Spool.Load` checks the grant's principal before it reads from
  `store`, so a wrong principal never triggers a store read.
- `SpoolTool` always calls `inner.Run` first. A result that is not a
  string, or one at or under `maxBytes`, passes through unchanged with
  no store write. `inner`'s own error passes through unchanged, with
  no spooling attempted.
- `SpoolTool`'s returned `tools.Tool` implements `tools.ProfiledTool`,
  `tools.ResultBudgetTool`, `tools.PrivilegedTool`, and
  `tools.SchemaTool` only when `inner` itself does, forwarding each
  call straight to `inner`. `SpoolTool` changes only `Run`'s result
  handling, never `inner`'s declared execution class, result budget,
  privilege, or schema.

## Why this shape

`spool` declares `ContentStore` locally instead of importing `memory`,
so it stays a leaf a caller can back with any conforming store, not
only `memory.Store`. `WithPrincipal` and `PrincipalFrom` carry the
calling principal through `context.Context`, following the
`flow.LoopStateFrom` precedent, since `tools.Tool.Run`'s signature is
fixed and shared by every tool in this SDK. The ref's format is a
`ContentStore` implementation's own choice: `spool` does not guarantee
a `contextstate.ContentRef`-shaped string, though `memory.Store`'s
refs happen to be `envelope.ContextRef` values today. See
[../plans/spool.md](../plans/spool.md).

## Cross-references

- [tools.md](tools.md) — `SpoolTool` wraps a `tools.Tool` and forwards
  its optional `ProfiledTool`, `ResultBudgetTool`, `PrivilegedTool`,
  and `SchemaTool` interfaces. Stripping `SchemaTool` would make a
  wrapped tool unreachable to an `agentloop.Loop`'s model, since
  `agentloop.Definitions` skips a tool with no published schema.
- [memory.md](memory.md) — `memory.Store` satisfies `ContentStore`
  with no import needed on either side.

## Usage

```go
store, _ := memory.New(1 << 20)
sp, err := spool.NewSpool(store, 1<<20)
if err != nil {
    // maxGrantBytes was zero or negative
}

ctx := context.Background()
view, ref, err := sp.Spool(ctx, "agent-a", []byte("oversized content..."))
// view is a bounded, human-readable preview; ref names the full blob

full, err := sp.Load(ctx, "agent-a", ref)
// full == the original bytes

_, err = sp.Load(ctx, "agent-b", ref)
// err is spool.ErrWrongPrincipal

wrapped := spool.SpoolTool("big-tool", 4096, store, myTool)
ctx = spool.WithPrincipal(ctx, "agent-a")
out, err := wrapped.Run(ctx, in)
// out.Value truncates and names a ref when myTool's result exceeds 4096 bytes
```
