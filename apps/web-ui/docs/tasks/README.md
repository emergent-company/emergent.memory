# Tasks — remaining work

Repository of future work, follow-ups, and deferred ideas. Distinct from the spec roadmap (`docs/spec/13-roadmap.md`), which tracks architectural phasing. If an item is architectural phasing, put it in the roadmap instead; if it's a concrete follow-up or idea, put it here.

## Structure

- `BACKLOG.md` — index of every task with its status.
- `<slug>.md` — one file per task (the detail). Create only when a task needs more than a backlog line.

## Backlog entry format (`BACKLOG.md`)

A markdown table with columns: `Status`, `Slug`, `Title`, `Source`, `Created`.

Statuses: `proposed` (default) → `accepted` → `in-progress` → `done`, or `rejected`.

## Task file template

```markdown
# <Title>

**Status:** proposed
**Created:** <date>
**Source:** <link to session log>

## What
<what to build or do>

## Why
<rationale / context>

## Depends on
<links to other tasks or specs, or "none">

## Notes
<constraints, gotchas, links>
```

## Rules

- One idea = one task. Don't merge unrelated ideas.
- Don't duplicate items tracked in the roadmap — link instead.
- Keep statuses current as work progresses.
