# Memories

Team-shared, git-committed operational memory for this repo, following the
open `.agents` protocol (https://dotagentsprotocol.com/).

One markdown file per memory. Frontmatter:

```markdown
---
id: unique_id
title: Human-readable title
content: One-line summary (also shown without opening the file)
importance: high | medium | low
tags: comma, separated, tags
---

Full detail in the body: what happened, why it matters, how to apply it.
```

This directory starts empty — add a file here the first time an agent
corrects a real mistake or the user states a standing preference for this
repo, the same way `AGENTS.md`'s own rules were built up. A fact that
hardens into an enforced rule belongs in `AGENTS.md` or a gate script under
`scripts/`, not here — memories are operational context, not policy.
