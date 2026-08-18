# Package reference: usage

The usage package gives a caller a per-session running total of
`provider.Usage`. A caller calls `Record` once per completed model
call and reads the accumulated total for that session at any time.
`usage` adds no gate and no policy; it only counts. The exported
surface below mirrors `api/usage.txt`.

## Types

- `Accumulator` — one running `provider.Usage` total per session
  identifier. Safe for concurrent use. The zero value is not usable;
  create one with `New`.

## Functions and methods

- `New()` — creates an empty `Accumulator`.
- `WrapCompleter(sessionID, a, c)` — wraps one `provider.Completer`
  so every completed turn records its usage under sessionID in `a`.
  The composition seam for any provider consumer; a blank sessionID
  fails construction, and an erroring turn records nothing.
- `Accumulator.Record(sessionID, u)` — adds `u`'s four fields onto the
  running total keyed by `sessionID`. Creates the session's total on
  its first call; every later call for the same `sessionID` adds onto
  the existing total.
- `Accumulator.Total(sessionID)` — returns the current summed
  `provider.Usage` for `sessionID` and `true`, or the zero
  `provider.Usage` and `false` when no `Record` call has ever named
  that `sessionID`.
- `Accumulator.Reset(sessionID)` — clears the session's total back to
  zero, as if no `Record` call had ever named it. A no-op returning
  `nil` when `sessionID` has no prior `Record` call.

## Failure modes

Use `errors.Is` to test this.

- `ErrBlankSessionID` ("usage: sessionID must not be blank") —
  `Record` and `Reset` return it, wrapped, when `sessionID` is empty
  after `strings.TrimSpace`. The name and definition match the
  existing blank-identifier sentinels in this codebase:
  `tools.ErrBlankName`, `trigger.ErrBlankName`,
  `providerregistry.ErrBlankName`, and `scheduler.ErrBlankID`. Pinned
  by `usage/usage_test/accumulator_test.go`.

## Invariants

- `Record` rejects a blank `sessionID`, after trim, with
  `ErrBlankSessionID`, and leaves every existing session's total
  unchanged.
- `Record` creates a session's total on its first call for that
  `sessionID`; every later call adds onto the existing total, field by
  field (`PromptTokens`, `CompletionTokens`, `TotalTokens`,
  `CachedTokens`).
- `Total` returns `(provider.Usage{}, false)` for a `sessionID` with
  no prior `Record` call. It never panics.
- `Reset` rejects a blank `sessionID`, after trim, with
  `ErrBlankSessionID`. `Reset` on an unknown `sessionID` returns `nil`
  and changes nothing.
- A `Record` call after `Reset` starts a fresh sum; it never carries
  over a total from before the reset.
- `Accumulator` is safe for concurrent `Record`, `Total`, and `Reset`
  calls, for the same or different `sessionID` values; a mutex guards
  the internal map.

## Wire contract

`usage` defines no wire format. It carries in-process values only, the
same as `provider` and `contextbudget`; no conformance vector applies.

## Usage

```go
acc := usage.New()

_ = acc.Record("session-1", provider.Usage{
    PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
})
_ = acc.Record("session-1", provider.Usage{
    PromptTokens: 130, CompletionTokens: 25, TotalTokens: 155, CachedTokens: 90,
})

total, ok := acc.Total("session-1")
// total == provider.Usage{PromptTokens: 230, CompletionTokens: 45,
//     TotalTokens: 275, CachedTokens: 90}, ok == true
```

### What the program shows

`Record` sums each call's four fields onto the running total kept
under `"session-1"`. `Total` reads the accumulated sum at any point; a
caller who needs a dollar figure multiplies these fields by its own
current price outside this package, since `usage` ships no cost
conversion.
