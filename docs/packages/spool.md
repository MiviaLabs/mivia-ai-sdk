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
- `SpoolTool(name, maxBytes, sp, inner)` — wraps `inner` so a string
  result over `maxBytes` spools through `sp` under the `ctx`
  principal instead of returning in full. A nil `sp` wraps
  `ErrNilSpool`. Two `SpoolTool` calls sharing one `sp` share its
  grant budget.

## Read-back tool and grant expiry

- `SpoolExpiring(ctx, principal, data, ttl)` — writes data, grants
  principal read-back, and sets a time-to-live on the grant. A
  non-positive ttl wraps `ErrInvalidExpiry` before any store write.
  Re-spooling an existing ref under the same principal refreshes the
  expiry.
- `Expire(ref)` — marks one live grant expired immediately. An unknown
  ref wraps `ErrUnknownRef`.
- `GrantExpiry(ref)` — reports a grant's expiry: false for no live
  grant, the zero time for a grant from plain `Spool`.
- `Load` checks in this order: an unknown ref wraps `ErrUnknownRef`; a
  principal mismatch wraps `ErrWrongPrincipal`, even on an expired
  grant, so a probing principal learns nothing about the expiry; an
  expired grant under the right principal wraps `ErrExpired`, and the
  grant drops on that Load, freeing its budget. Expiry is lazy: no
  wall-clock sweep runs.
- `ReadOutputTool(sp, maxPageBytes)` — the model-facing read-back
  tool, bound to one `*Spool` at construction. A nil sp wraps
  `ErrNilSpool`; a non-positive page budget wraps `ErrInvalidLimit`.
  The tool implements `tools.SchemaTool`: it publishes one parameter
  schema for ref, offset, and limit, and decodes its own arguments.
  `Run` reads the ctx principal through `PrincipalFrom` and fails
  `ErrNoPrincipal` when absent. A limit of zero means one full page of
  `maxPageBytes` bytes; a limit over the bound clamps to it. The page
  carries `MoreMarker` with the next offset when bytes remain, and
  nothing extra on the final page. An offset past the end returns an
  empty page. `ErrBadArguments` covers a malformed decode, a mistyped
  `Run` call, a negative offset, and a negative limit.
- `MoreMarker` — `"[more: offset=%d]"`, the suffix naming the next
  page's offset.

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

wrapped, err := spool.SpoolTool("big-tool", 4096, sp, myTool)
if err != nil {
    // sp was nil
}

readBack, err := spool.ReadOutputTool(sp, 2048)
if err != nil {
    // sp was nil, or maxPageBytes was non-positive
}

registry := tools.New()
registry.Add(wrapped)
registry.Add(readBack)

ctx := spool.WithPrincipal(context.Background(), "agent-a")
out, err := wrapped.Run(ctx, in)
// out.Value truncates and appends a ref when myTool's result exceeds
// 4096 bytes. A model reads that ref from the text and calls
// read_spooled_output with it to page the full body back.

argsJSON := []byte(`{"ref":"the-ref-from-out.Value"}`)
args, _ := readBack.(tools.SchemaTool).DecodeArguments(argsJSON)
page, err := readBack.Run(ctx, args)
// page returns one bounded page of the body wrapped spooled
```

`SpoolTool` and `ReadOutputTool` share `sp`, so `readBack` resolves a
ref that `wrapped` spooled. A second `SpoolTool` call built against
the same `sp` shares its grant budget with `wrapped`.
