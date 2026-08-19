# Package reference: mcp

The mcp package connects to a Model Context Protocol (MCP) server,
lists its tools, and calls them, through the official MCP Go SDK
(`github.com/modelcontextprotocol/go-sdk`). It maps each remote tool
onto a `tools.Tool` a `tools.Registry` already knows how to hold and
run. The exported surface below mirrors `api/mcp.txt`.

## Types

- `Transport` — a type alias for the official SDK's own `Transport`
  interface. `NewStdioTransport` and `NewStreamableHTTPTransport` each
  build one of the SDK's shipped implementations.
- `ClientInfo` — names the caller to the MCP server during the
  initialize handshake.
- `ProgressHandler` — receives one progress notification for a call
  this package's `Client` made: a token, a message, a progress value,
  and a total.
- `ClientOptions` — configures `Connect`: `Info` and an optional
  session-wide `OnProgress`.
- `Client` — one connection to one MCP server. The caller owns it and
  must call `Close`. Safe for concurrent `ListTools`, `CallTool`, and
  `CallToolWithProgress` calls.
- `ContentBlock` — one block of a tool call result: `Type`, `Text`,
  `Data`, `MimeType`, and `Raw` for a kind this package does not
  decompose further.
- `CallResult` — the mapped result of one `tools/call` invocation:
  `Content` and `IsError`.

## Functions

- `NewStdioTransport(name, args...)` — starts `name` as a subprocess
  and speaks MCP's stdio wire form over its stdin and stdout.
- `NewStreamableHTTPTransport(endpoint, httpClient)` — speaks MCP's
  streamable HTTP transport against `endpoint`. A nil `httpClient`
  uses `http.DefaultClient`.
- `Connect(ctx, t, opts)` — opens a `Client` over `t`, performing the
  MCP initialize handshake. Returns an error, not a partial `Client`,
  when the handshake fails.
- `RegisterAll(ctx, c, reg)` — calls `c.ListTools` and adds every
  returned `tools.Tool` to `reg`. Stops on the first error.

## Methods

- `(*Client) Close() error` — closes the underlying session.
  Idempotent: a second call returns nil.
- `(*Client) ListTools(ctx) ([]tools.Tool, error)` — calls
  `tools/list`, draining every page, and maps each tool. Each mapped
  `tools.Tool` calls back into the `Client` when run, and implements
  `tools.SchemaTool`, so `agentloop.Definitions` offers it.
- `(*Client) CallTool(ctx, name, args) (tools.Out, error)` — calls
  `tools/call` and maps the result into a `tools.Out` wrapping a
  `*CallResult`. Returns a non-nil error only for a transport or
  protocol-level failure; a tool-level failure surfaces as
  `CallResult.IsError`.
- `(*Client) CallToolWithProgress(ctx, name, args, onProgress) (tools.Out, error)`
  — behaves like `CallTool`, except every progress notification for
  this call reaches `onProgress` instead of `ClientOptions.OnProgress`.
  `onProgress` must not be nil.

`CallTool` and `CallToolWithProgress` perform no approval check. To
gate an MCP-discovered tool behind `tools.Scope.Approve`, register it
into a `tools.Registry` through `ListTools`'s mapping, then invoke it
with `Registry.RunScoped`. A caller who calls `CallTool` directly
skips approval gating entirely, the same way `Registry.Run` does.

## Invariants

- `Connect` returns an error, not a partial `Client`, when the
  handshake fails.
- `Close` is idempotent.
- `ListTools`, `CallTool`, and `CallToolWithProgress` return
  `ErrClosed` once `Close` already ran.
- `CallTool` mints a progress token for every call, whether or not a
  handler is registered for it.
- A progress notification for a call made through `CallToolWithProgress`
  reaches only that call's own handler, never another call's, even
  under concurrent calls on the same `Client`.
- A `CallToolWithProgress` handler entry is retained for the `Client`'s
  whole lifetime, not released after its call returns; `Close` clears
  every entry at once. This guards against a late notification the SDK
  dispatches on a goroutine unordered against the call's own response
  goroutine.

## Failure modes

- `ErrClosed` ("mcp: client is closed") — `CallTool`,
  `CallToolWithProgress`, and `ListTools` return it once `Close` already
  ran. Pinned by `mcp/client_test.go`.
- `ErrNilProgressHandler` ("mcp: onProgress must not be nil") —
  `CallToolWithProgress` returns it when `onProgress` is nil. Pinned by
  `TestCallToolWithProgressRejectsNilHandler` in `mcp/connect_test.go`
  with `errors.Is`.

## Why this shape

`mcp` wraps the official MCP Go SDK's client rather than
reimplementing MCP's JSON-RPC framing and transports from the standard
library, the same tradeoff phase 10 made for `a2aclient` against
`a2a-go`. Unlike `a2a`/`a2aclient`, `mcp` does not split into a
mapping leaf and a client: its tool mapping has no offline shape
independent of a live `Client`, so a caller of the mapping already
needs the SDK's dependency closure regardless of where the package
boundary sits. `mcp` is the second package, after `a2aclient`, allowed
to carry a third-party import: `github.com/modelcontextprotocol/go-sdk`;
see `AGENTS.md`'s Rules section for the stated exception. See
`docs/plans/mcp.md` for the full design record.

## Usage

```go
c, err := mcp.Connect(ctx, mcp.NewStdioTransport("my-mcp-server"), mcp.ClientOptions{
    Info: mcp.ClientInfo{Name: "my-agent", Version: "v1"},
})
if err != nil {
    // handle error
}
defer c.Close()

reg := tools.New()
if err := mcp.RegisterAll(ctx, c, reg); err != nil {
    // handle error
}

out, err := reg.Run(ctx, "my-tool", tools.InOut{Value: map[string]any{"arg": "value"}})
// out.Value.(*mcp.CallResult) holds the mapped content
```
