---
id: positive_control_reachability
title: A passing positive control is not proof by itself
content: Trace a test's fixture against the code's actual preconditions before trusting it as coverage; a passing test can still never reach the branch it claims to prove.
importance: high
tags: testing, review, concurrency
---

Across one session this pattern recurred at least five times: a test that
reads as a positive/concurrency control, passes, and is cited as proof —
but its fixture cannot actually reach the code path under test.

Concrete cases: a ledger eviction security test whose load never crossed
the eviction threshold; a mutation-gate check whose "empty package" case
never ran under mutation because the killing test lived in the package's
own directory, not the external `_test` directory; a longtermmemory
core-flag test whose two fixtures used identical tags, so no re-key ever
fired; and twice (two separate review rounds) a `Calibrated.Observe`
concurrency test whose fixture was numerically incapable of distinguishing
correct pairing from a mispaired race.

Why it matters: a test with the right name and a green checkmark reads as
coverage. It is only coverage if the code under test actually executes the
assertion-relevant branch before the assertion fires.

How to apply: for any test presented as proof of a fix (especially
concurrency or threshold-triggered behavior), trace the fixture's
preconditions against the code's actual trigger conditions before
accepting it. For concurrency specifically, demand proof both ways: correct
behavior passes, and a planted mispairing or race genuinely fails, under
the same fixture. This is now written into `.agents/skills/review/SKILL.md`
and the `reviewer`/`plan-reviewer` agent instructions directly — this
memory is the "why," those are the enforced rule.
