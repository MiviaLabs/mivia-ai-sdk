---
name: architect
description: Assess the architecture of code in this SDK. Trigger when the user asks whether a package needs a refactor, whether the implementation is overengineered, whether complexity is too high, whether design patterns are used well, or wants a review of design, structure, coupling, or complexity. Also trigger for "is this the right design", "should I split this", "is this too complex", and any architecture review of a package. Use the four lenses: refactor need, overengineering, complexity, and design patterns.
---

# Architect

Assess the architecture of a package. Use four lenses: refactor need,
overengineering, complexity, and design patterns. Report findings with
a verdict per lens. Do not change code during the assessment. Report
first.

## Read first

Read these before any assessment.

- `AGENTS.md` — the contribution contract. The Building blocks section
  is the design rule this skill enforces.
- `docs/architecture.md` — the module map and the message flow.
- `policy/layers.json` — the allowed import edges.
- `docs/plans/<pkg>.md` — the plan the package promised.

## The four lenses

Apply every lens to the target. Each lens has a checklist. Use the
evidence you gather, not your instinct.

### Lens one: refactor need

Does the package need to split, merge, or rename?

- Read the package doc comment and the plan. Does the package do one
  thing? A package with several responsibilities is a candidate for a
  split.
- Do the files share one type or one invariant set? If they do, they
  belong together. Shared invariants hold a package together.
- Is the package below the cohesion threshold? Count the exported
  symbols. Group them by concern. More than one concern per package is
  a split signal.
- Follow the Building blocks rule in AGENTS.md. Do not split a working
  package for purity alone. Split only when a real consumer needs the
  concern by itself. There must be a caller.
- Does another package copy a type instead of importing it? A copy is a
  fork. Use the source package instead.

### Lens two: overengineering

Does the implementation add abstraction without a caller?

- For every interface, struct, and generic function, find the caller.
  No caller means speculative generality. Remove it or justify it.
- Does the abstraction hide a single concrete behavior? One
  implementation behind an interface is usually needless. The interface
  earns its place only with two or more callers or a test boundary.
- Are there config options, hooks, or extension points nobody uses?
  Unused flexibility is cost.
- Does the design solve the stated problem with the fewest moving
  parts? AGENTS.md says three files beat a framework.
- Is error handling, state, or validation duplicated? Duplication is
  simpler to remove than a clever indirection.

### Lens three: complexity

Is the package too hard to reason about?

- Run the structure gate. `scripts/check_structure.py` enforces files
  at or below 500 lines and functions at or below 80 lines. A near-limit
  file is a warning.
- Look for nested conditionals and long control flow. Count branching
  in each function. High branch density is a complexity signal.
- Look for memory and concurrency. A mutex-guarded data structure is
  complex. A lock-free structure is a red flag unless it is justified.
- Count the ways a value can be invalid. More invalid states than valid
  ones is a smell. `Validate` should own the full rule set.
- Measure the public surface. Many exported symbols are hard to learn.
  Each exported symbol is a promise you must keep.

### Lens four: design patterns

Which patterns should be present, and are they present and correct?

- Naming maps to the pattern. A pattern misused is worse than none.
- A package that validates its own invariants uses a Check method or
  the Validate pattern. This SDK centralizes validation in `Validate`
  and calls it from `Encode` and `Decode`.
- A package with a lifecycle uses a constructor plus value semantics.
  `New` returns a value or pointer plus an error. The caller owns the
  value.
- A package with roles and membership uses a roster with sentinel
  errors. Sentinel errors are checked with `errors.Is`.
- A package that does one step on the wire uses the Encode-Decode pair.
  Invalid data never crosses a boundary because `Validate` runs first.
- Composition happens through the public API. A package does not see
  its caller. Direction flows inward.
- Do not invent a pattern to force one in. The simplest correct shape
  is the right pattern.

## Repo-aware evidence

Run these for the target package. Each command is a fact source.

- `make verify` — the full gate. Record which passes and which fails.
- `go vet ./...` and `go test -race ./...` — safety and data races.
- `python3 scripts/check_structure.py` — file and function size.
- `python3 scripts/check_deps.py` — import edges against layers.json.
- `cat api/<pkg>.txt` — the locked public surface.
- `go test -cover ./<pkg>/...` — coverage versus the 85 floor.
- `gofmt -l .` and `go vet ./...` — mechanics.

Some facts override the lenses. A coverage score below 85 is a finding.
A plan that promises a shape the code does not keep is a finding. A dep
edge the plan does not list is a finding.

## Verdict format

Report one verdict per lens. Each verdict is one of these:

- `CLEAN` — the concern holds; no change needed.
- `WEAK` — the concern has issues, but no refactor is required.
- `REFACTOR` — the concern fails; plan a change.

Then give the summary. The summary is the total architecture health.
It is one of the same three words. State it first.

Every finding ends with a solution. A finding with no solution is not a
finding. The solution is one of two kinds.

- One clear fix. Name the concrete change and the file:line it touches.
  The fix is a single step the builder can take.
- Options A, B, C. Use this when the direction is a decision, not a
  fact. Mark the recommended option. Say why in one sentence. This
  matches the AGENTS.md orchestrator rule for choices.

For every finding, give a file:line and the severity (low, medium,
high). Then give the solution in the chosen form.

## Output structure

Write the report in this order. Keep it checkable, not long.

1. The one-word verdict for each lens and for the total.
2. Evidence: the gate results you collected.
3. Findings: each with file:line, severity, and a solution. End every
   finding with either a recommended fix or options A/B/C.
4. Pattern map: which patterns are present and where.
5. Recommendation: the smallest change that works. Pick one option when
   the finding offered choices.

Examples of the two solution forms:

```text
Finding 3 (medium): VerifyThread assumes one writer per thread.
  room/envelope-go: a second writer forks the chain and the thread
  fails. This is a known, documented limit, not a bug.
  Options:
    A. Keep the limit. Document it as deliberate (Recommended).
       Reason: no current caller needs concurrent writers.
    B. Move to a multi-parent DAG. More work, speculative today.
    C. Enforce serialization at the transport layer now.
```

```text
Finding 1 (low): Acks are not cryptographically signed.
  envelope/ack.go: a forged Ack can claim a wrong `from`.
  Fix: sign the ack bytes like Message, or document that attribution
  is out of band. Add a conformance vector for the signed ack.
```

A recommendation is not optional. The report ends with a named path
forward, even when that path is "do nothing, keep the documented
limit".

## Constraints

- Never change code during the assessment. Report first.
- A finding needs a reproduction and a solution. Guesses are not
  findings. A finding with no named fix or option set is incomplete.
- Follow the STE writing standard: one idea per sentence, at most 25
  words, no filler words.
- No audit-finding labels. A label is a letter A through G followed by
  a digit. Describe rules with words.
- Do not weaken a gate or raise a limit to make a verdict pass. Change
  the design instead.

## After the assessment

When the user asks for the fix, route a refactor through the delivery
loop in `.claude/skills/delivery/SKILL.md`: planner, plan-reviewer,
builder, reviewer, verify. This skill only assesses. It does not build.
