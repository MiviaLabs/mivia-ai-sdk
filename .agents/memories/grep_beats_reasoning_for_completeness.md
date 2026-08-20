---
id: grep_beats_reasoning_for_completeness
title: Grep for completeness claims, never reason your way to them
content: "I found all the stale sites" or "no other call site exists" was wrong at least eight times in one session when reached by reasoning instead of a grep.
importance: high
tags: review, doc-drift, grep
---

Whenever a fix, a doc update, or a plan claimed to be the complete set of
sites affected by a change (stale doc references, call sites needing
migration, other places a bug pattern repeats), reasoning alone missed
sites a grep for the distinctive term would have caught. This happened
repeatedly across doc-drift fixes and plan reviews in one session — never
zero misses.

Why it matters: "I checked and that's all of them" is a claim about the
whole tree, not about the files already open in context. An agent's
context window is not the tree.

How to apply: any time a plan, a fix, or a review report claims
completeness over the whole repo, run the grep before accepting the claim,
not after. This is now written into `.agents/skills/review/SKILL.md`
("Do not reason your way to 'I found them all.' Grep the distinctive term
or claim across the whole tree, .md and .go both") and into the
`reviewer`/`plan-reviewer` agent instructions. This memory is the record
of why that rule exists.
