# Phase 43: channel reference transport (NDJSON over stdio)

Status: future. Plan-only; it has not yet gone through plan review.
`channel` (folded from phase 37; see `docs/plans/channel.md`) has
shipped.

## Revision note

An earlier draft of this plan designed a terminal, human-readable
prompt and shipped it as a `docs/examples/` walkthrough only, reasoning
that no real caller existed yet to justify exported `channel` API. The
user has since stated a concrete, named integration target: wiring
`channel.Notifier` to `mivia-agent`'s desktop app over stdio, using the
same newline-delimited-JSON (NDJSON) convention that app's own `--json`
line mode and its `internal/hub` process-to-process protocol already
use. This revision replaces the terminal-prompt design with an NDJSON
stdio transport and reconsiders the package-vs-example call below.

## What `mivia-agent` actually does, verified before designing against it

This plan does not guess at a shape. It reads `mivia-agent`'s own
NDJSON code before designing this package's transport against it.

- `internal/cli/chat_json_writer.go`'s `writeNDJSONEvent` writes one
  JSON object per line: `json.Marshal(ev)`, then appends a single
  `'\n'` byte, then one `Write` call. Every line carries a `"type"`
  string field naming which of several optional fields is populated,
  with `omitempty` on every field that is not always present.
- `internal/hub/connection.go`'s `conn.writeLoop` writes its own
  `WireEvent` wire type through `json.NewEncoder(c.nc).Encode(ev)` in
  a loop; `Encoder.Encode` appends its own trailing newline per call,
  achieving the same one-object-per-line shape through the standard
  library's own encoder instead of a hand-rolled marshal-plus-append.
- `internal/hub/connection.go`'s `conn.readLoop` reads the other
  direction with `bufio.NewScanner(c.nc)`, sized with
  `sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)` (a 64 KB initial
  buffer, a 1 MB per-line cap), then `json.Unmarshal(sc.Bytes(), &w)`
  per scanned line. A line that fails to decode is skipped, not fatal,
  in that continuous multi-event stream reader.
- `internal/cli/chat_repl_linemode.go`'s own input side scans
  `os.Stdin` the same way: `bufio.NewScanner(os.Stdin)` with the same
  64 KB/1 MB buffer sizing.

This package's NDJSON `Notifier` reuses this exact shape: `Encoder.
Encode` on write, a sized `bufio.Scanner` plus `json.Unmarshal` on
read. It differs from `hub`'s `readLoop` in one deliberate way: a
`Notifier` call is a single, one-shot request-response
(`func(ctx, Question) (Answer, error)`), not a continuous, many-events
stream with no single caller waiting on a specific reply. A decode
failure on the one line this call reads back is therefore a real
error for that call, returned to the caller at once, not silently
skipped the way `hub.readLoop` skips a bad line inside its
fire-and-forget relay.

## Design: `NewNDJSONNotifier`

A `Notifier`-returning constructor, still entirely stdlib
(`encoding/json`, `bufio`, `io`, `context`, `fmt`, `errors`; no new
dependency):

- `func NewNDJSONNotifier(r io.Reader, w io.Writer) Notifier` returns
  a closure with `Notifier`'s exact signature. Calling it:
  1. Marshals `q` into an internal, `channel`-package-owned wire
     struct carrying a `"type":"question"` tag plus `id`, `recipient`,
     and `payload` fields, snake_case, matching the field-naming
     convention `mivia-agent`'s own `ndjsonEvent` uses. Writes it to
     `w` through `json.NewEncoder(w).Encode(...)`, the same call
     shape `hub.connection.go`'s `writeLoop` uses.
  2. Blocks reading one line from `r` through a sized `bufio.Scanner`
     (`64*1024` initial, `1024*1024` cap, matching `hub`'s and
     `chat_repl_linemode.go`'s own sizing exactly, not an arbitrary
     smaller number this package invents), respecting `ctx`: the
     blocking `Scan` call runs in a goroutine, and the closure selects
     on `ctx.Done()` against a channel that goroutine closes when
     `Scan` returns, the same select-on-ctx-or-done shape
     `docs/plans/agents/phase30_flow_retry.md`'s default `Sleep`
     already uses, adapted from a timer to a blocking read.
  3. `json.Unmarshal`s the scanned line into a matching
     `"type":"answer"` wire struct with `question_id`, `approved`, and
     `payload` fields. A decode error, or a scanned line whose `type`
     is not `"answer"`, or whose `question_id` does not match `q.ID`,
     returns a non-nil error at once; none of these three cases
     returns a partial or best-guess `Answer`.
  4. On success, returns `channel.Answer{QuestionID: <question_id>,
     Approved: <approved>, Payload: <payload>}` and a nil error.
  5. A `bufio.Scanner` error (including `io.EOF` with no line ever
     read, `bufio.ErrTooLong` past the 1 MB cap, or a canceled `ctx`)
     returns that error, wrapped `channel: ndjson: %w`, and no
     `Answer`.

The wire struct stays internal to `channel/ndjson_notifier.go`; it is
not `Question` or `Answer` with JSON tags added. `docs/plans/
channel.md`'s existing API section states this exact seam: "a caller
that wants a wire form wraps the fields in its own transport-specific
type." This transport is that wrapping, now shipped inside `channel`
itself instead of left for every caller to hand-write; `Question` and
`Answer` keep zero JSON tags, unchanged, matching the already-shipped,
already-locked `api/channel.txt` surface exactly.

`ErrAnswerMismatch` is a new sentinel, returned when a decoded line's
`question_id` does not match the `Question.ID` the same call sent.
This guards against a misbehaving or multiplexed peer answering out of
order; a `Notifier` call is always 1:1 by contract, so a mismatch is
always a protocol error, never a valid case to fall through silently.

## Decision, reconsidered: real `channel` package API, not example-only

The earlier draft of this plan shipped the terminal-prompt design as a
`docs/examples/` walkthrough only, reasoning that zero real callers
existed for any concrete `Notifier` implementation and that AGENTS.md's
Building-blocks rule needs a real consumer before an abstraction earns
library status. This revision reaches a different conclusion, because
the situation is materially different now:

- The stated goal is not an illustrative walkthrough; it is wiring a
  real product integration, the `mivia-agent` desktop app, over a real
  process boundary (stdio) using that app's own already-shipped wire
  convention. That is a real, named consumer, not a hypothetical one,
  even though the consumer lives outside this module: the desktop app
  spawns or is spawned alongside a process that needs a
  `channel.Notifier` speaking its NDJSON dialect, and every such
  wiring needs the identical encode/decode/buffer-sizing logic this
  plan designs once, here.
- Leaving this logic in a `docs/examples/` walkthrough would mean
  every real integration copies the same ~40 lines of `bufio.Scanner`
  sizing, `ctx`-cancellation plumbing, and wire-struct mapping out of
  a markdown code fence by hand, with no shared, tested, versioned
  implementation to import and no `api/channel.txt` lock holding its
  behavior stable across a future `channel` change. A copied,
  unlocked snippet is a worse outcome here than it was for the
  terminal-prompt case, because a real product depends on this one
  behaving correctly and consistently, not a reader following a
  tutorial.
- The cost side of the tradeoff stays the same as before: still
  stdlib-only, still a small, self-contained addition (one
  constructor, two unexported wire structs, one sentinel error) that
  does not widen `channel`'s Scope section's declared boundary against
  Slack, email, or webhook transports, which stay out of scope, per
  the original plan, exactly as before.
- This still respects AGENTS.md's Building-blocks rule: the rule asks
  for a real consumer, not a specific count of two. `channel.Notifier`
  itself already cleared that bar with three internal call sites
  before phase 37 shipped (`docs/plans/channel.md`'s own Goal
  section). A first concrete, named, product-real external consumer
  for a concrete implementation is the same kind of real-caller
  evidence the rule asks for; this plan does not wait for a second one
  before shipping the first transport whose caller is already named
  and whose wire shape is already fixed by that caller's own existing
  protocol.

This phase therefore ships `NewNDJSONNotifier` as exported `channel`
API, landing in `api/channel.txt`, not only in `docs/examples/`.

## Scope

Inside: `NewNDJSONNotifier`, the internal NDJSON wire structs for
`Question` and `Answer`, the `ctx`-aware blocking read, and
`ErrAnswerMismatch`. Inside: a short walkthrough,
`docs/examples/channel-ndjson-stdio.md`, showing the constructor wired
to `os.Stdin`/`os.Stdout`, matching every other package's existing
example-plus-package-API pairing (for example `tools` ships both a
`docs/packages/tools.md` reference and worked examples elsewhere in
this doc tree).

Outside: any change to `Question`, `Answer`, or `Notifier`'s
signature. Outside: any concrete wiring into `mivia-agent` itself;
that lives in `mivia-agent`'s own repository and its own change, not
in this module. Outside: Slack, email, webhook, or any other vendor
transport, exactly as the original plan already scoped out. Outside:
a bidirectional, multiplexed, many-questions-in-flight protocol; this
phase's `Notifier` stays 1:1 per call, matching `Notifier`'s own
signature, the same synchronous contract `docs/plans/channel.md`
already commits to. A caller needing to multiplex several concurrent
questions over one shared stdio pair builds that multiplexing layer
on top of one or more `NewNDJSONNotifier` closures, following
`hub.connection.go`'s own token-correlation pattern if it needs one;
this package does not build that layer itself.

`channel` gains no new import: `encoding/json`, `bufio`, `io`,
`context`, `fmt`, and `errors` are all standard library, already
implicitly available. `policy/layers.json`'s `"channel": []` row is
unchanged.

## API

The surface below lands in `api/channel.txt` via `make api-update`.

- `func NewNDJSONNotifier(r io.Reader, w io.Writer) Notifier` — builds
  a `Notifier` that writes one JSON-encoded question line to `w` and
  blocks reading one JSON-encoded answer line from `r`, in the
  newline-delimited-JSON shape described above. Respects `ctx`
  cancellation while waiting for the reply line.
- `ErrAnswerMismatch` — returned when a decoded answer line's
  `question_id` does not match the `Question.ID` the same call sent.
  Checked with `errors.Is`.

No change to `Question`, `Answer`, `Notifier`, either existing
`Validate` method, or the four existing sentinel errors
(`ErrEmptyID`, `ErrEmptyRecipient`, `ErrEmptyPayload`,
`ErrEmptyQuestionID`).

## Tests

Test files live in `channel/channel_test/`, the existing external test
package; this file adds no third-party import, so no Semgrep-scoping
concern applies here the way it does for `docs/plans/agents/
phase42_ledger_durable_store.md`'s `ledger` change.

- `ndjson_notifier_test.go` — red-green cases using an
  `io.Pipe`-backed reader/writer pair so the test drives both sides
  without touching the real filesystem or a subprocess:
  - A closure built over one end of two `io.Pipe`s, called with a
    valid `Question`; a goroutine reads the written line, asserts its
    decoded `type`, `id`, `recipient`, and `payload` fields match `q`
    exactly, then writes back a matching, well-formed answer line; the
    call returns the expected `Answer` and a nil error.
  - A goroutine that writes back an answer line whose `question_id`
    does not match the sent question's `ID`: the call returns
    `ErrAnswerMismatch`, checked with `errors.Is`, and a zero `Answer`.
  - A goroutine that writes back a malformed (non-JSON) line: the call
    returns a non-nil, wrapped error and a zero `Answer`.
  - A goroutine that writes back a well-formed JSON line whose `type`
    is not `"answer"`: the call returns a non-nil error and a zero
    `Answer`.
  - A goroutine that closes its write end without ever sending a line:
    the call returns a non-nil error (`io.EOF` or a `bufio.Scanner`
    error wrapped `channel: ndjson: %w`) and a zero `Answer`.
  - A `ctx` canceled from a second goroutine before any reply line
    arrives: the call returns promptly with `ctx.Err()`, under a
    test-level timeout, proving the closure does not block forever on
    a peer that never answers.
  - A line at or just under the 1 MB scanner cap decodes successfully;
    a line past the cap returns a non-nil error naming the overflow,
    proving the buffer sizing this plan states
    (`64*1024` initial, `1024*1024` cap) is the real, effective bound,
    not only a comment.
- `ndjson_notifier_integration_test.go` — imports `agent` directly,
  following the same cross-layer `_test`-subpackage precedent
  `docs/plans/channel.md`'s own Tests section already uses for
  `notifier_integration_test.go`: builds an `NewNDJSONNotifier`
  closure, wraps it in a second closure that sources the extra
  `envelope.Ack.From` field a fixed identity string supplies, and
  assigns the result to a variable of the real `agent.AckWait` type.
  This proves the NDJSON transport composes with the same real call
  site the terminal-prompt design would have targeted, over a real
  `io.Pipe` pair standing in for the desktop app's stdio pipe.
- `ndjson_notifier_bench_test.go` — benchmarks one `NewNDJSONNotifier`
  call round trip over an `io.Pipe` pair with a fixture goroutine
  answering immediately, reporting ns/op and allocs/op with a fixed
  allocation budget: `json.NewEncoder`'s per-call allocation and the
  `bufio.Scanner`'s fixed initial buffer are the expected allocation
  sources, so a budget is meaningful here, unlike a pure comparison
  benchmark.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block. `channel` adds no
  third-party import, so none of `docs/plans/agents/
  phase42_ledger_durable_store.md`'s Semgrep- or `go.mod`-scoping
  machinery applies to this phase.
- The coverage floor of 85 holds for `channel` and for the total, with
  every new line counted in.
- `api/channel.txt` gains `NewNDJSONNotifier` and `ErrAnswerMismatch`
  through `make api-update`, committed in the same change as the code.
- `policy/layers.json`'s `"channel": []` row is unchanged.
- `docs/packages/channel.md` gains an `NDJSON transport` section next
  to `Question`, `Answer`, and `Notifier`, describing the wire shape
  and the `mivia-agent` convention it mirrors.
- `docs/examples/channel-ndjson-stdio.md` is added, compiled and run
  against the real module before it is marked done: a small program
  wiring `NewNDJSONNotifier(os.Stdin, os.Stdout)` into a `channel.
  Question` call, with prose stating what a peer process (for example
  the desktop app) would write back to answer it. `docs/README.md`'s
  Examples list gains a matching one-line entry.
- No conformance vector change: `channel` still carries no signed or
  hash-chained wire form; the NDJSON shape here is a plain,
  application-level JSON line, not this module's envelope format.
