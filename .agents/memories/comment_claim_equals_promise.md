---
id: comment_claim_equals_promise
title: A doc comment inside a branching code path is a behavioral claim, not a description
content: A multi-line comment inside a branching code path narrates behavior it does not control; treat it as a claim and re-read the branch.
importance: high
tags: review, doc-drift, comments
---

The review of commit `d914611 feat(agentloop): add pull-based steer injector`
surfaced a comment-drift finding: the `// Steered-stop branch` block at
`agentloop/run.go:147-173` described "the loop drains whatever the injector
has queued at the downgrade point." The actual drain lives at the iteration
top (`run.go:71-85`), not at the downgrade branch. The comment's prose was
plausible on its own, the `Validate`/test surface passed, no gate fired —
the drift went uncatchable by automation because a doc comment is not part
of any AST a script walks.

Why it matters: AGENTS.md caps comments at "one line of what, plus
invariants." A comment that runs longer than that inside a branch is
making a claim about control flow. The drift is silent until a reviewer
re-reads the branch. This is the same shape as doc drift in
`docs/architecture.md`, just moved one level closer to the code — and
therefore closer to silent.

How to apply: on review, any comment inside `if`/`switch`/`for`/`select`
that exceeds one line of "what" gets re-read against the branch's actual
control flow. Do not trust the prose. The rule is now written into
`.agents/agents/reviewer.md` as a primary review-surface bullet. See
[[grep_beats_reasoning_for_completeness]] for the general doc-drift
principle this is a specific instance of.