# E2E — drive agent-model-warning fixture state through the UI model select

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-e2e-ui-driven-mutations](../sessions/2026-09-09-e2e-ui-driven-mutations.md)

## What

`agent-model-warning-ui.spec.ts` shapes its fixtures via API today: agents with no model are
`POST /api/agents` seeds, and the "no alert when the agent has an explicit model" test sets the
model through a best-effort `PUT /api/agents/:id` (falling back to a POST, and `test.skip` when
memory rejects the model name). The agent settings page already exposes the model select
(`#agent-settings-model`, asserted at the end of that same test). Consider setting the fixture
model by driving that select instead of the API.

## Why

Aligns with the UI-first mutation policy (see 12-operations.md): state-shaping that mirrors a
real edit should go through the edit UI so the wiring under test matches production input.

## Depends on

None.

## Notes

- Memory's unsynced model catalog can reject unknown model names; the existing test already
  degrades to `test.skip`. A UI-driven variant needs the same graceful skip, or a model name
  guaranteed to be in the synced catalog.
- The other two severities (error/warning alerts) seed *absence* of model config, which is the
  default state of a freshly created agent — no fixture shaping needed there.
