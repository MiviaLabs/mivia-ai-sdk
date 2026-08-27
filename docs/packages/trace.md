# Package reference: trace

The trace package gives a caller a structured trace of a multi-step
run. A `Span` records one named operation; a `Tracer` issues spans
and links them through `ctx`. `trace` is a leaf package: no internal
imports, no exporter, no sampling policy. The exported surface below
mirrors `api/trace.txt`.

## Types

- `SpanID` — identifies one `Span` within its `Tracer`. A `uint64`,
  comparable and allocation-free. The zero value means "no parent".
- `Span` — one named operation. Exported fields `ID`, `ParentID`,
  `Name`, and `Start`. `Tracer.Start` is the constructor; a caller
  never builds one directly. The end time and the attributes stay
  unexported.
- `Tracer` — issues sequential `SpanID` values through `Start`.
  Create one with `New`. Safe for concurrent `Start` calls.

## Functions and methods

- `New()` — creates a `Tracer` with no spans started. No error path.
- `Tracer.Spans()` — every started span in start order, non-nil and empty before the first start.
- `Tracer.Start(ctx, name)` — creates a `Span` named `name`, sets
  its `ParentID` from the span already in `ctx`, if any, and returns
  a `ctx` carrying the new span alongside the span itself. IDs start
  at one and never repeat.
- `SpanFrom(ctx)` — reads the `*Span` a `Start` call injected into
  `ctx`. Returns `(nil, false)` when `ctx` carries no span.
- `Span.End()` — records the current time as the span's end time. A
  second call is a no-op.
- `Span.EndTime()` — the recorded end time. The zero `time.Time`
  before `End` runs.
- `Span.Duration()` — `EndTime` minus `Start`. Zero before `End`
  runs.
- `Span.SetAttribute(key, value)` — records one key-value pair. A
  later call with the same key overwrites the earlier value.
- `Span.Attributes()` — a copy of the attribute map, safe to read
  and mutate without the span's lock. Empty and non-nil when no
  attribute was ever set.

## Failure modes

`trace` returns no error. `New` and `Start` cannot fail. `SpanFrom`
reports an absent span through its boolean, not an error. The `Span`
methods record values; they reject nothing.

## Invariants

- IDs start at one and never repeat, so the zero `SpanID` stays
  reserved for "no parent".
- `Start` on a `ctx` with no span produces a root span with a zero
  `ParentID`.
- `Start` on a `ctx` carrying a span sets the new span's `ParentID`
  to that span's `ID`.
- A second `End` call is a no-op; only the first call's time sticks.
- `EndTime` returns the zero `time.Time` before `End` runs.
- `Duration` is zero before `End` runs and non-negative after.
- `SetAttribute` overwrites an existing key.
- `Attributes` returns a fresh copy each call. Mutating the result
  never reaches the span.
- The attribute backing store is a lazily-grown key-value slice. The
  first `SetAttribute` call allocates once.
- `Tracer` is safe for concurrent `Start` calls; a `sync.Mutex`
  guards the counter.
- One shared `*Span` is safe for concurrent `End` and
  `SetAttribute` calls; a `sync.Mutex` guards the end time and the
  attributes. A parent span may serve several child goroutines, the
  way a `flow` panel wave fans one `ctx` out.

## Why this shape

Context propagation follows `flow`'s own `LoopStateFrom` and
`FailureFrom` shape: an unexported context key, a `with*` setter,
and a `*From` getter returning `(value, bool)`. A caller threads the
`ctx` a `Start` call returns; nesting comes free.

The attribute store is a slice, not a map. The first `SetAttribute`
call costs one allocation, where a Go map would cost two (header
plus first bucket). This holds the pinned three-allocation budget
for `Start` plus one `SetAttribute` plus `End`. `Attributes` still
returns a `map[string]string`.

`trace` imports no `events` bus and defines no exporter, sampling
policy, or event emitter. A caller wanting span events builds that
integration outside this package. See
[../plans/trace.md](../plans/trace.md) for the full design
rationale.

## Wire contract

`trace` defines no wire format. It carries in-process values only;
no conformance vector applies.

## Usage

```go
package main

import (
    "context"
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/trace"
)

func main() {
    tr := trace.New()

    rootCtx, root := tr.Start(context.Background(), "run")
    childCtx, child := tr.Start(rootCtx, "step")
    _, grand := tr.Start(childCtx, "step.retry")

    fmt.Println(grand.ParentID == child.ID) // true
    fmt.Println(child.ParentID == root.ID)  // true
    fmt.Println(root.ParentID == 0)         // true

    if s, ok := trace.SpanFrom(childCtx); ok {
        s.SetAttribute("attempt", "2")
    }
    grand.End()
    child.End()
    root.End()
    fmt.Println(root.Duration() >= 0) // true
}
```

### What the program shows

`Start` nests each level through the `ctx` the prior call returned.
The three `ParentID` checks print `true`. `SpanFrom` reads the child
back, `SetAttribute` records one pair, and leaf-to-root `End` calls
leave every duration non-negative.

## Exporting spans

`trace` ships no exporter. `Tracer.Spans()` is the export path: it
returns every started span in start order after a run ends. A caller
walks that slice and maps each `Span`'s fields onto whatever backend
they use, the same way a caller maps a `provider.Completer` response
onto their own request type.

```go
for _, s := range tr.Spans() {
    // Map s.ID, s.ParentID, s.Name, s.Start, s.EndTime(),
    // s.Duration(), and s.Attributes() onto a backend-specific
    // record: an OTLP span, a log line, a metrics point.
    emit(s.Name, s.Start, s.EndTime(), s.Attributes())
}
```

This pull pattern needs no exporter interface. `Spans()` already
gives a caller every field an exporter needs; a caller who wants a
push model wraps the walk in their own function. `trace` stays a
leaf package with zero internal imports, so it defines no backend
type to map onto.
