# Phase 35: MCP tool-calling client

Status: ready to build. New top-level package, `mcp`. Depends on the
shipped `tools` package (phase 14) only, for its internal imports.
Also depends on the official `github.com/modelcontextprotocol/go-sdk`
module; see the third-party exception below, the same shape phase 10
recorded for `a2aclient`. Composes with `tools.Registry` the way
`a2a.ToPart`/`FromPart` compose with `envelope.Message`: `mcp` maps a
remote server's tools into `tools.Tool` values a `Registry` can hold
and run. See `docs/plans/agents/PHASES.md`.

## Goal

Let a caller connect to a Model Context Protocol server, list its
tools, and call them, including a tool call that reports progress
while it runs, without depending on any vendor's hosted MCP offering.
`mcp` wraps the official MCP Go SDK's client over its two standard
transports, a local subprocess over stdio and a remote server over
streamable HTTP, and maps each remote tool into a `tools.Tool` a
`tools.Registry` already knows how to hold and run.

## Scope

Inside this package:

- The MCP client lifecycle: connect, negotiate the protocol version,
  handshake, close. This package delegates the handshake itself to the
  official SDK's `Client.Connect`.
- The `tools/list` call, mapped into `[]tools.Tool`.
- The `tools/call` call, mapped from a `tools.Tool.Run` invocation and
  back into a `tools.Out`.
- Progress notifications for an in-flight `tools/call`: a caller
  receives a stream of message, progress, and total updates while a
  long-running tool call is still pending, through a callback this
  package correlates to the call that requested it. See "Progress
  streaming" below.
- Two transports, both built by wrapping an official-SDK transport
  type: `NewStdioTransport` for a local subprocess, and
  `NewStreamableHTTPTransport` for a remote HTTP endpoint.

Outside this package:

- Any MCP feature beyond tool calling: resources, prompts, sampling,
  and roots. This package advertises no capability beyond tool calling
  during `Connect`; it does not surface the SDK's sampling, roots, or
  logging handlers, all three now deprecated in the spec by SEP-2577,
  in this phase's `ClientOptions`.
- Any hosted or vendor-specific MCP directory, catalog, or auth flow.
  `mcp` connects to any MCP server reachable over its own standard
  transports, self-hosted or third-party. It names no vendor and
  imports no OAuth package; the SDK's `auth` subpackage stays unused.
- Any change to the shipped `tools` package. `Tool`, `Registry`,
  `Add`, `Get`, `Remove`, and `Run` stay exactly as phase 14 shipped
  them.
- Advanced SDK-level client configuration: a custom logger, a keepalive
  ping interval, or the deprecated sampling and elicitation handlers.
  A caller needing one of these builds an `*mcpsdk.Client` directly and
  does not go through this package's `Connect`; wiring more SDK
  options through `ClientOptions` is a later phase's own plan.

### The official MCP Go SDK, not a stdlib reimplementation

This plan supersedes an earlier draft that reimplemented MCP's
JSON-RPC framing and both transports from the standard library alone.
`github.com/modelcontextprotocol/go-sdk` (package path
`.../go-sdk/mcp`) is the official Go SDK for MCP, maintained in
collaboration with Google. Verified against the module's tagged source
at `v1.7.0`: it already tracks the 2026-07-28 protocol revision,
including the deprecation window SEP-2577 opened for sampling, roots,
and logging.

Reimplementing JSON-RPC framing and chasing every future spec revision
inside this repository is wasted effort once a maintained, spec-current
SDK exists. This is the same tradeoff phase 10 made for `a2aclient`
against `a2a-go`: `a2aclient` did not reimplement A2A's gRPC transport
by hand, and `mcp` does not reimplement MCP's JSON-RPC transport by
hand either. This package's job narrows to what phase 10's `a2aclient`
plan states for its own relationship to `a2a-go`: opening a connection
lifecycle around the SDK's client, and mapping the SDK's `Tool` and
`CallToolResult` types onto this repository's `tools.Tool` and
`tools.Registry`.

### The third-party exception this phase needs

`mcp` imports exactly one package from the SDK module,
`github.com/modelcontextprotocol/go-sdk/mcp`, aliased `mcpsdk` to avoid
a name collision with this package's own package clause, the same
reason `a2aclient/client.go` aliases `a2a-go`'s own `a2aclient` import.
`mcp` imports no other SDK subpackage: not `.../go-sdk/jsonrpc`, since
this package builds no custom `Transport` and never touches a raw
JSON-RPC frame; not `.../go-sdk/auth`, since OAuth stays out of scope.

AGENTS.md's Rules section states today: "Exception: `a2aclient` may
import `github.com/a2aproject/a2a-go` and `google.golang.org/grpc`; no
other package may add a third-party import without its own plan
review." This phase needs the matching second exception, added in the
same one sentence, so the stated rule and the gates below stay in
agreement. The builder edits this exact line in `AGENTS.md`, in the
same change as the `mcp` code:

```text
Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
`google.golang.org/grpc`; `mcp` may import
`github.com/modelcontextprotocol/go-sdk`; no other package may add a
third-party import without its own plan review.
```

`policy/layers.json` still gains only one row: `"mcp": ["tools"]`. The
import-policy gate governs internal edges between this module's own
packages; the SDK is a third-party module, so it needs the Semgrep and
`go.mod` exceptions below instead, not a `policy/layers.json` entry.
`mcp` needs neither `envelope` nor `events`: it carries no signed
message and no in-process event. No other package's row changes; no
package gains `mcp` as an allowed import in this phase, matching
`tools` itself, which shipped with no caller.

### One package, not a mapping-leaf-plus-client split

`a2a` and `a2aclient` split because `a2a`'s mapping, `envelope.Message`
to an A2A `Part`, is a pure, offline transformation: a caller can build
and inspect a `Part` with no live connection at all. Forcing every such
caller to carry `a2a-go`'s third-party import, and the Go-version floor
that import needs, for a mapping call that never touches the network,
would have been an unjustified cost on a caller who never sends
anything.

`mcp`'s mapping has no equivalent offline shape. A `tools.Tool` this
package returns from `ListTools` calls back into the live `Client` to
run; the mapping is meaningless without a connection behind it. No
caller in this module wants the `tools.Tool` mapping with no `Client`
to back it, so splitting `mcp` into a pure-mapping leaf and a stateful
client, mirroring `a2a`/`a2aclient` for symmetry alone, would split a
package with no second consumer. AGENTS.md's Building blocks section
forbids that split without a real caller. Every caller of this
package's mapping already needs the SDK's transitive dependency
closure, whether the package boundary sits at the mapping or at the
client, so drawing that boundary buys no caller a smaller dependency
footprint here, unlike the `a2a`/`a2aclient` case. One package, `mcp`,
carries the SDK wrapping, the connection lifecycle, and the
`tools.Tool` mapping together.

### Progress streaming: what the SDK supports, verified

A single `tools/call` in the current MCP spec is atomic: one server
response, one final `CallToolResult`. There is no protocol mechanism
for a tool to stream partial *content*, such as incremental text
tokens, ahead of that final result. What the protocol does define, and
what this phase verified against the SDK's real, released API
(`v1.7.0`, confirmed by a live in-process round trip against the SDK's
own test-oriented `NewInMemoryTransports` and `NewServer` during this
plan's research), is a `notifications/progress` message: a free-text
`Message`, a numeric `Progress`, and an optional `Total`, sent by the
server while a call it tagged with a progress token is still pending.

The SDK exposes this through three verified pieces: `CallToolParams.
SetProgressToken(t any)` on the request side, `ClientOptions.
ProgressNotificationHandler func(context.Context,
*ProgressNotificationClientRequest)` registered once per `mcpsdk.
Client`, and `ServerSession.NotifyProgress` on the side a tool handler
uses to send one. A server tool handler can call `NotifyProgress`
zero, one, or many times before it returns its final result; each call
reaches the client's handler as a separate, asynchronous notification
while `ClientSession.CallTool` is still blocked waiting for that final
result.

This still works over streamable HTTP under the 2026-07-28 revision.
That revision removed the old session-id-plus-standalone-GET-stream
mechanism, but it kept the per-request response able to open as either
one JSON object or a Server-Sent-Events stream carrying the
`notifications/progress` messages ahead of the final response, on the
same POST. The SDK's `StreamableClientTransport` also keeps an
optional standalone GET-based SSE stream (its `DisableStandaloneSSE`
field, default `false`) for a server that pushes a notification
outside any single request; this package's `NewStreamableHTTPTransport`
leaves that default in place, so a server using either delivery path
still reaches this package's progress handler.

This phase delivers progress notifications, message plus two numbers,
per in-flight call, correlated by an opaque token this package mints.
It does not deliver incremental content streaming, because the
protocol's `tools/call` result carries content atomically, not the
message. Every claim in this section was checked against the SDK's
live, released API before this plan was written; this plan raises no
open question on the progress mechanism itself.

## API

The surface below lands in `api/mcp.txt` via `make api-update`. Every
type below is defined by this package; none re-exports an SDK type
directly, so a caller of this package needs no import of the SDK
itself.

```go
package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Transport is the connection a Client opens over. It is a type alias
// for the official MCP Go SDK's own Transport interface
// (mcpsdk.Transport). NewStdioTransport and NewStreamableHTTPTransport
// each build one of the SDK's shipped implementations; a caller
// needing an SDK transport this package does not wrap, such as the
// SDK's in-memory transport pair used in this package's own tests,
// builds it against the SDK directly and passes it to Connect
// unchanged.
type Transport = mcpsdk.Transport

// NewStdioTransport returns a Transport that starts name as a
// subprocess with args and speaks MCP's stdio wire form over the
// subprocess's stdin and stdout, through the SDK's own
// CommandTransport.
func NewStdioTransport(name string, args ...string) Transport

// NewStreamableHTTPTransport returns a Transport that speaks MCP's
// streamable HTTP transport against endpoint, through the SDK's own
// StreamableClientTransport. A nil httpClient uses
// http.DefaultClient. The standalone SSE stream stays enabled, at the
// SDK's own default, so a server-initiated progress notification
// reaches this package's progress handler regardless of which stream
// carries it.
func NewStreamableHTTPTransport(endpoint string, httpClient *http.Client) Transport

// ClientInfo names the caller to the MCP server during the initialize
// handshake Connect performs through the SDK.
type ClientInfo struct {
	Name    string
	Version string
}

// ProgressHandler receives one progress notification for a call this
// package's Client made. token identifies the call the notification
// belongs to: CallToolWithProgress's caller supplies token's meaning
// only implicitly, by receiving it back on every notification for the
// call it started. message, progress, and total mirror the MCP
// notifications/progress fields; total is zero when the server does
// not report one.
type ProgressHandler func(ctx context.Context, token any, message string, progress, total float64)

// ClientOptions configures Connect.
type ClientOptions struct {
	// Info names this client to the server.
	Info ClientInfo
	// OnProgress, when non-nil, receives a progress notification for
	// any call this Client makes that has no more specific handler
	// registered through CallToolWithProgress. A call made through
	// the mapped tools.Tool returned by ListTools, or through
	// Registry.Run, only ever reaches this session-wide handler.
	OnProgress ProgressHandler
}

// Client is one connection to one MCP server, wrapping the official
// MCP Go SDK's Client and ClientSession. The caller owns the Client
// and must call Close when done with it. Client is safe for
// concurrent ListTools, CallTool, and CallToolWithProgress calls from
// multiple goroutines.
type Client struct {
	// unexported: the wrapped *mcpsdk.Client, *mcpsdk.ClientSession,
	// a per-call progress-token counter, and the token-to-handler map
	// CallToolWithProgress populates.
}

// Connect opens a Client over t: it builds an SDK Client configured
// with opts.Info and a progress-notification handler wired to this
// Client's per-call correlation, then calls the SDK's own Connect,
// which performs the MCP initialize handshake. Connect returns an
// error, not a partial Client, when the handshake fails.
func Connect(ctx context.Context, t Transport, opts ClientOptions) (*Client, error)

// Close closes the underlying session. Close is idempotent: a second
// call returns nil.
func (c *Client) Close() error

// ListTools calls the server's tools/list method, draining every page
// through the SDK's own pagination, and maps each returned mcpsdk.Tool
// into a tools.Tool. Each returned tools.Tool calls back into c
// through CallTool when run, and implements SchemaTool.
func (c *Client) ListTools(ctx context.Context) ([]tools.Tool, error)

// CallTool invokes name through the wrapped session's CallTool,
// sending args as the call's arguments value (any value the SDK can
// marshal to JSON; nil sends no arguments), and maps the result into
// a tools.Out wrapping a *CallResult. CallTool mints a progress token
// for the call; a notification for it reaches ClientOptions.OnProgress
// when set, and is otherwise dropped. CallTool returns a non-nil
// error only for a transport or protocol-level failure; a tool-level
// failure the server reports through the result's isError field
// surfaces as CallResult.IsError, not as a Go error. See the design
// note above.
func (c *Client) CallTool(ctx context.Context, name string, args any) (tools.Out, error)

// CallToolWithProgress behaves like CallTool, except every progress
// notification for this specific call reaches onProgress, not
// ClientOptions.OnProgress, for the call's whole duration. onProgress
// must not be nil.
func (c *Client) CallToolWithProgress(ctx context.Context, name string, args any, onProgress ProgressHandler) (tools.Out, error)

// SchemaTool is an optional interface a tools.Tool returned by
// ListTools implements. InputSchema returns the tool's input schema
// exactly as the server published it and the SDK decoded it, a
// map[string]any in the common case.
type SchemaTool interface {
	InputSchema() any
}

// ContentBlock is one block of an MCP tool call result. Type names
// the block's kind: "text", "image", "audio", "resource_link",
// "resource", or another value this package does not model beyond
// Raw. Text carries a text block's content. Data carries an image or
// audio block's binary payload. MimeType names Data's media type.
// Raw carries the block's own JSON encoding for a kind this package
// models by Type alone, without further fields.
type ContentBlock struct {
	Type     string
	Text     string
	Data     []byte
	MimeType string
	Raw      json.RawMessage
}

// CallResult is the mapped result of one tools/call invocation.
// IsError reports a tool-level failure the server signaled through
// the result object; Content still carries whatever the server
// returned alongside that failure.
type CallResult struct {
	Content []ContentBlock
	IsError bool
}

// RegisterAll calls c.ListTools and adds every returned tools.Tool to
// reg through reg.Add. RegisterAll stops and returns the first error
// either call produces, leaving reg holding whichever tools were
// already added.
func RegisterAll(ctx context.Context, c *Client, reg *tools.Registry) error

// ErrClosed is CallTool's, CallToolWithProgress's, and ListTools's
// error when the Client's Close already ran. Test with errors.Is.
var ErrClosed error
```

Design notes:

- `Connect`, not `New`. `a2aclient.New` builds a `Client` from a base
  URL alone; this package's `Client` needs a `Transport` value the
  caller already built, plus the MCP handshake `New` does not suggest.
  `Connect` states plainly that a network or subprocess exchange
  happens before the call returns, matching the SDK's own `Client.
  Connect` name for the same reason.
- `Transport = mcpsdk.Transport` is a type alias, not a wrapping
  interface. An advanced caller who needs a transport this package's
  two constructors do not build, such as the SDK's own in-memory pair
  this package's tests use, passes it to `Connect` with no adapter
  type in between. This mirrors `a2aclient`'s choice to build its
  `grpc.ClientConn` directly rather than hide `grpc.DialOption` behind
  a narrower type.
- `CallResult.IsError`, not a returned `error`, carries an MCP-level
  tool failure. The MCP spec defines `isError` as part of a successful
  `tools/call` response: the server still returns whatever `Content`
  it produced alongside the failure, for example a partial log or an
  error message meant for the model to read. Mapping `isError` to a
  Go `error` would force `CallTool`'s caller to choose between the
  error and the content, when MCP hands back both together. `CallTool`
  returns a non-nil `error` only when no `CallResult` exists at all: a
  transport failure or an SDK-reported protocol-level error.
- `CallTool` always mints a fresh, opaque progress token for its call,
  whether or not any handler is registered. A server that never sends
  a progress notification simply never uses it; a server that does
  reaches `ClientOptions.OnProgress` when set, or is silently dropped
  when not, the same way an unread channel value would be. This keeps
  `CallTool` and `CallToolWithProgress` symmetric: both request
  progress from the server, and only the destination of a resulting
  notification differs.
- `CallToolWithProgress` registers `onProgress` under the minted token
  in `Client`'s internal map before the call. The entry stays in the
  map for the `Client`'s whole lifetime; `Close` is the only call that
  clears it. The SDK dispatches an incoming `notifications/progress`
  message on its own goroutine, unordered against the goroutine that
  unblocks the matching `CallTool` response, so a late notification
  can still arrive after `CallToolWithProgress` has already returned.
  Removing the entry synchronously on return can drop that
  notification; retaining it until `Close` cannot, since the minted
  token counter is monotonic and never repeats within one `Client`'s
  lifetime. A concurrent second call reusing the counter's next value
  gets its own distinct token, so two overlapping
  `CallToolWithProgress` calls never mix each other's notifications;
  this design's own test proves that directly.
- Each `tools.Tool` `ListTools` returns wraps one MCP tool descriptor
  and this `Client`. Its `Run` method calls `c.CallTool(ctx,
  descriptor.Name, in.Value)` and returns the mapped `tools.Out`. It
  never calls `CallToolWithProgress`; a caller wanting per-call
  progress on a specific tool call uses `Client.CallToolWithProgress`
  directly instead of the generic `tools.Tool`/`Registry.Run` path,
  since that path's `Run(ctx, in) (Out, error)` signature has no room
  for a handler argument, and phase 14 locked that signature.
- `SchemaTool.InputSchema` returns `any`, not `json.RawMessage`. The
  SDK's own `Tool.InputSchema` field is typed `any` and holds a
  `map[string]any` on the client side, the SDK's own default JSON
  marshaling of the server's schema. Converting that to raw bytes
  would need a `json.Marshal` call this package has no other reason to
  make; a future caller building a `provider.ToolDefinition.Schema
  []byte` (`docs/plans/agents/phase29_provider.md`) does that
  conversion itself, in its own plan, when that wiring lands.
  `SchemaTool` earns its place today through that same named future
  caller, following the optional-interface pattern phase 31 already
  set for `tools.ProfiledTool`, `tools.ResultBudgetTool`, and
  `tools.PrivilegedTool`.
- `ContentBlock.Raw` is filled by calling the SDK's own `Content.
  MarshalJSON` method on a block this package's `Type` switch does not
  otherwise decompose, for example `EmbeddedResource` or
  `ToolResultContent`. Calling a method the SDK's own `Content`
  interface already exposes is not a call to the package-level
  `json.Marshal`, `json.MarshalIndent`, or `json.NewEncoder(...).
  Encode(...)` functions `sdk.go.marshal-via-encode` forbids outside a
  short, named file list; this package needs no exclusion added to
  that rule.
- `RegisterAll` is the one call most callers use to bring a whole MCP
  server's tools into a `Registry`, the way `provider.RunTurn` is the
  one call most `provider` callers use. It composes two existing
  calls, `ListTools` and `Registry.Add`, and adds no new mapping rule
  of its own.

## Tests

Test files live directly in `mcp/`, not in a nested
`mcp/mcp_test/` directory. This differs from the flat
`<pkg>/<pkg>_test/` layout most other packages in this module use, per
`docs/plans/agents/PHASES.md`, for the same reason `a2aclient`'s own
tests do: `sdk-standards.yml` scopes the third-party-import exception
to `mcp/*.go` only, so a test package in a nested directory could
never import the SDK to build a realistic fixture server, and this
module's coverage-floor computation measures a package with a
`<pkg>_test` directory only through that directory. Every test file
below is `package mcp`, following the `a2aclient` and
`flow/wave_select_internal_test.go` whitebox pattern; none needs an
unexported constructor the way `a2aclient`'s tests needed
`newFromTransport`, since `Transport`, `Connect`, and `Client`'s
exported methods are already enough to drive every case against a
fixture built from the SDK's own server and in-memory transport types.

- `client_test.go` — red-green unit cases for `Connect`, `Close`,
  `ListTools`, `CallTool`, and `RegisterAll`. Every case builds a
  fixture MCP server with `mcpsdk.NewServer` and `mcpsdk.AddTool`,
  connects it to this package's `Client` over `mcpsdk.
  NewInMemoryTransports()`, no subprocess and no real network. Cases:
  - `Connect` returns a non-nil `*Client` once the fixture server
    completes the handshake.
  - `ListTools` maps a fixture server's two registered tools to two
    `tools.Tool` values; each mapped tool implements `SchemaTool` and
    returns the fixture tool's `InputSchema` value.
  - `ListTools` drains more than one page, against a fixture server
    configured with a small page size, proving the SDK's own
    pagination iterator is exhausted, not just its first page.
  - `CallTool` against a fixture tool returning one `TextContent`
    block maps to a `CallResult` with one `ContentBlock` of type
    `"text"` and the expected `Text`.
  - `CallTool` against a fixture tool returning `IsError: true` maps
    to `CallResult.IsError == true` with a nil `error` from `CallTool`.
  - `CallTool` against a fixture tool the fixture server never
    registered returns a non-nil error, proving an unknown-tool
    failure surfaces as a Go error, not a zero `CallResult`.
  - `Close` is idempotent: two calls both return nil.
  - `ListTools` and `CallTool` on a `Client` whose `Close` already ran
    return `ErrClosed`, and `errors.Is` holds.
  - `RegisterAll` adds every tool `ListTools` returns to a fresh
    `tools.Registry`, proven by resolving each name through
    `Registry.Get` afterward, then calling `Registry.Run` on one of
    them and asserting the mapped result.
  Each case asserts the failing behavior first and turns green once
  the implementation lands.
- `client_progress_test.go` — red-green cases proving the progress
  design note above. The fixture tool handler calls `req.Session.
  NotifyProgress` two or three times, with increasing `Progress`
  values, before it returns its final result. Cases:
  - `CallToolWithProgress` against that fixture tool: `onProgress`
    receives every notification, in order, each carrying the same
    token, before `CallToolWithProgress` returns the final
    `CallResult`.
  - Two concurrent `CallToolWithProgress` calls against the same
    fixture tool, run under `go test -race`: each call's `onProgress`
    receives only its own notifications, proven by asserting the
    token each call's handler observes never matches the other call's
    token.
  - `CallTool` (no per-call handler) against the same fixture tool,
    with `ClientOptions.OnProgress` set on the `Client`: the
    session-wide handler receives the notifications instead, proving
    the fallback path.
  - `CallTool` with no `ClientOptions.OnProgress` set: the fixture's
    notifications are sent and silently dropped, proving the mint
    always happens but a missing handler never panics or blocks the
    call.
- `stdio_transport_test.go` — builds `NewStdioTransport` against a
  real subprocess: this test binary re-executes itself under a
  dedicated environment variable, and the re-executed process runs an
  `mcpsdk.NewServer` over `mcpsdk.StdioTransport{}` through the SDK's
  own `Server.Run`, registering one fixture tool. This is the same
  self-exec pattern Go's own `os/exec` tests use; it proves the real
  subprocess and pipe wiring this package's one-line wrapper builds,
  not a mock. Cases: `Connect`, `ListTools`, and `CallTool` all
  succeed over the real subprocess; `Close` terminates the subprocess.
- `http_transport_test.go` — builds `NewStreamableHTTPTransport`
  against an `httptest.Server` wrapping `mcpsdk.
  NewStreamableHTTPHandler`, a loopback listener with no external
  network dependency, the standard Go idiom for testing an HTTP
  client. Cases: `Connect`, `ListTools`, and `CallTool` all succeed
  over the real HTTP round trip; a fixture tool that calls
  `NotifyProgress` before returning still reaches
  `CallToolWithProgress`'s handler over the streamable HTTP transport,
  proving the design note's streaming claim against a real HTTP
  connection, not only the in-memory transport; a nil `httpClient`
  argument still succeeds, using `http.DefaultClient`.
- `client_bench_test.go` — `BenchmarkCallTool` runs `CallTool`
  repeatedly against the in-memory fixture server from
  `client_test.go`, on a fixture tool that returns immediately with no
  progress notification. `ReportAllocs` states the allocation budget;
  the SDK's own JSON-RPC marshaling on both ends of the in-memory pipe
  is the expected allocation source, so the budget is not zero.

Conformance vectors land in `mcp/testdata/vectors/`, narrower in scope
than `a2a/testdata/vectors/`: this package owns no wire framing, so it
pins its mapping function against a real `tools/call` result shape,
not the JSON-RPC envelope around it. Each vector is a JSON object
matching `CallToolResult`'s wire shape; the test decodes it through
`mcpsdk.CallToolResult`'s own `UnmarshalJSON` (calling a decoder the
SDK exposes, not `json.Unmarshal` on a private shape this package
invents), then maps it and compares against the vector's expected
`CallResult`. `valid_tools_call_text_result.json`,
`valid_tools_call_image_result.json`, and
`valid_tools_call_error_result.json` cover the three cases above.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan (including the two rule changes below), and the
  coverage block.
- The coverage floor of 85 holds for `mcp` and for the total.
- `policy/layers.json` gains the `"mcp": ["tools"]` row, landed with
  this plan before the code. `python3 scripts/check_deps.py` passes
  against it; the row is unchanged by this revision.
- `go test -race ./mcp/...` passes, covering the concurrent-use claim
  on `Client` and the per-call progress-token correlation proof in
  `client_progress_test.go`.
- The builder creates `docs/plans/mcp.md` from `docs/plans/TEMPLATE.md`
  before the code lands, since `scripts/check_plan.py` requires a plan
  for every top-level package directory that has `.go` files.
- `docs/architecture.md` gains an `mcp/` package-map entry, listing its
  one internal import (`tools`), its third-party dependency on the
  official MCP Go SDK, and its two transports, in the same change as
  the code.
- `AGENTS.md`'s Layout section gains one `mcp/` bullet, naming
  `Client`, `Connect`, `Transport`, `NewStdioTransport`,
  `NewStreamableHTTPTransport`, `ListTools`, `CallTool`, and
  `CallToolWithProgress`, and stating it imports `tools` internally
  and the official MCP Go SDK externally, matching the shape of the
  existing `a2aclient` bullet.
- No `agent` change lands in this phase. `agent`'s row in
  `policy/layers.json` stays unchanged; wiring `mcp` into `agent` is a
  later phase's own plan.

### Semgrep: scoped stdlib-only exception

Mirrors `docs/plans/a2aclient.md`'s pattern exactly, for the same
rule, with `mcp` in place of `a2aclient`. The build to implement, by
the builder, in `semgrep/sdk-standards.yml`:

- Add `"/mcp/*.go"` to `sdk.go.stdlib-only-imports`'s
  `paths.exclude` list, alongside the existing `a2aclient` exclusion.
- Add a new rule, `sdk.go.mcp-scoped-third-party-import`, `severity:
  ERROR`, scoped to `paths.include: ["/mcp/*.go"]`. It reuses the same
  `pattern-regex` that finds a dotted-domain import string, and adds a
  `pattern-not-regex` permitting only
  `"github\.com/modelcontextprotocol/go-sdk(/[^"\n]*)?"` in addition
  to the existing module-path exemption. Any other third-party import
  inside `mcp/*.go` still fires this rule.

`scripts/check_semgrep_probes.py` gains a new probe case, parallel to
the existing `a2aclient`-scoped block: an `mcp/` subdirectory under
the probe's temp root, holding `viol_other_import.go` (importing an
unrelated third-party path) and `clean_go_sdk_import.go` (importing
`github.com/modelcontextprotocol/go-sdk/mcp`). Both basenames register
in `expected` against `sdk.go.mcp-scoped-third-party-import`, and an
explicit assertion block, parallel to the `a2aclient` one, proves: the
new rule fires on `viol_other_import.go` and stays silent on
`clean_go_sdk_import.go`; `sdk.go.stdlib-only-imports` fires on
neither, proving the scoped exclude took effect; and the existing
outside-the-directory probe file still proves `sdk.go.
stdlib-only-imports` fires normally outside both scoped directories.

### go.mod and go.sum: the closed dependency allowlist, extended

No Go-version change lands in this phase. `go.mod` already declares
`go 1.25` from phase 10's bump; the SDK's own `go.mod` declares `go
1.25.0`, so this module's floor already meets it.

`go.mod` gains a `require` line for `github.com/modelcontextprotocol/
go-sdk`, plus the indirect lines `go mod tidy` adds beneath it. This
plan verified the actual set `go mod tidy` produces, against a probe
module importing `.../go-sdk/mcp`'s `Client`, `CommandTransport`, and
`StreamableClientTransport`, the same three names this package's own
code imports:

```text
require github.com/modelcontextprotocol/go-sdk v1.7.0

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)
```

`go.sum` additionally carries `/go.mod`-only hash lines for two
modules the resolved build list references but no imported package
reaches at build time: `github.com/golang-jwt/jwt/v5` and
`golang.org/x/tools`. `github.com/google/go-cmp` also appears in
`go.sum`; it is already in `ALLOWED_MODULES` from the `a2aclient`
exception. `golang.org/x/sync` and `golang.org/x/sys` are likewise
already allowed.

`scripts/check_gomod.py`'s `ALLOWED_MODULES` set gains the new module
paths this phase's dependency closure adds, beyond what `a2aclient`
already permits:

```python
ALLOWED_MODULES |= {
    "github.com/modelcontextprotocol/go-sdk",
    "github.com/golang-jwt/jwt/v5",
    "github.com/google/jsonschema-go",
    "github.com/segmentio/asm",
    "github.com/segmentio/encoding",
    "github.com/yosida95/uritemplate/v3",
    "golang.org/x/oauth2",
    "golang.org/x/time",
    "golang.org/x/tools",
}
```

The builder runs `go mod tidy` once `mcp`'s own `.go` files import
`.../go-sdk/mcp`, records the resulting `require` and `go.sum` module
set, and reconciles `ALLOWED_MODULES` against that real output in the
same change, trimming any entry above `go mod tidy` does not actually
add, the same reconciliation step `docs/plans/a2aclient.md` records
for its own allowlist. `check_gomod.py`'s module docstring, which
today names only the `a2aclient` exception, gains one sentence naming
the `mcp` exception too, in the same change.

### Summary

`make verify` passes only once all of the following land together in
one change: the `mcp` package and its tests; the `policy/layers.json`
row (already landed with this plan); the `go.mod` and `go.sum`
additions; the `check_gomod.py` allowlist extension and docstring
update; the two `semgrep/sdk-standards.yml` rule edits and the new
`check_semgrep_probes.py` case; the `AGENTS.md` exception-sentence
edit; and the `docs/architecture.md` and `docs/plans/mcp.md` doc
updates. `docs/protocol-design.md` does not change in this phase: `mcp`
adds no message-semantics rule to this module's own envelope wire
format; it wraps a separate, already-specified protocol.
