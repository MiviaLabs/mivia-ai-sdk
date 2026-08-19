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
