# Package reference: toolcallctx

The toolcallctx package carries one `provider.ToolCall` value through a
`context.Context`. It is a leaf: its only imports are `context` and
`provider`. The exported surface below mirrors `api/toolcallctx.txt`.

## Functions

- `WithToolCall(ctx, call)` — returns a child context carrying `call`.
  A second `WithToolCall` call on the returned context replaces the
  carried value for any lookup downstream of that second call.
- `ToolCallFromContext(ctx)` — extracts the `provider.ToolCall`
  attached by `WithToolCall`. Returns `provider.ToolCall{}, false` for
  a nil `ctx`, and for a non-nil `ctx` carrying no attached call.

## Invariants

- `ToolCallFromContext` never panics on a nil `ctx`; it checks for nil
  before it reads any value.
- The attached value round-trips unchanged: `ToolCallFromContext`
  returns the exact `provider.ToolCall` passed to the matching
  `WithToolCall` call, field for field.
- The package defines its context key as an unexported empty struct
  type, so no caller outside `toolcallctx` can collide with it or read
  the value through a raw `ctx.Value` call.

## Cross-references

- [provider.md](provider.md) — `toolcallctx` carries a
  `provider.ToolCall` value; it names no other type from `provider`.
- [agentloop.md](agentloop.md) — the only consumer in this module.
  `agentloop.Loop`'s `runOneToolCall` calls `WithToolCall` to attach the
  in-flight call to the context passed to hooks and event emission for
  that call, so a hook or subscriber can read which tool call is
  running through `ToolCallFromContext`.
- [../architecture.md](../architecture.md) — the dependency diagram
  places `toolcallctx` as a leaf under `agentloop`, importing only
  `provider`.

## Usage

```go
ctx := toolcallctx.WithToolCall(parent, provider.ToolCall{
    ID:        "call_1",
    Name:      "search",
    Arguments: []byte(`{"query":"weather"}`),
})

call, ok := toolcallctx.ToolCallFromContext(ctx)
if !ok {
    // ctx was nil, or carried no attached call
}
// call.Name == "search"
```
