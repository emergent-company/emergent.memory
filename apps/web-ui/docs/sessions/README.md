# Session logs

One file per coding session. Purpose: durable handoff — what happened, why, and what's left, so a future session can resume without re-deriving context.

## Naming

`YYYY-MM-DD-<slug>.md`, e.g. `2026-09-01-p1-gateway.md`. Slug = 2–4 words, lowercase, hyphen-separated.

## Template

```markdown
# <Date> — <Short title>

## Goal
<what this session set out to do>

## Outcome
<done / partially done / blocked, with specifics>

## Decisions
- <decision> — <one-line rationale>

## Changes
- `<file>` — <what changed and why>

## Verification
- <command> — <result>

## Open questions / follow-ups
- <unresolved item or next step>

## Tasks
- [<slug>](../tasks/<slug>.md) — <one-line>
```

## Rules

- Append new logs; never rewrite the history of a past session log.
- Keep entries factual and specific. Include code snippets only where the diff is the point.
- Link related tasks and specs rather than restating them.
