# Package toolcallctx plan

## Goal

Provide context-attached access to the current in-flight `provider.ToolCall` during agent loop tool execution.

## Scope

The package contains context getter and setter helpers for attaching and retrieving `provider.ToolCall` values.
All execution logic and loop orchestration remain outside this package.

## API

- `WithToolCall(ctx context.Context, call provider.ToolCall) context.Context`
- `ToolCallFromContext(ctx context.Context) (provider.ToolCall, bool)`

## Tests

Unit tests verify that `WithToolCall` attaches a call and `ToolCallFromContext` retrieves it accurately or reports false when absent.
TestToolCallContextRoundTrip

## Verification

`make verify` runs tests, layers check, and coverage floor.

## Addendum: per-batch dispatch order ledger

### Addendum goal

Provide a thread-safe dispatch order ledger published on context for concurrent tool execution.

### Addendum scope

Add `BatchOrder` type, constructor, settlement methods, and context attachment helpers.
All dispatch execution logic remains in `agentloop`.

### Addendum API

- `NewBatchOrder(dispatched []int) *BatchOrder`
- `WithBatchOrder(ctx context.Context, order *BatchOrder) context.Context`
- `BatchOrderFromContext(ctx context.Context) (*BatchOrder, bool)`
- `(*BatchOrder) Dispatched() []int`
- `(*BatchOrder) Settle(index int)`
- `(*BatchOrder) Settled(index int) bool`
- `(*BatchOrder) Changed() <-chan struct{}`
- `(*BatchOrder) UnsettledBefore(limit int) bool`

### Addendum tests

Names the tests covering `BatchOrder`:
TestBatchOrderContextRoundTrip
TestBatchOrderDispatchedIsSortedCopy
TestBatchOrderSettleIsIdempotentAndWakesWaiters
TestBatchOrderUnsettledBefore

### Addendum verification

`make verify` passes and statement coverage remains at 100%.
