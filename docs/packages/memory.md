# Package reference: memory

The memory package stores and fetches context blobs by content
address. `Put` computes the `sha256:` ref with `envelope.ContextRef`
and returns it. `Get` fetches a blob by that ref. A size budget bounds
the store. The exported surface below mirrors `api/memory.txt`.

## Types

- `Store` — holds content-addressed blobs under a fixed byte budget.
  Safe for concurrent use. The zero value is not usable; create one
  with `New`.

## Functions and methods

- `New(maxBytes)` — creates a `Store` with a fixed byte budget.
- `Store.Put(content)` — stores `content` under its content ref and
  returns the ref.
- `Store.Get(ref)` — returns a copy of the blob stored under `ref`.

## Failure modes

Use `errors.Is` to test these.

- `ErrNoBudget` ("memory: maxBytes must be positive") — `New` wraps it
  when `maxBytes` is zero or negative. Pinned by
  `memory/memory_test/store_test.go`.
- `ErrBudgetExceeded` ("memory: content exceeds store budget") —
  `Store.Put` wraps it when the content's size exceeds `maxBytes`.
  Pinned by `memory/memory_test/store_test.go` and
  `memory/memory_test/store_integration_test.go`.
- `ErrUnknownRef` ("memory: unknown ref") — `Store.Get` wraps it for a
  ref the store does not hold, including a ref already evicted. Pinned
  by `memory/memory_test/store_test.go`,
  `memory/memory_test/store_integration_test.go`, and
  `memory/memory_test/store_concurrent_test.go`.

## Invariants

- `New` rejects a non-positive `maxBytes` with `ErrNoBudget`.
- `Put` computes `ref` as `envelope.ContextRef(string(content))`. A
  `content` whose length exceeds the budget wraps `ErrBudgetExceeded`
  and stores nothing; the store stays as it was before the call.
- A `content` that fits evicts the oldest-inserted blobs, in
  insertion order, until the new blob fits within the budget, then
  stores it. Eviction is by insertion order only; there is no eviction
  by use.
- Putting a `content` whose ref already exists overwrites the stored
  bytes and refreshes its insertion order to most-recent.
- `Get` returns `ErrUnknownRef` for a ref the store does not hold. It
  never changes insertion order.
- `Put` and `Get` both copy bytes on the way in and out, so a
  caller's later mutation of its own slice never reaches the store's
  internal state, and a caller's mutation of a `Get` result never
  reaches the store.
- `Store` is safe for concurrent use; a `sync.Mutex` guards the map
  and the insertion-order list.

## Why this shape

`memory` holds opaque bytes. It does not parse or validate the
content, and it does not know about `envelope.Message` or any other
wire type. It reuses `envelope.ContextRef` for addressing, so a ref
computed by `memory.Store.Put` is the same ref a caller would embed in
`Message.ContextRefs`. Insertion-order eviction is the chosen policy;
policy-based eviction and eviction by use are out of scope for this
package. See [../plans/memory.md](../plans/memory.md).

## Cross-references

- [envelope.md](envelope.md) — `ContextRef` is the addressing scheme
  `Put` reuses.

## Usage

```go
store, err := memory.New(1024) // 1 KiB budget
if err != nil {
    // maxBytes was zero or negative
}
ref, err := store.Put([]byte("shared context blob"))
if err != nil {
    // content was larger than the budget
}
blob, err := store.Get(ref)
// blob == []byte("shared context blob"), err == nil

_, err = store.Get("sha256:does-not-exist")
// err is memory.ErrUnknownRef
```
