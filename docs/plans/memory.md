# Plan: memory

Status: shipped. `memory` depends on envelope only, for
`ContextRef`. This file is the package plan docs/plans/TEMPLATE.md
and scripts/check_plan.py require.

## Goal

Store and fetch context blobs by content address. `Put` computes the
`sha256:` ref with `envelope.ContextRef` and returns it. `Get` fetches
a blob by that ref. A size budget bounds the store; a `Put` that would
exceed the budget fails instead of growing past it.

## Scope

Inside: the `Store` type, `New`, `Put`, `Get`, and the size budget.
`Put` stores content addressed by `envelope.ContextRef`. `Get`
resolves a ref back to the stored bytes. `New` enforces a positive
budget.

Outside: policy-based eviction, eviction by use (LRU/LFU), and a
distributed store. A first version evicts by insertion order only,
and only when a new `Put` would exceed the budget. Memory holds
opaque bytes; it does not parse or validate the content itself, and
it does not know about `envelope.Message` or any other wire type.

`memory` imports `envelope` only, for `ContextRef`. No other internal
import. Stdlib only beyond that: `crypto/sha256` is not needed here
since `envelope.ContextRef` already hashes; `memory` itself needs
only `errors`, `fmt`, and `sync`.

## API

The surface below is the lock target. It lands in `api/memory.txt`
via `make api-update`.

- `var ErrNoBudget` — the sentinel for a non-positive `maxBytes`
  passed to `New`.
- `var ErrBudgetExceeded` — the sentinel for a blob that `Put` rejects
  because it is larger than the store's budget.
- `var ErrUnknownRef` — the sentinel for a `ref` that `Get` does not
  hold.
- `type Store struct` — holds the blobs and the byte budget.
  Unexported fields. Built only through `New`. Mutex-guarded, safe
  for concurrent use. The zero value is not usable; create a `Store`
  with `New`, matching `heartbeat.Monitor` and `room.Room`.
- `func New(maxBytes int) (*Store, error)` — creates a `Store` with a
  fixed byte budget. A non-positive `maxBytes` wraps `ErrNoBudget`.
- `func (s *Store) Put(content []byte) (ref string, err error)` —
  computes `ref` as `envelope.ContextRef(string(content))` and stores
  `content` under it. A `content` whose length exceeds the store's
  budget wraps `ErrBudgetExceeded` and stores nothing. A `content`
  that fits evicts the oldest-inserted blobs, in insertion order,
  until the new blob fits within the budget, then stores it. Putting
  a `content` whose ref already exists overwrites the stored bytes
  and refreshes its insertion order to most-recent.
- `func (s *Store) Get(ref string) ([]byte, error)` — returns a copy
  of the blob stored under `ref`. An unknown `ref` wraps
  `ErrUnknownRef`. `Get` does not change insertion order; the first
  version evicts by insertion order only, not by use.

The expected lock content:

```text
package memory
  func (s *Store) Get(ref string) ([]byte, error)
  func (s *Store) Put(content []byte) (ref string, err error)
  func New(maxBytes int) (*Store, error)
  type Store struct {
}
  var ErrBudgetExceeded
  var ErrNoBudget
  var ErrUnknownRef
```

`Put` converts `content` to a `string` only for the `ContextRef` call;
the stored bytes stay the caller's `[]byte`, copied on the way in and
on the way out of `Get`, so a caller's later mutation of its own slice
never reaches the store's internal state.

## File layout

- `memory/doc.go` — package doc and file map.
- `memory/store.go` — `Store`, `New`, `Put`, `Get`, and the three
  sentinel errors.

## Tests

Test files live in `memory/memory_test/`, an external test package.

- `store_test.go` — the red-green unit cases for `New`, `Put`, and
  `Get`. Table-driven, one `Test` function per method.
  - `TestNew`: a positive `maxBytes` (no error), a zero `maxBytes`
    (`ErrNoBudget`), a negative `maxBytes` (`ErrNoBudget`).
  - `TestPut`: a blob under budget (returns a ref, no error), a blob
    exactly at the budget (no error), a blob over budget
    (`ErrBudgetExceeded`, store unchanged), a `Put` of the same
    content twice (same ref both times).
  - `TestGet`: a known ref (returns the stored bytes), an unknown ref
    (`ErrUnknownRef`).
- `store_integration_test.go` — put two distinct blobs into one
  `Store`, get each back by ref, and prove each ref equals
  `envelope.ContextRef` of the original content. Put a third blob
  that pushes the store over budget and prove the oldest blob is
  evicted: its `Get` now returns `ErrUnknownRef`, while the newer
  blobs still resolve. Put a blob larger than the whole budget and
  prove `Get` still returns `ErrUnknownRef` for its ref, since `Put`
  rejected it with `ErrBudgetExceeded`. Run under `go test -race`.
  - Insertion-order-refresh case: `Put` blob A, `Put` blob B, re-`Put`
    blob A (same ref, refreshes A's insertion order to most-recent).
    `Put` blob C, sized to force exactly one eviction. Assert B, not
    A, was evicted: `Get` on B's ref returns `ErrUnknownRef`; `Get` on
    A's and C's refs still resolves. This proves the refresh happens,
    not just that a repeat `Put` returns the same ref.
  - Multi-blob eviction case: `Put` three small blobs that together
    fill the budget. `Put` a fourth blob sized to force the eviction
    of two of the three, not just the oldest one. Assert both evicted
    blobs return `ErrUnknownRef` from `Get`, and the one surviving
    blob still resolves.
- `store_concurrent_test.go` — modeled on
  `heartbeat/heartbeat_test/monitor_concurrent_test.go`: N goroutines,
  a concrete outcome asserted, run under `go test -race`.
  1. N goroutines each call `Put` with distinct content concurrently,
     on a `Store` sized to hold every blob without eviction, then
     join. A following loop of `Get` calls, one per returned ref, must
     resolve every blob, proving concurrent `Put` calls all land.
  2. N goroutines call `Put` concurrently on a `Store` sized to hold
     only a few of the N blobs, forcing eviction, while N other
     goroutines concurrently call `Get` on refs returned by earlier
     `Put` calls. No call may panic or corrupt the store; every `Get`
     call returns either the correct blob or `ErrUnknownRef`, never a
     wrong or partial blob, proving `Put`-driven eviction and `Get`
     serialize correctly against each other.
- `store_bench_test.go` — benchmark `Put` and `Get` on a store built
  with a ten-megabyte budget, using a small (under one kilobyte)
  blob. Target under one microsecond per call. State the measured
  baseline in the file.

## Metamorphic test suite

New file `memory/memory_test/metamorphic_test.go`, package
`memory_test`, distinct from the fixed-scenario cases already in
`store_integration_test.go`. Each case is a property pair: apply a
transformation to a valid input, assert the stated outcome.
Table-driven; one `TestMetamorphic*` function per property.

- `TestMetamorphicPutNeverExceedsBudget` — property: a `Put` that
  triggers eviction never leaves the store's total size above its
  configured budget. Table varying the budget and a sequence of blob
  sizes chosen to force zero, one, and multiple evictions. For each
  case: run the `Put` sequence, tracking every returned ref. After
  each `Put`, call `Get` on every ref seen so far, sum the length of
  every blob whose `Get` still succeeds, and assert the sum is at
  most the budget. `Store.total` is unexported, so the test measures
  the budget through the public `Get` surface, not the field.
  Confirmed true against `store.go`: `Put` rejects an over-budget
  blob before storing anything, and its eviction loop runs while
  `s.total+len(content) > s.maxBytes`, so a stored blob never leaves
  `s.total` over `s.maxBytes`.
- `TestMetamorphicGetEvictedFailsYoungerAnswers` — property: a `Get`
  of an evicted ref fails while a younger, non-evicted ref still
  answers. Table varying the budget and blob sizes across single- and
  multi-blob eviction depths, distinct from
  `TestPutMultiBlobEviction`'s fixed case. For each case: run the
  `Put` sequence, identify the oldest ref driven out by the budget
  from the known insertion order, and the newest ref still within
  budget. Assert `Get` on the evicted ref wraps `ErrUnknownRef`, and
  `Get` on the younger ref returns its original bytes. Confirmed true
  against `store.go`: the eviction loop drops `s.order[0]` first,
  oldest-inserted, and never touches a later entry that already fits.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `memory` and for the total.
- The `memory` row in `policy/layers.json` lists `envelope` only. The
  row lands with this plan, before the code.
- `api/memory.txt` lands through `make api-update` in the same change
  as the code. The lock matches the surface in the API section.
- `go test -race ./memory/...` passes, covering the eviction and
  concurrency-sensitive paths in `store_integration_test.go` and
  `store_concurrent_test.go`.
- The metamorphic suite is test-only: no exported symbol changes, so
  `make api-update` must produce no diff for `api/memory.txt` in that
  change. `go test -race ./memory/...` covers the new file.
- The phase adds no conformance vectors. Memory carries no wire
  format of its own; it reuses `envelope.ContextRef` for addressing
  and stores opaque bytes.
- docs/architecture.md gains the memory/ entry in the package map;
  docs/packages/memory.md is added and linked from docs/README.md.
