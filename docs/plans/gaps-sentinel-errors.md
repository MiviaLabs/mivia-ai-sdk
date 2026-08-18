# Plan: gap fixes, exported sentinel errors

Status: planned, not yet built. Revision 2, after plan-reviewer
findings. This is a build spec for the delivery loop, not a new
package plan. It collects five small, deliberate API changes across
five existing packages, found during the doc-review pass that added
Failure modes sections to `docs/packages/*.md`. Each package's own
`docs/plans/<pkg>.md` already carries the full design detail, under a
"Gap fix" subsection this plan adds. Read this file first for the
work list and order; read each package's plan for the exact code.

## Revision 2 changes

The plan-reviewer returned REVISE on the first draft. Two medium
findings, both fixed in this revision:

- `a2aclient.md`'s "Out of scope" paragraph for `loopback.go`'s two
  inline errors stated the wrong mechanism. The draft said these
  errors surface as `StateFailed` through `Status`/`Result`; traced
  against `a2a-go` v0.3.15, a failed `Execute` call instead propagates
  as a genuine `error` return through `grpcTransport.Send` into
  `Client.Send`, not through `Status`/`Result` at all. The paragraph
  now states the real reason these two errors are unreachable: `Send`
  builds its request through `a2a.ToPart`, which always encodes a full
  `envelope.Message` payload, so no real caller ever sends a request
  missing a message or a payload. The conclusion (no sentinel here)
  is unchanged.
- `dispatch.md`'s gap-5 `requestFailure` helper compared the response
  body's trimmed text against `ErrBadMethod.Error()` and
  `ErrBadRequest.Error()`. `dispatch/endpoint.go` enforces a fixed 1:1
  mapping between status code and sentinel in the same package, so the
  helper now switches on `resp.StatusCode` alone. This drops the
  `strings` import, the body trim, and the fragility of matching
  against a body a reverse proxy or other middleware might rewrite or
  truncate.

Two non-blocking observations, folded into the affected package plans:

- `a2aclient.md`'s `TestResultRejectsTamperedSignature` strengthening
  now states precisely what the double-`%w` wrap can prove:
  `envelope.VerifySignature` returns a plain, non-sentinel error, so
  the test asserts the underlying message text survives the wrap, not
  a second `errors.Is` match.
- `subagent.md`'s `TestNewMailboxRejectsBadCapacity` strengthening now
  covers both `capacity == 0` and `capacity == -1`, not zero alone.

## Goal

Give every inline error condition this audit found a matchable
sentinel, so a caller can use `errors.Is` instead of parsing an error
string. No behavior changes; no message text changes except where a
sentinel wrap needs one, noted per package below.

## Scope

Inside, five packages, five independent changes, no shared code:

1. `a2aclient` — nine new exported sentinels. See
   `docs/plans/a2aclient.md`'s "Gap fix: exported sentinel errors"
   section.
2. `mcp` — export `errNilProgressHandler` as `ErrNilProgressHandler`.
   See `docs/plans/mcp.md`'s "Gap fix: export the
   nil-progress-handler sentinel" section.
3. `tools` — export `errInvalidExecutionClass` as
   `ErrInvalidExecutionClass`. See `docs/plans/tools.md`'s "Gap fix:
   export the invalid-execution-class sentinel" section.
4. `subagent` — add `ErrInvalidCapacity` for `NewMailbox`'s
   non-positive-capacity check. See `docs/plans/subagent.md`'s "Gap
   fix: export the mailbox-capacity sentinel" section.
5. `dispatch` — make `Send` match its own server's `ErrBadMethod` and
   `ErrBadRequest` sentinels by status code, so a client-side caller
   can use `errors.Is`. No new exported symbol. See
   `docs/plans/dispatch.md`'s "Gap fix: Send matches ErrBadMethod and
   ErrBadRequest" section.

Outside: any change to `policy/layers.json`. Every edge these five
packages need already exists; this plan adds sentinel variables and
one client-side status check, never a new import. Outside also: any
change to `a2aack`, `agent`, or any other package that calls into
these five; none of their call sites break, since every changed
function keeps its signature and every changed sentinel keeps its
message text (`a2aclient.ErrNotTerminal` and
`a2aclient.ErrSignatureCheckFailed` keep the dynamic detail in the
wrap, not the sentinel itself).

## API

Five packages gain the following exported symbols. Each is declared
`var Err<Name> = errors.New("<pkg>: <message>")`, the naming
convention `a2aack`, `dispatch`, `mcp`, `subagent`, and `tools`
already use for their existing sentinels.

`a2aclient` (nine new vars, `client.go` unless noted):

- `ErrNoBaseURL`
- `ErrNoTransport`
- `ErrNoTaskID`
- `ErrZeroTaskHandle`
- `ErrNotTerminal`
- `ErrSignatureCheckFailed`
- `ErrNoTask` (`grpc.go`)
- `ErrNoResultMessage` (`grpc.go`)
- `ErrNoDataPart` (`grpc.go`)

`mcp`: `ErrNilProgressHandler`, renamed from `errNilProgressHandler`.

`tools`: `ErrInvalidExecutionClass`, renamed from
`errInvalidExecutionClass`.

`subagent`: `ErrInvalidCapacity`, new.

`dispatch`: no new exported symbol. `ErrBadMethod` and `ErrBadRequest`
already exist; this change makes `Send` reach them.

Every rename keeps the original error's message text unchanged.
`make api-update` must run once per package, after its code change,
and the resulting `api/<pkg>.txt` diff lands in the same commit as
that package's code. `api/dispatch.txt` gets no diff: `dispatch`'s
public surface does not change.

## Tests

Full detail lives in each package's own plan under its "Gap fix"
section. Summary, one bullet per package:

- `a2aclient`: `errors.Is` assertions added to eight existing cases in
  `client_test.go`, plus one new or strengthened case per `grpc.go`
  sentinel in `grpc_internal_test.go`.
- `mcp`: `TestCallToolWithProgressRejectsNilHandler` in
  `mcp/connect_test.go` gains an `errors.Is` assertion, replacing its
  current non-nil-only check.
- `tools`: `TestExecutionClassValidate` in
  `tools/tools_test/execution_profile_test.go` gains an `errors.Is`
  assertion on its `true`-want cases, replacing the current
  nil-versus-non-nil check.
- `subagent`: `TestNewMailboxRejectsBadCapacity` in
  `subagent/subagent_test/mailbox_test.go` gains an `errors.Is`
  assertion, replacing or joining its current `strings.Contains`
  check.
- `dispatch`: two new cases in `dispatch/dispatch_test/client_test.go`,
  `TestSendMatchesBadMethodSentinel` and
  `TestSendMatchesBadRequestSentinel`, each against a raw
  `httptest.NewServer` handler that mirrors `Endpoint.Handler`'s own
  bad-method or bad-request write.

No conformance vector changes: none of these five packages owns a
wire-format vector suite.

## Verification

Per-package order, one commit per package recommended, since each
change is independent and the plan-reviewer can approve them
separately:

1. `a2aclient`, then `mcp`, then `tools`, then `subagent`, then
   `dispatch`. This order runs the largest surface change first and
   the pure-test-side `dispatch` change last.
2. For each package: make the code change, run `make api-update`, run
   `make verify`, then update `docs/packages/<pkg>.md`'s Failure modes
   section (see the docs deliverable list below), then run
   `python3 scripts/check_prose.py` and
   `python3 scripts/check_labels.py` again over the doc edit.
3. After all five land: `python3 scripts/check_plan.py` and
   `python3 scripts/check_deps.py` must both pass, confirming no
   package lost its required plan sections and no import edge changed.
4. `make verify` must pass at the end: gofmt, vet, tests, the doc
   gate, the structure gate, the Semgrep scan, and the probes. The 85
   percent coverage floor holds for `a2aclient`, `mcp`, `tools`,
   `subagent`, `dispatch`, and the module total.

### docs/packages/*.md updates

Each Failure modes entry that names an unexported sentinel or a
weak-pin test today gets rewritten once the code change lands:

- `docs/packages/a2aclient.md`: replace the "This package returns
  plain errors, not sentinels" opening sentence and the five bullets
  under it with one bullet per new sentinel, naming the sentinel, its
  message, its call site, and the test file that pins it with
  `errors.Is`.
- `docs/packages/mcp.md`: change the `errNilProgressHandler` bullet to
  name `ErrNilProgressHandler` and drop the "unexported... weak pin"
  sentence, replacing it with the `errors.Is` pin.
- `docs/packages/tools.md`: change the `errInvalidExecutionClass`
  bullet to name `ErrInvalidExecutionClass` and drop the "unexported...
  weak pin" sentences, replacing them with the `errors.Is` pin.
- `docs/packages/subagent.md`: change the "`NewMailbox` returns a
  plain, non-sentinel error" bullet to name `ErrInvalidCapacity` and
  its `errors.Is` pin.
- `docs/packages/dispatch.md`: keep the existing `ErrBadMethod` and
  `ErrBadRequest` bullets' "weak pin" note about the `http.Handler`
  path unchanged, since that limit is real and permanent (an
  `http.Handler` cannot return a Go error). Add one sentence to each
  bullet naming the new `Send`-side `errors.Is` pin in
  `client_test.go`, so the doc states both the server-side limit and
  the client-side fix.

### Why no `dispatch` server-side fix

`Endpoint.Handler`'s `http.Handler` signature is
`func(http.ResponseWriter, *http.Request)`. Go's `net/http` package
defines that signature; `Endpoint.Handler` cannot change it without
breaking the `http.Handler` interface `Endpoint.Handler()` must
satisfy. This is a permanent, documented design shape, not a bug:
`http.Error` writing the status and body is the only channel an
`http.Handler` has toward its caller. `Send`, the one caller inside
this module that both issues the request and reads the response, is
the correct place for a matchable error, and this plan's `dispatch`
gap fix puts it there.
