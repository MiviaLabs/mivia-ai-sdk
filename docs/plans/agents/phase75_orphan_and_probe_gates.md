# Phase 75: orphan-package gate and probe-basename collision gate

Status: plan, ready for plan review. Closes two mistake patterns that
recurred this session despite being documented in prose: an uncalled
package shipping undetected, and a Semgrep probe fixture silently
overwriting another rule's expected entry. Both patterns are
mechanical. Neither has a gate today.

## Why this plan exists

AGENTS.md already states the rule: "Reject planner output that adds
abstraction without a caller. No speculative generality." The rule is
prose only. Five packages shipped with zero non-test callers across
this session, found only by manual review each time: `workspace`,
`envfile`, `secretpath`, `diff`, and `longtermmemory`. `workspace`,
`secretpath`, and `diff` later gained a real caller through
`subagent`'s phase 71 wiring. `envfile` and `longtermmemory` did not.

A repo-wide scan for this plan found the orphan set is larger than
those five. Thirteen packages carry zero non-test, non-test-directory
internal callers today (see the Current orphan status table below).
Some are genuine instances of the same mistake. Others are
composition roots or test-only fixtures the architecture always
leaves uncalled by design. A gate that fails on both kinds alike would
either block legitimate work or get disabled. This plan's gate must
tell the two apart, explicitly, in a reviewable file.

The second pattern: `scripts/check_semgrep_probes.py` keys its
`expected` and `hits` dicts by fixture basename alone, pooled across
every scoped-rule probe. A new probe reusing an existing basename
silently overwrites the earlier rule's `expected` entry. This session
caught that twice, both times by a reviewer reading the script's own
warning comment, never by the script itself. The check is cheap and
fully mechanical; it belongs in the script, not in review.

## Goal

Give both mistake patterns a gate. An orphan package must be declared
in a checked-in file, with a reason and a target, the same way
`scripts/mutation_denylist/*.json` declares a mutation-floor
exception. A probe-fixture basename collision must fail loudly before
Semgrep runs, not after a confusing count mismatch.

## Scope

Inside:

- `scripts/check_orphan_packages.py`, a new gate script. Detects
  every top-level Go package with zero non-test callers among other
  top-level packages' non-test `.go` files, and fails on any such
  package missing from `policy/pending_wiring.json`.
- `policy/pending_wiring.json`, a new checked-in file. Declares every
  currently orphaned package, each with a `reason`, a `target`, and a
  `permanent` flag. Populated in this same change with all thirteen
  packages the scan below found, so the gate ships green.
- `scripts/check_orphan_packages.py` joins `make verify-fast`,
  immediately after `python3 scripts/check_plan.py` in the Makefile.
  Only the real-tree scan, run with no `--probe` flag, joins
  `verify-fast`.
- `scripts/check_orphan_packages.py --probe` joins `make verify`, in
  the same block as `check_mutation.py --probe` and
  `check_semgrep_probes.py`. It does not join `verify-fast`, matching
  the repo's existing convention of keeping self-test suites out of
  the pre-commit tier.
- A self-check inside `scripts/check_semgrep_probes.py`: after every
  fixture file is written to the temp scan directory, and before the
  `scan(tmp)` call, assert every written basename is unique. Fails
  loudly, naming both rule IDs or block labels and the shared
  basename, on a collision.
- A one-sentence addition to AGENTS.md's Orchestrator role bullet and
  one new bullet in AGENTS.md's Enforcement ladder section, naming
  the new mechanical gate. Exact text below.

Outside:

- Fixing the `runconfig`-to-`subagent` gap this scan found: `policy/
  layers.json` and `docs/plans/runconfig.md` both describe a
  `runconfig` import of `subagent`, but no `runconfig/*.go` file
  imports it yet. That is a permitted-but-unused policy edge, a
  different problem from an undeclared orphan package. This plan
  records it as the `target` for `subagent`'s `pending_wiring.json`
  entry and leaves the wiring itself to a future phase.
- Any change to `scripts/mutation_denylist/*.json` or `make
  mutation-gate`. Unrelated gate.
- Widening or narrowing any existing Semgrep rule. This plan adds a
  Python-side self-check only.
- Cross-checking `pending_wiring.json` against each package's plan
  prose with text matching. See "Plan-doc consistency" below for why.

## Ground truth: grep, not `policy/layers.json`

`policy/layers.json` states which imports a package is *allowed* to
make. It does not state which imports a package's code *actually*
makes. `runconfig`'s row lists `subagent`, but no `runconfig/*.go`
file imports `subagent` today (confirmed by grep, see Outside above).
Treating the policy file as ground truth would call `subagent`
non-orphan on the strength of a permission nobody has used yet. That
hides the exact pattern this gate exists to catch.

`check_orphan_packages.py` instead re-derives the same import fact
`scripts/check_deps.py` already computes: it greps every non-test
`.go` file's import lines for the module prefix, using
`check_deps.py`'s own `IMPORT` regex and `package_dirs` helper,
imported directly rather than duplicated. A package is an orphan when
no other top-level package's non-test source imports it. A test file
never counts, matching `check_deps.py`'s own test exemption and
`durablefence`'s documented test-only contract.

## `policy/pending_wiring.json` shape

```json
{
  "comment": "Packages with zero internal, non-test callers, declared on purpose. check_orphan_packages.py fails on any orphan package missing here, or on a stale entry: one whose package no longer exists, or one that has gained a real caller. permanent=false means an internal caller is expected later, named in target. permanent=true means this package is a composition root or a test-only fixture, consumed only by application code outside this module or only from another package's _test directory, and target names that consumer.",
  "packages": {
    "envfile": {
      "reason": "Dotenv parser leaf. No package reads its output yet.",
      "target": "not yet planned",
      "permanent": false
    },
    "longtermmemory": {
      "reason": "Ported from mivia-agent as a leaf package. No package wires it into a running agent yet.",
      "target": "not yet planned",
      "permanent": false
    },
    "mcp": {
      "reason": "MCP client leaf. Docs/plans/mcp.md ships it ahead of a caller by design, matching the tools and ledger precedent.",
      "target": "not yet planned",
      "permanent": false
    },
    "skills": {
      "reason": "Skill registry leaf, shipped ahead of the subagent wiring that will expose it as an internal tool.",
      "target": "not yet planned",
      "permanent": false
    },
    "spool": {
      "reason": "Truncation-spool leaf plus its tools.Tool wrapper, shipped ahead of a caller.",
      "target": "not yet planned",
      "permanent": false
    },
    "agentloop": {
      "reason": "Model tool-calling loop, shipped as a composition package with no internal caller yet.",
      "target": "not yet planned",
      "permanent": false
    },
    "subagent": {
      "reason": "Blocks-as-tools composition package. docs/plans/runconfig.md and policy/layers.json both describe a runconfig import of subagent; runconfig's code does not import it yet.",
      "target": "runconfig",
      "permanent": false
    },
    "a2aack": {
      "reason": "Edge adapter turning a remote A2A task round trip into agent.AckWait, per its AGENTS.md entry. An external application wires it to a flow.Step, not another SDK package.",
      "target": "external application code (outside this module)",
      "permanent": true
    },
    "a2aloopback": {
      "reason": "Test-only fixture. AGENTS.md's a2aclient entry calls it 'the loopback test fixture,' imported only from a2aclient and a2aack _test subdirectories, which this scan excludes.",
      "target": "a2aclient and a2aack _test subdirectories only",
      "permanent": true
    },
    "dispatch": {
      "reason": "NDJSON envelope HTTP endpoint. An external application's server wiring calls Endpoint.Handler, not another SDK package.",
      "target": "external application code (outside this module)",
      "permanent": true
    },
    "durablefence": {
      "reason": "Test-only conformance kit. AGENTS.md states it is imported only from another package's _test subdirectory.",
      "target": "ledger/ledger_test and other packages' _test subdirectories only",
      "permanent": true
    },
    "e2e": {
      "reason": "End-to-end scenario harness. Its own e2e_test subdirectory is the only intended caller, by design.",
      "target": "e2e/e2e_test scenarios only",
      "permanent": true
    },
    "runconfig": {
      "reason": "JSON-config composition layer over agentrun. A deployment's own main package loads a runner definition; no SDK package composes runconfig further.",
      "target": "external application code (outside this module)",
      "permanent": true
    }
  }
}
```

Every entry carries three required, non-empty fields. `reason`
explains why the package has no caller today. `target` names the
future consumer for a `permanent: false` entry, or the permanent
external or test-only consumer for a `permanent: true` entry.
`"not yet planned"` is a valid `target` value; an empty string is not.

## Two exemption kinds, one file

A temporary orphan and a permanent-by-design orphan are different
problems. A temporary orphan is this session's mistake pattern: a
package that should gain a caller and has not yet. A permanent orphan
is architecture: a composition root or a test-only fixture the SDK
never expects another internal package to import, the same way an
`agent` never imports a block that imports `agent`.

Folding both into one `pending_wiring.json` file, distinguished by the
`permanent` flag, keeps one schema and one gate instead of two parallel
files with the same shape. A reviewer reads the flag to see which kind
of exemption they are approving. This matches AGENTS.md's simplicity
rule: one file with a flag beats two files with the same three fields.

## Plan-doc consistency

`pending_wiring.json` does not require every touched package's own
`docs/plans/<pkg>.md` to restate its `target` in matching prose. Text
matching between a JSON field and free-form Markdown is brittle: a
builder rephrasing a plan's prose would break the check without
changing any fact. Instead, `check_orphan_packages.py` enforces the
one fact that matters mechanically: a `target` naming a real package
must name a package that exists as a top-level directory today (an
early check, not a rename tripwire). A `target` naming an external
caller or "not yet planned" needs no such check. This keeps the cross
check objective without parsing prose.

## Verify tier: `verify-fast`

`check_orphan_packages.py` performs the same class of work as
`check_deps.py` and `check_plan.py`, both already in `verify-fast`:
a single pass over already-small `.go` source files with a regex, no
subprocess, no network call, no Semgrep invocation. `check_deps.py`
scans the same file set today at negligible cost. Running the orphan
scan costs one more pass over the same files `check_deps.py` already
reads, not a new class of cost. `verify-fast` runs on every commit
through the pre-commit hook; a check this cheap belongs there, not
gated behind the slower `verify` tier reserved for coverage and
Semgrep probes.

## Current orphan status (this scan, commit at HEAD before this plan)

Every top-level package with zero non-test callers among other
packages' non-test `.go` files, found by grepping every package's
source for `"github.com/MiviaLabs/mivia-ai-sdk/<name>"`:

| Package | Callers found | Status this plan assigns |
|---|---|---|
| `envfile` | none | pending, not yet planned |
| `longtermmemory` | none | pending, not yet planned |
| `mcp` | none | pending, not yet planned |
| `skills` | none | pending, not yet planned |
| `spool` | none | pending, not yet planned |
| `agentloop` | none | pending, not yet planned |
| `subagent` | none | pending, target `runconfig` |
| `a2aack` | none (test-only: `a2aclient`) | permanent, external caller |
| `a2aloopback` | none (test-only: `a2aclient`, `a2aack`) | permanent, test-only |
| `dispatch` | none | permanent, external caller |
| `durablefence` | none (test-only, by design) | permanent, test-only |
| `e2e` | none (test-only: `e2e/e2e_test`) | permanent, test-only |
| `runconfig` | none | permanent, external caller |

`workspace`, `secretpath`, and `diff` are confirmed no longer
orphaned: `subagent/filetoolset.go` imports `workspace` and
`secretpath`, and `subagent/difftool.go` imports `diff`. Neither
needs a `pending_wiring.json` entry.

## `check_orphan_packages.py` self-tests

Following `check_mutation.py`'s `--probe` convention and
`check_semgrep_probes.py`'s fixture-directory pattern, the script
proves its own logic against small, temp-directory fixtures before
trusting its output on the real tree. A `--probe` flag runs these
checks and exits nonzero on any failure, independent of the real-tree
scan:

- `probe_true_orphan`: a fixture package with no importer anywhere is
  reported orphan.
- `probe_caller_found`: a fixture package imported by another
  package's non-test `.go` file is not reported orphan.
- `probe_test_only_caller_ignored`: a fixture package imported only
  from another package's `*_test.go` file is still reported orphan;
  a test import never counts.
- `probe_pending_exempts`: an orphan package listed with
  `permanent: false` and both fields set passes the gate.
- `probe_permanent_exempts`: an orphan package listed with
  `permanent: true` and both fields set passes the gate.
- `probe_missing_field_fails`: an entry missing `reason`, missing
  `target`, or holding an empty string for either, fails loudly and
  names the package and the missing field.
- `probe_stale_entry_fails`: an entry naming a package directory that
  no longer exists, or a package that has gained a real caller since
  the entry was written, fails loudly and names the stale entry. This
  mirrors `check_mutation.py`'s `denylisted_spans` snippet check: a
  stale exception never rubber-stamps silently.
- `probe_real_tree_matches_declared_set`: run against the real repo
  tree, the orphan set found equals exactly the key set of
  `policy/pending_wiring.json`, with no undeclared orphan and no
  stale declared one.

## Probe-basename collision self-check

`check_semgrep_probes.py` writes its full fixture set to the temp scan
directory across the flat `PROBES` list and six scoped-rule blocks
(`a2aclient`, `a2aloopback`, `no_a2aloopback`, `mcp`, `ledger`,
`schema`). The six scoped blocks hold no pre-existing registry of
their `(vfile, cfile)` pairs: each is an inline `write_text(...)` call
naming its basename as a string literal, unlike the flat `PROBES`
list. A pre-write walk has nothing to walk for these six blocks; their
basenames are known only by running the same code that writes them.

The fix is a post-write pass instead. In `main()`, after the last
scoped-rule `write_text` call (the schema block's
`clean_jsonschema_import.go`, the line just before `data = scan(tmp)`)
and before that `scan(tmp)` call runs, insert one assertion: walk
`tmp.rglob("*.go")`, group the matches by `Path.name`, and assert no
basename maps to more than one file. On a collision, raise
`AssertionError` naming the shared basename and the full paths of
every file sharing it, before any Semgrep invocation runs. This
verifies what was actually written to disk, across all eight fixture
groups, instead of a pre-execution registry that does not exist for
the six scoped blocks. It turns a collision into an immediate, loud
failure instead of a confusing count mismatch after a slow Semgrep
scan.

## API

No new exported Go symbols. This phase adds two Python gate scripts
and one JSON policy file; none is a Go API surface, so `api/` locks
are unaffected.

## Tests

- `scripts/check_orphan_packages.py --probe` runs the eight self-tests
  listed above, added the same session the script lands.
- `scripts/check_semgrep_probes.py`'s existing run, unchanged in
  count, now additionally proves the new basename-uniqueness
  assertion passes on the current fixture set (no collision exists
  today) and would fail if a future probe reused a basename.
- No new Go test file. This phase changes tooling and one JSON data
  file, not package behavior.

## Verification

- `python3 scripts/check_orphan_packages.py` — new gate, must pass
  against the tree this plan ships, with `policy/pending_wiring.json`
  populated for all thirteen entries above.
- `python3 scripts/check_orphan_packages.py --probe` — must pass under
  `make verify`, alongside `check_mutation.py --probe` and
  `check_semgrep_probes.py`, before the real-tree gate is trusted in
  `verify-fast`.
- `make verify-fast` — must still pass, now running the new real-tree
  gate (no `--probe` flag) after `check_plan.py`.
- `python3 scripts/check_semgrep_probes.py` — must still pass, now
  running the new collision self-check before Semgrep.
- `python3 scripts/check_plan.py` — unaffected; this phase adds no
  new top-level Go package.
- `python3 scripts/check_deps.py` — unaffected; no new import edge.
- `python3 scripts/check_prose.py` and `python3 scripts/check_labels.py`
  — run against this plan file itself before it lands.

## AGENTS.md text change

Orchestrator role bullet, append one sentence to the existing
"Simplicity over complexity" bullet:

> Simplicity over complexity. Prefer the smallest change that works.
> Three files beat a framework. Reject planner output that adds
> abstraction without a caller. No speculative generality.
> `scripts/check_orphan_packages.py` enforces the no-caller rule
> mechanically against `policy/pending_wiring.json`.

Enforcement ladder section, new bullet after the plan-file bullet:

> Do not leave a package with zero internal callers undeclared. List
> it in `policy/pending_wiring.json` with a reason and a target. Gate:
> `scripts/check_orphan_packages.py`.
