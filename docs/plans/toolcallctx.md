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

## Verification

`make verify` runs tests, layers check, and coverage floor.
