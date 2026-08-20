---
id: mutex_prevents_races_not_mispairing
title: A mutex that stops the race detector can still hide a logic race
content: The absence of a data race does not prove concurrently read/written values are paired correctly — check the pairing logic itself, not just synchronization.
importance: high
tags: concurrency, correctness
---

The largest single fix of one session was a bug in `Calibrated.Observe`
(`contextplan`/`agentloop`): an estimate/actual pairing that was protected
by a mutex, so `go test -race` and normal execution never flagged it, but
the *values* being paired under that lock were still the wrong ones from a
concurrent caller's perspective — a logic race, not a data race.

Why it matters: "no race detector flag" and "correct" are different
claims. A mutex only proves exclusive access to memory, not that the
memory holds the values the algorithm assumes it holds at that point.

How to apply: when reviewing concurrent code that pairs two values read at
different times (an estimate and a later observation, a snapshot and a
delta), check whether the pairing can be scrambled by concurrent callers
even under correct locking discipline. Demand a test that plants a
deliberate mispairing and proves it fails, not just a test that proves no
panic/race occurs. See [[positive_control_reachability]] for the general
test-adequacy principle this is a specific instance of.
