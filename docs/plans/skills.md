# Plan: skills

Status: shipped.

## Goal

Let a caller register a reusable instruction bundle under a name, and
find the bundles that fit a query. A skill is read, not called: it
carries instructions text, a set of trigger phrases that say when it
applies, and the tool names it expects available. `skills` answers
"which registered skill fits this query," the same way `discovery`
answers "which capability fits this need."

## Scope

Inside:

- The `Skill` struct: `Name`, `Instructions`, `Triggers`,
  `RequiredTools`.
- `Skill.Validate`, the invariant check `Add` runs before it
  registers a skill.
- The `Registry` type and its `Add`, `Get`, `Remove`, `Names`, and
  `Match` methods.
- A mutex-guarded map, matching `tools.Registry`'s concurrency shape:
  built only through `New`, safe for concurrent `Add`, `Get`,
  `Remove`, `Names`, and `Match`.

`skills` is a leaf package: no I/O, no goroutine, no persistence, and
no file format of its own. It imports nothing of this module. Stdlib
only: `errors`, `sort`, `strings`, `sync`. The policy row is
`"skills": []`.

Outside:

- SKILL.md or any frontmatter parsing. A caller's own loader builds a
  `Skill` value from whatever source format it uses, the same way no
  package in this module parses an agent card's source file for it;
  `discovery.Parse` reads bytes already in the wire shape, not a
  markdown dialect.
- Permission or policy enforcement. `RequiredTools` and any future
  permission field are metadata this package publishes; a caller
  cross-checks `RequiredTools` against its own `tools.Registry`, and
  gates execution through its own `hooks` wiring or an equivalent
  check. `skills` enforces nothing.
- Resource or lazy-reference loading. A caller that wants an attached
  file, prompt fragment, or example loads it itself; that is
  caller-owned I/O, out of scope for a leaf package.
- Versioning. `Skill` carries no version field. A caller that needs
  one names it in `Name` or manages it in its own store.
- A poller, a scheduler binding, or any change to `scheduler`,
  `trigger`, `discovery`, `hooks`, `tools`, `agent`, or `subagent`.
  This plan edits none of their files and none of their plans.
- A `Run` method or any callable field. `tools.Tool.Run` takes an
  input and returns an output: a caller invokes it and gets a result.
  `Skill` has no `Run` method. A caller reads `Skill.Instructions`
  into a prompt or a subagent's context, the same way it reads a
  file. `skills` never runs anything and never wraps
  `context.Context`.

### Why zero import edges, not two

`trigger.Condition` is a runtime predicate a caller evaluates once,
tied to a `trigger.Action` that then runs. A `Skill.Triggers` entry is
a static phrase, matched by string comparison, never executed.
Importing `trigger` would buy `skills` nothing; `Skill` has no
`Action` to pair a `Condition` with.

`discovery.Card.Match` is the closer shape: a value-receiver method
that compares a need string against a capability list with
`strings.EqualFold`, returning the first hit. `skills.Registry.Match`
reuses that exact comparison rule but not the method itself.
`Card.Match` runs on one `Card`; `Registry.Match` scans many `Skill`
values and returns every hit, not the first — a different return
shape (`[]Skill` versus `(string, bool)`). Wrapping each `Skill` in a
synthetic `Card` would add an import edge and an adapter step for one
reused loop body. `skills` implements the same `EqualFold` rule
directly, in its own `match.go`. `skills` stays at zero internal
imports, matching `tools`, `trigger`, and `discovery`.

## API

The surface below lands in `api/skills.txt` via `make api-update`.

- `type Skill struct { Name string; Instructions string; Triggers []string; RequiredTools []string }`
  — a reusable instruction bundle. `Name` is the registration key.
  `Instructions` is the full guidance text a caller reads. `Triggers`
  is the phrase list `Match` compares a query against. `RequiredTools`
  names tool names this skill expects available; this package never
  reads or enforces it. `Triggers` and `RequiredTools` are exported
  slices; `Add` does not defensively copy either, matching
  `discovery.Card`'s documented no-copy convention for `Capabilities`
  and `envelope.Message`'s same rule for its own slice fields. A
  caller that mutates a slice after `Add` mutates the registry's
  stored `Skill` too.
- `func (s Skill) Validate() error` — checks: `Name` non-blank after
  `strings.TrimSpace`; `Instructions` non-blank after
  `strings.TrimSpace`; every `Triggers` entry non-blank after
  `strings.TrimSpace`; no two `Triggers` entries equal under
  `strings.EqualFold` after trim. `Triggers` may be empty. A skill a
  caller loads only by explicit `Get(name)` call needs no trigger
  phrase. `RequiredTools` carries no check; it is advisory metadata.
  `Add` calls `Validate` before it registers a skill.
- `type Registry struct` — holds skills by name. Unexported fields.
  Built only through `New`. Safe for concurrent `Add`, `Get`,
  `Remove`, `Names`, and `Match`; a `sync.RWMutex` guards the map.
- `func New() *Registry` — builds an empty registry.
- `func (r *Registry) Add(s Skill) error` — calls `s.Validate()` and
  returns its error unchanged on failure. Rejects a duplicate `Name`
  with `ErrDuplicateName`. Registers `s` under `s.Name` otherwise.
- `func (r *Registry) Get(name string) (Skill, bool)` — resolves a
  name. Returns `false` when the name is absent.
- `func (r *Registry) Remove(name string) bool` — removes a name.
  Returns whether the name was present, matching
  `tools.Registry.Remove`'s exact contract. Removing an absent name
  changes nothing.
- `func (r *Registry) Names() []string` — lists every registered
  name. Order is unspecified, matching `providerregistry.Registry.
  Names`; a caller that needs a stable order sorts the result.
- `func (r *Registry) Match(query string) []Skill` — returns every
  registered skill with a `Triggers` entry equal to `query` under
  `strings.EqualFold`. Returns `nil` for a blank `query` or no hit.
  `Match` never trims `query`; a padded query does not match an
  unpadded trigger entry, mirroring `discovery.Card.Match`. Results
  sort by `Name` ascending, so the result is deterministic across
  calls regardless of Go's unspecified map iteration order.
- `var ErrBlankName` — `Validate` returns this when `Name` is blank
  after `strings.TrimSpace`.
- `var ErrBlankInstructions` — `Validate` returns this when
  `Instructions` is blank after `strings.TrimSpace`.
- `var ErrBlankTrigger` — `Validate` returns this when a `Triggers`
  entry is blank after `strings.TrimSpace`.
- `var ErrDuplicateTrigger` — `Validate` returns this when two
  `Triggers` entries are equal under `strings.EqualFold` after trim.
- `var ErrDuplicateName` — `Add` returns this for a `Name` already in
  the registry.

Every sentinel is tested with `errors.Is`.

## Tests

Test files live in `skills/skills_test/`, an external test package.

- `skill_test.go` — red-green cases for `Validate`. A blank `Name`
  returns `ErrBlankName`. A whitespace-only `Name` returns
  `ErrBlankName`. A blank `Instructions` returns
  `ErrBlankInstructions`. A blank `Triggers` entry returns
  `ErrBlankTrigger`. Two `Triggers` entries equal under `EqualFold`
  (for example `"Deploy"` and `"deploy"`) return `ErrDuplicateTrigger`.
  A `Skill` with empty `Triggers` and non-blank `Name` and
  `Instructions` passes `Validate`. A fully populated `Skill`,
  including a non-empty `RequiredTools`, passes `Validate` with no
  check on `RequiredTools`' contents.
- `registry_test.go` — red-green cases for `Add`, `Get`, `Remove`,
  and `Names`. `Add` returns an invalid `Skill`'s `Validate` error
  unchanged, and the registry stays empty. `Add` accepts a valid
  `Skill` and rejects a second `Add` under the same `Name` with
  `ErrDuplicateName`. `Get` returns the skill and `true` for a
  registered name; returns the zero `Skill` and `false` for an
  unknown name. `Remove` on a present name returns `true`; a
  following `Get` returns `false`. `Remove` on an absent name returns
  `false` and leaves the registry unchanged. `Names` returns every
  registered name, checked as a set (order unspecified). A slice
  aliasing case: `Add` a `Skill` whose `Triggers` slice the caller
  keeps a reference to; mutate index zero of that slice after `Add`;
  `Get` the same name back and assert its `Triggers[0]` reflects the
  mutation, proving `Add` shares backing storage rather than copying
  it, matching `discovery.Card`'s no-copy convention.
- `match_test.go` — red-green cases for `Match`. A query equal to one
  skill's trigger entry, case-insensitive, returns that skill. A
  query matching no trigger entry returns `nil`. A blank query
  (`""`) against a populated registry returns `nil`. A padded query
  (a leading space) does not match an unpadded trigger entry. A query
  matching triggers on three different skills returns all three,
  sorted by `Name`. `Match` searches `Triggers` only, never `Name`:
  register a skill with `Name: "deploy-bot"` and `Triggers` that do
  not contain `"deploy-bot"`; assert `Match("deploy-bot")` returns
  `nil`.
- `registry_integration_test.go` — register three skills: one for
  code review (`Triggers: []string{"code review", "review pr"}`), one
  for deployment (`Triggers: []string{"deploy", "release"}`), and one
  for incident response (`Triggers: []string{"incident", "outage"}`),
  each with a distinct `RequiredTools` list. Run `Match("deploy")` and
  assert only the deployment skill returns. Run `Match("code review")`
  and assert only the code-review skill returns. Prove the returned
  `Skill.RequiredTools` value is the exact slice registered, unread
  and unenforced by this package: a tool name absent from any real
  `tools.Registry` still registers and still matches.
- `match_bench_test.go` — benchmark `Match` over a registry of one
  hundred skills, five triggers each, querying a trigger phrase that
  matches exactly one skill. Target under one microsecond per call.
  `AllocsPerRun` states a budget of two: one slice grow for the
  single-match result and one for the sort call's internal state; the
  builder records the measured baseline in this file.
- `registry_concurrent_test.go` — modeled on `tools/tools_test/
  registry_concurrent_test.go`'s pattern: N goroutines each call `Add`
  with a distinct name concurrently, then join; a following `Names`
  call finds every one of the N names. A second case races N `Match`
  calls for a shared query against N `Add` calls for N distinct new
  skills, under `go test -race`, asserting no call panics and every
  `Match` call returns a result consistent with some point-in-time
  registry state.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `skills` and for the total.
- `go test -race ./skills/...` passes, covering
  `registry_concurrent_test.go`.
- The `skills` row in `policy/layers.json` is `[]`, already present
  ahead of the code. `scripts/check_deps.py` passes with no edge from
  `skills` to `trigger`, `discovery`, `tools`, `hooks`, `agent`, or
  `subagent`, and no edge from any of those to `skills`.
- `api/skills.txt` lands via `make api-update` in the same change as
  the code, and locks `Skill`, `Validate`, `Registry`, `New`, `Add`,
  `Get`, `Remove`, `Names`, `Match`, `ErrBlankName`,
  `ErrBlankInstructions`, `ErrBlankTrigger`, `ErrDuplicateTrigger`,
  and `ErrDuplicateName`.
- `AGENTS.md`'s package layout list gains a `skills/` bullet, matching
  the existing bullets' level of detail: package name, one-sentence
  purpose, and its import edges (none). The bullet ends with the
  sentence "No caller yet; the agent/subagent wiring is a later
  phase," matching the honesty marker `tools`, `hooks`, and `trace`
  each carry for their own no-caller state.
- `docs/plans/agents/PHASES.md` gains a phase 63 paragraph recording
  the shipped shape, in the same change that ships the code, matching
  how phase 57 through phase 61 each closed out.
- This phase adds no conformance vectors. `skills` carries no wire
  format of its own; nothing crosses the envelope wire boundary.
