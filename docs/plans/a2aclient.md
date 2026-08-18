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
