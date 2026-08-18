# Package reference: channel

The channel package gives every part of this SDK that must ask a
question and wait for a typed answer one shared shape to build a
closure from: `Question`, `Answer`, and `Notifier`. `channel` supplies
the shape only. It sends no bytes over any real transport; a caller
builds its own concrete implementation. The exported surface below
mirrors `api/channel.txt`.

## Types

- `Question` — one thing being asked: `ID`, `Recipient`, `Payload`.
  `ID` names the question so an `Answer` can reference it; the caller
  sets `ID`, `channel` never generates one. `Recipient` names who or
  what should answer. `Payload` carries the question's content as an
  opaque string.
- `Answer` — one response: `QuestionID`, `Approved`, `Payload`.
  `QuestionID` echoes the `Question.ID` it answers. `Approved` gives a
  yes-or-no reading for the common case. `Payload` carries free-form
  response content.
- `Notifier` — a caller-implemented func type:
  `func(ctx context.Context, q Question) (Answer, error)`. `channel`
  ships one reference implementation, `NewNDJSONNotifier`; a caller
  builds any other transport itself.

## Functions and methods

- `Question.Validate()` — rejects an empty `ID`, an empty `Recipient`,
  and an empty `Payload`, each with its own sentinel error.
- `Answer.Validate()` — rejects an empty `QuestionID`. Rejects nothing
  else.
- `NewNDJSONNotifier(r io.Reader, w io.Writer) Notifier` — builds a
  `Notifier` that speaks newline-delimited JSON over `r` and `w`. See
  the NDJSON transport section below.

## Failure modes

Use `errors.Is` to test these.

- `ErrEmptyID` ("channel: question id must not be empty") —
  `Question.Validate` returns it when `ID` is empty or
  whitespace-only. Pinned by `channel_test/question_validate_test.go`.
- `ErrEmptyRecipient` ("channel: recipient must not be empty") —
  `Question.Validate` returns it when `Recipient` is empty or
  whitespace-only. Pinned by `channel_test/question_validate_test.go`.
- `ErrEmptyPayload` ("channel: payload must not be empty") —
  `Question.Validate` returns it when `Payload` is empty or
  whitespace-only. Pinned by `channel_test/question_validate_test.go`.
- `ErrEmptyQuestionID` ("channel: answer question id must not be
  empty") — `Answer.Validate` returns it when `QuestionID` is empty
  or whitespace-only. Pinned by `channel_test/answer_validate_test.go`.
- `ErrAnswerMismatch` ("channel: ndjson: answer question id does not
  match question") — the `NewNDJSONNotifier` closure returns it when
  a decoded answer line's `question_id` does not match the sent
  `Question.ID`. Pinned by `channel_test/ndjson_notifier_test.go`.
- `ErrNotifierBusy` ("channel: ndjson: notifier is busy with another
  call") — the `NewNDJSONNotifier` closure returns it when a call
  arrives while another call on the same closure already holds its
  internal lock. Pinned by
  `channel_test/ndjson_notifier_lockout_test.go`.

## Invariants

- `Question.Validate` and `Answer.Validate` both trim with
  `strings.TrimSpace` before comparing; a whitespace-only string
  counts as empty.
- `Question.Validate` checks `ID`, then `Recipient`, then `Payload`,
  in that order; the first failing field's sentinel wins.
- `Answer.Validate` checks only `QuestionID`. A decline
  (`Approved: false`) needs no `Payload`, and `Approved` is a plain
  bool with no invalid state.
- `Notifier` is a func type with no method; `channel` enforces no
  context-cancellation behavior of its own. That is a `Notifier`
  implementation's concern.

## Wire contract

`channel`'s `Question` and `Answer` types themselves carry no wire
format: no JSON tags, no boundary crossed inside the package, no
conformance vector. A caller that needs a wire form wraps the fields
in its own transport-specific type. `NewNDJSONNotifier`, described
below, is the one reference wire form `channel` ships.

## NDJSON transport

`NewNDJSONNotifier(r io.Reader, w io.Writer) Notifier` builds a
`Notifier` that speaks newline-delimited JSON (NDJSON) over an
`io.Reader`/`io.Writer` pair, matching the convention `mivia-agent`'s
desktop app already uses for its own `--json` line mode and its
`internal/hub` process-to-process protocol.

### Wire shape

Calling the returned `Notifier` writes one question line to `w`:

```json
{"type":"question","id":"q1","recipient":"reviewer","payload":"proceed?"}
```

It then blocks reading one answer line from `r`:

```json
{"type":"answer","question_id":"q1","approved":true,"payload":"ok"}
```

Both wire structs are internal to `channel`; `Question` and `Answer`
keep zero JSON tags. The scanner that reads the answer line sizes its
buffer 64 KB initial, 1 MB cap, matching `mivia-agent`'s own
`hub.connection.go` and `chat_repl_linemode.go` sizing exactly.

### One caller at a time

The returned `Notifier` serves one call at a time, enforced with an
internal `sync.Mutex` and `TryLock`. `ctx` cancellation is honored
during both phases of a call:

- The write phase runs `Write` on `w` in a background goroutine and
  selects on `ctx.Done()`, exactly like the read phase already does
  for `r`. A blocked `Write` (for example an unbuffered pipe with no
  reader) does not stop a canceled `ctx` from returning `ctx.Err()` to
  the calling goroutine at once.
- A call that acquires the lock releases it only once whichever phase
  is pending truly finishes: the write completes or errors, or, once
  the write has succeeded, a line arrives, `r` errors, or `r` closes.
  It never releases early on `ctx` cancellation, because the pending
  phase's operation runs in a background goroutine that keeps going
  past that point.
- A call that fails to acquire the lock returns `ErrNotifierBusy` at
  once, touching neither `r` nor `w`.

**Permanent-lockout limit.** If the peer never drains `w`, never
answers, and never closes or errors `r` or `w` after a call's `ctx` is
canceled, the closure stays locked forever: every later call on that
closure returns `ErrNotifierBusy` indefinitely, misreporting a dead
peer as busy rather than gone. `ctx` cancellation frees the calling
goroutine from waiting; it does not free the closure for its future
callers. The recourse depends on which phase was pending: closing `w`
makes a stale blocked write return an error; closing `r` makes a stale
blocked read return an error. Either releases the lock and makes the
closure usable again.

A caller needing more than one concurrent question builds its own
correlation layer over more than one `NewNDJSONNotifier` closure, or
one closure per stdio pipe pair; this package does not multiplex one
shared stream itself.

### Sentinel errors

- `ErrAnswerMismatch` — a decoded answer line's `question_id` does not
  match the `Question.ID` the same call sent. Checked with
  `errors.Is`.
- `ErrNotifierBusy` — a call arrived while another call on the same
  closure already held its internal lock. Checked with `errors.Is`.

### NDJSON usage

See
[examples/channel-ndjson-stdio.md](../examples/channel-ndjson-stdio.md)
for a runnable walkthrough wiring `NewNDJSONNotifier` to
`os.Stdin`/`os.Stdout`.

## Usage

```go
package main

import (
    "context"
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// terminalNotifier is a minimal Notifier for demonstration: it never
// calls a real transport.
func terminalNotifier(ctx context.Context, q channel.Question) (channel.Answer, error) {
    return channel.Answer{QuestionID: q.ID, Approved: true, Payload: "ok"}, nil
}

func main() {
    q := channel.Question{ID: "q1", Recipient: "reviewer", Payload: "proceed?"}
    if err := q.Validate(); err != nil {
        panic(err)
    }
    a, err := terminalNotifier(context.Background(), q)
    if err != nil {
        panic(err)
    }
    fmt.Println(a.Approved, a.Payload)
}
```

### What the program shows

`terminalNotifier` stands in for a real transport such as a terminal
prompt or a Slack call. It echoes `q.ID` into `Answer.QuestionID` and
approves unconditionally. The program prints `true ok`.
