# Package reference: longtermmemory

`longtermmemory` holds durable-feeling learnings an agent wants
across turns: entries in core and archive tiers, a small
never-evicted core per scope, automatic consolidation near capacity,
keyword search, and a bounded core-context frame a caller renders
into its own system prompt. In-memory only; a leaf package with no
internal imports. The exported surface below mirrors
`api/longtermmemory.txt`.

## Types

- `Verdict` — the agent's assessment of one recorded experience.
  Constants: `VerdictGood`, `VerdictBad`, `VerdictMixed`,
  `VerdictNeutral`.
- `Entry` — one memory: `Title`, `Scope`, `Verdict`, `Tags`,
  `Created`, `Summary`, `Detail`. `Created` is YYYY-MM-DD; empty
  means today at save time.
- `Result` — one search hit or core listing row: `ID`, `Scope`,
  `Title`, `Verdict`, `Tags`, `Created`, `Snippet`.
- `Query` — one search request: `Text`, `Scope` (required),
  `MaxResults`.
- `Store` — the in-memory tiered entry store, safe for concurrent
  use through one mutex. Built only through `New`.

## Constants

- `CoreTierCap` (24) — core-tier entries per scope.
- `DefaultMaxEntries` (500) — rows per scope when `New` receives
  zero.
- `DefaultMaxSearchResults` (8) — search results when `Query` carries
  zero.
- `DefaultFrameBytes` (4 KiB) — `CoreFrame` output when `maxBytes` is
  zero.
- `ConsolidateLoadFactor` (0.8) — the fill ratio that triggers
  consolidation.
- `FrameAdvisory` — the fixed first line of every frame block, ported
  verbatim from the reference chat layer.
- `FrameOpenTag`, `FrameCloseTag` — the delimiters of every frame
  block.

## Functions and methods

- `New(maxEntries int) *Store` — builds a Store. A non-positive
  `maxEntries` means `DefaultMaxEntries`.
- `Save(ctx, e)` — validates and stores one entry. An identical
  re-save is idempotent: same id, no duplicate. Entry ids are
  content-addressed, SHA-256 over every field. A merge rewrites the
  survivor's tags, so the survivor takes a new id. At the consolidation
  load factor it consolidates first, then refuses with `ErrStoreFull`
  when the scope is still full. It repeats the idempotency check after
  consolidation, because consolidation can mint the id it is saving.
- `Search(ctx, q)` — returns one scope's entries in which every query
  token matches the title, summary, or detail, case-insensitive.
  Tokenizing lowercases, splits on non-alphanumerics, and drops the
  reference stopword list. When tokenizing yields nothing, the whole
  trimmed query matches as one substring. Hits order by `Created`
  descending, then id ascending, capped at `MaxResults`.
- `Count(ctx, scope)` — the row count of one scope.
- `PromoteToCore(ctx, id)` — marks one entry core. Already-core is a
  no-op. Fails `ErrEntryNotFound` and `ErrCoreTierFull`.
- `CoreEntries(ctx, scope)` — core rows, ordered created DESC, title
  ASC, id ASC, capped at `CoreTierCap`.
- `Delete(ctx, id)` — removes one entry by id. Consolidation never
  deletes core rows; only this call can.
- `CoreFrame(ctx, scope, maxBytes)` — renders the scope's core
  entries as one bounded text block: the open tag, the advisory line,
  the neutralized entries, and the close tag. Whole entries only; the
  first entry that would not fit stops the block. A literal
  occurrence of either tag inside entry text becomes its HTML-escaped
  form first, so agent-writable text can never close the block early.
  An empty core, and a cap smaller than the frame overhead, both
  render the empty block.

## Failure modes

Use `errors.Is` to test these.

- `ErrEntryNotFound` — `Delete` and `PromoteToCore` return it for an
  unknown id. Pinned by
  `longtermmemory/longtermmemory_test/store_test.go`.
- `ErrCoreTierFull` — `PromoteToCore` returns it when the scope's
  core tier already holds `CoreTierCap` rows. Pinned by
  `longtermmemory/longtermmemory_test/tier_test.go`.
- `ErrStoreFull` — `Save` returns it when the scope is still full
  after consolidation, which for an all-core scope means no eviction
  was possible. Pinned by
  `longtermmemory/longtermmemory_test/store_test.go`.
- `ErrQueryRequired` — `Search` returns it for blank query text.
  Pinned by `longtermmemory/longtermmemory_test/search_test.go`.
- `ErrScopeRequired` — `Search` returns it for a blank scope. Pinned
  by `longtermmemory/longtermmemory_test/search_test.go`.

## Invariants

- Consolidation order is fixed: exactly one merge pass over
  near-duplicates (Jaccard similarity of title-plus-summary tokens at
  or above 0.82), then oldest-archive eviction in a loop until the
  scope holds fewer than `MaxEntries` rows or nothing evictable
  remains. The merge never repeats inside one consolidation.
- A core row is never evicted. With exactly one core side, the core
  row survives the merge regardless of creation order; with both sides
  sharing a tier, the earlier `Created` row survives, id breaking a
  tie.
- The merge survivor keeps the union of both tag lists, deduplicated,
  in first-seen order, capped at eight tags. The survivor's own tags
  fill the list first, so the dropped row loses tags past the cap.
- The merge survivor takes a new id, recomputed from its merged
  content, after the merge pass ends. A caller holding a pre-merge id
  gets `ErrEntryNotFound` from `Delete` and `PromoteToCore`, the same
  failure an evicted row gives.
- Eviction order is `Created`, then id, oldest first.
- The frame counts the advisory line and both tags toward the byte
  cap and never emits a partial entry.

## Cross-references

None. `longtermmemory` declares no internal import edge; a caller
renders `CoreFrame` into its own system prompt.

## Wire contract

`longtermmemory` carries no wire format; no conformance vector
applies.

## Usage

```go
store := longtermmemory.New(0)
res, _ := store.Save(context.Background(), longtermmemory.Entry{
    Title:   "Prefer prepared statements",
    Scope:   "proj-alpha",
    Verdict: longtermmemory.VerdictGood,
    Tags:    []string{"db"},
    Summary: "Prepared statements avoided injection issues.",
})
_ = store.PromoteToCore(context.Background(), res.ID)
frame, _ := store.CoreFrame(context.Background(), "proj-alpha", 0)
```

### What the program shows

`Save` fills `Created` with today when empty, stores the entry under
its content address, and returns its `Result`. `PromoteToCore` pins
it inside the scope's core tier, and `CoreFrame` renders it inside
the bounded block a caller pastes into its system prompt.
