# spool

Status: shipped. One new leaf package plus a `tools.Tool` wrapper,
implementing `docs/plans/agents/phase67_truncation_spool.md` under
the phase 65 `contextstate` contract. No standalone phase 67 plan
file remains.

## Goal

`spool` stores oversized content under a principal-scoped grant and
hands the caller a bounded view plus a reference. `SpoolTool` gives
any `tools.Tool` this behavior: a large string result truncates, and
the reference names where the full result lives.

The ref's format is a `ContentStore` implementation's own choice.
`spool` does not guarantee a `contextstate.ContentRef`-shaped string;
`memory.Store`'s refs happen to be `envelope.ContextRef` values today,
but a caller using a different `ContentStore` may mint refs some other
way. `contextplan`, the expected consumer that resolves these refs,
must pick a `ContentStore` whose ref format it can parse; it does not
yet consume `spool`.

## Scope

Inside:

- `Spool`, `NewSpool`: a grant store keyed by reference and principal.
  `Spool` writes content to a caller-supplied `ContentStore` and
  records which principal may read it back.
- `Load`: reads a grant back, refusing a principal that does not match
  the one recorded at `Spool` time.
- Grant expiry by byte age: `NewSpool` takes a byte budget for its
  grant bookkeeping. A `Spool` call that would push the tracked grant
  total over budget drops the oldest grants first, the same shape
  `memory.Store` already uses for its blobs. A dropped grant's `Load`
  fails, even if the underlying `ContentStore` still holds the bytes.
- `WithPrincipal`, `PrincipalFrom`: carry the calling principal through
  `context.Context`, so `SpoolTool` can read it without a signature
  change to `tools.Tool`. This follows the `flow.LoopStateFrom`
  precedent already in this SDK: inject before the call, read inside.
- `SpoolTool`: wraps a `tools.Tool`. A small string result passes
  through untouched. An oversized one spools to the caller-supplied
  `ContentStore` and returns a truncated view naming the reference.
  The returned wrapper forwards `tools.ProfiledTool.ExecutionProfile`,
  `tools.ResultBudgetTool.MaxResultBytes`, and
  `tools.PrivilegedTool.Privileged` from `inner` whenever `inner`
  implements them, so `tools.ExecutionProfileOf`,
  `tools.ResultBudgetOf`, and `tools.IsPrivileged` see `inner`'s
  values through the wrapper. `RunScoped`'s approval gate reads these
  through `ExecutionProfileOf`; a wrapper that dropped them would
  silently downgrade every spooled tool to the unclassified,
  unbudgeted, unprivileged default.
- Sentinels: `ErrUnknownRef`, `ErrWrongPrincipal`, `ErrNoPrincipal`,
  `ErrNoBudget`, `ErrGrantTooLarge`, `ErrPrincipalConflict`.

Outside:

- Any persistence backend. `spool` defines the storage shape it needs
  as the `ContentStore` interface; it ships no store of its own. A
  caller wires in `memory.Store`, which already satisfies the two
  methods the interface asks for, or any other store shaped the same
  way.
- Context elision decisions. Phase 66 owns those.
- Mivia's operator-facing spool views and its principal mapping.
- Wall-clock TTL. Expiry here is byte-age (oldest-inserted-first under
  a budget), not a timer.

## API

```go
// ContentStore is the storage a Spool writes spooled bytes to and
// reads them back from. memory.Store satisfies this interface with no
// import needed on either side; a caller wires the two together.
type ContentStore interface {
	Put(content []byte) (ref string, err error)
	Get(ref string) ([]byte, error)
}

// Spool stores oversized content under a principal-scoped grant and
// returns a bounded view plus a reference to the full content.
type Spool struct { /* unexported fields */ }

// NewSpool creates a Spool backed by store, tracking grants under a
// maxGrantBytes budget. A non-positive maxGrantBytes wraps
// ErrNoBudget.
func NewSpool(store ContentStore, maxGrantBytes int) (*Spool, error)

// Spool writes data to the underlying store, grants principal the
// right to read it back, and returns a bounded view of data plus the
// content's reference. Spool evicts the oldest grants, by insertion
// order, until the new grant fits the byte budget. Spool wraps
// ErrGrantTooLarge when data alone exceeds maxGrantBytes, and
// ErrPrincipalConflict when the store's ref already belongs to a
// different principal's live grant: a content-addressed ContentStore
// returns the same ref for identical bytes regardless of caller, so
// this check stops a second principal from silently taking over the
// first principal's grant.
func (s *Spool) Spool(ctx context.Context, principal string, data []byte) (view string, ref string, err error)

// Load returns the full bytes stored under ref. It wraps
// ErrUnknownRef when no live grant matches ref, and ErrWrongPrincipal
// when principal does not match the grant's recorded principal. When
// the grant is live but the underlying ContentStore.Get fails (for
// example, the store's own independent budget evicted the blob
// first), Load wraps that error under ErrUnknownRef too: a live grant
// whose bytes are gone is, from the caller's view, an unknown ref.
func (s *Spool) Load(ctx context.Context, principal, ref string) ([]byte, error)

// WithPrincipal returns a context carrying principal for a later
// SpoolTool call to read.
func WithPrincipal(ctx context.Context, principal string) context.Context

// PrincipalFrom reads the principal WithPrincipal attached to ctx. The
// second return is false when no principal was attached.
func PrincipalFrom(ctx context.Context) (string, bool)

// SpoolTool wraps inner so any string result longer than maxBytes
// spools to store under the ctx principal (see WithPrincipal) instead
// of returning in full. The wrapped tool's Out.Value becomes the
// truncated view string; the reference is appended to the view text.
// A result that is not a string, or one at or under maxBytes, passes
// through unchanged. A call with no principal in ctx returns
// ErrNoPrincipal.
// The returned tools.Tool also implements tools.ProfiledTool,
// tools.ResultBudgetTool, and tools.PrivilegedTool whenever inner
// does, forwarding each call straight to inner: SpoolTool changes
// only Run's result handling, not inner's declared execution class,
// result budget, or privilege.
func SpoolTool(name string, maxBytes int, store ContentStore, inner tools.Tool) tools.Tool

// Sentinel errors for Spool and SpoolTool; test with errors.Is.
var (
	ErrUnknownRef        = errors.New("spool: unknown ref")
	ErrWrongPrincipal    = errors.New("spool: wrong principal")
	ErrNoPrincipal       = errors.New("spool: no principal in context")
	ErrNoBudget          = errors.New("spool: maxGrantBytes must be positive")
	ErrGrantTooLarge     = errors.New("spool: content exceeds grant budget")
	ErrPrincipalConflict = errors.New("spool: ref already granted to a different principal")
)
```

### Deviations from the phase-67 sketch

- `Spool` returns `(view, ref string, err error)`, not `(view, ref
  string)`. The underlying `ContentStore.Put` can fail (a budget
  error, for example, from `memory.Store`), and `Spool` must not hide
  that failure behind an empty ref.
- `NewSpool` takes a second `maxGrantBytes` argument and returns
  `(*Spool, error)`. The phase plan's scope section asks for "grant
  expiry by byte age," which needs a budget to expire against; the
  sketch's one-argument, no-error form has no room for it.
- `SpoolTool` takes an explicit `store ContentStore` parameter instead
  of holding a package-level or hidden store. This keeps `SpoolTool`
  testable and consistent with `NewSpool`'s explicit store, and avoids
  hidden shared state between unrelated wrapped tools.
- `spool` imports neither `memory` nor `contextstate`. `ContentStore`
  is a two-method interface; `memory.Store`'s `Put`/`Get` pair already
  matches it structurally, and `memory.Store`'s refs are already
  `envelope.ContextRef` values, which delegate to
  `contextstate.Mint`. Declaring the interface locally keeps `spool` a
  true leaf and lets a caller substitute any conforming store, not
  only `memory.Store`. `WithPrincipal`/`PrincipalFrom` replace a
  principal argument threaded through `tools.Tool.Run`, since that
  interface's signature is fixed and shared by every tool in this SDK.
- `SpoolTool`'s returned value is a concrete wrapper type, not a bare
  closure over `tools.Tool`, so it can carry optional-interface
  methods (`ExecutionProfile`, `MaxResultBytes`, `Privileged`) that
  type-assert to `inner`'s own methods. A plain function value could
  not implement those interfaces at all. `SpoolTool` builds one of
  several concrete wrapper types, chosen by which optional interfaces
  `inner` implements, so `tools.ResultBudgetOf` and the other
  `*Of`/`Is*` helpers never see a wrapper claiming a capability
  `inner` lacks.
- `Spool` gained two sentinels not in the original sketch,
  `ErrGrantTooLarge` and `ErrPrincipalConflict`, found during review.
  A single grant larger than `maxGrantBytes` could never be evicted
  down to fit, so it now fails fast instead of silently exceeding the
  budget forever. A ref collision across principals, which a
  content-addressed `ContentStore` produces whenever two principals
  spool identical bytes, now fails instead of silently transferring
  the earlier principal's grant to the later caller.

## Tests

- `Spool` then `Load` round trips the same bytes for the granted
  principal.
- `Spool` on oversized input returns a truncated `view` shorter than
  `data`, while `Load` still returns the full bytes.
- `Load` with a principal that did not receive the grant fails with
  `ErrWrongPrincipal`.
- `Load` with an unknown ref fails with `ErrUnknownRef`.
- `Load` with a live grant whose `ContentStore.Get` fails (a store
  stub that returns an error for that ref, simulating the store's own
  independent eviction) wraps the store's error under `ErrUnknownRef`,
  not some untyped or leaked foreign error.
- `NewSpool` with a non-positive `maxGrantBytes` fails with
  `ErrNoBudget`.
- Grant expiry: spooling past `maxGrantBytes` drops the oldest grant
  first; its `Load` then fails with `ErrUnknownRef` even though the
  newest grant still loads.
- `Spool` with data longer than `maxGrantBytes` fails with
  `ErrGrantTooLarge`, and never calls `ContentStore.Put`.
- `Spool` with a second principal spooling byte-identical content
  already granted to a first principal fails with
  `ErrPrincipalConflict`; the first principal's grant is unchanged and
  still loads.
- `WithPrincipal` then `PrincipalFrom` round trips the principal
  string; `PrincipalFrom` on a bare context reports `false`.
- `SpoolTool`: a result at or under `maxBytes` passes through with the
  inner tool's exact `Out.Value`.
- `SpoolTool`: a result over `maxBytes` truncates and its returned
  view names a ref that `Spool.Load` (via the same store) resolves to
  the inner tool's full result.
- `SpoolTool`: a call with no principal attached to `ctx` still
  succeeds when the inner result is at or under `maxBytes`, since no
  grant is needed. The same call fails with `ErrNoPrincipal` when the
  inner result is oversized and a grant is needed. `Run` calls
  `inner.Run` first in both cases, so the inner tool always runs.
- `SpoolTool`: the inner tool's own error passes through unchanged,
  with no spooling attempted.
- `SpoolTool`: an oversized inner result whose spool attempt fails
  (a store stub whose `Put` returns an error) returns that error from
  `Run`, not a truncated view built on a failed write.
- `SpoolTool` forwarding: an inner tool implementing
  `tools.ProfiledTool`, `tools.ResultBudgetTool`, and
  `tools.PrivilegedTool` with fixed values makes
  `tools.ExecutionProfileOf`, `tools.ResultBudgetOf`, and
  `tools.IsPrivileged`, called on the wrapper `SpoolTool` returns,
  report the same values as calling them on `inner` directly.
- Concurrency: many goroutines call `Spool` and `Load` on one shared
  `*Spool` concurrently, past `maxGrantBytes` so eviction runs
  interleaved with lookups; run with `go test -race` and assert no
  race and no panic. Each goroutine's own grant, when not evicted,
  still round-trips its own bytes.

## Verification

- `make verify` passes.
- `go test -race ./spool/...` passes, covering the concurrency test.
- `policy/layers.json` gains the `spool` row: `spool` may import
  `tools` only.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass.
- `make api-update` locks `api/spool.txt` to the API section above.
- Coverage for `spool` reaches the 85 floor.
- One e2e case in `e2e/e2e_test/` wires `SpoolTool` around a
  large-output tool inside an `agentrun` step, confirming the spooled
  view carries a ref that a follow-up `Spool.Load` call resolves.

## Schema forwarding

Status: shipped. `tools.SchemaTool` landed in commit 16a7478.
`spool/tool.go` forwards it, and
`spool/spool_test/tool_parity_test.go` covers it. See
`docs/plans/agentloop.md`.

`SpoolTool` strips a capability silently when its combinatorial
switch misses an optional interface. A schema-less wrapper tool is
skipped by `agentloop.Definitions`, so the model never sees the tool.
That failure is silent unless `Definitions` fails closed, which the
`agentloop` plan now requires.

`spool` adopted `tools.SchemaTool` in the same change:

- A `schemaCap` forwarding struct carries `ParameterSchema` and
  `DecodeArguments`, beside the other three caps.
- The variant switch covers sixteen cases.
- `spool/spool_test/tool_parity_test.go` enumerates every subset of
  the known optional interfaces, including `tools.SchemaTool`. A
  missed variant fails the parity test, not a live run.

## Correctness fix: view budget and rune-safe cut

Two defects, one root each.

Defect one: a panic. `SpoolTool` validates nothing, so a caller may
pass a negative `maxBytes`. The predicate at `spool/tool.go:38`,
`!ok || len(s) <= t.maxBytes`, is then false for every string result,
including an empty one. `Run` spools and reaches `data[:maxBytes]` in
`buildView`. The runtime panics with a slice-bounds error. No recover
exists in `tools` or in `agentloop`, so one tool result stops the
process.

Defect two: invalid UTF-8. `buildView` in `spool/spool.go` cuts
`data[:maxBytes]` with no rune alignment. The cut may split a
multi-byte rune, and the broken bytes reach the model transcript.
`agentloop/wire.go` already fixed this class with `validPrefix` and
`strings.ToValidUTF8`.

### Decision on the panic fix

Two options existed.

- Option A, chosen: clamp a negative `maxBytes` to zero in
  `SpoolTool`, at construction.
- Option B, rejected: change `SpoolTool` to return
  `(tools.Tool, error)`.

Option A is chosen because it is one line and keeps `api/spool.txt`
unchanged. Option B breaks every caller of `SpoolTool` for a
programming error with one sane answer: with no byte budget, every
non-empty result spools.

The clamp belongs at the constructor, not in `buildView`. `buildView`
is a distant helper, and a clamp there leaves `spoolTool.maxBytes`
holding the nonsense value. The predicate at `spool/tool.go:38` then
still misbehaves: with `maxBytes` of -1 an empty result spools, and
with `maxBytes` of 0 the same empty result passes through. The
constructor clamp fixes the predicate and the cut together, and it
puts the invariant at the boundary, as the AGENTS.md Validate rule
requires.

### The change

In `spool/tool.go`, in `SpoolTool`:

- Add `if maxBytes < 0 { maxBytes = 0 }` before
  `base := &spoolTool{...}` at `spool/tool.go:225`.
- In `SpoolTool`'s doc comment, state that a negative `maxBytes`
  clamps to zero. Do not describe the resulting view text in the
  comment. The test pins that shape.

In `spool/spool.go`, in `buildView`:

- Cut with a rune-safe prefix. Call `bytes.ToValidUTF8` on the cut
  prefix with an empty replacement, matching `agentloop/wire.go`.
  `buildView` receives `[]byte`, so `bytes` is the matching package.
- No new package and no copied type. `bytes` is standard library,
  called directly here.
- Add one clause to `buildView`'s doc comment: the empty replacement
  drops every invalid byte in the prefix, not the trailing partial
  rune alone. `Spool` stores arbitrary bytes, so a binary payload's
  view may collapse to little more than the marker. `Spool.Load`
  still returns the stored bytes unchanged.

Note the exact view text for a zero budget. `buildView` produces
`" [truncated, ref=X]"`, with a leading space, for a non-empty
payload. This is the current format string's output, and this change
does not alter it.

No exported symbol changes. `api/spool.txt` stays as locked. The
`spool` row in `policy/layers.json` stays `["tools"]`.

### Tests

In `spool/spool_test/spool_tool_test.go`:

- `SpoolTool` with `maxBytes` of -1 and a non-empty string result.
  `Run` returns no panic and no error. The view names a ref that
  `Spool.Load` resolves to the full result. The view equals the exact
  text `buildView` produces for a zero budget, asserted literally.
  This case kills the mutation that removes the clamp: without the
  clamp the test panics.
- `SpoolTool` with `maxBytes` of -1 and an empty string result. The
  result passes through unchanged, with no principal in `ctx` and no
  `ErrNoPrincipal`. This case is the honest parity pin: without the
  clamp the empty result spools and fails.
- `SpoolTool` with `maxBytes` of 0, run over both the non-empty and
  the empty result. Assertions match the two cases above, proving the
  clamped value and the zero value behave the same.
- `SpoolTool` with a caller-chosen `maxBytes` landing inside a
  multi-byte rune of the inner result. Assert `utf8.ValidString` on
  the returned view. Assert `Spool.Load` on the named ref returns the
  inner result's full bytes. This case kills the mutation that
  restores the raw `data[:maxBytes]` cut. Test rune safety here, not
  through `Spool.Spool`, because `Spool.Spool` would force the test
  to hardcode the unexported view budget.
- `SpoolTool` with a binary inner result whose bytes are not valid
  UTF-8, and a `maxBytes` under its length. Assert the view is valid
  UTF-8, and assert `Spool.Load` round-trips the original bytes. This
  pins the documented trade of the empty replacement.

In `spool/spool_test/spool_test.go`:

- Keep the `Spool.Spool` path to the existing oversize assertion: the
  view is shorter than the data and `Load` returns the full bytes. Do
  not add a rune-boundary case here.

### Verification

- `make verify` passes. `spool` holds the 85 coverage floor.
- `go test -race ./spool/...` passes.
- `python3 scripts/check_api.py` passes with no `api/` diff.
- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  and `python3 scripts/check_prose.py` pass.
- `docs/plans/agentloop.md` and the `policy/layers.json` row adding
  `schema` to `agentloop` stay out of this commit. They belong to the
  concurrent `agentloop` change and need their own plan review.

## Change: read-back tool and grant expiry

Status: shipped. Two additions: a model-facing
read-back tool, and time-based grant expiry.

### Change goal

Let the model page a spooled body back by reference, one bounded page
per call. Let a grant carry an expiry, fail its `Load` after that
expiry with a distinct error, and give the caller a way to mark and
observe expiry.

### Change scope

Inside:

- `ReadOutputTool`, a `tools.Tool` bound to a caller-supplied `*Spool`
  at construction. The model supplies a ref, an offset, and a limit.
- Principal enforcement through the existing
  `WithPrincipal`/`PrincipalFrom` pair: the tool loads under the ctx
  principal.
- `SpoolExpiring`, `Expire`, and `GrantExpiry`: grant creation with a
  TTL, immediate marking, and observation.
- `ErrExpired`, returned by `Load` after expiry.

Outside:

- Any new filesystem or registry concern. The tool is patterned on
  `subagent`'s file tools: bound at construction, never to a
  model-chosen store.
- Wall-clock eviction sweeps. Expiry is lazy: `Load` checks and drops
  an expired grant, freeing its budget.
- Changes to `Spool`, `Load`, or `SpoolTool` signatures. The shipped
  surface stays as locked; expiry arrives through new methods.

### Change API

One addition to `api/spool.txt`, landed through `make api-update`:

```go
// SpoolExpiring writes data, grants principal read-back, and sets a
// time-to-live on the grant. A non-positive ttl wraps
// ErrInvalidExpiry.
func (s *Spool) SpoolExpiring(ctx context.Context, principal string, data []byte, ttl time.Duration) (view string, ref string, err error)

// Expire marks one live grant expired immediately. Unknown ref wraps
// ErrUnknownRef.
func (s *Spool) Expire(ref string) error

// GrantExpiry reports a grant's expiry. The second return is false
// for no live grant. A zero time means no expiry.
func (s *Spool) GrantExpiry(ref string) (time.Time, bool)

// ReadOutputTool builds the model-facing read-back tool over sp. The
// model pages a spooled body back by ref, offset, and limit. A nil
// sp wraps ErrNilSpool; a non-positive maxPageBytes wraps
// ErrInvalidLimit.
func ReadOutputTool(sp *Spool, maxPageBytes int) (tools.Tool, error)

// MoreMarker ends a page with more bytes after it, naming the next
// offset.
const MoreMarker = "[more: offset=%d]"

// Sentinel errors; test with errors.Is.
var (
    ErrExpired       = errors.New("spool: grant expired")
    ErrInvalidExpiry = errors.New("spool: ttl must be positive")
    ErrNilSpool      = errors.New("spool: spool is required")
    ErrInvalidLimit  = errors.New("spool: maxPageBytes must be positive")
    ErrBadArguments  = errors.New("spool: bad arguments")
)
```

Behavior rules, exact:

- `Load` checks in this order: no live grant wraps `ErrUnknownRef`;
  a principal mismatch wraps `ErrWrongPrincipal`, even on an expired
  grant, so a wrong principal probing an expired grant learns nothing
  about it; an expired grant under the right principal wraps
  `ErrExpired` and the grant is dropped, freeing its budget.
- `SpoolExpiring` stores `time.Now().Add(ttl)` on the grant. Re-spool
  an existing ref under the same principal refreshes the expiry, the
  same way `Spool` refreshes insertion order today.
- `ReadOutputTool` returns a `tools.Tool` that also implements
  `tools.SchemaTool`: it publishes one parameter schema for ref,
  offset, and limit, and decodes its own arguments.
- The tool's `Run` reads the ctx principal through `PrincipalFrom` and
  fails `ErrNoPrincipal` when absent.
- A limit of zero means one full page of `maxPageBytes` bytes. A
  limit over `maxPageBytes` clamps to it.
- `ErrBadArguments` covers a malformed argument decode, a mistyped
  `Run` call, a negative offset, and a negative limit — the same
  coverage `subagent`'s file tools give their `ErrBadArguments`.
- The page text carries `MoreMarker` with the next offset when bytes
  remain after the page, and nothing extra on the final page.
- `ErrWrongPrincipal`, expired grants, and unknown refs surface as the
  tool's own error return, so a caller's error policy decides what the
  model sees.
- An offset past the body's end returns an empty page with no marker.

### Change tests

In `spool/spool_test/`:

- `SpoolExpiring` then `Load` before expiry round trips the bytes.
- `Load` after expiry fails `ErrExpired`, not `ErrUnknownRef`; the
  same ref re-granted after expiry loads again.
- `Load` under a wrong principal on an expired grant fails
  `ErrWrongPrincipal`, not `ErrExpired`: the principal check runs
  first, so a probing principal learns nothing about the expiry.
- A non-positive ttl fails `ErrInvalidExpiry` before any store write.
- `Expire` marks a live grant; its `Load` then fails `ErrExpired`;
  `Expire` on an unknown ref fails `ErrUnknownRef`.
- `GrantExpiry` reports the stored time, false for an unknown ref, and
  the zero time for a grant from plain `Spool`.
- An expired grant frees budget: after expiry and one `Load`, a new
  grant of the same size fits without evicting others.
- `ReadOutputTool`: nil spool and non-positive page budget fail at
  construction.
- Paging: a body three pages long pages back in order; the marker
  names the next offset; the last page carries none; offset past the
  end yields an empty page.
- Zero limit and an over-limit limit both yield at most
  `maxPageBytes` bytes; a negative offset, a negative limit, and
  a malformed argument decode each fail `ErrBadArguments`.
- The tool reads the ctx principal: `WithPrincipal` with the granting
  principal succeeds; the wrong principal surfaces `ErrWrongPrincipal`
  from `Run`; no principal surfaces `ErrNoPrincipal`.
- An unknown ref surfaces `ErrUnknownRef` from `Run`.
- The tool's published schema and `DecodeArguments` round trip the
  three arguments; `agentloop.Definitions` would offer it.
- Concurrency: goroutines page, expire, and load one shared `*Spool`
  under `go test -race`; no panic, no torn page.

### Change verification

- `make verify` passes; `spool` holds the 85 coverage floor.
- `api/spool.txt` gains the new surface through `make api-update`, in
  the same change as the code.
- `go test -race ./spool/...` passes.
- `python3 scripts/check_plan.py`, `check_deps.py`, and
  `check_prose.py` pass. The `spool` row stays `["tools"]`; no new
  import edge exists.
- `docs/packages/spool.md` gains the tool and the expiry surface in
  the same change as the code.
