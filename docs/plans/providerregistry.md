# Plan: providerregistry

Status: shipped. One composition package over the shipped `provider`
contract (docs/plans/provider.md). It imports `provider` only, plus
stdlib; no third-party import.

## Goal

Give a caller one place to hold several named `provider.Completer`
values and try them in a caller-chosen order. Route falls through to
the next name only when a caller-supplied predicate says the failure
is worth retrying on a different provider.

## Scope

Inside:

- A `Registry` type: a concurrency-safe, name-keyed collection of
  `provider.Completer` values. Register, look up, and list names.
- A `Route` method that runs `provider.RunTurn` against a caller-given
  order of registered names, in sequence, stopping at the first
  success and falling through only when the caller's `Retryable`
  predicate approves the failure.
- Sentinel errors for the registration and routing failure modes:
  `ErrNilCompleter`, `ErrBlankName`, `ErrDuplicateName`,
  `ErrUnknownName`, `ErrEmptyOrder`, and `ErrAllFailed`.

Outside:

- Cost or usage accounting. `provider.Response.Usage` already reports
  token counts per call; summing or pricing counts across a session
  is a later package's concern.
- A model catalog or capability metadata beyond
  `provider.Request.Model`. `provider` carries the model string on
  every request; this package adds no second source of truth.
- Retry or backoff within one named `Completer`. A caller composes
  `flow.RetryPolicy` or an equivalent loop around one `Route` call.
  Route's only retry behavior is falling through to the next name; it
  never repeats the same name twice in one call.
- Health checks, warm-up calls, or background provider probing. Route
  observes success or failure of the current call only.
- Streaming aggregation. Route calls `provider.RunTurn`, which owns
  the `Chat`/`ChatStream` dispatch and chunk aggregation.
- A `Remove` method. No current caller needs it; a future phase adds
  one when a caller does.

## API

The surface below is the lock target. It lands in
`api/providerregistry.txt` via `make api-update`.

- `type Registry struct { ... }` holds named `provider.Completer`
  values behind a `sync.RWMutex`, the shape `tools.Registry` uses.
  Unexported fields; built only through `New`.
- `func New() *Registry` builds an empty `Registry`. The only
  constructor; a caller never builds one by struct literal.
- `func (r *Registry) Register(name string, c provider.Completer) error`
  adds `c` under `name`. It rejects a nil `c` with `ErrNilCompleter`,
  checked before any method call on `c`. It rejects a blank name
  (empty after `strings.TrimSpace`) with `ErrBlankName`. It rejects a
  name already registered with `ErrDuplicateName`; it never replaces
  an entry.
- `func (r *Registry) Get(name string) (provider.Completer, bool)`
  resolves `name` to its registered `Completer`, or returns
  `(nil, false)` when `name` is absent.
- `func (r *Registry) Names() []string` lists every registered name.
  Order is unspecified; a caller that needs a stable order sorts the
  result itself.
- `type Retryable func(error) bool` is the fallback predicate Route
  consults after each failed attempt. A nil `Retryable` falls through
  on every error, mirroring `flow.RetryPolicy.Retryable`'s nil rule.
- `func (r *Registry) Route(ctx context.Context, req provider.Request, order []string, retryable Retryable) (provider.Response, error)`
  tries each name in `order`, in sequence, calling
  `provider.RunTurn(ctx, c, req)` for the `Completer` `Get` resolves.
  It returns the first successful `Response` at once. On a `RunTurn`
  error it checks `retryable(err)`: nil or true moves to the next
  name; false stops and returns that error unwrapped. It rejects an
  empty `order` with `ErrEmptyOrder` before any call. It rejects a
  name `Get` cannot resolve with `ErrUnknownName` naming the missing
  entry, and stops at once. It checks `ctx.Err()` before each attempt
  after the first; a canceled ctx returns `ctx.Err()`. It walks
  `order` once, in the caller's sequence.
- `ErrAllFailed` is Route's error when every name was tried and every
  attempt failed the `retryable` check. The returned error matches
  `ErrAllFailed` and the last attempt's error under `errors.Is`;
  `errors.Unwrap` yields the last attempt's error. Route builds it as
  an unexported wrap whose text and `errors.Is` semantics come from
  one `fmt.Errorf` `%w` wrap of both errors, since `fmt.Errorf`'s
  multi-`%w` form alone yields nil for `errors.Unwrap`.
- The six sentinels are test targets for `errors.Is`.

No second `Get`-like accessor for a single default `Completer`. A
caller that wants one provider builds an `order` of length one.

## Tests

Test files live in `providerregistry/providerregistry_test/`:

- `registry_test.go` — red-green cases for `Register`, `Get`, and
  `Names`. A valid registration resolves through `Get`. A nil
  `Completer` returns `ErrNilCompleter` and leaves the registry
  unchanged. A blank name, empty or all-whitespace, returns
  `ErrBlankName`; a padded name registers under the raw, untrimmed
  key. A duplicate name returns `ErrDuplicateName` and keeps the
  first `Completer` resolvable. `Get` on an unregistered name returns
  `(nil, false)`. `Names` after three registrations lists all three,
  checked as a set.
- `route_test.go` — red-green cases for `Route` over fake
  `Completer` values. An empty or nil `order` returns `ErrEmptyOrder`
  and calls no `Completer`. An `order` naming an unregistered name
  returns `ErrUnknownName`, names the entry, and calls no `Completer`
  past it. A single-name success returns that `Response` unchanged. A
  retryable first failure falls through and returns the second
  fake's `Response`, with both fakes called in order. A non-retryable
  first failure stops at once and returns that error unwrapped. A nil
  `Retryable` falls through on every error. An all-retryable order
  returns `ErrAllFailed`; `errors.Unwrap` yields the last fake's
  error. A fake that cancels ctx during its `Chat` stops the loop;
  Route returns `ctx.Err()` and never calls the remaining names.
- `route_integration_test.go` — two fakes through a real `Registry`
  and a real `provider.RunTurn` call, with no mock of `RunTurn`: the
  first fake's `Chat` fails, the second returns a valid `Response`
  for a non-streaming `Request`, and Route returns the second's
  `Response` under an always-retryable predicate.
- `route_bench_test.go` — benchmark Route against two no-I/O fakes on
  the first-name-succeeds path, one hundred sequential calls. Target
  under one microsecond per call. `AllocsPerRun` states the measured
  allocation budget.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `providerregistry` and for the
  total.
- `policy/layers.json` carries the `providerregistry` row importing
  `provider` only.
- `api/providerregistry.txt` lands via `make api-update` in the same
  change as the code, holding `Registry`, `New`, `Register`, `Get`,
  `Names`, `Retryable`, `Route`, and the six sentinels.
- `docs/architecture.md` carries a `providerregistry/` bullet
  describing the routing package and its one internal import.
- `AGENTS.md`'s Layout section carries a `providerregistry/` line.
- `docs/plans/agents/PHASES.md` carries the phase 56 entry.
- `docs/packages/providerregistry.md` documents the exported surface.
- This package defines no wire format, so it adds no conformance
  vector.
- No `agent` change lands with this package; `agent`'s row in
  `policy/layers.json` stays unchanged. Wiring `providerregistry`
  into `agent` is a later phase's plan.
