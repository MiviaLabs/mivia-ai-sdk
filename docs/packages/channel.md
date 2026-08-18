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
  ships no implementation.

## Functions and methods

- `Question.Validate()` — rejects an empty `ID`, an empty `Recipient`,
  and an empty `Payload`, each with its own sentinel error.
- `Answer.Validate()` — rejects an empty `QuestionID`. Rejects nothing
  else.

## Sentinel errors

Use `errors.Is` to test these.

- `ErrEmptyID` — `Question.ID` is empty or whitespace-only.
- `ErrEmptyRecipient` — `Question.Recipient` is empty or
  whitespace-only.
- `ErrEmptyPayload` — `Question.Payload` is empty or whitespace-only.
- `ErrEmptyQuestionID` — `Answer.QuestionID` is empty or
  whitespace-only.

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

`channel` defines no wire format. `Question` and `Answer` carry no
JSON tags and cross no boundary inside this package; no conformance
vector applies. A caller that needs a wire form wraps the fields in
its own transport-specific type.

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
