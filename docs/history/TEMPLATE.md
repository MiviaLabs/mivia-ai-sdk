# Plan template

Copy this file to `docs/plans/<package>.md` when adding a package.
`scripts/check_plan.py` requires every section below. Fill each with
short declarative sentences. The plan is the design contract an agent
writes before or with the code; the gates enforce its consequences
(API lock in `api/`, import policy in `policy/layers.json`).

## Goal

What the package is for, in one or two sentences.

## Scope

What belongs inside the package and what stays outside it.

## API

The exported surface and the reasoning behind its shape. Every entry
must land in `api/<package>.txt` via `make api-update`.

## Tests

What proves the package works: unit cases, integration flows,
conformance vectors, fuzz seeds, benchmarks.

## Verification

The commands that must pass and any gate this package adds or changes.
