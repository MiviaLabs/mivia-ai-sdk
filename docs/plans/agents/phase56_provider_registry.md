# Phase 56: provider registry

Status: ready. Depends only on the shipped `provider` package
(docs/plans/provider.md). No other phase blocks it.

## Why this plan exists

The sibling repo `mivia-agent` runs a mature `internal/providerregistry`
that holds five LLM providers, a model catalog, and provider
switching. The SDK's `provider` package defines one contract,
`Completer`, for one model at a time. `provider`'s own plan scopes out
retry, backoff, rate limiting, cost tracking, and multi-provider
routing as future work. That gap is the largest blocker to
`mivia-agent` ever dropping its own registry for the SDK's.

This phase closes the routing half of that gap: a named collection of
`provider.Completer` values, plus an ordered fallback call across
them. It does not close the cost-tracking half. A later phase, planned
alongside this one under a working name of session or cost tracking,
owns usage and cost accounting; this plan does not depend on it and
does not name its file, since it may not exist yet.

## Goal

Give a caller one place to hold several named `provider.Completer`
values and try them in a caller-chosen order, falling through to the
next name only when a caller-supplied predicate says the failure is
worth retrying on a different provider.

## Scope

Inside:

- A `Registry` type: a concurrency-safe, name-keyed collection of
  `provider.Completer` values. Register, look up, and list names.
- A `Route` method that runs `provider.RunTurn` against a caller-given
  order of registered names, in sequence, stopping at the first
  success and falling through only when the caller's `Retryable`
  predicate approves the failure.
- Sentinel errors for the registration and routing failure modes,
  matching the pattern `tools.Registry` already sets: `ErrNilCompleter`,
  `ErrBlankName`, `ErrDuplicateName`, `ErrUnknownName`, `ErrEmptyOrder`,
  and `ErrAllFailed`.

Outside:

- Cost or usage accounting. `provider.Response.Usage` already reports
  token counts per call; summing or pricing those counts across a
  session is a separate, later concern, not this package's job.
- A model catalog or capability metadata beyond `provider.Request.Model`.
  `provider` already carries the model string on every request; this
  package adds no second source of truth for what a model can do.
- Retry or backoff within one named `Completer`. A caller composes
  `flow.RetryPolicy` or an equivalent loop around one `Route` call, or
  around one `Completer.Chat` call directly. `Route`'s only retry
  behavior is falling through to the next name; it never repeats the
  same name twice in one `Route` call.
- Health checks, warm-up calls, or background provider probing.
  `Route` observes success or failure of the current call only.
- Streaming aggregation logic. `Route` calls `provider.RunTurn`, which
  already owns the `Chat`/`ChatStream` dispatch and chunk aggregation.

## Package name and placement

The package is `providerregistry`, a new top-level directory,
`providerregistry/`. Two reasons decide the name over a shorter
SDK-idiomatic alternative such as `providers` or `route`:

- It matches `mivia-agent`'s own `internal/providerregistry` name
  exactly. A caller migrating from the sibling repo's package to this
  one renames an import path, not a type or a call site.
- The SDK already uses multi-word, no-underscore compound names for a
  composition-adjacent package: `agentrun`, `taskrun`, `durablefence`,
  `contextbudget`. `providerregistry` follows the same convention.

`providerregistry` imports `provider` only, and stdlib packages
`context`, `errors`, `strings`, and `sync`. It sits one layer above
`provider` in the import graph, the same position `mcp` holds over
`tools`: a leaf contract package below, one small composition package
above it. `providerregistry` adds no third-party import.

`policy/layers.json` gains one row:

```json
"providerregistry": ["provider"]
```

## API

The surface below is the lock target. It lands in
`api/providerregistry.txt` via `make api-update`.

- `type Registry struct { ... }` holds named `provider.Completer`
  values behind a `sync.RWMutex`, the same concurrency shape
  `tools.Registry` already uses. Unexported fields.
- `func New() *Registry` builds an empty `Registry`. The only
  constructor; a caller never builds a `Registry` by struct literal.
- `func (r *Registry) Register(name string, c provider.Completer) error`
  adds `c` under `name`. Rejects a nil `c` with `ErrNilCompleter`,
  checked before any method call on `c`. Rejects a blank name (empty
  after `strings.TrimSpace`) with `ErrBlankName`. Rejects a name
  already registered with `ErrDuplicateName`. `Register` never
  replaces an existing entry; a caller that wants to swap a provider
  removes it first through a future `Remove`, out of this phase's
  scope since no current caller needs it.
- `func (r *Registry) Get(name string) (provider.Completer, bool)`
  resolves `name` to its registered `Completer`. Returns `(nil, false)`
  when `name` is absent.
- `func (r *Registry) Names() []string` lists every registered name.
  Order is unspecified; a caller that needs a stable order sorts the
  result itself.
- `type Retryable func(error) bool` is the caller-supplied fallback
  predicate `Route` consults after each failed attempt. A `nil`
  `Retryable` falls through on every error, mirroring
  `flow.RetryPolicy.Retryable`'s "nil retries every error" rule so the
  two caller-supplied predicates in this SDK read the same way.
- `func (r *Registry) Route(ctx context.Context, req provider.Request, order []string, retryable Retryable) (provider.Response, error)`
  tries each name in `order`, in sequence, calling
  `provider.RunTurn(ctx, c, req)` for the `Completer` `Get` resolves.
  `Route` returns the first successful `Response` at once. On a
  `RunTurn` error, `Route` checks `retryable(err)`: a `nil` predicate
  or a `true` result moves to the next name; a `false` result stops
  the loop and returns that error unwrapped. `Route` rejects an empty
  `order` with `ErrEmptyOrder` before any call. `Route` rejects a name
  in `order` that `Get` cannot resolve with `ErrUnknownName`, naming
  the missing entry, and stops the loop at once rather than skipping
  it silently. When every name in `order` is tried and every attempt
  fails the `retryable` check, `Route` returns `ErrAllFailed` wrapping
  the last attempt's error through `fmt.Errorf`'s `%w`. `Route` checks
  `ctx.Err()` before each attempt after the first; a canceled `ctx`
  stops the loop and returns `ctx.Err()` instead of trying the
  remaining names.
- `ErrNilCompleter`, `ErrBlankName`, `ErrDuplicateName`,
  `ErrUnknownName`, `ErrEmptyOrder`, and `ErrAllFailed` are sentinel
  errors. Test each with `errors.Is`.

No second `Get`-like accessor for a single default `Completer`. A
caller that wants "the one provider" builds an `order` of length one.

## Tests

Test files live in `providerregistry/providerregistry_test/`, per
PHASES.md's flat layout.

- `registry_test.go` — red-green cases for `Register` and `Get`.
  Registering a valid `Completer` under a valid name, then resolving
  it through `Get`, succeeds. A nil `Completer` returns
  `ErrNilCompleter` and leaves the registry unchanged. A blank name
  (empty, or all-whitespace) returns `ErrBlankName`. Registering the
  same name twice returns `ErrDuplicateName` on the second call and
  keeps the first `Completer` resolvable. `Get` on an unregistered
  name returns `(nil, false)`. `Names` after three registrations
  contains all three, checked as a set, not an order.
- `route_test.go` — red-green cases for `Route`, using two or more
  fake `Completer` values built in the test package. An empty `order`
  returns `ErrEmptyOrder` and calls no `Completer`. An `order` naming
  an unregistered name returns `ErrUnknownName` and calls no
  `Completer` past the unresolved entry. A single-name `order` whose
  `Completer` succeeds returns that `Response` unchanged. A two-name
  `order` where the first `Completer` fails with a `retryable`-true
  error falls through and returns the second `Completer`'s successful
  `Response`; the test asserts both fakes were called, in order. A
  two-name `order` where the first `Completer` fails with a
  `retryable`-false error stops immediately: the test asserts the
  second fake was never called, and the returned error is the first
  fake's error, unwrapped. A `nil` `Retryable` falls through on every
  error, same as a predicate that always returns true. An `order`
  where every name fails and every failure is retryable returns
  `ErrAllFailed`, and `errors.Unwrap` on that error yields the last
  fake's error. A canceled `ctx`, canceled between two `order` entries
  by a test-controlled fake, stops the loop and returns `ctx.Err()`
  without calling the remaining names.
- `route_integration_test.go` — an integration case with two fake
  `provider.Completer` implementations wired through a real `Registry`
  and a real `provider.RunTurn` call, not a hand-rolled response: one
  fake's `Chat` returns an error, the other's `Chat` returns a valid
  `Response` for a non-streaming `Request`. `Route` with a
  two-name order and an always-retryable predicate returns the
  second fake's `Response`. This proves `Registry`, `Route`, and
  `provider.RunTurn` compose with no mock of `RunTurn` itself.
- `route_bench_test.go` — benchmark `Route` against two fakes that do
  no I/O, one hundred sequential calls, first-name-succeeds path.
  Target under one microsecond per call. `AllocsPerRun` states the
  measured allocation budget.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `providerregistry` and for the
  total.
- `policy/layers.json` carries a `providerregistry` row importing
  `provider` only, landed with this plan before the code.
- `api/providerregistry.txt` lands via `make api-update` in the same
  change as the code, holding `Registry`, `New`, `Register`, `Get`,
  `Names`, `Retryable`, `Route`, `ErrNilCompleter`, `ErrBlankName`,
  `ErrDuplicateName`, `ErrUnknownName`, `ErrEmptyOrder`, and
  `ErrAllFailed`.
- `docs/architecture.md` gains a `providerregistry/` bullet describing
  the routing package and its one internal import, in the same change
  as the code.
- `AGENTS.md`'s Layout section gains a `providerregistry/` line, in
  the same change as the code.
- `docs/plans/agents/PHASES.md` gains a phase 56 entry once this plan
  passes plan review, following the pattern phase 51 and phase 52 set.
- `docs/packages/providerregistry.md` documents the exported surface,
  matching the docs-maintenance convention already used for `provider`
  and `tools`.
- This phase adds no conformance vector. `providerregistry` defines no
  wire format; it composes in-process `provider.Completer` values.
- No `agent` change lands in this phase. `agent`'s row in
  `policy/layers.json` stays unchanged; wiring `providerregistry` into
  `agent` is a later phase's plan.
