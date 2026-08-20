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
way. `contextplan` consumes `spool` today: `Planner` writes a
budget-driven elision's full payload to a wired `*spool.Spool`, keyed
to the payload's `SubjectID`. See `docs/plans/contextplan.md`'s
"contextplan spools its own overflow" section.
`e2e/e2e_test/spool_test.go` proves a caller-driven
`SpoolTool`/`ReadOutputTool` pairing runs through a live `agentrun`
composition path; see "Change: prove ReadOutputTool reaches a live
composition path" below.

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

## Change: document the SpoolTool and ReadOutputTool pairing

Status: superseded. The section below stayed accurate about the
orphan-package check and the no-in-repo-caller fact. It is wrong
about the fix. Do not follow its "docs-only" conclusion. See "Change:
SpoolTool takes an existing Spool" below for the corrected plan.

`SpoolTool` has zero in-repo callers today; `spool` is not an orphan
package, since `contextplan` already imports it. Both facts still
hold and stayed true during investigation for the corrected change
below.

The prior conclusion claimed a caller could construct a working
`ReadOutputTool` paired against the same `*Spool` a `SpoolTool` call
uses, and that only the docs needed the pairing spelled out. That
claim is false. `SpoolTool` built its own unexported `*Spool`
internally and never returned it, so no caller value existed to pass
to `ReadOutputTool`. A docs-only change could not produce a working
example, because the API had no path to the value the example
needed. The corrected change below changes `SpoolTool`'s signature so
the pairing becomes possible, then updates the docs to show it.

## Change: SpoolTool takes an existing Spool

Status: shipped.

### Change goal

Let a caller pair one `SpoolTool`-wrapped tool with a
`ReadOutputTool` reading from the same grants, and let two or more
`SpoolTool`-wrapped tools share one budget. Today neither is
possible: `SpoolTool` builds its own internal `*Spool` and never
exposes it.

### Change scope

Inside:

- `SpoolTool`'s signature changes from `(name string, maxBytes int,
  store ContentStore, inner tools.Tool) tools.Tool` to `(name string,
  maxBytes int, sp *Spool, inner tools.Tool) (tools.Tool, error)`.
  The caller now builds the `*Spool` with the existing, unchanged
  `NewSpool` and passes it in, the same value it later passes to
  `ReadOutputTool`.
- A nil `sp` fails at construction with `ErrNilSpool`, the same
  sentinel `ReadOutputTool` already wraps for the same condition.
- Removal of the unexported `toolGrantBudget` constant. `SpoolTool`
  no longer builds a `*Spool`, so the constant has no reader.
- Every call site in `spool/spool_test/` and
  `e2e/e2e_test/spool_test.go` updates to the new signature in the
  same commit.
- `docs/packages/spool.md`'s usage example replaces the incomplete
  pairing with the full one: one `NewSpool` call feeds both
  `SpoolTool` and `ReadOutputTool`.
- `docs/plans/subagent.md`'s "Deliberate non-goals" prose quotes the
  old four-argument call shape (`spool.SpoolTool(name, maxBytes,
  store, ...)`). Update that line to the new shape in the same
  commit; a stale signature in a plan is documentation drift.

Outside:

- Any change to `NewSpool`, `Spool.Spool`, `Spool.Load`,
  `ReadOutputTool`, `SpoolExpiring`, `Expire`, or `GrantExpiry`. Those
  signatures stay as locked.
- Any change to `ContentStore`. `NewSpool` still takes a
  `ContentStore`; only `SpoolTool` stops taking one directly.
- A default or package-level `*Spool`. The caller always supplies its
  own, matching `ReadOutputTool`'s existing convention.

### Decision: SpoolTool gains an error return

`SpoolTool` must reject a nil `sp`. A silently accepted nil `*Spool`
would not panic at construction; it would panic later, inside `Run`,
the first time an oversized result reaches `t.sp.Spool` with `t.sp`
nil. That failure lands during a live model turn, far from the
construction call that caused it.

The prior correctness-fix section in this file chose a different
answer for a negative `maxBytes`: clamp to zero instead of adding an
error return, because zero is a sane default for a missing byte
budget. A nil `*Spool` has no sane default. `SpoolTool` cannot spool
without a store to spool into, so there is no clamp that preserves
correct behavior the way clamping `maxBytes` does.

The "keep every caller unchanged" reason that favored the clamp for
`maxBytes` does not apply here. Swapping `store ContentStore` for `sp
*Spool` already breaks every call site's signature. Adding a second
return value costs each call site one more token, a check of the
error or an assignment to `_`. It does not add a second, independent
breaking change beyond the one already required.

`SpoolTool` therefore matches `ReadOutputTool`'s own convention:
`(tools.Tool, error)`, wrapping `ErrNilSpool` for a nil `sp`. This
keeps the failure at construction, in the caller's own error-handling
path, instead of at a later `Run` call the caller may not control.

### Change API

`api/spool.txt` changes in the same commit as the code, through
`make api-update`:

```go
// SpoolTool wraps inner so any string result longer than maxBytes
// spools to sp under the ctx principal (see WithPrincipal) instead
// of returning in full. The wrapped tool's Out.Value becomes the
// truncated view string; the reference is appended to the view text.
// A result that is not a string, or one at or under maxBytes, passes
// through unchanged. A call with no principal in ctx returns
// ErrNoPrincipal. A nil sp wraps ErrNilSpool. A negative maxBytes
// clamps to zero.
// The returned tools.Tool implements tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, and tools.SchemaTool
// only when inner itself does, forwarding each call straight to
// inner.
// Two or more SpoolTool calls sharing one sp share its grant budget
// and its Load-time principal checks. A caller pairs a SpoolTool
// call with a ReadOutputTool call by passing the same sp to both.
func SpoolTool(name string, maxBytes int, sp *Spool, inner tools.Tool) (tools.Tool, error)
```

`toolGrantBudget` is removed from `spool/tool.go`. It was already
unexported and never appeared in `api/spool.txt`, so its removal
needs no lock update beyond the `SpoolTool` line above.

### Change tests

In `spool/spool_test/`:

- Update every existing `SpoolTool` call site
  (`spool_tool_test.go`, `tool_parity_test.go`) to the new signature:
  build one `*Spool` with `NewSpool`, pass it to `SpoolTool`, and
  check the new error return. Each site's existing assertions stay,
  proving the behavior did not change under the new signature.
  `NewSpool`'s `maxGrantBytes` at each migrated site must stay an
  independent, generous value, decoupled from that site's `maxBytes`
  view-truncation threshold, sized to the largest content the site
  spools. This mirrors the role the removed `toolGrantBudget`
  constant played. A site must never pass its `maxBytes` value as
  `maxGrantBytes`: several existing tests spool content far larger
  than their tiny `maxBytes` values (for example,
  `spool_tool_test.go:51-52` spools 1000 bytes with `maxBytes` of 10;
  `tool_parity_test.go` uses `maxBytes` of 8), and reusing `maxBytes`
  as the grant budget would fail those spools with `ErrGrantTooLarge`
  instead of producing a view.
- `SpoolTool` with a nil `sp` fails with `ErrNilSpool`, and returns a
  nil `tools.Tool`.
- New: two `SpoolTool`-wrapped tools share one `*Spool`. Wrap two
  different inner tools, each returning an oversized string, against
  the same `sp`. Spool both past the point where their combined size
  would overflow a budget smaller than their sum. Assert the older
  grant evicts, the same way one `*Spool`'s own eviction test already
  proves for direct `Spool.Spool` calls. This shows the two wrapped
  tools observe one shared budget, not two independent ones.
- New: a full round trip. Build one `sp` with `NewSpool`. Wrap a tool
  with `SpoolTool` against `sp`; run it with an oversized result and
  capture the returned view's ref. Build a `ReadOutputTool` against
  the same `sp`; call it with that ref and assert the pages it
  returns reconstruct the original oversized string. This is the
  pairing the bug made impossible to test; it must now pass.

In `e2e/e2e_test/spool_test.go`:

- Update the existing call site to the new signature, with the same
  independent, generous `maxGrantBytes` rule as above: the ~4500-byte
  content it spools against a `maxBytes` of 64 needs a
  `maxGrantBytes` sized to the content, not to 64.

### Change verification

- `make verify` passes; `spool` holds the 85 coverage floor.
- `go test -race ./spool/...` passes, including the new shared-budget
  and round-trip cases.
- `api/spool.txt` changes: `SpoolTool`'s line changes signature and
  return type as shown above; no other line changes. Land the diff
  through `make api-update` in the same commit as the code.
- `policy/layers.json`: no change. The `spool` row stays `["tools"]`;
  this change alters a signature inside `spool`, not its imports.
- `python3 scripts/check_plan.py`, `check_deps.py`, `check_prose.py`,
  and `check_labels.py` pass.
- `docs/packages/spool.md`'s usage example and function-list entry
  for `SpoolTool` update in the same commit; see "Change docs" below.
- `docs/plans/subagent.md`'s stale four-argument `SpoolTool` mention
  updates in the same commit.

### Change docs

`docs/packages/spool.md`'s function list entry for `SpoolTool`
changes from:

```
- `SpoolTool(name, maxBytes, store, inner)` — wraps `inner` so a
  string result over `maxBytes` spools to `store` under the `ctx`
  principal instead of returning in full.
```

to:

```
- `SpoolTool(name, maxBytes, sp, inner)` — wraps `inner` so a string
  result over `maxBytes` spools through `sp` under the `ctx`
  principal instead of returning in full. A nil `sp` wraps
  `ErrNilSpool`. Two SpoolTool calls sharing one `sp` share its grant
  budget.
```

The "Usage" example's `SpoolTool` line and the paragraph around it
change from the single unpaired call to the full pairing:

```go
store, _ := memory.New(1 << 20)
sp, err := spool.NewSpool(store, 1<<20)
if err != nil {
    // maxGrantBytes was zero or negative
}

wrapped, err := spool.SpoolTool("big-tool", 4096, sp, myTool)
if err != nil {
    // sp was nil
}

readBack, err := spool.ReadOutputTool(sp, 2048)
if err != nil {
    // sp was nil, or maxPageBytes was non-positive
}

registry := tools.New()
registry.Add(wrapped)
registry.Add(readBack)

ctx := spool.WithPrincipal(context.Background(), "agent-a")
out, err := wrapped.Run(ctx, in)
// out.Value truncates and appends a ref when myTool's result exceeds
// 4096 bytes. A model reads that ref from the text and calls
// read_spooled_output with it to page the full body back.

argsJSON := []byte(`{"ref":"the-ref-from-out.Value"}`)
args, _ := readBack.(tools.SchemaTool).DecodeArguments(argsJSON)
page, err := readBack.Run(ctx, args)
// page returns one bounded page of the body wrapped spooled
```

The example drops the standalone `sp.Spool`/`sp.Load` walkthrough
lines that duplicated `Spool`'s own doc comment. The paired
`SpoolTool`/`ReadOutputTool` walkthrough now covers the same `sp`
value end to end, matching what a tool-registry caller needs.

## Change: prove ReadOutputTool reaches a live composition path

Status: planned.

### Change goal

Show that `ReadOutputTool` is present, schema-typed, and resolvable
by name and scope in the same `*tools.Registry` object
`agentrun.New` validates and wires for a chain, not only through
`spool`'s own tests. Today no test outside `spool/spool_test/` calls
`spool.ReadOutputTool`. The one e2e test that wires `SpoolTool` into
a live `agentrun.Options.Tools` registry
(`e2e/e2e_test/spool_test.go`) never registers `ReadOutputTool` and
never resolves it through that registry. It resolves the spooled body
by calling `sp.Load` straight from the test, bypassing the registry a
live agent run would use. This change does not run `ReadOutputTool`
through `agentrun.Runner.chain()`'s own `AckWait` closure; it calls
`Registry.RunScoped` directly, after the chain has already finished,
using the same registry and scope value the chain was built with.

### Finding: this is a test gap, not a missing feature

`spool` does not need a new exported symbol, and `agentrun` does not
need an auto-wiring feature. Three facts support this.

First, every composition layer in this SDK already requires the
caller to build and register each tool by hand.
`agentrun.Options.Tools` takes a caller-built `*tools.Registry`
(`agentrun/options.go:57`). `subagent`'s file tools follow the same
shape: a caller calls `subagent.WorkspaceReadTool(...)` and then adds
the result to a registry itself. No package in this SDK inspects a
registry's contents and adds a matching tool on the caller's behalf.
An auto-wiring feature in `agentrun` or `agent` would be the first of
its kind, with one caller in mind. AGENTS.md's building-block rule
forbids adding abstraction without a caller.

Second, auto-wiring is not mechanically possible without a new,
purpose-built marker. `spoolTool` (`spool/tool.go:16`) is unexported.
A composition layer outside `spool` cannot type-assert a registry
entry as "a `SpoolTool`-wrapped tool wrapping this `*Spool`". Building
that detection would need a new exported marker interface on `spool`,
serving one caller, which is the same speculative-generality problem
stated a different way.

Third, the caller already has everything it needs. `SpoolTool` takes
an existing `*Spool` since the prior change in this file, so the
caller holds the same value it passes to `ReadOutputTool`. Pairing the
two calls and registering both is two extra lines in code the caller
already writes. `docs/packages/spool.md`'s usage example already shows
this pairing.

The real gap is narrower: nothing proves the pairing works end to end
through a live registry a composition layer drives. The fix is a test
change, not a new production symbol.

### Change scope

Inside:

- Extend `e2e/e2e_test/spool_test.go`,
  `TestSpoolToolTruncatesLargeStepResult`: register a
  `spool.ReadOutputTool` built over the same `sp` into the same `reg`
  used by `agentrun.Options.Tools`, alongside the existing
  `SpoolTool`-wrapped tool.
- Build a `*tools.Scope` with `tools.NewScope` naming both tool names
  (the `SpoolTool` name and `read_spooled_output`) in its Allowlist.
  Set this scope on the shared `agentrun.Options.Scope` field, so the
  chain `agentrun.New` builds runs the spool step through this same
  scope, not a nil one.
- After the existing assertions confirm the spooled view and its ref,
  call `reg.RunScoped(ctx, "read_spooled_output", in, scope)` on that
  same registry and scope value, with `in` built through
  `readBack.(tools.SchemaTool).DecodeArguments` over a JSON payload
  naming the captured ref. `agentrun.Runner.chain()`
  (`agentrun/wire.go:128`) and `agentloop.Loop.Run`
  (`agentloop/run.go:13`) both call `Registry.RunScoped`, never
  `Registry.Run`; calling `Registry.Run` in the test would miss the
  scope-gating branch a real chain step or loop iteration always goes
  through. Passing the same non-nil `scope` value used to build the
  chain, rather than nil, means the assertion actually exercises that
  branch instead of coincidentally matching a scope-blind nil case.
- Assert the page `reg.RunScoped` returns reconstructs `full`, the
  tool result `SpoolTool` truncated. Also assert `reg.RunScoped` at
  that name returns no error, proving `ReadOutputTool` is present,
  schema-typed, and resolvable by name and scope in the same
  `*tools.Registry` object `agentrun.New` validated and wired for the
  chain's own run.

Outside:

- Any change to `spool`'s exported surface. `api/spool.txt` stays as
  locked; this change touches only test code.
- Any change to `agentrun`, `agent`, or `tools`. No auto-wiring
  feature. No new exported symbol anywhere.
- A flow step that calls `read_spooled_output` through
  `agentrun`'s own chain. `flow.Step.Payload` is a static string
  resolved once at plan-build time; the ref `SpoolTool` mints is
  content-addressed and known only after the first step runs.
  Threading a dynamic ref through a static `Payload` needs
  `flow`-level templating this SDK does not have, and inventing it
  for one test is out of scope. Calling `reg.RunScoped` directly
  after the chain finishes proves the narrower resolvability claim
  above without that machinery: the registry and scope under test
  are the same live values `agentrun.Options.Tools` and
  `agentrun.Options.Scope` held, not second ones built for the
  assertion.

### Change tests

In `e2e/e2e_test/spool_test.go`:

- `TestSpoolToolTruncatesLargeStepResult` (extended): after the chain
  runs and the view and ref are captured, register `readBack` into
  `reg` before the chain runs (both tools must be present when
  `agentrun.New` builds the runner, matching how a real caller
  assembles a registry once, up front), and set the same non-nil
  `*tools.Scope` on `agentrun.Options.Scope`. Call
  `reg.RunScoped(ctx, "read_spooled_output", in, scope)` with the
  captured ref and assert the reconstructed page equals `full`. This
  proves `ReadOutputTool` is present, schema-typed, and resolvable by
  name and scope in the same registry object `agentrun.New` already
  validated and wired; it does not prove the call runs inside
  `agentrun.Runner.chain()`'s own `AckWait` closure.
- No new test function. The existing test already builds the exact
  registry, `*Spool`, and oversized-tool scaffolding this addition
  needs; splitting it into two tests would duplicate that setup for no
  added coverage.

### Change docs

`docs/packages/spool.md`'s usage example already shows the paired
registration (see "Change: SpoolTool takes an existing Spool" above).
No further doc edit is needed there. Add one sentence to this file's
Goal section, next to the existing `contextplan` consumer line,
naming `e2e/e2e_test/spool_test.go` as the proof that a caller-driven
`SpoolTool`/`ReadOutputTool` pairing resolves by name and scope in
the same registry a live `agentrun` composition wires.

### Change verification

- `make verify` passes; `spool` holds the 85 coverage floor.
- `go test ./e2e/e2e_test/...` passes, covering the extended
  `TestSpoolToolTruncatesLargeStepResult`.
- `python3 scripts/check_plan.py`, `check_deps.py`, `check_prose.py`,
  and `check_test_tampering.py` pass. The change only extends an
  existing test with new assertions; it does not weaken or remove any
  existing one.
- `api/spool.txt`: no diff. `policy/layers.json`: no diff. This change
  adds no exported symbol and no import edge.
