# Package reference: contextsummary

`contextsummary` turns the messages a compaction drops into one
validated, bounded `Summary` document, through one bounded
`provider.Completer` call. A summary failure is a caller-visible
error; no structural fallback exists. Compaction policy lives in
`contextplan`; the loop wiring lives in `agentloop`. The exported
surface below mirrors `api/contextsummary.txt`.

## Types

- `Summary` — one validated summary document: `Objective`, `State`,
  `Decisions`, `OpenWork`, `Risks`. Data only: no tool, policy, or
  credential fields.
- `Summarizer` — one `provider.Completer` adapted to summary
  generation. Built through `NewSummarizer` only.

## Constants

- `MaxFieldBytes` — 2 KiB, the bound on every individual `Summary`
  text field and every list item.
- `MaxItems` — 32, the bound on every `Summary` list.
- `MaxExcerptTotalBytes` — 16 KiB, the bound on the whole
  source-excerpt section of one summarize prompt.
- `SummaryTimeout` — 20 seconds, the bound on one summarize call, even
  when the caller's ctx carries no deadline.
- `SummaryMessageName` — "context-summary", the
  `provider.Message.Name` of the injected summary message. Compaction
  preserves it through `PreserveNames`.

## Functions and methods

- `NewSummarizer(c provider.Completer) (*Summarizer, error)` — binds
  one Completer. A nil Completer wraps `ErrNilCompleter`.
- `(*Summarizer) Summarize(ctx, msgs) (Summary, error)` — makes one
  bounded Completer call over `msgs` and returns the validated
  `Summary`. Never retries. The prompt is two messages: a system
  message stating the task and the exact JSON reply schema, and a user
  message carrying bounded excerpts, newest first. The call sets
  `Stream` false, leaves `Model` empty, never reads `Request.Tools`,
  and never mutates `msgs`.
- `Summary.Validate()` — enforces valid UTF-8, no control characters,
  non-empty `Objective` and `State`, `MaxFieldBytes` per field and per
  item, at most `MaxItems` per list, no duplicate items, and no blank
  item.
- `Summary.Render()` — the deterministic text form: one labeled line
  or bullet per field, in field order.
- `SummaryMessage(s Summary) provider.Message` — renders `s` as one
  `RoleUser` message named `SummaryMessageName`, whose `Content` is
  `s.Render()`. The message passes `provider.Message.Validate`.
- `TokenEstimate(n int) int` — prices `n` bytes at `n/4` tokens,
  minimum one for non-zero input, zero for zero input.

## Failure modes

Use `errors.Is` to test these.

- `ErrNilCompleter` ("contextsummary: completer is required") —
  `NewSummarizer` returns it for a nil Completer. Pinned by
  `contextsummary/contextsummary_test/summarizer_test.go`.
- `ErrNoMessages` ("contextsummary: no messages to summarize") —
  `Summarize` returns it for an empty message list, before any
  Completer call. Pinned by
  `contextsummary/contextsummary_test/summarizer_test.go`.
- `ErrInvalidReply` ("contextsummary: reply failed strict parsing or
  validation") — `Summarize` returns it when the reply is empty,
  malformed, carries more than one markdown code fence, holds unknown
  fields or trailing bytes, or fails `Summary.Validate`. Decoding runs
  through `encoding/json` with `DisallowUnknownFields`. Pinned by
  `contextsummary/contextsummary_test/summarizer_test.go`.
- `ErrCallFailed` ("contextsummary: summary call failed") —
  `Summarize` returns it wrapping the Completer's own error. Pinned by
  `contextsummary/contextsummary_test/summarizer_test.go`.

## Invariants

- One call, one 20-second timeout, no retry. A Completer that fails
  once is called exactly once.
- The call is bounded three ways: excerpts cap the input at
  `MaxExcerptTotalBytes`, `SummaryTimeout` caps the duration, and
  strict parsing plus `Summary.Validate` cap the accepted output.
- The request carries no output-token cap; `provider.Request` has no
  such field.
- The reply decode accepts at most one enclosing markdown code fence.
- The excerpt walk runs newest first, caps each excerpt at
  `MaxFieldBytes` rune-safely, and stops at the first excerpt that
  does not fit the remaining `MaxExcerptTotalBytes`.
- The caller's ctx stays the outer authority: a canceled ctx fails the
  call with `ErrCallFailed` wrapping the ctx error.

## Cross-references

- `docs/plans/contextsummary.md` — the change contract.
- `docs/plans/contextplan.md` — the compaction policy whose `Compact`
  supplies the dropped messages.
- `docs/plans/agentloop.md` — the loop wiring that joins the two.

## Wire contract

`contextsummary` defines no wire format of its own. The reply it
decodes is an in-process `provider.Response`; no conformance vector
applies.

## Usage

```go
package main

import (
    "context"
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
)

func main() {
    s := contextsummary.Summary{
        Objective: "Ship the release",
        State:     "Two tests fail",
        Risks:     []string{"Deadline slips"},
    }
    fmt.Print(s.Render())
    fmt.Println(contextsummary.TokenEstimate(len(s.Render())))
}
```

### What the program shows

`Render` prints one labeled line per field and one bullet per list
item. `TokenEstimate` prices the rendered bytes at one token per four
bytes. A caller injects `SummaryMessage(s)` into its own history; the
summarizer itself never stores anything.
