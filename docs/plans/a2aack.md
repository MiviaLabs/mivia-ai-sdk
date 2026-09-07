# a2aack

## Goal

`a2aack` turns a remote A2A task round trip into the composition
layer's `AckWait`. One `Wait` call sends a gated step as a remote
task, polls it, fetches its result, and resolves the step's ack.

## Scope

Inside:

- `Options` and `Options.Validate` configure the poll loop.
- `Remote` is the send, status, and result round trip one `AckWait`
  polls. `*a2aclient.Client` implements it.
- `Wait` returns the `AckWait` and validates eagerly. A nil `Remote`
  returns `ErrNoClient`. Invalid options return their error.
- The ack references the sent step message, not the server reply.
  `MessageID` keys off the step's own id; `From` and the restatement
  come from the verified result. `a2aack` re-verifies the result's
  signature after the remote hop.

Outside:

- Retry of a failed remote task stays with `flow.Step.Retry`.
- Any server side. Receiving A2A traffic is phase 52's stdlib
  endpoint and phase 53's `a2a-go` server. The `a2aclient.Loopback`
  test fixture is the one server exception.
- Any a2a-go import. `a2aack` imports none; its importers pay the
  grpc dependency only through `a2aclient`.

## API

```go
package a2aack

type Options struct {
	Poll    time.Duration
	Timeout time.Duration
}

func (o Options) Validate() error

type Remote interface {
	Send(ctx context.Context, msg envelope.Message) (a2aclient.TaskHandle, error)
	Status(ctx context.Context, h a2aclient.TaskHandle) (a2aclient.State, error)
	Result(ctx context.Context, h a2aclient.TaskHandle) (envelope.Message, error)
}

func Wait(c Remote, opts Options) (agent.AckWait, error)

var ( ErrNoClient, ErrNoPoll, ErrShortTimeout, ErrRemoteFailed, ErrTimeout )
```

The `AckWait` loop sends once, then polls `Status` every `Poll`,
bounded by `Timeout` and `ctx`. Completed fetches the result and
re-verifies its signature. Failed, canceled, and rejected return a
wrapped `ErrRemoteFailed`, and so do auth-required and input-required.
The deadline or `ctx` returns a wrapped
`ErrTimeout` with the last seen state. A Send, Status, or Result
error that is a context error maps to `ErrTimeout` with the last
seen state. Any other transport error propagates unwrapped.

## Tests

`a2aack/a2aack_test/` holds one external test package. A fake
`Remote` drives every loop, timing, and error outcome. The live
`a2aclient.Loopback` appears only in the happy-path and integration
tests. Cases cover the validation order, the failed and canceled
paths, the timeout path, the poll-loop invariants, the transport
errors, and one real `agent.Agent` resolving its step through `Wait`.

## Verification

`policy/layers.json` allows `a2aack` to import `a2aclient`, `agent`,
and `envelope`. `make api-update` lands `api/a2aack.txt`. `make
verify` passes and `a2aack` holds the 85 coverage floor.
## Addendum: states the poll loop cannot resolve
Status: superseded. Phase 86 folded this package into a2aclient;
the symbols live in a2aclient now. See
docs/plans/agents/phase86_package_consolidation.md.


`a2aclient.State` gains four constants. See the addendum "Mirror the
whole upstream task state enum" in `docs/plans/a2aclient.md` for the
design, the tests, and the doc sites. That addendum owns the change;
this note records its effect on the poll contract.

The `poll` switch gains two cases. `StateRejected` returns
`ErrRemoteFailed` beside `StateFailed` and `StateCanceled`. All three
are terminal and none carries a result. `StateAuthRequired` and
`StateInputRequired` also return `ErrRemoteFailed`. Both wait for
client action that `a2aack` never sends, so polling cannot resolve
them. The error names the state in each case.

`StateUnspecified` and `StateUnknown` stay in the default branch. The
loop keeps polling and records the name for the timeout message.

`ErrRemoteFailed` widens its stated contract. Its doc comment and
`docs/packages/a2aack.md` must read "failed, canceled, rejected, or a
state `a2aack` cannot resolve".

`api/a2aack.txt` does not change. The package gains no exported
symbol.

## Addendum: pin the result signer
Status: shipped.


`Options` gains one field, `ExpectSigner string`. When set, the
result's `Signer` must equal it. A mismatch fails with the new
exported error `ErrSignerMismatch`, and the step never confirms.
`VerifySignature` proves only that the result signer controls its own
key. Over a plaintext channel, an injected self-signed result would
otherwise confirm a step. An empty pin accepts any verifying signer;
loopback and tests use that form. The mismatch check runs after the
signature check and before `NewAck`.

`api/a2aack.txt` gains the field and the error. `docs/packages/
a2aack.md` states the pin contract beside the verification contract.

## Addendum: maintenance batch — return an unnamed ack resolver
Status: shipped.


This addendum is one of three that ship in one commit. See "Addendum:
maintenance batch — drop StaleMembers and the heartbeat edge" in
`docs/plans/room.md` for the batch.

### Addendum goal

Remove `a2aack`'s only reason to import `agent`. The `a2aack` row in
`policy/layers.json` becomes `["a2aclient", "envelope"]`.

### Addendum scope

`agent.AckWait` is declared at `agent/run.go:22`:

```go
type AckWait func(ctx context.Context, msg envelope.Message) (envelope.Ack, error)
```

Verified by grep: `command grep -n "agent\." a2aack/*.go` returns one
line, `a2aack/a2aack.go:68`, the `Wait` signature. The whole import
serves one type name in one return position.

Change `Wait` to return the unnamed type instead:

```go
func Wait(c Remote, opts Options) (func(context.Context, envelope.Message) (envelope.Ack, error), error)
```

The unnamed type is identical to `agent.AckWait`'s underlying type.
Go assignability lets the result stand wherever an `agent.AckWait` is
wanted, because at least one side of the assignment is unnamed. Every
existing call site therefore still compiles.

Call sites, all found by grep, all inside `a2aack/a2aack_test/`:
`options_test.go`, `wait_test.go`, `failed_test.go`,
`timeout_test.go`, `transport_error_test.go`, `signer_test.go`,
`poll_invariants_test.go`, and `integration_test.go`. No package
outside `a2aack` calls `Wait`. `integration_test.go:44` is the one
site that needs the assignability rule: it returns the value from
`integrationFixture`, whose declared result type is `agent.AckWait`.

Finding, reported instead of assumed. The nested external test
package `a2aack/a2aack_test` still imports `agent`, at
`integration_test.go:13`. It drives a real `agent.Agent` through
`agent.New` and subscribes to `agent.MessageAckedEvent`. That import
is the point of the integration test and must stay. The edge that
disappears is the production one. `scripts/check_deps.py` exempts test
files, and `policy/layers.json` carries no row for
`a2aack/a2aack_test`, so the narrowed row is correct and the gate
passes.

Tradeoff, stated plainly: the unnamed type reads worse than the named
alias at the call site. One type name does not justify a policy edge
into the composition layer.

Required test edit, forced by `go vet`. `make verify-fast` runs
`go vet ./...`. The printf rule reports a `%v` argument whose type is
an unnamed func type, and stays silent on the named `agent.AckWait`.
Baseline `go vet ./a2aack/...` passes, so this change causes the
failure:

```text
a2aack/a2aack_test/options_test.go:21:34: (*testing.common).Fatalf format %v arg ackFn is a func value, not called
```

The same report fires at `options_test.go:35` and `:49`. Fix all three
by dropping the func value from the failure message:

```go
- t.Fatalf("Wait(nil) AckWait = %v, want nil", ackFn)
+ t.Fatal("Wait(nil) AckWait must be nil")
```

Apply the equivalent edit at `:35` for `Wait(Poll=0)` and at `:49` for
`Wait(short timeout)`. The `ackFn != nil` comparison above each line is
the real assertion and does not change. Only the message loses the
value. `t.Fatal(` is in `_ASSERT_PATTERNS` at
`scripts/test_tampering_rules.py:146`, so the assertion count holds.

Lesson, recorded because this plan first missed it. The first draft
named `integration_test.go` as the assignability positive control and
traced no further. The break landed in `options_test.go`, one of the
seven files never traced. A type change can alter tool behavior in
every file that names the value, not only in the file that pins the
type. Trace all of them.

Nothing else changes. The poll loop, the sentinels, `Options`, and
`Remote` all keep their behavior. The `agent` import line leaves
`a2aack/a2aack.go`; `context` and `envelope` are already imported and
stay.

### Addendum API

`api/a2aack.txt` changes one line:

```text
- func Wait(c Remote, opts Options) (agent.AckWait, error)
+ func Wait(c Remote, opts Options) (func(context.Context, envelope.Message) (envelope.Ack, error), error)
```

`scripts/api_surface.go` prints the result type from the source, so
the lock line matches the source signature exactly. Run
`make api-update` and commit the diff it produces.

The `policy/layers.json` row change:

```text
- "a2aack": ["a2aclient", "agent", "envelope"]
+ "a2aack": ["a2aclient", "envelope"]
```

The row narrows in the builder's commit, not in this plan update.
Narrowing it before the import is gone fails
`scripts/check_deps.py`.

### Addendum tests

Three failure messages change. No test is added, deleted, renamed,
skipped, or weakened. `go vet` forces the three edits, and the scope
above gives them line by line at `options_test.go:21`, `:35`, and
`:49`. Every `ackFn != nil` comparison survives untouched.

`TT04` does not fire for `a2aack`. The rule counts `t.Fatal(` and
`t.Fatalf(` as assertion sites alike, so three rewrites move the count
by zero. `TT01` needs a dropped function and this change drops none.

`integration_test.go` is the positive control for the assignability
claim. It assigns the `Wait` result to an `agent.AckWait` result and
runs a real `agent.Agent`, so a wrong signature fails the build rather
than passing silently.

### Addendum verification

Commands:

- `make verify`.
- `go vet ./...` must be clean. Run it early; it is the gate this
  change breaks first.
- `python3 scripts/check_plan.py`.
- `python3 scripts/check_deps.py` passes with the narrowed `a2aack`
  row.
- `python3 scripts/check_api.py` after `make api-update`.
- `python3 scripts/check_docs.py`. `Wait`'s doc comment must still
  start with `Wait`.
- `python3 scripts/check_orphan_packages.py`. `a2aack` keeps its
  existing `policy/pending_wiring.json` entry; the reason text there
  names `agent.AckWait` as the adapted shape, which stays true.
- `python3 scripts/check_prose.py`.
- `python3 scripts/check_test_tampering.py`. It must report no
  `a2aack` finding. The commit-wide `TT11` finding comes from
  `policy/layers.json`; `docs/plans/room.md` records it.

Coverage: `a2aack` and the total must stay at or above 85. The
package measures 100.0 percent after the change. No executable line
is added or removed.

Doc sites, all found by grep and all landing in the same commit:

- `docs/architecture.md`, mermaid map: delete the `a2aack --> agent`
  edge line.
- `docs/architecture.md`, the `a2aack/` bullet: it says `a2aack`
  imports `a2aclient`, `agent`, and `envelope`. Drop `agent`. State
  that `Wait` returns a resolver assignable to `agent.AckWait`.
- `docs/packages/a2aack.md`: the opening paragraph says the package
  imports `agent` for the `AckWait` type. Replace that clause. The
  package now adapts the remote transport without importing the
  composition layer.

Leave these alone:

- `docs/examples/a2aack.md` binds the result with `:=` and passes it
  to `ag.Run`. Assignability keeps the sample correct as written.
- `docs/README.md` describes the purpose, not the import. It stays
  true.
- The quoted `AGENTS.md` bullet inside `docs/plans/a2aclient.md`
  records a former state of that file. It is history, not a live
  rule. Do not rewrite it.
