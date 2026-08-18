# Phase 59: observability

Status: depends on the shipped `events` package only for precedent,
not for an import. `trace` ships as a pure leaf package with zero
internal imports, matching the `tools` and `trigger` precedent of "no
caller yet."

## Goal

Give a caller a structured trace of a multi-step run: `flow.Run`, a
`subagent` spawn tree, or an `agentrun.Runner`. Today a caller must
hand-wire `events.Bus` subscribers to see what ran, in what order,
and for how long. This phase ships one generic primitive, `Span`, and
one generator, `Tracer`, that any caller or later integration phase
can adopt. It defines the shape and the propagation mechanism only.

## Scope

Inside:

- `Span`: a named operation with a start time, an end time, a parent
  link, and caller-set string attributes.
- `SpanID`: a small, allocation-free identifier. The zero value means
  "no parent," so a root span's `ParentID` is the zero `SpanID`.
- `Tracer`: issues sequential `SpanID` values and links each new span
  to whatever span already sits in the caller's `ctx`.
- Context propagation: `Start` returns a `ctx` carrying the new span;
  `SpanFrom` reads it back. This follows `flow`'s own
  `LoopState`/`LoopStateFrom` and `Failure`/`FailureFrom` pattern: an
  unexported context-key type, a `with*` setter, and a `*From` getter
  that returns `(value, bool)`. `flow/loop.go`'s `loopContextKey` and
  `withLoopState` are the exact precedent this phase reuses.
- Attribute recording on a live or ended span through `SetAttribute`
  and a safe, copy-based `Attributes` reader.

Outside:

- Any OpenTelemetry, Jaeger, or Zipkin exporter. AGENTS.md permits no
  new third-party dependency outside the three named exceptions, and
  `trace` claims no exception. A caller maps `Span` fields onto
  whatever backend they use, the same way `provider` defines
  `Completer` with no concrete client.
- Any sampling policy. Every `Start` call creates a span. A caller who
  wants sampling wraps `Tracer.Start` in their own guard.
- Automatic instrumentation of `flow`, `agent`, `subagent`, or
  `agentrun`. This phase ships the primitive alone. Wiring `trace`
  into another package's run loop is that package's own later
  integration phase, matching how `tools` and `trigger` shipped with
  "no caller yet."
- An `events` import. `heartbeat` imports `events` only because a real
  caller emits its `MissedEvent` immediately. `trace` has no such
  caller in scope: the packages that would emit span-start and
  span-end events onto a bus are exactly the instrumentation targets
  this phase excludes. Importing `events` today for a constant no code
  reads would be speculative generality, which AGENTS.md's Building
  blocks section forbids. A later integration phase adds the `events`
  edge when a real emitter exists.
- A held `*events.Bus` field on `Tracer` or `Span`. Out of scope by
  the point above.

### Why `trace`, not `observability`

`trace` is the shorter, idiomatic name for this concern: Go's own
`runtime/trace` and the wider ecosystem's `context`-propagated tracing
libraries use "trace" for exactly this shape (a `Span`, a start and
end time, and ctx-carried linkage). No package or exported symbol in
this module already uses the name `trace`. `observability` is a wider
word covering metrics and logs this phase does not ship; the narrower
name states the scope honestly.

## API

The surface below is the lock target. It lands in `api/trace.txt` via
`make api-update`.

- `type SpanID uint64` — a small, comparable, allocation-free
  identifier. The zero value means "no parent."
- `type Span struct` — exported fields `ID SpanID`, `ParentID SpanID`,
  `Name string`, `Start time.Time`. The end time and the attribute map
  stay unexported, guarded by an internal mutex, so concurrent `End`
  and `SetAttribute` calls on one shared `*Span` stay race-free. A
  caller who fans a `ctx` out to goroutines, the way `flow`'s panel
  wave does, may pass the same parent span to several children safely.
- `func (s *Span) End()` — records the current time as the span's end
  time. A second call is a no-op; only the first call's time sticks.
- `func (s *Span) EndTime() time.Time` — the recorded end time, or the
  zero `time.Time` before `End` runs.
- `func (s *Span) Duration() time.Duration` — `EndTime` minus `Start`.
  Zero before `End` runs.
- `func (s *Span) SetAttribute(key, value string)` — records one
  key-value pair. A later call with the same key overwrites the
  earlier value. The backing map allocates on the first call only.
- `func (s *Span) Attributes() map[string]string` — a copy of the
  attribute map, safe to read without the span's lock. Returns an
  empty, non-nil map when no attribute was ever set.
- `type Tracer struct` — issues sequential `SpanID` values. The zero
  value is not usable; create one with `New`.
- `func New() *Tracer` — creates a `Tracer` with no spans started.
  It has no error path.
- `func (t *Tracer) Start(ctx context.Context, name string) (context.Context, *Span)`
  — creates a `Span` named `name`, sets its `ParentID` from the span
  already in `ctx`, if any, and returns a `ctx` carrying the new span
  alongside the span itself. This is the exported entry point; a
  caller never constructs a `Span` directly.
- `func SpanFrom(ctx context.Context) (*Span, bool)` — reads the
  `*Span` a `Start` call injected into `ctx`. The boolean is false
  when `ctx` carries no span, matching `flow.LoopStateFrom` and
  `flow.FailureFrom`'s exact shape.

No `Validate` method: `Span` and `Tracer` carry no field a caller sets
directly outside `Start`, so there is no invalid combination to reject.

## Tests

Tests live in `trace/trace_test/`, following the flat layout in
`docs/plans/agents/PHASES.md`.

- `span_test.go` — red-green unit cases: a span's `Duration` is
  `EndTime` minus `Start`; `Duration` is zero before `End` runs; a
  second `End` call does not move the end time; `SetAttribute`
  overwrites an existing key; `Attributes` returns an empty map before
  any `SetAttribute` call and a map with one entry after one call.
- `tracer_test.go` — red-green unit cases: `Start` on a bare `ctx`
  (no prior span) produces a root span, `ParentID` zero; `Start` on a
  `ctx` already carrying a span sets the new span's `ParentID` to the
  parent's `ID`; two `Start` calls on the same `Tracer` produce
  distinct `ID` values; `SpanFrom` on a `ctx` with no span returns
  `(nil, false)`.
- `nested_integration_test.go` — builds a three-level span tree: a
  root span starts a child, the child starts a grandchild, each
  through its own `ctx.Context` returned by the prior `Start` call.
  Asserts the grandchild's `ParentID` equals the child's `ID`, the
  child's `ParentID` equals the root's `ID`, and the root's `ParentID`
  is zero. Calls `End` on all three in leaf-to-root order and asserts
  each `Duration` is non-negative.
- `concurrent_test.go` — starts spans from many goroutines, each on
  its own `ctx` derived from one shared root `ctx`, under
  `go test -race`. Asserts every child's `ID` is unique and every
  child's `ParentID` equals the shared root's `ID`. A separate case
  calls `SetAttribute` and `End` concurrently on one shared `*Span`
  from several goroutines under `-race` and asserts no data race and
  exactly one recorded end time.
- `span_bench_test.go` — benchmarks `Tracer.Start` immediately followed
  by `(*Span).End`, with `testing.AllocsPerRun`. States the allocation
  budget: at most two heap allocations per `Start` call (the `Span`
  value and the `context.WithValue` node) and zero further allocations
  from `End` alone. A second benchmark measures `Start`, one
  `SetAttribute` call, and `End` together, and states its own budget
  of at most three allocations, the extra one being the attribute
  map's first bucket.

## Verification

- `make verify` passes: gofmt, vet, tests (including `-race`), the
  doc gate, the structure gate, the Semgrep scan, and the probes.
- The coverage floor for `trace` reaches 85, counted in the total.
- `api/trace.txt` lands via `make api-update`, committed in the same
  change as the code.
- `policy/layers.json` gains a `"trace": []` row before any code
  lands, matching this plan's zero-internal-import scope.
- `make bench` runs `span_bench_test.go`'s two benchmarks and reports
  allocs/op against the stated budgets.
- No new `docs/architecture.md` edit is required: this phase adds no
  message-semantics change and no new module in the message flow. A
  later integration phase that wires `trace` into `flow`, `agent`, or
  `subagent` updates the module map then.
