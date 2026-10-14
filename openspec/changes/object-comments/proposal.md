# object-comments

## Why

Object-driven work items today surface **rework feedback** in the board drawer
(`kb.work_item_feedback`) — a round-scoped, transition-driving record that only
exists when a human requests changes. There is no way for a human reviewer or a
work agent to leave a **durable, non-transitioning note** on an item: an agent
cannot record context, findings, or decisions a reviewer should see, and a human
cannot annotate an item without moving it through the review cycle.

That gap forces agents to bury rationale in the final `work_complete` summary
(one blob, only at the end) and forces humans to either request changes (which
transitions the item) or lose the note entirely.

## What Changes

A new append-only `kb.object_comments` table records durable comments on a work
item, authored by a human (`actor_type='user'`) through the work path or by a
work agent (`actor_type='agent'`) through a per-run `comment` tool. A comment
carries no round and never transitions the item.

- **Server**: migration `00241_object_comments.sql`; a `WorkItemComment` entity +
  `AppendWorkComment`/`ListWorkComments` store; a `POST
  /api/projects/:projectId/work-items/:canonicalId/comments` route (agents:write,
  human author = the calling user, `actor_type='user'`, 400 on empty body, 404 on
  a missing item); the work-item detail projection gains a `comments` field.
- **Web UI**: the board's "Feedback" section becomes a merged "Activity" thread
  (durable comments + feedback rounds, ascending), rendered in **both** board
  surfaces — the card-click object preview's board slot and the native
  drag→review drawer — with an inline form that posts a human comment and
  re-renders the board. Each surface tags the thread with its own `data-testid`
  family, since both can be present in one document.
- **Agent tool**: a whitelist-gated `comment` run tool (built alongside
  `work_complete`/`work_block`) that appends a comment with `actor_type='agent'`,
  the agent's display name as author, and the run id. It does **not** end the run.

## Impact

- `apps/server/domain/agents/` — `work_item_comments.go` (entity + store),
  `handler_work.go` (DTOs + handler + Swagger), `work_actions.go` (AddComment +
  detail projection), `routes.go` (route), `toolpool.go`/`work_tools.go`/
  `executor.go` (agent tool), `internal/schemadrift/models.go` (entity).
- `apps/server/route-authority.yaml` — regenerated, one new route.
- `apps/server/migrations/00241_object_comments.sql` — new table.
- `apps/web-ui/gateway/` — `memory_work.go`, `backend.go`, `board.go`,
  `board.templ`, `object_preview.go`, `main.go`, tests.
- `openspec/changes/object-comments/` — this change.

## Non-Goals

- No work-config toggle: the `comment` tool is gated by the agent's tools
  whitelist alone (consistent with `work_complete`/`work_block`).
- Comments never transition the item and are never edited or deleted (append-only).
- No comment threading/replies, reactions, or per-comment permissions.

## Out of Scope

- Surfacing comments outside the board surfaces (e.g. a standalone comments API
  list endpoint) — the detail projection is the only reader today.
