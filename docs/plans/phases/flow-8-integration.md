# Plan: flow phase 8 — end-to-end, concurrency, benchmarks

Adds the end-to-end workflow, the concurrency proof, and the flow
benchmarks. This phase closes the machine and flow work.

## Goal

Prove a full workflow runs over the envelope and verifies its audit
trail. Prove parallel panels stay race-free. Give the flow package a
cost model.

## Scope

Inside: the end-to-end integration suite, the concurrency stress, and
the benchmarks. Outside: any new API or feature. This phase measures
and proves the work already built.

## Integration

An end-to-end suite runs a real workflow:

- sign and encode each step message.
- pass steps through the runner across panels.
- verify every signature after transport.
- verify the thread chain with VerifyThread after the run.

The audit thread records the workflow. VerifyThread confirms the order
and rejects tampering.

## Concurrency

A stress test mixes panels and chains against one Definition. It
synchronizes with a WaitGroup. It never uses time.Sleep. The race
detector proves the flow package shares no unprotected state.

## Benchmarks

- schedule and validate a large graph.
- run a sequential chain end to end.
- run a parallel panel of independent steps.
- chain a three-level workflow.

Run with the bench target and the mem profile on.

## TDD

Write the end-to-end suite first. Then the stress test and the
benchmarks. The suite fails until the runner and chaining hold their
contracts.

## Verification

`make verify`, `make bench`, and `go test -race ./...` all pass.
