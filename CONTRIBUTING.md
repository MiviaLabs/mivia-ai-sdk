# Contributing

This is a Go SDK for building AI agents. Standard library only, no
third-party dependencies.

## Before you start

1. Clone the repo.
2. Run `make install-hooks` once. This wires the pre-commit hook.

## Making a change

1. Write the code and its test in the same change.
2. Run `make verify`. This is the full gate: gofmt, vet, tests, the
   API and dependency locks, orphan and symbol wiring, test
   tampering, coverage, and the Semgrep scan.
3. If you added, removed, or changed an exported symbol, run
   `make api-update` and commit the `api/` diff in the same change.
4. If you added an exported symbol, name its first caller in the
   change. An unused exported symbol fails `make verify`.
5. If you added a new package, add an entry to `policy/layers.json`
   naming the packages it may import.

## Coverage

Every package needs 85% test coverage or higher. `make verify` checks
this and fails below the floor.

## What CI checks

GitHub Actions runs `make verify` on every push and pull request to
`main`. This is the same command you run locally, so a green
`make verify` before you push should stay green in CI.

## What CI does not check

`make verify-maintainer` runs a second, smaller set of gates: a
plan-document template, a prose style check, and an audit-label ban.
These cover maintainer process, not the public contract. CI does not
run this tier, and your PR does not need to pass it.

## Style

- Format Go code with `gofmt`.
- Keep files at or below 500 lines and functions at or below 80
  lines.
- Write a one-line doc comment for every exported symbol, starting
  with the symbol's name.
- Do not add a comment that only restates the code.

## Getting help

Open an issue or a pull request. Describe the problem or the change
before you write code, if the design is not obvious.
