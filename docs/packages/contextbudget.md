# Package reference: contextbudget

`contextbudget` is a pure, storage-agnostic way to state and check a
budget for one model call's context: a byte cap, an event or message
count cap, and a check for whether more content still fits. It holds
no data of its own beyond the two caps, and it does no I/O. The
exported surface below mirrors `api/contextbudget.txt`.

## Types

- `Limits` — the budget: `MaxBytes int`, `MaxEvents int`. Both fields
  default to zero. A zero `MaxBytes` means no byte cap; a zero
  `MaxEvents` means no event-count cap. Both zero means uncapped.
  `Limits` needs no constructor; a caller builds it with a struct
  literal.

## Methods

- `Limits.Validate()` — reports an error when `MaxBytes` or
  `MaxEvents` is negative. Checks `MaxBytes` first; when both are
  negative, only the `MaxBytes` message returns.
- `Limits.Fits(bytes, events int) bool` — reports whether `bytes` and
  `events` both stay at or under their respective caps. A zero cap
  always reports fit for its own dimension. `Fits` takes the
  candidate totals the caller already has; it keeps no running total
  of its own and does not call `Validate`.

## Invariants

- `Limits` holds no state beyond `MaxBytes` and `MaxEvents`; a caller
  tracks its own running byte and event counts and passes them to
  `Fits`.
- The caller owns the trim or summarize decision. `Limits` only
  reports whether content still fits.

## Cross-references

- [agent.md](agent.md) — `agent.Run` takes an optional
  `*contextbudget.Limits` and calls `Validate` once and `Fits` before
  every gated step's `wait` call; a run that does not fit returns
  `agent.ErrOverBudget`.

## Usage

```go
budget := contextbudget.Limits{MaxBytes: 4096, MaxEvents: 20}
if err := budget.Validate(); err != nil {
    // a negative field
}
if !budget.Fits(runningBytes, runningEvents) {
    // trim or summarize before the next call
}
```
