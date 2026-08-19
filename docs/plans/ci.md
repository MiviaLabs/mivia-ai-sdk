# Plan: CI

## Goal

Add GitHub Actions CI so the gates in `make verify` run on every push
and pull request, not only inside a contributor's local pre-commit
hook. Today no CI exists; AGENTS.md calls the gates "aspirational
until CI exists." This plan closes that gap with one workflow file.

## Scope

In scope:

- One workflow file, `.github/workflows/ci.yml`, that checks out the
  repo, sets up Go, and runs `make verify`.
- Go module and build caching so CI stays fast across runs.
- A documented decision on which Go version CI pins.
- A written recommendation for branch protection, scoped as a
  follow-up the user applies through GitHub repo settings.
- Update AGENTS.md:483-484 and docs/architecture.md's Gate system
  section once the workflow ships. State that CI runs `make verify`
  on push and pull request to `main`. State that CI stays
  informational only until branch protection is enabled. This is a
  doc edit alongside the workflow file, not a Makefile or gate-script
  change.

Out of scope:

- The workflow YAML content itself. This plan describes it; the
  builder writes it.
- Branch protection rules. GitHub repo settings are not a code
  change; no agent can apply them through git. This plan only
  recommends the setting and states why.
- `make mutation`. It runs on demand today, costs minutes per
  package, and stays out of both `verify` and CI. A separate plan
  covers mutation-coverage gating; this plan does not depend on it
  and does not block on it.
- Any change to `Makefile`, `.githooks/`, or the gate scripts. CI
  calls the existing `make verify` target unchanged.
- Repository visibility. The repo stays private. GitHub Actions runs
  on private repositories with no feature gated behind public
  visibility; nothing in this plan requires making the repo public.

## API

Not a Go package; this plan has no exported Go surface and adds no
row to `policy/layers.json`. The "API" here is the workflow's
observable contract:

- Workflow file: `.github/workflows/ci.yml`.
- Workflow name: `CI`.
- Triggers:
  - `push` on branch `main`.
  - `pull_request` targeting branch `main`.
- One job, `verify`, on `ubuntu-latest`:
  1. `actions/checkout` (standard action, pinned to a full commit
     SHA, with the version tag as a trailing comment).
  2. `actions/setup-go` (standard action, pinned to a full commit
     SHA, with the version tag as a trailing comment), configured
     with `go-version-file: go.mod` so CI always
     builds with the version go.mod declares, and with its built-in
     module cache enabled (`cache: true`), keyed on `go.sum`.
  3. Install Semgrep through `pip install semgrep`, pinned to one
     fixed version. Semgrep is a Python CLI tool the Makefile already
     assumes is on `PATH`; installing it via `pip` is not a GitHub
     Action and needs no separate justification under the
     no-third-party-Action rule.
  4. `make verify`.
- Standing rule: pin every GitHub Action in this workflow to a full
  commit SHA, with the version tag as a trailing comment. This rule
  applies to `actions/checkout`, `actions/setup-go`, and any Action
  added to this workflow later.
- No third-party GitHub Action beyond `actions/checkout` and
  `actions/setup-go`.
- No repository secret. `make verify` needs none; see Verification.

Go version decision: pin through `go-version-file: go.mod`, not a
hardcoded version string. go.mod declares `go 1.25.0`. The local
toolchain observed during planning is 1.26.0, but that is this
machine's installed toolchain, not the module's declared minimum.
Reading the version from go.mod keeps one source of truth: bumping
go.mod's `go` directive is the only change needed to move CI to a
newer Go version, and CI never drifts silently ahead of or behind
what the module declares.

## Tests

CI has no unit tests of its own; it runs the module's existing test
suite through `make verify`. Verification for this plan is that the
workflow triggers correctly and that `make verify` passes inside it:

- A pull request against `main` triggers one `verify` job run.
- A direct push to `main` triggers one `verify` job run.
- The job fails when any gate inside `make verify` fails: gofmt, go
  vet, `go test ./...`, the Python gates, `check_api.py`,
  `check_prose.py`, `check_labels.py`, the Semgrep scan, the
  suppression-marker scan, `go test -race ./...`, the coverage floor
  block, `verify-ledger-sqlite`, and the Semgrep probe suite.
- The job passes on a clean tree with no code change, proving the
  workflow itself introduces no new failure.

## Verification

Findings from this planning pass, folded into the plan:

- `make verify` already runs `go test -race ./...` as its own step,
  after `verify-fast` and before the coverage block. CI needs no
  separate race step; `make verify` covers it.
- `make verify` already depends on `verify-ledger-sqlite`, which
  builds and tests the `ledger` package under the `ledger_sqlite`
  tag and enforces its coverage floor. CI needs no second job for the
  tagged build; one `make verify` call exercises both the default
  build and the `ledger_sqlite`-tagged build.
- No test in the tree makes a real network call or reads a real
  secret. Two network-shaped strings are inert: an unresolvable
  `http://example.com/...` `$ref` used to test schema validation
  failure in `schema/schema_test/compile_test.go`, and a literal
  `https://example.test/v1` string used to test secret redaction in
  `workspace/workspace_test/secret_integration_test.go`.
  `mcp/stdio_transport_test.go` re-execs the test binary as a local
  subprocess gated by an environment variable; it opens no network
  connection. The `dispatch/dispatch_test` package
  (`badrequest_test.go`, `ladder_test.go`, `replay_test.go`,
  `reject_test.go`, `bodylimit_test.go`) and
  `mcp/http_transport_test.go` open real sockets through
  `httptest.NewServer`, and `dispatch/dispatch_test/client_test.go`
  dials `http://127.0.0.1:1` to force a connection-refused error.
  Every one of these stays on loopback and needs no egress, so none
  is a CI risk. CI needs no repository secret and no network-access
  exception.
- Pinning Semgrep in CI (see API, step 3) but not in local
  development creates a version-drift risk: a contributor's local
  `make verify` can pass against a different Semgrep version than CI
  enforces. Recommend, as a follow-up outside this plan's scope, that
  local dev pin the same Semgrep version, for example documented in
  README's dev-setup section as a future addition.
- Private-repo GitHub Actions minutes are quota-limited on GitHub's
  free tier. This repo's expected commit frequency stays well under
  that quota. A period of heavy commit volume should watch usage.
- CI enforcement today is informational only: the repo has no branch
  protection rule, so a failing check does not block a merge or a
  direct push to `main`. This plan's workflow file does not change
  that; it only makes the check visible on every commit and pull
  request.
- Recommend, as a follow-up outside this plan's scope, that the user
  turn on branch protection for `main` in GitHub repo settings,
  requiring the `verify` check to pass before merge. This is a
  repo-settings change, not a code change; no agent can apply it
  through git, so it stays a recommendation, not a deliverable here.

Commands that must pass before this plan is considered delivered:

- `python3 scripts/check_prose.py`
- `python3 scripts/check_labels.py`
- `python3 scripts/check_plan.py` (informational for this file; it
  gates only packages with `.go` files, and CI is not a Go package)
- `make verify` run locally once, to confirm the exact command CI
  will call still passes on the current tree.

This plan adds no new gate script and changes no existing gate. The
workflow file is the only new artifact the builder produces.
