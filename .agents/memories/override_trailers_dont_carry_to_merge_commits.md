---
id: override_trailers_dont_carry_to_merge_commits
title: Allow-Test-Change / Allow-Gate-Change trailers do not carry to the merge commit
content: A branch commit's override trailer does not protect its later merge commit; re-authorize the same override on the merge commit itself if the gate fires there too.
importance: medium
tags: gates, test-tampering, git, merge
---

The test-tampering gate (`scripts/check_test_tampering.py`) scans the
commit being made, not the branch history behind it. A branch commit
carrying a verified `Allow-Test-Change: TT<NN> <reason>` or
`Allow-Gate-Change: TT<NN> <reason>` trailer does not exempt a later merge
commit that reintroduces the same diff content into `main`. This happened
at least twice in one session: once for a nested-package-visibility change
merged with `Allow-Gate-Change: TT11`, and once for the `Calibrated.Observe`
pairing fix's merge, which independently re-triggered TT01(x2)/TT05 on the
merge commit even though the branch commit had already justified them.

Why it matters: without knowing this, an agent might reach for
`--no-verify` on the merge, or worse, mistake the second firing for a new,
unverified violation and either bypass it or block on it unnecessarily.

How to apply: when a merge commit re-triggers a gate that a branch commit
already justified, re-verify the same finding against the merge diff (it
should be identical content) and re-issue the same override trailer,
citing the prior verification. Do not treat the second firing as proof of
a new problem, and do not skip re-verifying it either — confirm the diff
content is actually the same before reusing the justification. See
[[positive_control_reachability]] for the general principle that an
override must be independently verified, never taken on the builder's
word.
