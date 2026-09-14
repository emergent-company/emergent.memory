# Archive the add-account-avatar OpenSpec change

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-account-avatar](../sessions/2026-09-08-account-avatar.md)

## What
Archive the completed `add-account-avatar` OpenSpec change: sync its delta spec
(`account-avatar`) into the main specs and mark the change archived.

## Why
The change is fully implemented, tested, and merged (backend PR #384 + gateway commits
`d17eb26`/`5211d6f`/`69c3951`), but the OpenSpec artifacts are still un-archived in
`openspec/changes/add-account-avatar/`. Archiving moves the new capability spec into
`openspec/specs/account-avatar/spec.md` and closes the change.

## Depends on
- none (implementation is complete and verified).

## Notes
- Run `openspec archive add-account-avatar` (or the archive-change skill) and commit the result.
- The delta spec is a new capability (`account-avatar`), so archiving creates a new main spec.
- `tasks.md` task 5.2 (manual browser check) is now satisfied by the e2e spec
  `tests/e2e/specs/profile-avatar-ui.spec.ts`, which passed against the live dev stack.
