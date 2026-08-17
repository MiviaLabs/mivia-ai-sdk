# Phase 15: memory context store

Status: future. Builds the memory block. Shared context is content
addressed. A blob has a `sha256:` content ref. The store holds the
blobs and enforces a size budget. See `envelope.ContextRef`.

## Goal

Store and fetch context blobs by content address. The store rebuilds
the `ContextRef` of a blob and returns it. A size budget evicts old
blobs. This is the memory a later agent works on.

## Scope

Inside: the `Store`, the `Put` and `Get` by content ref, and the size
budget. Outside: policy, eviction by use, and a distributed store.
A first version evicts by insertion order only.

## API

- `type Store struct` holding the blobs and the budget.
- `New(maxBytes int) *Store`
- `(*Store).Put(content []byte) (ref string, err error)`
- `(*Store).Get(ref string) ([]byte, error)`

`Put` computes the `sha256:` content address with `envelope.ContextRef`.
It rejects a blob over the budget. `Get` returns the blob for a known
ref and an error for an unknown one. The store is safe for concurrent
use.

## Tests

Test files live in `memory/memory_test/`:

- `store_test.go` — the red-green cases for `Put` and `Get`.
  Start with the assertions. Confirm they fail on the empty phase.
  Implement and watch them pass.
- `store_integration_test.go` — put two blobs, get them by ref, and
  prove the refs match `envelope.ContextRef`. Exceed the budget and
  prove the store rejects it. Run under `go test -race`.
- `store_bench_test.go` — benchmark `Put` and `Get` on a ten-megabyte
  budget. Target under one microsecond for a small blob.

## Verification

`make verify` passes. The coverage floor for `memory` holds. The
memory package declares its import of `envelope` in
`policy/layers.json`. `api/memory.txt` lands via `make api-update`.
