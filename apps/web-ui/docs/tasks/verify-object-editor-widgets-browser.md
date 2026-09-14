# Browser/e2e pass over object editor schema-driven widgets

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-object-schema-widgets](../sessions/2026-09-08-object-schema-widgets.md)

## What

A browser/e2e verification of the object editor's schema-driven property widgets on a project
with a ui-declaring schema (e.g. agent-notes installed):
- string props render as auto-growing textareas (single-line, grow while typing);
- `enum` string props render as native selects with the stored value selected;
- an off-enum stored value is preserved (visible + not dropped on save);
- `widget: "input"` props stay single-line inputs;
- create form and detail edit form behave consistently; values survive a save round-trip.

## Why

Rendering is covered by Go unit tests asserting exact HTML, but no browser/e2e pass exercises the
live interaction (auto-grow JS, select submit semantics, HTMX/partial reloads). This is the same
gap the memory-side integration suite (PR #390) closed for the backend contract.

## Depends on

- The e2e harness/flow already used by `tests/e2e/specs/object-create-ui.spec.ts` / `objects.spec.ts`.
- A running gateway against a memory project with agent-notes (or any `enum`/`widget`-bearing pack) installed.

## Notes

- Check the concurrent, uncommitted `openspec/changes/add-p0-e2e-coverage/` change first — it may
  already add object-editor coverage; avoid duplicating.
