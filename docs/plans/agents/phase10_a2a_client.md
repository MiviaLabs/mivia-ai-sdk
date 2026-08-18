# Phase 10: a2a client adapter

Status: future. Builds on the landed phase 9 `a2a` package. This phase
adds the a2a-go client. It sends a task, reads the status, and fetches
the result. This is the first external network call. See
`docs/research-agents.md` for the Go SDK decision.

This record tracks the phase. The build contract is
`docs/plans/a2aclient.md`; follow that plan, not the summary below, on
any conflict.

## Goal

Delegate a task to a remote agent through `a2aproject/a2a-go`. `Send`
creates the task. `Status` reads it. `Result` fetches the output. The
envelope arrives at the remote side intact and its signature still
verifies after the hop.

## Corrected design

An architect review of the original in-`a2a`-package sketch found it
broke the phase 9 design: bolting a network client onto the stdlib-only
`a2a` package would force every caller of the pure mapping functions to
carry the third-party dependency. A later plan-reviewer round found
five further gaps in the first `a2aclient` draft. The corrected design
in `docs/plans/a2aclient.md` fixes all of them.

- The client lands in a new top-level package, `a2aclient`, not inside
  `a2a`. `a2aclient` imports `a2a` and `envelope`. The `a2a` package
  gains no new exported symbol and no new import; `api/a2a.txt` does
  not change in this phase.
- `a2aclient` is the only package in the module allowed to import
  `a2aproject/a2a-go`. `policy/layers.json` declares that boundary
  through the row `"a2aclient": ["a2a", "envelope"]`; the third-party
  import itself is not a `policy/layers.json` concern, since that file
  tracks internal module edges, but no other package's plan may add
  the third-party dependency without its own review.
- `scripts/check_gomod.py` gains a named, closed allowlist, not a
  line-count rule. `require` and `go.sum` entries must name a module
  path from a fixed set: `a2aproject/a2a-go` and its verified
  dependency closure (`go-sql-driver/mysql`, `google/go-cmp`,
  `google/uuid`, `x/sync`, `x/net`, `x/sys`, `x/text`,
  `genproto/googleapis/api`, `genproto/googleapis/rpc`, `grpc`,
  `protobuf`, `filippo.io/edwards25519`). The gate keeps rejecting
  every `replace`, `exclude`, and `retract` directive, and any
  `require`/`go.sum` module path outside that set. See
  `docs/plans/a2aclient.md` for the full list and the trimming step
  the builder runs against the real `go mod tidy` output.
- `semgrep/sdk-standards.yml`'s `sdk.go.stdlib-only-imports` rule
  gains a scoped exclude for `a2aclient/*.go`, paired with a new rule,
  `sdk.go.a2aclient-scoped-third-party-import`, that permits only the
  `a2aproject/a2a-go` import path inside that directory and still
  rejects every other third-party import there.
  `scripts/check_semgrep_probes.py` gains a matching probe case.
- AGENTS.md's Rules section gains one sentence naming the exact
  permitted exception, so the stated policy and the two gates above
  stay in agreement. See `docs/plans/a2aclient.md` for the exact
  sentence; the builder applies it alongside the code.
- The constructor is concrete: `New(baseURL string) (*Client, error)`.
  The original sketch's `New(opts ...Option) (*Client, error)` named no
  `Option`, which is speculative generality with no caller. The
  concrete signature replaces it.
- `Client` gains a `Close() error` method. `a2aproject/a2a-go`'s gRPC
  transport holds a persistent `grpc.ClientConn`; `Close` forwards to
  the transport's teardown call and is idempotent.

This phase also carries a module-wide side effect: `go.mod`'s Go
version moves from `1.22` to `1.25`, because `a2aproject/a2a-go`
v0.3.15 declares `go 1.24.4`. Every package in the module builds under
the new version after this phase, not only `a2aclient`.

## Scope

Inside: the `a2aclient` package, wrapping `a2aproject/a2a-go`, and the
task lifecycle (`Send`, `Status`, `Result`). Outside: Agent Card
discovery and running a server. `a2aclient` is a client of a2a, not a
server, in this version. The `a2a` package stays untouched.

## API

See `docs/plans/a2aclient.md` for the full signatures. Summary:

- `type Client struct` wrapping the a2a-go client.
- `New(baseURL string) (*Client, error)`
- `(*Client).Close() error`
- `(*Client).Send(ctx context.Context, msg envelope.Message) (TaskHandle, error)`
- `(*Client).Status(ctx context.Context, h TaskHandle) (State, error)`
- `(*Client).Result(ctx context.Context, h TaskHandle) (envelope.Message, error)`
- `type TaskHandle struct` identifying the remote task.
- `type State int` with a closed `const` block and a `String` method.

The signature verifies again after every remote hop, inside `Result`.

## Tests

Test files live directly in `a2aclient/`, not a nested `_test/`
directory; see `docs/plans/a2aclient.md`'s Tests section for why:

- `client_test.go` — the red-green cases for the client against a
  recorded transcript: happy path, invalid message, tampered
  signature, zero `TaskHandle`, transport failure propagation, ctx
  cancellation and deadline expiry mid-poll, and `Close` idempotency.
  Start with the assertions. Confirm they fail on the empty phase.
  Implement and watch them pass.
- `client_integration_test.go` — run a contract test against a
  recorded a2a server transcript. Send a signed message, poll the
  status, fetch the result. Verify the signature after the hop, then
  `Close` the client.
- `client_concurrency_test.go` — `go test -race` case exercising
  `Send`/`Status`/`Result` from multiple goroutines on one `*Client`,
  proving the "safe for concurrent use" doc comment.
- `client_bench_test.go` — benchmark a full send-status-result cycle
  against the recorded transcript. Target under ten milliseconds.
  State the allocation budget.

## Verification

`make verify` passes. The coverage floor for `a2aclient` holds at 85.
`make api-update` locks the new symbols into `api/a2aclient.txt`; `a2a`
stays unchanged. `scripts/check_deps.py` passes against the new
`policy/layers.json` row. `scripts/check_gomod.py`'s new allowlist
accepts `a2aproject/a2a-go` and its named dependency closure in both
`require` and `go.sum`, and still rejects every `replace`, `exclude`,
`retract`, and out-of-allowlist directive. `semgrep/sdk-standards.yml`
gains the scoped exclude and the new `a2aclient`-only rule, proven by
a new `check_semgrep_probes.py` case. AGENTS.md's Rules section states
the one permitted exception. See `docs/plans/a2aclient.md` for the
full verification contract.
