# Plan: machine phase 4 — benchmarks

Adds the performance tests for the machine package. Nothing changes in
the API. These measures give the automation consumer a cost model.

## Goal

Benchmark the hot paths of the machine. Report a budget per operation.
Find regressions after any later change.

## Scope

Inside: benchmark functions and a cheap budget table. Outside: any
change to the machine API or behavior. This phase measures only.

## Benchmarks

- Fire on a small definition, medium definition, and large definition.
- Validate on a large definition.
- Encode and Decode of a medium definition.
- Concurrent Fire calls on one read-only definition.

Run with the bench target in the Makefile and the mem profile on.

## TDD

Write each benchmark before any tuning. The benchmark defines the
expected cost. Then set a generous budget from the measured result.

## Tests

The benchmarks are the tests. A regression gate asserts each path
stays under its budget. The budget table lives in machine/doc.go as a
comment and in the benchmark file as a constant.

## Verification

`make bench` and `make verify` both pass. The race detector stays
green during concurrent Fire runs.
