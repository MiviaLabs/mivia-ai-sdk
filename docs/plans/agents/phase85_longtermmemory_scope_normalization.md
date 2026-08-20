# Phase 85: longtermmemory Scope normalization

Status: plan, reviewed, not built.

## Why this plan exists

A bug audit of `longtermmemory` found a confirmed, reproducible data
bug: `Save(Entry{Scope: "proj "})` followed by `Search(Query{Scope:
"proj"})` returns zero hits. No error surfaces, and no existing test
catches it. This phase closes that gap.

## Goal

Save with a padded `Scope` must be findable by `Search`, `Count`, and
`CoreEntries` under the trimmed spelling, and the reverse. `Entry.
Validate()` (`longtermmemory/entry.go`) checks `strings.TrimSpace(e.
Scope) == ""` but never trims `e.Scope` itself. `Store.Save`
(`longtermmemory/store.go`) then uses the raw, untrimmed scope as the
`s.scopes` map key and as part of the `entryID` hash input. `Search`
(`longtermmemory/search.go`) does the same blank check but indexes
`s.scopes[q.Scope]` with the raw value. `Store.Count` and `Store.
CoreEntries` index `s.scopes[scope]` with no trim at all. Two
spellings of the same scope silently partition into separate,
mutually invisible buckets.

## Scope

Inside:

- One unexported helper, `normalizeScope(scope string) string`, in
  `longtermmemory/store.go`: returns `strings.TrimSpace(scope)`.
- `Store.Save` calls `normalizeScope(e.Scope)` and assigns the
  normalized value back onto the local `e.Scope`, applied after
  `Validate` passes and before `entryID(e)` is computed and before
  `scope` is used as a map key. The stored `Entry.Scope`, the
  returned `Result.Scope`, and the hash input are therefore all the
  trimmed form, for every entry saved from this point on.
- `Store.Search` calls `normalizeScope(q.Scope)` and indexes `s.
  scopes` with the normalized value, not `q.Scope`.
- `Store.Count` and `Store.CoreEntries` call `normalizeScope(scope)`
  on their `scope` parameter before every use of it, including the
  `s.scopes[scope]` lookup.
- `Store.PromoteToCore` and `Store.Delete` need no change: both key
  off `id`, not `Scope`, and once `Save` stores a normalized `Entry.
  Scope`, `Delete`'s use of `r.entry.Scope` to call `removeFromScope`
  already carries the normalized value.
- `Entry.Validate` keeps its value receiver and its existing blank
  check unchanged. `Validate` cannot mutate the caller's struct, and
  the fix does not need it to: normalization lives once, in `Store`.

Outside:

- No change to `Entry`'s field set, `Query`'s field set, or any
  exported signature. `api/longtermmemory.txt` is unaffected: this
  is a behavior fix inside already-locked signatures.
- No change to `entryID`'s hash algorithm beyond the value it now
  receives. A pre-existing stored id computed from an unnormalized
  scope keeps its old id; this fix changes behavior for saves made
  after it lands, not a migration of existing rows. This package is
  in-memory only, so no stored state survives a process restart
  anyway.

## API

No exported surface change. `api/longtermmemory.txt` stays as
locked.

## Tests

In `longtermmemory/longtermmemory_test/`, a new or existing file
under the 500-line limit:

- Save with a padded scope (`"proj "`), then `Search` with the
  trimmed scope (`"proj"`): the saved entry is a hit.
- Save with a trimmed scope (`"proj"`), then `Search` with a padded
  scope (`" proj"`): the saved entry is a hit. The reverse direction.
- The same round trip for `Count`: a save under a padded scope, then
  `Count` under the trimmed scope, reports `1`.
- The same round trip for `CoreEntries`: a save and `PromoteToCore`
  under a padded scope, then `CoreEntries` under the trimmed scope,
  returns the entry.
- Two saves whose `Scope` differ only by surrounding whitespace
  produce the same `entryID`: the second `Save` call returns the
  first call's id, proving idempotent dedupe now sees them as one
  scope.
- `Result.Scope` and the stored `Entry.Scope`, read back through
  `Search`, carry the trimmed form, not the padded input.

## Verification

- `make verify` passes; `longtermmemory` holds the 85 coverage floor.
- `go test -race ./longtermmemory/...` passes.
- `python3 scripts/check_api.py` passes with no `api/` diff.
- No `policy/layers.json` change: this package stays a leaf.
