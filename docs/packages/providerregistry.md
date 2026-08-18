# Package reference: providerregistry

The providerregistry package holds named `provider.Completer` values
and routes one request across them in a caller-chosen order. `Route`
falls through to the next name only when the caller's `Retryable`
predicate approves the failure. The exported surface below mirrors
`api/providerregistry.txt`.

## Types

- `Registry` — completers keyed by name. Safe for concurrent use. The
  zero value is not usable; create one with `New`.
- `Retryable` — the caller-supplied fallback predicate `Route`
  consults after each failed attempt. A nil `Retryable` falls through
  on every error.

## Functions and methods

- `New()` — creates an empty `Registry`.
- `Registry.Register(name, c)` — adds `c` under `name`. Rejects a nil
  `c`, a blank name, and a duplicate name. Never replaces an entry.
- `Registry.Get(name)` — resolves `name` to its registered
  `Completer`. Returns `(nil, false)` when `name` is absent.
- `Registry.Names()` — lists every registered name. Order is
  unspecified; sort the result when a stable order matters.
- `Registry.Route(ctx, req, order, retryable)` — tries each name in
  `order`, in sequence, calling `provider.RunTurn` for the
  `Completer` `Get` resolves. Returns the first successful `Response`.

## Failure modes

Use `errors.Is` to test these.

- `ErrNilCompleter` ("providerregistry: completer must not be nil") —
  `Registry.Register` returns it for a nil `Completer`, checked
  before any method call on it. Pinned by
  `providerregistry/providerregistry_test/registry_test.go`.
- `ErrBlankName` ("providerregistry: name must not be blank") —
  `Registry.Register` returns it when `name` is empty after
  `strings.TrimSpace`. Pinned by
  `providerregistry/providerregistry_test/registry_test.go`.
- `ErrDuplicateName` ("providerregistry: name already registered") —
  `Registry.Register` returns it for a name already present. The
  first registration stays resolvable. Pinned by
  `providerregistry/providerregistry_test/registry_test.go`.
- `ErrUnknownName` ("providerregistry: unknown name") —
  `Registry.Route` returns it for a name in `order` that `Get` cannot
  resolve. The error text names the missing entry. Pinned by
  `providerregistry/providerregistry_test/route_test.go`.
- `ErrEmptyOrder` ("providerregistry: order must not be empty") —
  `Registry.Route` returns it for an order with no entries, before it
  calls any `Completer`. Pinned by
  `providerregistry/providerregistry_test/route_test.go`.
- `ErrAllFailed` ("providerregistry: every name in order failed") —
  `Registry.Route` returns it when every name was tried and every
  attempt failed the `retryable` check. The returned error matches
  `ErrAllFailed` and the last attempt's error under `errors.Is`;
  `errors.Unwrap` yields the last attempt's error. Pinned by
  `providerregistry/providerregistry_test/route_test.go`.

## Invariants

- `Register` rejects a nil `c` with `ErrNilCompleter`, before it calls
  any method on `c`. A typed nil pointer that implements `Completer`
  is caller error; see `ErrNilCompleter`.
- `Register` rejects a blank name, after trim, with `ErrBlankName`,
  and stores every accepted name under its raw, untrimmed form.
- `Register` rejects a duplicate name with `ErrDuplicateName`. It
  never overwrites an existing registration.
- `Get` returns `(nil, false)` for an unknown name. It never panics.
- `Registry` is safe for concurrent `Register`, `Get`, `Names`, and
  `Route`; a `sync.RWMutex` guards the map.
- `Route` rejects an empty `order` with `ErrEmptyOrder` before any
  call.
- `Route` rejects a name `Get` cannot resolve with `ErrUnknownName`
  naming the missing entry. It stops at once; it never skips the
  entry or calls a later `Completer` past it.
- `Route` returns the first successful `Response` at once.
- On a `RunTurn` error, `Route` consults `retryable`: nil or true
  moves to the next name; false stops and returns that error
  unwrapped.
- `Route` checks `ctx.Err()` before each attempt after the first. A
  canceled ctx stops the loop and returns `ctx.Err()`.
- `Route` walks `order` once, in the caller's sequence. It never
  repeats or skips a name on its own; a caller that repeats a name in
  `order` gets that attempt sequence.
- When every name was tried and every attempt failed the `retryable`
  check, `Route` returns the `ErrAllFailed` wrap. It carries the last
  attempt's error; `errors.Unwrap` yields that error.

## Wire contract

`providerregistry` defines no wire format. It composes in-process
`provider.Completer` values; no conformance vector applies.

## Usage

```go
type echoCompleter struct{ label string }

func (e echoCompleter) Name() string { return e.label }

func (e echoCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
    last := req.Messages[len(req.Messages)-1]
    return provider.Response{
        Model:        req.Model,
        Message:      provider.Message{Role: provider.RoleAssistant, Content: e.label + ": " + last.Content},
        FinishReason: "stop",
    }, nil
}

func (e echoCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
    ch := make(chan provider.Chunk, 1)
    ch <- provider.Chunk{Done: true, FinishReason: "stop"}
    close(ch)
    return ch, nil
}

reg := providerregistry.New()
_ = reg.Register("primary", echoCompleter{label: "primary"})
_ = reg.Register("fallback", echoCompleter{label: "fallback"})

req := provider.Request{
    Model:    "demo",
    Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
}
resp, err := reg.Route(context.Background(), req, []string{"primary", "fallback"}, nil)
// resp.Message.Content == "primary: hello": the first name succeeded,
// so Route never called the fallback.
```

### What the program shows

`Route` walks the order through `provider.RunTurn`. The first name
succeeds, so the fallback never runs. With a nil `Retryable`, a
failing first name would fall through to the next name on every
error.
