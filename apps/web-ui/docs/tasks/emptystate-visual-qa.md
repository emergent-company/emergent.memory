# Empty-state / CTA visual QA pass after EmptyState rollout

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-emptystate-cta-component](sessions/2026-09-09-emptystate-cta-component.md)

## What

Signed-in browser (and, where the mock harness allows, e2e) pass over every
empty-state/CTA surface migrated to go-daisy `ui.EmptyState` in commit
`61662eb`. Verify per site:

- Layout sanity of the two scales: compact `p-8` default vs `py-12`
  page-level panels vs the hero (Providers "Connect your first LLM provider",
  chat "No agents to talk to").
- CTA children render and sit in the action row with the guaranteed gap
  (`mt-6`/`mt-7`), especially children-block sites: agents "New agent", org
  landing "Create project", backups warning badge, objects "connect", member
  "Add member" actions.
- No residual stray gaps or squat panels; heading semantics (h2/h3) correct
  per page outline; action buttons look consistent with the convenience
  `ActionHref` buttons on other pages.

## Why

The rollout applied designer judgment per call site (~30 sites, 15 templates)
but only the Providers hero was verified visually and only one children-CTA
(org landing) was verified by a unit test. The whitespace choice
(`py-12` page-level vs `p-8` embedded) is worth confirming by eye across the
whole interface before it settles.

## Depends on

none (go-daisy `ui.EmptyState` already shipped and pinned)

## Notes

- Pages to check: agents, sessions, schedules, skills, documents, backups,
  objects, usage, api-tokens, org projects/members/settings, project settings
  overrides/rates, chat, agent memories/tools.
- Full Playwright run needs the mock-memory harness (`tests/e2e/run-e2e.sh`),
  which conflicts with the shared dev gateway on :8095 — coordinate or run on
  a free port/instance.
