## Why

Object-driven agent work (#1330) turned graph objects into work items and added
a Kanban board at `/board` (#1332), but blueprints cannot yet *express* that
surface. A blueprint manifest can describe object types and agents, but it has
no way to mark a type board-enabled, declare its allowed statuses and its
operational flags, wire an agent's work/reaction configuration, or set a seed
object's assignee. As a result, the only way to produce a board-backed project
is hand-editing the schema and agent in the UI — the `blueprints/` directory
cannot ship a reusable board lane. There is also no end-to-end coverage of a
board workflow through the blueprint path.

## What Changes

- **Blueprint manifests gain board/work surface.** Object types can declare
  `boardEnabled`, `allowedStatuses`, `skipEmbeddings`, `skipExtraction`, and
  `excludeFromSearch`. Agent definitions can declare `workConfig`,
  `triggerType`, `reactionConfig`, and `cronSchedule`. Seed objects can declare
  an `assignee`. These keys pass through the manifest loader and the apply path
  unchanged, matching the server's existing agent/schema surfaces.
- **A sample blueprint** `blueprints/task-board/` demonstrates the full shape:
  a board-enabled `Task` type, a reaction-triggered `task-worker` agent with a
  review-requiring work contract, a workflow skill, and a single `Task` seed.
- **Gateway `/objects` provenance guard.** The objects page normalizes the
  provenance filter before dispatch — an incomplete actor pair (`actor_type`
  without `actor_id`, or neither) clears the provenance mode to `any`, and an
  unrecognised value is coerced to `any` — and only renders the provenance
  control once a complete actor pair is selected, so the gateway never forwards
  a bare `provenance` the server rejects.
- **Board e2e coverage.** An end-to-end test seeds a board-enabled Task through
  the memory API, asserts it renders in the `ready` lane, opens the card
  drawer, and exercises the human actions: approve (`review` → `done`), retry
  (`blocked` → `ready`), reassign, and cancel (→ `blocked`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `blueprint-api`: the manifest schema gains board-enabled object-type keys,
  agent work/reaction wiring, and seed `assignee`.
- `object-browser`: the objects page guards its provenance filter (clearing
  provenance without a complete actor pair) and hides the control until an
  actor is selected.

## Impact

- **Server** (`apps/server/domain/blueprints/`): `manifest.go` and `apply.go`
  carry the new object-type, agent, and seed keys through create-or-update.
- **Sample** (`blueprints/task-board/`): new blueprint directory (schema pack,
  agent, skill, seed, README).
- **Gateway** (`apps/web-ui/gateway/objects.go`, `objects.templ`): provenance
  normalization and control gating on the `/objects` page.
- **Tests**: board workflow e2e; gateway render/handler tests for the
  provenance guard; manifest loader tests for the new keys.

## Out of Scope

- Changing the board projection, the worker claim, or the work-status write
  path itself — those already shipped in #1330/#1332 and are only *expressed*
  by blueprints here.
- CLI `memory work-items` parity (the board workflow CLI), which remains a
  separate change.
