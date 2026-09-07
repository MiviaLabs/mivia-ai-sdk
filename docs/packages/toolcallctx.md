> Status: relocated. Phase 86 folded this surface into `agentloop`
> (`toolcall_context.go`, `toolcall_batch.go`) as its sole producer,
> but external callers still consume it: any external `tools.Tool`
> wrapper reads the `provider.ToolCall` and `BatchOrder` that
> `agentloop.Loop` attaches to the `ctx` it passes into `Tool.Run`
> during dispatch. The types stayed exported for that reason. This
> page stays as reference, with names updated to their `agentloop`
> home; the exported surface below mirrors `api/agentloop.txt`.

# Package reference: toolcallctx

The toolcallctx surface carries tool execution state through a
`context.Context`. It attaches individual `provider.ToolCall` values and
per-batch `BatchOrder` dispatch ledgers.

## Types

- `BatchOrder` — the per-turn dispatch ledger an agent loop publishes to
  the tools it runs. Thread-safe.

## Functions and methods

- `NewBatchOrder(dispatched)` — builds a `*BatchOrder` for one batch.
  Copies and sorts `dispatched`.
- `WithBatchOrder(ctx, order)` — returns a child context carrying `order`.
- `BatchOrderFromContext(ctx)` — extracts `*BatchOrder` attached by
  `WithBatchOrder`. Returns `nil, false` for a nil `ctx` or when absent.
- `WithToolCall(ctx, call)` — returns a child context carrying `call`.
  A second `WithToolCall` call on the returned context replaces the
  carried value for downstream lookups.
- `ToolCallFromContext(ctx)` — extracts the `provider.ToolCall`
  attached by `WithToolCall`. Returns `provider.ToolCall{}, false` for
  a nil `ctx` or when absent.
- `(*BatchOrder) Dispatched()` — returns a sorted copy of dispatched
  call indices.
- `(*BatchOrder) Settle(index)` — marks `index` finished. Idempotent.
  Wakes current `Changed()` waiters.
- `(*BatchOrder) Settled(index)` — reports whether `index` has settled.
- `(*BatchOrder) Changed()` — returns a channel closed on the next
  settlement after the call.
- `(*BatchOrder) UnsettledBefore(limit)` — reports whether any
  dispatched index below `limit` has not settled.

## Invariants

- `ToolCallFromContext` and `BatchOrderFromContext` never panic on a
  nil `ctx`. They return false when `ctx` is nil.
- The attached values round-trip unchanged.
- `BatchOrder` methods are safe for concurrent use; a mutex guards
  internal state.
- `Settle` is idempotent: subsequent calls for the same index are no-ops
  and do not rotate the changed channel.
- `Changed` wakes current waiters once per settlement by rotating
  channels.
- The package defines unexported empty struct keys so keys cannot
  collide.

## Cross-references

- [provider.md](provider.md) — `toolcallctx` carries a
  `provider.ToolCall` value.
- [agentloop.md](agentloop.md) — `agentloop.Loop` publishes `BatchOrder`
  and attaches `provider.ToolCall` during tool dispatch.
- [../architecture.md](../architecture.md) — `toolcallctx` is a leaf
  under `agentloop`.

## Usage

```go
ctx := agentloop.WithToolCall(parent, provider.ToolCall{
    ID:        "call_1",
    Name:      "search",
    Arguments: []byte(`{"query":"weather"}`),
})

call, ok := agentloop.ToolCallFromContext(ctx)
if !ok {
    // ctx was nil, or carried no attached call
}
// call.Name == "search"

order := agentloop.NewBatchOrder([]int{0, 1})
ctxWithOrder := agentloop.WithBatchOrder(ctx, order)
orderFromCtx, ok := agentloop.BatchOrderFromContext(ctxWithOrder)
if ok {
    orderFromCtx.Settle(0)
}
```
