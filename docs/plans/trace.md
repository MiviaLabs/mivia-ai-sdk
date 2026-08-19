# Plan: trace

Status: shipped. `trace` ships as a pure leaf package
with zero internal imports. The shipped `events` package is precedent
only, never an import. This matches the `tools` and `trigger`
precedent of shipping with no caller yet.

## Goal

Give a caller a structured trace of a multi-step run: `flow.Run`, a
`subagent` spawn tree, or an `agentrun.Runner`. Today a caller must
hand-wire `events.Bus` subscribers to see what ran, in what order, and
for how long. This plan ships one generic primitive, `Span`, and one
generator, `Tracer`, that any caller or later integration phase can
adopt. It defines the shape and the propagation mechanism only.

## Scope

Inside:

- `Span`: a named operation with a start time, an end time, a parent
  link, and caller-set string attributes.
- `SpanID`: a small, allocation-free identifier. The zero value means
  "no parent," so a root span's `ParentID` is the zero `SpanID`.
- `Tracer`: issues sequential `SpanID` values and links each new span
  to whatever span already sits in the caller's `ctx`.
- Context propagation: `Start` returns a `ctx` carrying the new span;
  `SpanFrom` reads it back. The pattern follows `flow`'s own
  `LoopState`/`LoopStateFrom` and `Failure`/`FailureFrom` shape: an
  unexported context-key type, a `with*` setter, and a `*From` getter
  that returns `(value, bool)`. `flow/loop.go`'s `loopContextKey` and
  `withLoopState` are the exact precedent this plan reuses.
- Attribute recording on a live or ended span through `SetAttribute`
  and a safe, copy-based `Attributes` reader.
- Span retention: `Tracer.Spans` returns every started span in start
  order, so a composition layer's span tree reads after the run.

Outside:

- Any OpenTelemetry, Jaeger, or Zipkin exporter. AGENTS.md permits no
  new third-party dependency outside the three named exceptions, and
  `trace` claims no exception. A caller maps `Span` fields onto
  whatever backend they use, the same way `provider` defines
  `Completer` with no concrete client.
- Any sampling policy. Every `Start` call creates a span. A caller who
  wants sampling wraps `Tracer.Start` in their own guard.
- Automatic instrumentation of `flow`, `agent`, `subagent`, or
  `agentrun`. This plan ships the primitive alone. Wiring `trace` into
  another package's run loop is that package's own later integration
  phase, matching how `tools` and `trigger` shipped with no caller.
- An `events` import. `heartbeat` imports `events` only because a real
  caller emits its `MissedEvent` immediately. `trace` has no such
  caller in scope. The packages that would emit span-start and
  span-end events onto a bus are the instrumentation targets this plan
  excludes. Importing `events` today for a constant no code reads
  would be speculative generality, which AGENTS.md's Building blocks
  section forbids. A later integration phase adds the `events` edge
  when a real emitter exists.
- A held `*events.Bus` field on `Tracer` or `Span`. Out of scope by
  the point above.

### Why `trace`, not `observability`

`trace` is the shorter, idiomatic name for this concern. Go's own
`runtime/trace` and the wider ecosystem's `context`-propagated tracing
libraries use "trace" for exactly this shape. That shape is a `Span`,
a start and end time, and ctx-carried linkage. No package or exported
symbol in this module already uses the name `trace`. `observability`
is a wider word covering metrics and logs this plan does not ship. The
narrower name states the scope honestly.

## API

The surface below is the lock target. It lands in `api/trace.txt` via
`make api-update`.

- `type SpanID uint64` — a small, comparable, allocation-free
  identifier. The zero value means "no parent."
- `type Span struct` — exported fields `ID SpanID`, `ParentID SpanID`,
  `Name string`, `Start time.Time`. The end time and the attribute map
  stay unexported, guarded by an internal mutex. Concurrent `End` and
  `SetAttribute` calls on one shared `*Span` stay race-free. A caller
  who fans a `ctx` out to goroutines, the way `flow`'s panel wave
  does, may pass the same parent span to several children safely.
- `func (s *Span) End()` — records the current time as the span's end
  time. A second call is a no-op; only the first call's time sticks.
- `func (s *Span) EndTime() time.Time` — the recorded end time, or the
  zero `time.Time` before `End` runs.
- `func (s *Span) Duration() time.Duration` — `EndTime` minus `Start`.
  Zero before `End` runs.
- `func (s *Span) SetAttribute(key, value string)` — records one
  key-value pair. A later call with the same key overwrites the
  earlier value. The backing store is a lazily-grown key-value
  slice; the first call allocates once. A map would cost two
  allocations and break the three-allocation budget the Tests
  section enforces.
- `func (s *Span) Attributes() map[string]string` — a copy of the
  attribute map, safe to read without the span's lock. Returns an
  empty, non-nil map when no attribute was ever set.
- `type Tracer struct` — issues sequential `SpanID` values. Create one
  with `New`. Safe for concurrent `Start` calls.
- `func New() *Tracer` — creates a `Tracer` with no spans started.
  It has no error path.
- `func (t *Tracer) Start(ctx context.Context, name string) (context.Context, *Span)`
  — creates a `Span` named `name` and sets its `ParentID` from the
  span already in `ctx`, if any. It returns a `ctx` carrying the new
  span alongside the span itself. This is the exported entry point; a
  caller never constructs a `Span` directly.
- `func SpanFrom(ctx context.Context) (*Span, bool)` — reads the
  `*Span` a `Start` call injected into `ctx`. The boolean is false
  when `ctx` carries no span, matching `flow.LoopStateFrom` and
  `flow.FailureFrom`'s exact shape.

No `Validate` method: `Span` and `Tracer` carry no field a caller sets
directly outside `Start`. There is no invalid combination to reject.

## Tests

Test files live in `trace/trace_test/`, an external test package.

- `span_test.go` — red-green unit cases: a span's `Duration` is
  `EndTime` minus `Start`; `Duration` is zero before `End` runs; a
  second `End` call does not move the end time; `SetAttribute`
  overwrites an existing key; `Attributes` returns an empty map before
  any `SetAttribute` call and a map with one entry after one call. A
  copy case mutates the map `Attributes` returns. A second
  `Attributes` call still reports the span's values unchanged. A
  multi-key case overwrites a key that is not the first stored. A
  set after `End` still lands, and a set from before `End` survives.
- `tracer_test.go` — red-green unit cases: `Start` on a bare `ctx`
  (no prior span) produces a root span, `ParentID` zero; `Start` on a
  `ctx` already carrying a span sets the new span's `ParentID` to the
  parent's `ID`; the first `Start` issues ID one, and every later
  `Start` issues a strictly greater `ID`; `SpanFrom` on a `ctx` with
  no span returns `(nil, false)`.
- `nested_integration_test.go` — builds a three-level span tree: a
  root span starts a child, and the child starts a grandchild. Each
  level uses the `ctx` returned by the prior `Start` call. Asserts the
  grandchild's `ParentID` equals the child's `ID`, the child's
  `ParentID` equals the root's `ID`, and the root's `ParentID` is
  zero. Calls `End` on all three in leaf-to-root order and asserts
  each `Duration` is non-negative.
- `concurrent_test.go` — starts spans from many goroutines, each on
  its own `ctx` derived from one shared root `ctx`, under `go test
  -race`. Asserts every child's `ID` is unique and every child's
  `ParentID` equals the shared root's `ID`. A separate case calls
  `SetAttribute` and `End` concurrently on one shared `*Span` from
  several goroutines under `-race`. It asserts the race detector stays
  silent and `EndTime` is non-zero after the concurrent `End` calls.
- `span_bench_test.go` — holds two `Benchmark` functions and two
  `Test*AllocBudget` cases, following the `tools` precedent
  `TestRunAllocBudget` in `tools/tools_test/registry_bench_test.go`.
  The `Test*AllocBudget` cases use `testing.AllocsPerRun`, so a broken
  budget fails `go test` in `make verify`. One benchmark runs
  `Tracer.Start` immediately followed by `(*Span).End`. Its test
  asserts a ceiling of two allocations per run. The two are the `Span`
  value and the `context.WithValue` node. `End` alone makes zero
  further allocations. The second benchmark runs `Start`, one
  `SetAttribute` call, and `End` together. Its test asserts a ceiling
  of three allocations. The extra one is the attribute slice's first
  backing array.
- `span_fuzz_test.go` — a `Fuzz` target feeds random key-value pairs
  into one span. It asserts the last write per key wins and the entry
  count stays at the distinct-key count.

## Verification

`make verify` passes: gofmt, vet, tests (including `-race`), the doc
gate, the structure gate, the Semgrep scan, and the probes. The
coverage floor for `trace` reaches 85, counted in the total.
`api/trace.txt` lands via `make api-update`, committed in the same
change as the code.

`policy/layers.json` carries the `trace` row set to `[]`, in place
before any code lands, matching this plan's zero-internal-import
scope. `make bench` runs `span_bench_test.go`'s two benchmarks and
reports allocs/op against the stated budgets. The `Test*AllocBudget`
cases fail `go test` in `make verify` when a budget breaks.

No new `docs/architecture.md` edit is required. This plan adds no
message-semantics change and no new module in the message flow. A
later integration phase that wires `trace` into `flow`, `agent`, or
`subagent` updates the module map then.
