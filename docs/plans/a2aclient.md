# Plan: a2aclient

Status: shipped. Builds on the landed `a2a` package (`docs/plans/a2a.md`).

## Goal

Send an `envelope.Message` to a remote agent through the
`a2aproject/a2a-go` client, poll the task status, and fetch the
result. `Send` creates the remote task. `Status` reads it. `Result`
fetches the output and returns it as an `envelope.Message`. This is
the first external network call in the module.

## Scope

Inside: a new top-level package, `a2aclient`. It wraps the
`a2aproject/a2a-go` client and drives one task's lifecycle: send,
poll, fetch, close. It is the only package in this module allowed to
import `a2aproject/a2a-go`.

Outside: the `a2a` package. Phase 9 already landed `Part`, `Mapped`,
`ToPart`, and `FromPart` in `a2a`, and that package stays untouched by
this phase. `a2aclient` imports `a2a` for the mapping functions; it
adds no new exported symbol to `a2a` and no new import to `a2a`. Also
outside: Agent Card discovery (a later phase) and running a server
(`a2aclient` is a client only, never a server, in this version).
Phase 51 adds one exported exception: a `Loopback` gRPC test-fixture
server in non-test source, compelled by the `/a2aclient/*.go`
Semgrep exception. This is a reviewed scope extension for tests only,
not a server feature.

The original phase 10 sketch proposed putting `Client`, `Send`,
`Status`, `Result`, and `TaskHandle` inside the `a2a` package itself.
This plan replaces that sketch. Bolting a network client onto `a2a`
would force every caller of the pure mapping functions to carry the
third-party dependency and the Go version bump this phase needs, even
when that caller never sends a message over the network. A separate
package keeps `a2a` a stdlib-only leaf, as `docs/packages/a2a.md`'s
opening paragraph states: `a2a` "carries no network call and no
third-party import."

## API

```go
package a2aclient

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Client sends envelope messages to one remote A2A agent and reads
// task status and results back. Client wraps the a2aproject/a2a-go
// client for one base URL. The caller owns the Client; a Client is
// safe for concurrent use by multiple goroutines.
type Client struct {
	// unexported: the wrapped a2a-go transport and the base URL.
}

// New builds a Client that talks to the A2A agent at baseURL. New
// validates baseURL and opens the underlying a2a-go gRPC transport,
// which holds a persistent connection. It returns an error, not a
// partial Client, when baseURL is empty or the transport fails to
// open. The caller must call Close when done with the Client.
func New(baseURL string) (*Client, error)

// Close releases the resources New opened. a2a-go's gRPC transport
// wraps a persistent grpc.ClientConn; Close forwards to the
// transport's own teardown call. Close is idempotent: a second call
// returns nil. A Client whose Close was never called leaks the
// underlying connection, the same way an unclosed net.Conn does.
func (c *Client) Close() error

// TaskHandle identifies one remote task started by Send. The caller
// passes it back into Status and Result to track the same task. The
// zero TaskHandle identifies no task; Status and Result reject it.
type TaskHandle struct {
	// unexported: the a2a-go task id and context id.
}

// State is the state of a remote task, mirrored from the a2a-go task
// state enum. See the State constants for the closed set of values.
type State int

// The states a remote task passes through. A2A tasks move from
// submitted through working to one terminal state: completed,
// failed, or canceled.
const (
	StateUnspecified State = iota
	StateSubmitted
	StateWorking
	StateCompleted
	StateFailed
	StateCanceled
)

// String returns the constant name for state, or "unknown" for a
// value outside the declared range.
func (s State) String() string

// Send maps msg to an A2A part through a2a.ToPart, then sends it to
// the remote agent as a new task. Send returns the TaskHandle
// identifying the created task. msg must already be signed; Send
// performs no signing of its own, matching a2a.ToPart's contract. A
// transport failure, a canceled ctx, or an expired ctx deadline
// returns an error and a zero TaskHandle, never a partial one.
func (c *Client) Send(ctx context.Context, msg envelope.Message) (TaskHandle, error)

// Status reads the current state of the task identified by h from
// the remote agent. Status rejects the zero TaskHandle with an error.
// A canceled ctx or an expired ctx deadline returns that ctx error,
// unwrapped, so the caller can distinguish it from a remote failure
// with errors.Is(err, context.Canceled) or context.DeadlineExceeded.
func (c *Client) Status(ctx context.Context, h TaskHandle) (State, error)

// Result fetches the output of the task identified by h and maps it
// back to an envelope.Message through a2a.FromPart. Result calls
// msg.VerifySignature on the mapped message before returning it: the
// signature must still verify after the remote hop. Result rejects
// the zero TaskHandle. Result returns an error, not a partial
// Message, when the task is not yet in a terminal state, when
// FromPart fails, when the signature check fails, or when ctx is
// canceled or its deadline expires.
func (c *Client) Result(ctx context.Context, h TaskHandle) (envelope.Message, error)
```

Design notes:

- `New(baseURL string) (*Client, error)` replaces the original
  sketch's `New(opts ...Option) (*Client, error)`. The original draft
  never named a single `Option`. This phase has one concrete
  configuration need, the remote agent's base URL, so the constructor
  names that one input directly. A variadic `Option` pattern waits
  until a second real configuration need exists.
- `TaskHandle` and `Client` keep their fields unexported. The caller
  never constructs either by hand; both come from `New` and `Send`.
  This matches `identity.Identity`'s shape: a value returned by a
  constructor, never assembled field by field.
- `State` follows `machine.Status`'s enum convention: a typed int, a
  closed `const` block, and a `String` method. No caller compares
  state against a string literal.
- `Send` calls `a2a.ToPart` to build the wire part; it does not
  reimplement the envelope-to-part mapping. `Result` calls
  `a2a.FromPart` for the reverse direction, then re-verifies the
  signature. Re-verifying is the one new invariant network transport
  adds: a message that was valid before the hop must still be valid
  after it, and the signature check is how the caller knows the
  remote agent, or a network intermediary, did not tamper with the
  payload.
- `Client` wraps exactly one `a2aproject/a2a-go` client value, built
  on that module's gRPC transport (`a2aproject/a2a-go`'s own
  `a2aclient.NewGRPCTransport`, confirmed against the module's tagged
  source at `v0.3.15`). That transport wraps a `grpc.ClientConn` and
  exposes an explicit teardown call. `Client.Close` exists because
  that connection is a real held resource, not a stateless call: an
  unclosed `Client` leaks the connection the way an unclosed
  `net.Conn` does. No other package in this repository wraps a
  persistent connection today, so `Close` is a new pattern for this
  module; it follows `io.Closer`'s convention (idempotent, returns
  `error`).
- `a2aproject/a2a-go`'s own client package is also named `a2aclient`
  (import path `github.com/a2aproject/a2a-go/a2aclient`). The
  `a2aclient/client.go` source file in this module imports it under
  an alias to avoid a name collision with its own package clause;
  for example `a2asdk "github.com/a2aproject/a2a-go/a2aclient"`. This
  is a naming coincidence between two unrelated modules, not a design
  problem: the import paths differ, and Go compiles fine with an
  alias.
- `a2aclient/grpc.go` dials the connection itself, with
  `grpc.NewClient` and `grpc.WithTransportCredentials`, before handing
  it to `a2asdk.NewGRPCTransport`. `a2a-go`'s own gRPC factory takes
  the same `grpc.DialOption` values, so this package needs
  `google.golang.org/grpc` directly, not only `a2a-go`. AGENTS.md's
  exception sentence and `sdk-standards.yml`'s scoped rule both name
  `google.golang.org/grpc` alongside `github.com/a2aproject/a2a-go`.
- `Client` drives its task lifecycle through an unexported `transport`
  interface (`Send`, `State`, `Result`, `Close`), not through
  `a2asdk.Transport` directly. `New` builds the one production
  implementation, `grpcTransport`, wrapping `a2asdk.Transport`. This
  package's own tests build a `stubTransport` that also satisfies
  `transport`, and construct a `Client` around it through an
  unexported `newFromTransport` constructor, in a same-package
  (`package a2aclient`) test file, following the whitebox-test pattern
  `flow/wave_select_internal_test.go` already uses in this module.
  Neither `transport` nor `newFromTransport` is exported: no caller
  outside this package's own tests needs either one, and
  `sdk-standards.yml`'s third-party-import exception is scoped to
  `a2aclient/*.go`, so a separate external test package could not
  import `a2a-go` to build its own transport double.

`policy/layers.json` gains one row: `"a2aclient": ["a2a", "envelope"]`.
No other package may import `a2aclient`; `agent`'s row does not list
it in this phase. A later phase adds that edge when the composition
layer wires in a real transport.

### Gap fix: exported sentinel errors

Status: planned, not yet built. This package returns every error as
an inline `errors.New` or `fmt.Errorf` string today. No caller can
match a failure with `errors.Is`. This addendum adds one exported
sentinel per distinct failure condition and updates the matching call
site to wrap it, keeping the existing message text where practical.

New exported vars, in `a2aclient/client.go` unless noted:

```go
// ErrNoBaseURL reports a New or newFromTransport call whose baseURL
// is empty. Test with errors.Is.
var ErrNoBaseURL = errors.New("a2aclient: baseURL is required")

// ErrNoTransport reports a newFromTransport call whose tr is nil.
// Test with errors.Is.
var ErrNoTransport = errors.New("a2aclient: transport is required")

// ErrNoTaskID reports a Send call whose transport returned an empty
// task id. Test with errors.Is.
var ErrNoTaskID = errors.New("a2aclient: transport returned an empty task id")

// ErrZeroTaskHandle reports a Status or Result call against the zero
// TaskHandle. Test with errors.Is.
var ErrZeroTaskHandle = errors.New("a2aclient: zero TaskHandle")

// ErrNotTerminal reports a Result call against a task that has not
// yet reached a terminal State. Test with errors.Is.
var ErrNotTerminal = errors.New("a2aclient: task is not terminal")

// ErrSignatureCheckFailed reports a Result call whose mapped message
// fails VerifySignature after the remote hop. Test with errors.Is.
var ErrSignatureCheckFailed = errors.New("a2aclient: signature check failed")
```

In `a2aclient/grpc.go`:

```go
// ErrNoTask reports a Send call whose remote response was not a Task.
// Test with errors.Is.
var ErrNoTask = errors.New("a2aclient: send did not return a task")

// ErrNoResultMessage reports a Result call against a task that
// carries no status message and no history entry. Test with
// errors.Is.
var ErrNoResultMessage = errors.New("a2aclient: task carries no result message")

// ErrNoDataPart reports a Result call whose result message carries no
// DataPart. Test with errors.Is.
var ErrNoDataPart = errors.New("a2aclient: result message carries no data part")
```

Call-site changes, each replacing an inline `errors.New`/`fmt.Errorf`
with a wrap of the matching sentinel; keep every other line unchanged:

- `client.go`, `New`: `return nil, ErrNoBaseURL` (was
  `errors.New("a2aclient: baseURL is required")`).
- `client.go`, `newFromTransport`: `return nil, ErrNoBaseURL`, then
  `return nil, ErrNoTransport`.
- `client.go`, `Send`, empty task id: `return TaskHandle{}, ErrNoTaskID`.
- `client.go`, `Status`: `return StateUnspecified, ErrZeroTaskHandle`.
- `client.go`, `Result`, zero handle: `return envelope.Message{}, ErrZeroTaskHandle`.
- `client.go`, `Result`, non-terminal: keep the dynamic state name in
  the text and wrap the sentinel:
  `fmt.Errorf("a2aclient: task is %s, not terminal: %w", state, ErrNotTerminal)`.
- `client.go`, `Result`, signature check: keep the underlying
  `VerifySignature` error visible and wrap both sentinels. Go 1.25,
  this module's floor, supports two `%w` verbs in one `fmt.Errorf`
  call: `fmt.Errorf("a2aclient: signature check failed: %w: %w", ErrSignatureCheckFailed, err)`.
  `errors.Is` then matches both `ErrSignatureCheckFailed` and the
  original `VerifySignature` error.
- `grpc.go`, `Send`, non-Task result: `return "", ErrNoTask`.
- `grpc.go`, `Result`, nil `resultMessage`: `return a2a.Mapped{}, ErrNoResultMessage`.
- `grpc.go`, `dataFromParts`, no `DataPart`: `return nil, ErrNoDataPart`.

Out of scope: `loopback.go`'s two inline errors
(`"loopback: request carries no message"`,
`"loopback: request carries no payload"`) stay unexported plain
errors. A real `Client.Send` call can never trigger either one: `Send`
builds its request through `a2a.ToPart`, which calls `m.Encode()` on
an already-`Validate`d `envelope.Message` and always produces a
request carrying both a message and a payload. `a2a.ToPart` and
`grpcTransport.Send` construct that request; neither has a code path
that omits the payload. These two `loopbackExecutor.Execute` checks
guard against a malformed request no in-module caller can produce, so
they stay unreachable through this package's public API, not because
of any `StateFailed` surfacing. (A failed `Execute` call does still
propagate as a genuine error from `grpcTransport.Send` through
`Client.Send`, not through `Status`/`Result`, but that path needs a
request no real caller builds.) Adding a sentinel here would let a
caller match an internal test-fixture detail that carries no
contract.

`newGRPCTransport`'s dial-failure wrap
(`fmt.Errorf("a2aclient: open transport: %w", err)` in `New`) stays as
is: it already wraps the underlying `grpc.NewClient` error with `%w`,
so `errors.Is` against that underlying error already works. No new
sentinel needed there.

## Tests

Test files live directly in `a2aclient/`, not in a nested
`a2aclient/a2aclient_test/` directory. This differs from the flat
`<pkg>/<pkg>_test/` layout most other packages in this module use,
per `docs/plans/agents/PHASES.md`. The nested layout does not work
for this package: `sdk-standards.yml` scopes the third-party-import
exception to `a2aclient/*.go` only (verified with a live Semgrep run;
a single `*` does not cross a directory boundary), so a test package
in a nested directory could never import `a2a-go` to build a
transport double, and this module's coverage-floor computation
measures a package with a `<pkg>_test` directory only through that
directory, so any code reachable only through an unexported seam
would count as uncovered. Co-locating the test files in `a2aclient/`
resolves both constraints, but only if every test file is internal:
`client_test.go`, `client_integration_test.go`,
`client_concurrency_test.go`, `client_bench_test.go`,
`grpc_internal_test.go`, and `stub_transport_test.go` are all
`package a2aclient`, following the `flow/wave_select_internal_test.go`
whitebox pattern. None of the four client-facing files uses Go's
external-test convention (`package a2aclient_test`): every one of
them builds a `Client` around the `stubTransport` double through the
unexported `newFromTransport` constructor, so an external package
could not compile them. `stubTransport` and `newFromTransport` stay
unexported (see the design note above), so the whole suite must sit
inside the package, not split across an internal/external boundary.

- `client_test.go` — red-green unit cases for `New`, `Close`, `Send`,
  `Status`, and `Result` against a recorded a2a server transcript, not
  a live network call. Cases:
  - `New` rejects an empty `baseURL`.
  - `Send` maps a valid signed message to a task and returns a
    non-zero `TaskHandle`.
  - `Send` returns an error when `msg` fails `a2a.ToPart`'s
    validation (mirrors the `a2a` package's own failure cases).
  - `Status` returns each `State` value the transcript can produce.
  - `Status` and `Result` reject the zero `TaskHandle` with an error.
  - `Result` returns an error when the task is not in a terminal
    state.
  - `Result` returns an error when the mapped message fails
    `VerifySignature`, using a transcript that carries a tampered
    payload.
  - A simulated transport failure (the recorded transcript returns a
    connection-level error instead of a response) propagates from
    `Send`, `Status`, and `Result` as an error value, never a panic
    and never a zero-value success.
  - A `ctx` canceled before `Send`, and a `ctx` whose deadline expires
    between two `Status` polls, both return the `ctx` error through
    `Send`/`Status`/`Result`; `errors.Is` against `context.Canceled`
    and `context.DeadlineExceeded` both hold. These cases use a
    `stubTransport` that ignores `ctx` itself, so the assertion can
    only pass if `Client` performs its own `ctx.Err()` check; a
    transport that also checked `ctx` would let this pass even if
    `Client`'s own check were removed.
  - `Close` is idempotent: two calls both return nil, and the
    underlying transport's own `Close` is called exactly once, not
    once per `Client.Close` call.
  Each case asserts the failing behavior first and turns green once
  the implementation lands.
- `client_integration_test.go` — a contract test against a recorded
  a2a server transcript. Build a message, sign it with a generated
  ed25519 key through `envelope.Sign`, send it with `Send`, poll
  `Status` until it reaches `StateCompleted`, call `Result`, assert
  `VerifySignature` succeeds on the returned message, then call
  `Close`. No test in this package or its test directory opens a live
  network connection; the transcript stands in for the remote agent.
- `client_concurrency_test.go` — `TestConcurrentSendStatusResult` runs
  under `go test -race`. It starts multiple goroutines that each call
  `Send`, poll `Status`, and call `Result` on the same `*Client`
  against the recorded transcript, and asserts no case returns an
  unexpected error and the race detector reports nothing. This proves
  the doc comment's "safe for concurrent use" claim instead of only
  asserting it in prose.
- `client_bench_test.go` — `BenchmarkSendStatusResult` runs a full
  send-status-result cycle against the recorded transcript. Target:
  under ten milliseconds per cycle on the reference machine.
  `ReportAllocs` states the allocation count per run; the network
  transcript replay and the `a2a.ToPart`/`FromPart` calls are the
  expected allocation sources, so the budget is not zero.

## Verification

`make verify` passes for the new `a2aclient` package: gofmt, vet, the
python gates, the Semgrep scan (including the two rule changes below),
and the coverage floor at 85 for `a2aclient`. `make api-update` locks
`Client`, `New`, `Close`, `TaskHandle`, `State`, the `State` constants,
`State.String`, `Send`, `Status`, and `Result` into `api/a2aclient.txt`
in the same change as the code. The `a2a` package's own lock,
`api/a2a.txt`, does not change: this phase adds no symbol to `a2a`.

`policy/layers.json`'s new row, `"a2aclient": ["a2a", "envelope"]`,
must pass `python3 scripts/check_deps.py`. The row grants exactly two
edges; `a2aclient` may import nothing else inside this module.

### Semgrep: scoped stdlib-only exception

`semgrep/sdk-standards.yml`'s `sdk.go.stdlib-only-imports` rule is an
unconditional ERROR on any non-module-path import, with no
per-package exclusion today. Without a change, that rule flags
`a2aclient/client.go`'s import of `a2aproject/a2a-go` and Semgrep
fails `make verify` even after `check_gomod.py` accepts the
dependency. The build to implement, by the builder, in
`semgrep/sdk-standards.yml`:

- Add `"/a2aclient/*.go"` to `sdk.go.stdlib-only-imports`'s
  `paths.exclude` list, alongside the existing exclusions on that
  rule.
- Add a new rule, `sdk.go.a2aclient-scoped-third-party-import`,
  `severity: ERROR`, scoped to `paths.include: ["/a2aclient/*.go"]`.
  It reuses the same `pattern-regex` that finds a dotted-domain import
  string, and adds a second `pattern-not-regex` permitting only
  `"github\.com/a2aproject/a2a-go(/[^"\n]*)?"` in addition to the
  existing module-path exemption. Any other third-party import inside
  `a2aclient/*.go` still fires this rule.

Shipped addition to this rule, beyond the draft above:
`a2aclient/grpc.go` also imports `google.golang.org/grpc` directly, to
dial the `grpc.ClientConn` that `a2asdk.NewGRPCTransport` needs; see
the design notes above. The rule's `pattern-not-regex` carries a third
line permitting `"google\.golang\.org/grpc(/[^"\n]*)?"` alongside the
`a2a-go` line. AGENTS.md's exception sentence names both modules.

`scripts/check_semgrep_probes.py` gains a new probe case proving both
rules behave correctly together. The existing `PROBES` list writes
flat files at the scan root, which cannot exercise a path-scoped rule,
so this case needs its own manual block after the main `PROBES` loop,
not a `PROBES` tuple.

The `d5` fixture's exemption (`name.startswith("d5")`, around
`scripts/check_semgrep_probes.py:178-180`) does not fit here: `d5`
exists because its violation fixture is checked only through the
suppression grep, never through the normal semgrep-hits path, so it
is exempt from the hits check entirely. The new `a2aclient` fixtures
are the opposite case: they must fire through the normal semgrep
scan, so exempting them from the hits check would let a silently
non-firing rule pass. The fix is not the `d5`-style exemption. The
fix registers the new fixtures in `expected` and asserts their firing
explicitly, the same two steps the `PROBES` loop already does for
every other rule:

- Create an `a2aclient/` subdirectory under the probe's temp root.
- Write `a2aclient/viol_other_import.go`, importing an unrelated
  third-party path, for example `"github.com/other/pkg"`, and
  `a2aclient/clean_a2a_import.go`, importing
  `"github.com/a2aproject/a2a-go/a2aclient"`.
- After the scan, add both basenames to the `expected` dict:
  `expected["viol_other_import.go"] = "sdk.go.a2aclient-scoped-third-party-import"`
  and `expected["clean_a2a_import.go"] = "sdk.go.a2aclient-scoped-third-party-import"`.
  This keeps the "unlisted probe file fired rules" check from flagging
  them, the same way every `PROBES` entry already keeps itself out of
  that check.
- Add an explicit assertion block, parallel to the `PROBES` loop's own
  assertions: `sdk.go.a2aclient-scoped-third-party-import` must appear
  in `hits["viol_other_import.go"]` and must not appear in
  `hits["clean_a2a_import.go"]`; `sdk.go.stdlib-only-imports` must
  appear in neither, proving the scoped exclude took effect.
- Write one more file outside `a2aclient/`, for example
  `viol_other_import_outside.go`, importing the same unrelated
  third-party path, and assert `sdk.go.stdlib-only-imports` still
  fires on it. This proves the exclude is scoped to `a2aclient/*.go`
  only, not global, and this file follows the ordinary `PROBES` tuple
  shape since it needs no subdirectory.

### go.mod and go.sum: a named, closed dependency allowlist

`go.mod` needs two changes this phase must land together:

- The module's Go version moves from `go 1.22` to `go 1.25`. This is
  a module-wide side effect: `a2aproject/a2a-go` v0.3.15 declares
  `go 1.24.4` in its own `go.mod`, so this module's floor must meet or
  exceed that. Every package in this module builds under Go 1.25
  after this change, not only `a2aclient`.
- A `require` line for `github.com/a2aproject/a2a-go` at the version
  this phase pins, plus whatever `go mod tidy` adds beneath it.

The earlier draft of this plan claimed `check_gomod.py` could allow
"exactly one require line." That claim is false. Fetched against the
module's tagged source, `a2aproject/a2a-go`'s own `go.mod` at
`v0.3.15` requires:

```text
github.com/go-sql-driver/mysql v1.9.3
github.com/google/go-cmp v0.7.0
github.com/google/uuid v1.6.0
golang.org/x/sync v0.15.0
google.golang.org/genproto/googleapis/api ...
google.golang.org/grpc v1.73.0
google.golang.org/protobuf v1.36.6
filippo.io/edwards25519 v1.1.0        # indirect
golang.org/x/net v0.41.0              # indirect
golang.org/x/sys v0.33.0              # indirect
golang.org/x/text v0.26.0             # indirect
google.golang.org/genproto/googleapis/rpc ...  # indirect
```

`go mod tidy` in this module adds `require` lines for `a2aproject/a2a-go`
plus any of the above that `a2aclient`'s actually-imported packages
reach; Go's pruned module graph (Go 1.17+) may add fewer than this
full list if `a2aclient` never imports a package that needs, for
example, the MySQL driver. `check_gomod.py`'s fix does not count
lines. It checks each `require` module path against a named, closed
allowlist:

```python
ALLOWED_MODULES = {
    "github.com/a2aproject/a2a-go",
    "github.com/go-sql-driver/mysql",
    "github.com/google/go-cmp",
    "github.com/google/uuid",
    "golang.org/x/sync",
    "golang.org/x/net",
    "golang.org/x/sys",
    "golang.org/x/text",
    "google.golang.org/genproto/googleapis/api",
    "google.golang.org/genproto/googleapis/rpc",
    "google.golang.org/grpc",
    "google.golang.org/protobuf",
    "filippo.io/edwards25519",
}
```

`replace`, `exclude`, and `retract` directives stay fully rejected, no
exception. A `require` line for any module path outside
`ALLOWED_MODULES` still fails the gate.

`go.sum` gains one line per module-and-checksum pair for every module
in the resolved build list, not one line total. `check_gomod.py`
extends to `go.sum` in this phase, since the earlier draft left it
unscoped: it parses the first whitespace-separated token of each
non-empty `go.sum` line (the module path, before the version and the
`/go.mod` suffix some lines carry) and checks it against the same
`ALLOWED_MODULES` set. A `go.sum` entry for a module path outside that
set fails the gate the same way an unlisted `require` line does.

`go mod tidy`'s actual output is the source of truth. The builder
runs `go mod tidy` once `a2aclient/client.go` imports
`a2aproject/a2a-go`, records the resulting `require` and `go.sum`
module set, and trims `ALLOWED_MODULES` in the same change to match
that set exactly, dropping any module from the list above that
`go mod tidy` never adds. No unused permitted module stays in the
allowlist. A future version bump of `a2aproject/a2a-go` that changes
its dependency graph needs its own plan revision to `ALLOWED_MODULES`.

### AGENTS.md: the stated exception

AGENTS.md's Rules section states "No third-party dependencies.
Standard library only." as a repository policy, not only a gate
detail. AGENTS.md's own Enforcement ladder requires recording an
exception in the gate file itself; this phase needs the matching
policy sentence too, so the stated rule and the two gates above stay
in agreement.

The required edit, one sentence, added immediately after the existing
"No third-party dependencies. Standard library only." line:

```text
Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
`google.golang.org/grpc`; no other package may add a third-party
import without its own plan review.
```

This edit lands in AGENTS.md itself, a file outside this plan's
`docs/plans/` and `policy/layers.json` write scope. The user has
approved this exact sentence, naming both modules the shipped design
needs: `a2a-go` for the client, and `google.golang.org/grpc` for the
`grpc.ClientConn` dial the client's transport requires (see the design
notes above). The delivery loop's builder step makes the edit
alongside the `a2aclient` code and the gate changes above, in the same
reviewed change, the same way a builder updates
`docs/architecture.md` for a message-semantics change. This plan
records the exact sentence so the builder does not improvise the
wording.

### Summary

`make verify` passes only once all of the following land together in
one change: the `a2aclient` package and its tests; the
`policy/layers.json` row; the `go.mod` version bump and the
`a2aproject/a2a-go` require; the `check_gomod.py` allowlist for
`go.mod` and `go.sum`; the two `semgrep/sdk-standards.yml` rule edits
and the new `check_semgrep_probes.py` case; and the AGENTS.md sentence
above. `docs/architecture.md` does not change in this phase:
`a2aclient` adds no message-semantics rule, only a transport for the
existing envelope wire form.

### Gap fix verification

`make api-update` locks the nine new sentinel vars
(`ErrNoBaseURL`, `ErrNoTransport`, `ErrNoTaskID`, `ErrZeroTaskHandle`,
`ErrNotTerminal`, `ErrSignatureCheckFailed`, `ErrNoTask`,
`ErrNoResultMessage`, `ErrNoDataPart`) into `api/a2aclient.txt`, in the
same change as the code. No `policy/layers.json` edit: this addendum
adds no import edge.

`client_test.go` gains one `errors.Is` assertion per sentinel, next to
the existing case that already exercises that failure:

These test files sit in `package a2aclient` (internal, see the Tests
section above), so every sentinel below is unqualified:

- `TestNewRejectsEmptyBaseURL`: assert `errors.Is(err, ErrNoBaseURL)`.
- `TestNewFromTransportRejectsEmptyBaseURL`: assert
  `errors.Is(err, ErrNoBaseURL)`.
- `TestNewFromTransportRejectsNilTransport`: assert
  `errors.Is(err, ErrNoTransport)`.
- `TestSendRejectsEmptyTaskID`: assert `errors.Is(err, ErrNoTaskID)`.
- `TestStatusRejectsZeroTaskHandle`: assert
  `errors.Is(err, ErrZeroTaskHandle)`.
- `TestResultRejectsZeroTaskHandle`: assert
  `errors.Is(err, ErrZeroTaskHandle)`.
- `TestResultRejectsNonTerminalState`: assert
  `errors.Is(err, ErrNotTerminal)`.
- `TestResultRejectsTamperedSignature`: assert
  `errors.Is(err, ErrSignatureCheckFailed)`. `envelope.VerifySignature`
  (`envelope/sign.go`) returns a plain `errors.New` value with no
  exported sentinel, so a second `errors.Is` assertion against it is
  not possible from this package's tests. Assert instead that the
  underlying detail text survives the wrap:
  `strings.Contains(err.Error(), "signature does not match message content")`,
  the exact string `VerifySignature` returns for a tampered payload.
  This proves the double-`%w` wrap keeps the underlying error's
  message visible, the reason this call site uses two `%w` verbs
  instead of one, even though `errors.Is` can only reach the outer
  sentinel here.

`grpc_internal_test.go` gains, or strengthens, one case per `grpc.go`
sentinel:

- a `Send` case where the stub transport's response is not a `*Task`:
  assert `errors.Is(err, ErrNoTask)`.
- `TestResultRejectsUnmappableData` or a sibling case: assert
  `errors.Is(err, ErrNoDataPart)`.
- a case where the task carries no status message and no history:
  assert `errors.Is(err, ErrNoResultMessage)`.

## Addendum: Loopback extraction to a2aloopback

Status: planned, not yet built.

### Problem

`a2aclient/loopback.go` is production, non-`_test.go` source. It
stands up a real gRPC A2A server as a test fixture. It carries no
build tag, so any binary that imports `a2aclient` for its real
purpose, the client adapter, also compiles `loopbackExecutor`,
`Loopback`, and their imports: `a2agrpc`, `a2asrv`,
`a2asrv/eventqueue`, and `google.golang.org/grpc`'s server-side
machinery. `a2aclient`'s own production files, `client.go`, `grpc.go`,
and `state.go`, import only `github.com/a2aproject/a2a-go/a2a`
(confirmed by direct read: `grpc.go`'s `a2acore` alias) and
`github.com/a2aproject/a2a-go/a2aclient` (`a2asdk`), plus
`google.golang.org/grpc` for the dial. They never import `a2asrv`,
`a2agrpc`, or `eventqueue`. The server-side packages are dead weight
for every real caller: they exist only because `loopback.go` lives in
the same package.

### Decision

Extract `Loopback` and its two supporting types to a new package,
`a2aloopback`, following the `durablefence` precedent already in this
module: a leaf-ish package that ships real (non-`_test.go`) source but
carries a documented, convention-only rule that no production code may
import it. See `docs/plans/a2aloopback.md`.

This is not the file's original author changing their mind carelessly.
The file's own doc comment gives a real reason to keep `Loopback` out
of a `_test.go` file: `a2aack/a2aack_test`, an external test package in
a different directory, calls `a2aclient.Loopback()`, and Go does not
let an external test package import another package's `_test.go`
files. That constraint still holds. The fix is not to move `Loopback`
into `a2aclient/*_test.go`; it is to move it into its own ordinary
package, which any external test package, including `a2aack_test`, can
import exactly like any other production package. `durablefence`
already proves this shape works in this module:
`ledger/ledger_test/scenario_test.go` imports it today.

The alternative, gating `loopback.go` behind a build tag inside
`a2aclient` (the `ledger_sqlite` precedent), was rejected. `ledger`'s
tag keeps one build variant fully out of the default binary; nobody
needs `SQLiteStore` and `Loopback` at once. Here, `a2aclient`'s own
integration test, `grpc_loopback_integration_test.go`, needs `Loopback`
inside the same `make verify` run that also builds the default,
untagged `a2aclient` package: a tag-gated `loopback.go` would force
that test file onto the same tag, dropping it out of `make verify`'s
default coverage run and into a second, easy-to-forget target the way
`verify-ledger-sqlite` already is. `a2aack_test` would need the same
tag on every file in its directory, even the files that never call
`Loopback`, since a build tag applies per file, not per call site.
Extraction avoids all of this: no tag, one `make verify` run, every
caller opts in by import, not by build flag.

### What moves, what stays

- `a2aclient/loopback.go`: deleted. Its content moves, unchanged in
  behavior, to `a2aloopback/loopback.go`. `dataFromRaw` gets a private
  copy in `a2aloopback`, since `a2aclient/grpc.go`'s original is
  unexported and cannot be imported across packages.
- `a2aclient/grpc_loopback_integration_test.go`: stays in `a2aclient`,
  `package a2aclient`, unchanged in every assertion. Its two calls to
  `Loopback()` become `a2aloopback.Loopback()`, with a new import of
  `github.com/MiviaLabs/mivia-ai-sdk/a2aloopback`. This file is
  `_test.go`, so `scripts/check_deps.py` never checks its imports;
  `policy/layers.json`'s `a2aclient` row does not change and does not
  need to list `a2aloopback`.
- `a2aack/a2aack_test/integration_test.go`: `a2aclient.Loopback()`
  becomes `a2aloopback.Loopback()`. The file keeps its `a2aclient`
  import for `a2aclient.New`; it adds an `a2aloopback` import.
- `a2aack/a2aack_test/wait_test.go`: same change, same reasoning.
- `a2aack/a2aack_test/helpers_test.go`: no functional change, only a
  comment fix. Its package doc comment names `a2aclient.Loopback`; the
  builder updates the wording to `a2aloopback.Loopback` and to note the
  stdlib-only rule now holds outside `a2aclient` and `a2aloopback`,
  not `a2aclient` alone.
- No other caller exists. `grep -rln "a2aclient.Loopback"` across the
  module returns exactly these three files plus the two in
  `a2aclient` itself, confirmed at plan time.

`a2aclient`'s production surface, `Client`, `New`, `Close`, `Send`,
`Status`, `Result`, `TaskHandle`, `State`, and the nine sentinel
errors, does not change. No real, non-test external consumer of
`a2aclient` is affected: only the test fixture moves.

### policy/layers.json

`a2aclient`'s row stays `["a2a", "envelope"]`, unchanged: its
production files never imported anything that needs a new edge, and
losing `loopback.go` removes no internal edge either, since
`loopback.go` used only `a2a` and `envelope` among internal packages.

A new row: `"a2aloopback": ["a2a", "envelope"]`. See
`docs/plans/a2aloopback.md`.

### api/a2aclient.txt

`make api-update` removes three lines from `api/a2aclient.txt`:
`func Loopback(...)`, `func (e *loopbackExecutor) Cancel(...)`, and
`func (e *loopbackExecutor) Execute(...)`. The same three land in the
new `api/a2aloopback.txt`, with `Loopback` as the only exported
symbol; `loopbackExecutor`'s methods appear in the lock file the same
way they did in `a2aclient` today, since the api-lock generator
records every top-level func declaration regardless of receiver export
status.

### AGENTS.md

Four edits, applied by the builder alongside the code, per this
file's existing "AGENTS.md: the stated exception" precedent above.
Read `AGENTS.md` directly before editing; the exact current text is
quoted below so the builder does not have to locate or paraphrase it.

**Edit one, the `a2aclient/` bullet (`AGENTS.md:110-115`).** The
current bullet reads:

```text
- `a2aclient/` — the a2a-go client adapter: Client, New, Close,
  TaskHandle, State, Send, Status, Result. Imports a2a and envelope.
  Sends one task, polls its status, and fetches its result over
  a2aproject/a2a-go's gRPC transport; re-verifies the signature after
  every remote hop. The only package allowed to import a2a-go and its
  google.golang.org/grpc dial dependency.
```

Its last sentence, "The only package allowed to import a2a-go and its
google.golang.org/grpc dial dependency," becomes false the moment
`a2aloopback` exists: two packages import those modules, not one.
Replace that last sentence with:

```text
  One of two packages allowed to import a2a-go and its
  google.golang.org/grpc dial dependency; a2aloopback is the other.
```

**Edit two, the `a2aack/` bullet (`AGENTS.md:116-124`), not the
`a2aclient/` bullet.** The current bullet reads:

```text
- `a2aack/` — the remote step ack: Options, Options.Validate, Remote,
  Wait, and sentinels. Turns a remote A2A task round trip into an
  `agent.AckWait`. This is an edge adapter, not an ordinary block: its
  one purpose is to adapt a remote transport to the composition layer,
  so it may import `agent` for the `AckWait` type, one of two
  exceptions to the rule that a block never imports the agent
  (`dispatch` is the other). Imports a2aclient, agent, and envelope.
  Carries no a2a-go import of its own; the loopback test fixture is
  exported a2aclient surface.
```

Its last sentence, "Carries no a2a-go import of its own; the loopback
test fixture is exported a2aclient surface," already correctly says
`a2aack` itself carries no a2a-go import; it needs a different fix
than the `a2aclient` bullet does. Only its second half is stale: the
loopback fixture no longer lives on `a2aclient`'s exported surface.
Replace that last sentence with:

```text
  Carries no a2a-go import of its own; the loopback test fixture lives
  in a2aloopback.
```

**Edit three, a new bullet, placed immediately after the `a2aclient/`
bullet:**

```text
- `a2aloopback/` — the A2A loopback test fixture: `Loopback` starts a
  real gRPC A2A server on a loopback port for cross-package tests. No
  production code may import it, the same convention `durablefence`
  uses. Imports `a2a` and `envelope` internally, and
  `github.com/a2aproject/a2a-go`'s server-side packages plus
  `google.golang.org/grpc` externally, the same exception `a2aclient`
  carries, scoped to this package instead.
```

**Edit four, the Rules section's existing exception sentence
(`AGENTS.md:389`):**

```text
Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
`google.golang.org/grpc`; no other package may add a third-party
import without its own plan review.
```

replaced with:

```text
Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
`google.golang.org/grpc`; `a2aloopback` may import the same two
modules, scoped to its own gRPC test-server fixture; no other package
may add a third-party import without its own plan review.
```

This names the same two modules, not a new one: `a2aloopback` carries
the server-side half of the one already-approved `a2a-go` dependency,
split out of `a2aclient` for the reason above, not a new, arbitrary
third-party import.

### Verification

`make verify` passes only once all of the following land together:
the `a2aloopback` package and its tests (see
`docs/plans/a2aloopback.md`); the deletion of
`a2aclient/loopback.go`; the updated
`grpc_loopback_integration_test.go` and the three `a2aack_test` files;
the `policy/layers.json` new row; the two `semgrep/sdk-standards.yml`
new rules, `sdk.go.a2aloopback-scoped-third-party-import` and
`sdk.go.no-a2aloopback-import`, plus the `stdlib-only-imports`
exclude-list entry; the two `check_semgrep_probes.py` new probe pairs
(see `docs/plans/a2aloopback.md`); and the four AGENTS.md edits above.
`docs/architecture.md` does not change: this move adds no
message-semantics rule and changes no module in the dependency map,
only which package one existing module lives behind.

## Addendum: a mutation floor at 96

`scripts/mutation_denylist/a2aclient.json` locks a mutation floor of
96, from a measured 96.43% (27 killed, 1 survived). The survivor sits
in `grpcTransport.Send`'s `!ok || task == nil` guard against a
type-asserted-but-typed-nil `*a2acore.Task`. No test seam reaches
this branch: `newFromTransport` substitutes the `transport` interface
above `grpcTransport`, not the third-party `a2asdk.Transport` field
`grpcTransport` wraps, and that field is unexported. Reaching the
branch would need the real gRPC wire path, through
`a2aloopback.Loopback`, to return a response the a2a-go SDK's own
client unmarshals into a typed-nil `*a2acore.Task` inside a non-nil
`any` -- a case the SDK's own marshaling behavior, not this package's
code, would have to be made to produce. `make mutation-gate` includes
`a2aclient` at this floor.

## Addendum: mirror the whole upstream task state enum

This addendum is commit two of two. Commit one fixes
`longtermmemory`; see `docs/plans/longtermmemory.md`. The two commits
do not share a file. This commit changes `a2aclient` and `a2aack`
together, because `a2aack` reads `a2aclient.State`.

### Problem

`state.go` says `State` mirrors the a2a-go task state enum. It maps
five of the ten upstream values.

`a2a-go@v0.3.15/a2a/core.go` declares ten `TaskState` constants. Its
`TaskState.Terminal` returns true for completed, canceled, failed, and
rejected. `stateFromTaskState` maps rejected, auth-required,
input-required, and unknown to `StateUnspecified`.

Two failures follow.

- `Client.Result` returns `ErrNotTerminal` forever for a rejected
  task, because `State.terminal` is false for `StateUnspecified`.
- `a2aack.Wait` polls a rejected task until its own deadline. It then
  returns `ErrTimeout after unspecified`. A rejected task and a hung
  task look the same to the caller.

A shipped test pins the defect. `a2aclient/grpc_internal_test.go`
asserts `TaskStateRejected` maps to `StateUnspecified`.

### Decision: add four State constants

Add `StateRejected`, `StateAuthRequired`, `StateInputRequired`, and
`StateUnknown`. Append them after `StateCanceled` so no existing iota
value shifts.

This is an exported API change. Run `make api-update` and commit the
`api/a2aclient.txt` diff in the same commit. The lock gains four
constant lines. `api/a2aack.txt` gains nothing.

Rejected option: add `StateRejected` alone. It fixes the reported
hang, and it leaves three upstream states mapped onto a value that
means "no state". The doc claim about mirroring stays false, and a
poller blocked on auth still reports "unspecified".

- `State.terminal` returns true for completed, failed, canceled, and
  rejected. That set equals upstream `TaskState.Terminal` exactly.
- `stateFromTaskState` names all ten upstream constants. It maps
  `TaskStateUnspecified` to `StateUnspecified`. Its default stays
  `StateUnspecified`, for an upstream value added after this commit.
- `String` gains "rejected", "auth-required", and "input-required".
  Each is the upstream literal, so the vocabulary matches a2a-go.
- `StateUnknown` returns "unknown", also the upstream literal. The
  out-of-range default returns the same text. State the overlap in the
  constant comment: a message reading "after unknown" means either the
  upstream indeterminate state or a `State` outside the declared
  range. Keep the default text as it is, because
  `docs/packages/a2aclient.md` states it and a shipped test asserts
  it.

### Decision: a2aack fails fast on a state it cannot resolve

`a2aack/a2aack.go`, the `poll` switch, gains two cases.

- `StateRejected` joins `StateFailed` and `StateCanceled`. All three
  are terminal and none carries a result. The call returns
  `ErrRemoteFailed` wrapping the state name.
- `StateAuthRequired` and `StateInputRequired` also return
  `ErrRemoteFailed` wrapping the state name. Both states wait for
  client action. `a2aack` sends one message and never sends more, so
  polling can never resolve them.

`StateUnspecified` and `StateUnknown` stay in the default branch. Both
mean the state is not yet known, so the loop keeps polling and records
the name for the timeout message.

Tradeoff. A blocked task reuses `ErrRemoteFailed` instead of a new
sentinel. A caller that must tell "rejected" from "needs auth" reads
the wrapped state name. Add a separate sentinel only when a caller
needs to branch on it.

The sentinel's stated contract widens with its use. The
`ErrRemoteFailed` doc comment and `docs/packages/a2aack.md` must read
"failed, canceled, rejected, or a state `a2aack` cannot resolve".

### Tests

Each test below fails against today's code.

- `a2aclient/grpc_internal_test.go`,
  `TestGRPCTransportStateMapsEachTaskState` — extend the case map to
  all ten upstream constants and correct the rejected row to
  `StateRejected`. Today the rejected row expects `StateUnspecified`.
  The case count grows, so the diff adds assertion sites and removes
  none.
- `a2aclient/grpc_internal_test.go`,
  `TestStateTerminalMatchesUpstream` — new. For every upstream
  `TaskState` constant, assert
  `stateFromTaskState(ts).terminal() == ts.Terminal()`. Today it fails
  on rejected. It also fails any fix that marks a non-terminal
  upstream state terminal, so it is its own positive control.
Put the three `Client` and `String` tests below in a new file,
`a2aclient/state_test.go`, package `a2aclient`. `client_test.go` holds
429 lines against the 500 limit, so it has no room.

- `a2aclient/state_test.go`, `TestResultAcceptsRejectedTask` — new.
  Script `stubTransport` with `StateRejected` and a valid signed
  result. Assert `Result` returns the message. Today it returns
  `ErrNotTerminal`.
- `a2aclient/state_test.go`, `TestResultRejectsBlockedTask` — new.
  Script `StateAuthRequired`, then `StateInputRequired`. Assert
  `Result` returns `ErrNotTerminal` for each. It passes today, and it
  blocks a fix that makes every state terminal.
- `a2aclient/state_test.go`, `TestStateStringNamesNewStates` — new. A
  table asserting `String` against literal text for `StateRejected`,
  `StateAuthRequired`, `StateInputRequired`, and `StateUnknown`. A
  subtest name is not an assertion, so a table is the only cover here.
  Today the four constants do not exist.
- `a2aack/a2aack_test/failed_test.go`,
  `TestWaitFailsOnUnresolvableStates` — a new function holding a table
  with three rows: `StateRejected`, `StateAuthRequired`,
  `StateInputRequired`. For each row assert the `AckWait` error
  satisfies `errors.Is(err, a2aack.ErrRemoteFailed)`, does not satisfy
  `errors.Is(err, a2aack.ErrTimeout)`, and names the state. Today each
  row polls to the deadline and returns `ErrTimeout`. Leave the
  shipped `TestWaitFailsCorrectly` byte-identical. Do not fold it into
  the new table.
- `a2aack/a2aack_test/poll_invariants_test.go` — a new function
  holding a table with `StateSubmitted`, `StateWorking`,
  `StateUnspecified`, and `StateUnknown`. Assert each keeps polling
  and ends in `ErrTimeout`. This blocks a fix that fails on any state
  other than completed. Leave every shipped function in that file
  unchanged.

Positive controls already shipped: `wait_test.go` still resolves a
confirmed ack from `StateCompleted`, and `integration_test.go` still
runs the live loopback round trip.

### Doc updates in the same commit

Every site below lists the state set or the terminal set. The list
comes from a grep over `.md` for the `State` constant names and for
"terminal".

- `a2aclient/state.go` — the `State` doc comment and the constant
  block comment.
- `a2aclient/grpc.go` — the `grpcTransport.State` doc comment.
- `a2aack/a2aack.go` — the `ErrRemoteFailed` doc comment and the
  `poll` doc comment.
- `docs/packages/a2aclient.md` — the state list near line 18 and the
  terminal list near line 66.
- `docs/packages/a2aack.md` — the poll steps near line 41 and the
  `ErrRemoteFailed` entry near line 67.
- `docs/examples/a2aack.md` — the terminal state list near line 71.
- `docs/plans/a2aclient.md` — the API code block near line 87.
- `docs/plans/a2aack.md` — the poll contract in its Scope section.
- `docs/architecture.md` — the `a2aclient` and `a2aack` module-map
  bullets, near lines 288 and 307.

Re-run the grep after the edits. No remaining site may list five
states or three terminal states.

The envelope wire format does not change, so
`docs/architecture.md`'s envelope rationale section stays as it is.

### Verification

- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  `python3 scripts/check_prose.py`, and
  `python3 scripts/check_labels.py` pass.
- `make api-update` runs in this commit. `api/a2aclient.txt` gains
  four constant lines and nothing else.
- `go test -race ./a2aclient/... ./a2aack/...` passes.
- `make verify` passes, including `scripts/check_test_tampering.py`.
- `make mutation PKG=a2aclient` holds the stored floor of 95 in
  `scripts/mutation_denylist/a2aclient.json`. `mutation_tokenize.py`
  swaps operator tokens only, so a switch returning constants or
  string literals carries no mutation site. The floor is not at risk.
- Coverage floor of 85 holds for `a2aclient`, for `a2aack`, and for
  the total.
- `policy/layers.json` needs no change. `a2aack` already imports
  `a2aclient`.
- Do not add an `Allow-Test-Change` trailer. The one shipped
  assertion this commit corrects is a wrong expectation, and the diff
  adds assertion sites. If a rule still fires, report it as a finding
  instead of waiving it.

### Out of scope

`a2aclient/grpc.go` dials every remote agent with
`insecure.NewCredentials()`. Envelope payloads cross the network in
plaintext. That needs an options struct and a product decision, so it
is separate work. Do not change it here.
