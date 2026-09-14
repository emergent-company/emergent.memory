# Archive enhance-object-detail-view OpenSpec change

**Status:** proposed
**Created:** 2026-09-05
**Source:** [2026-09-05-fix-object-chat-no-agent](sessions/2026-09-05-fix-object-chat-no-agent.md)

## What
Check off the tasks in `openspec/changes/enhance-object-detail-view/tasks.md` and
archive the change (via the openspec archive workflow) once all items are verified.

## Why
The object-detail-view feature (editable properties, relationship connect,
object search, "Chat about object" with editor-agent resolution and
`canonical_id` linking) is implemented in the gateway, but the OpenSpec change's
task list is entirely unchecked and the change has not been archived. This
session fixed a bug in the "Chat about object" handler (task 3.4) and confirmed
resume semantics (task 3.5) against the live memory backend.

## Depends on
- none (feature is implemented; remaining work is check-off + archive)

## Notes
- `canonicalId` sent to the memory backend must be a bare UUID — an `obj-` prefix
  fails `uuid.Parse` (`invalid canonicalId format`).
- Resume semantics confirmed: `POST /api/chat/conversations` get-or-creates by
  `canonicalId`, the first seeded message persists, and a resume returns the
  existing conversation without re-seeding.
- The "No editor agent configured" spec scenario is satisfied by the `?err=` flash
  toast added in `ac7ec84`.
