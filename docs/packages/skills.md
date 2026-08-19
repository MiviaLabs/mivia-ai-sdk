# Package reference: skills

The skills package holds a reusable instruction bundle a caller
registers under a name and finds again by trigger phrase or by name:
`Skill` and a `Registry`. A skill is read, not called: it carries
guidance text, not a callable action. `skills` is a leaf package: no
I/O, no goroutine, no persistence, and no file format of its own. The
exported surface below mirrors `api/skills.txt`.

## Types

- `Skill` — `Name`, `Instructions`, `Triggers []string`,
  `RequiredTools []string`. `Name` is the registration key.
  `Instructions` is the full guidance text a caller reads. `Triggers`
  is the phrase list `Registry.Match` compares a query against.
  `RequiredTools` names tool names this skill expects available;
  `skills` never reads or enforces it. `Triggers` and `RequiredTools`
  are exported slices; `Registry.Add` does not defensively copy
  either, matching `discovery.Card`'s no-copy convention for
  `Capabilities`.
- `Registry` — holds skills by name. Built only through `New`. Safe
  for concurrent `Add`, `Get`, `Remove`, `Names`, and `Match`; a
  `sync.RWMutex` guards the map.

## Functions and methods

- `New()` — creates an empty `Registry`.
- `Skill.Validate()` — checks `Skill`'s invariants. `Registry.Add`
  calls this before it registers a skill.
- `Registry.Add(s)` — calls `s.Validate()` and returns its error
  unchanged on failure. Rejects a duplicate `Name` with
  `ErrDuplicateName`. Registers `s` under `s.Name` otherwise.
- `Registry.Get(name)` — resolves `name`. Returns `false` when `name`
  is absent.
- `Registry.Remove(name)` — removes `name`. Returns whether `name` was
  present.
- `Registry.Names()` — lists every registered name. Order is
  unspecified, matching `providerregistry.Registry.Names`.
- `Registry.Match(query)` — returns every registered skill with a
  `Triggers` entry equal to `query` under `strings.EqualFold`. Results
  sort by `Name` ascending.

## Failure modes

Use `errors.Is` to test these.

- `ErrBlankName` ("skills: name must not be blank") — `Validate`
  returns it when `Name` is blank after `strings.TrimSpace`. Pinned by
  `skills/skills_test/skill_test.go`.
- `ErrBlankInstructions` ("skills: instructions must not be blank") —
  `Validate` returns it when `Instructions` is blank after
  `strings.TrimSpace`. Pinned by `skills/skills_test/skill_test.go`.
- `ErrBlankTrigger` ("skills: trigger entry must not be blank") —
  `Validate` returns it when a `Triggers` entry is blank after
  `strings.TrimSpace`. Pinned by `skills/skills_test/skill_test.go`.
- `ErrDuplicateTrigger` ("skills: duplicate trigger entry") —
  `Validate` returns it when two `Triggers` entries are equal under
  `strings.EqualFold` after trim. Pinned by
  `skills/skills_test/skill_test.go`.
- `ErrDuplicateName` ("skills: name already registered") — `Add`
  returns it for a `Name` already in the registry. Pinned by
  `skills/skills_test/registry_test.go`.

## Invariants

- `Validate` checks `Name`, then `Instructions`, then `Triggers`, in
  that fixed order; the first failing check wins. Pinned by
  `skills/skills_test/skill_test.go`'s `TestValidateCheckOrder`.
- `Triggers` may be empty. A skill a caller loads only by explicit
  `Get(name)` needs no trigger phrase.
- `RequiredTools` carries no check. It is advisory metadata; `Add`
  never cross-checks it against any `tools.Registry`.
- `Add` does not defensively copy `Triggers` or `RequiredTools`. A
  caller that mutates a slice after `Add` mutates the registry's
  stored `Skill` too. Pinned by
  `skills/skills_test/registry_test.go`'s
  `TestAddSharesTriggersBackingStorage`.
- `Match` searches `Triggers` only, never `Name`. `Match` never trims
  `query`; a padded query does not match an unpadded trigger entry.
  `Match` returns `nil`, not an empty non-nil slice, when nothing
  matches, and for a blank `query`.

## Why this shape

`skills` is the SDK-side analog to a sibling consumer repo's
`internal/skills` package: a reusable, policy-bearing instruction
bundle an agent reads as guidance, distinct from `tools.Tool`, an
atomic callable action. `skills` declines an import edge to `trigger`
and `discovery`: a `Skill.Triggers` entry is a static phrase matched
by string comparison, never a runtime predicate like
`trigger.Condition`, and `Registry.Match` must return every hit, not
the first one `discovery.Card.Match` returns. `skills` implements the
same `strings.EqualFold` comparison rule directly rather than import
either package for one reused loop body. See
[../plans/skills.md](../plans/skills.md) for the full design
rationale.

## Wire contract

`skills` defines no wire format. `Skill` crosses no boundary inside
this package; no conformance vector applies.

## Usage

```go
package main

import (
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/skills"
)

func main() {
    r := skills.New()

    deploy := skills.Skill{
        Name:          "deploy",
        Instructions:  "ship the release through the deploy pipeline",
        Triggers:      []string{"deploy", "release"},
        RequiredTools: []string{"deploy-cli"},
    }
    if err := r.Add(deploy); err != nil {
        panic(err)
    }

    matches := r.Match("release")
    for _, s := range matches {
        fmt.Println(s.Name, "-", s.Instructions)
    }
}
```

### What the program shows

`Add` registers one skill named `"deploy"` under two trigger phrases.
`Match("release")` compares the query against every registered
skill's `Triggers`, case-insensitively, and finds the one hit. The
program prints `deploy - ship the release through the deploy
pipeline`.
