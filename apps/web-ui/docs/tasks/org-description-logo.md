# Organization description + logo

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-review-bot-and-backlog](../sessions/2026-09-13-review-bot-and-backlog.md)

## What

Extend org management beyond the name rename shipped in PR #54 / emergent.memory #416 to support
an optional **description** and **logo/avatar**. Requires a `kb.orgs` column (description, and an
object key for the logo) plus an update-capable `PATCH /api/orgs/:id` (currently name-only), and
gateway UI on the org General settings section.

## Why

`org-rename-description` originally covered rename **plus** description; the memory `Org` entity
has no description column, so only rename shipped. Description/logo are commonly requested
identity fields.

## Depends on

- `org-rename-description` (name-only rename) — done.
- emergent.memory #416 (`PATCH /api/orgs/:id`).

## Notes

- Logo likely reuses the avatar storage/normalize path (see `avatar-reencode-normalize`).
- Keep the rename endpoint's validation/error semantics; add fields additively.
