## Context

See `proposal.md` — Why. Two constraints shape the approach:

- The user-facing "session" is a `kb.chat_conversations` row. The chat rail (`apps/web-ui/gateway/chat.templ`) renders conversation rows (resumable), scheduled-run rows, and owner-shared rows from three different sources; only the conversation source is the entity a user thinks of as "their chat session".
- The `chat-conversation-ownership` spec already defines one access predicate ("owner-or-shared") that *every* by-id accessor applies, and a fail-closed 404 convention. New archive accessors inherit both; no ownership requirement needs editing.
- `kb.sessions.is_archived` exists (retained from the removed ACP archive) but is unmapped and unused. It is deliberately not reused here — see Decision D1.

## Goals / Non-Goals

**Goals:**

- A non-destructive archive state on chat conversations, with symmetric archive/unarchive operations and a list contract that hides archived rows unless explicitly requested.
- A user-reachable permanent-delete operation, kept distinct from archive, with a destructive-action confirmation in the rail.
- A rail filter option and a per-row action menu that make all three operations reachable without leaving the chat workspace.
- One new capability spec; no change to existing spec requirement text.

**Non-Goals:**

- Archiving scheduled-agent runs or owner-shared sessions (different entities; the latter already has its own archive).
- Reworking hard `DELETE`; archive and delete remain distinct actions.
- Thread-level (`kb.sessions`) archive spanning chat/A2A/share — deferred (see D1).
- CLI or iOS surfaces; they inherit the API's default-exclude behaviour with no change.

## Decisions

### D1: Archive the conversation, not the session thread

**Decision:** Add `is_archived` + `archived_at` to `kb.chat_conversations` and archive that row.

**Why:** The rail's archiveable rows are conversations, and the conversation id is what the rail resumes by. Not every conversation has a backing `kb.sessions` row (a plain Q&A conversation may never create one), so a thread-level archive would silently be unavailable for some rows — a worse contract than the feature it replaces. This also matches `agent-session-consolidate`, which kept the chat conversation's soft state adapter-local while deferring a thread-level archive.

**Alternative considered:** wire up the retained `kb.sessions.is_archived` (the "one archive concept across chat/share/A2A" direction). Rejected for this change: it cannot cover session-less conversations and would pull A2A/scheduled-run semantics into scope. It remains a viable later change; the retained column is left untouched.

### D2: Two columns — `is_archived` and `archived_at`

**Decision:** Persist a boolean state plus a nullable `timestamptz` stamp set on archive and cleared on unarchive.

**Why:** The boolean is the filter predicate and is cheap to index (`WHERE is_archived = false` is the default path); the timestamp is needed to eventually sort or expire archived rows and is otherwise unrecoverable. Alternative — a nullable `archived_at` alone — forces `IS NULL` predicates and loses the explicit default-false.

### D3: Dedicated idempotent operations, `chat:use` scope

**Decision:** `POST /api/chat/:id/archive` and `.../unarchive`, idempotent, requiring `chat:use` (not `chat:admin`), applying the owner-or-shared predicate with 404 fail-closed.

**Why:** Archiving your own session is an ordinary user action; the `chat:admin` gate on `PATCH`/`DELETE` (`routes.go`) is wrong for it. Dedicated verbs keep the state transition explicit and idempotent, and avoid overloading `PATCH` (whose scope is admin-only today).

**Alternative considered:** `PATCH /api/chat/:id` with `{archived: true}`. Rejected: reuses an admin-gated route and makes an idempotent toggle look like a general update.

### D4: Default-exclude list with an explicit include option

**Decision:** `GET /api/chat/conversations` excludes archived rows by default; an `includeArchived=true` query parameter includes them.

**Why:** Preserves today's behaviour for every existing caller with no flag (no breaking change), and makes the intent self-documenting at each call site that wants history.

**Alternative considered:** `state=active|archived|all`. More expressive (can list *only* archived) but the requirement is "include archived alongside active", which the boolean expresses exactly. If a future "Archived only" view is wanted, `state` can be added additively.

### D5: Archived ≠ deleted; still retrievable by id

**Decision:** Archived conversations remain fully readable by id (get, history, dump); they are excluded only from the default *list*.

**Why:** Archive is a list-hygiene action, not a tombstone. Fail-closed 404 stays reserved for genuinely inaccessible rows, so a client that already has the id is not surprised.

### D6: Statistics consumers opt in to including archived

**Decision:** Internal consumers that count or aggregate conversations (`usage.go` session series) pass `includeArchived=true`; user-facing list surfaces (rail, agent dashboard "recent chats", the Sessions trace page) use the default.

**Why:** Archiving must not silently rewrite historical usage counts. Keeping the default exclude at the API boundary while statistics opt in preserves both properties.

### D7: UI touches the existing rail filter row and a per-row dropdown

**Decision:** Extend the rail's filter row with an "Include archived" control, add an action menu to `sessionRailItem`, and mark archived rows (badge/muted styling). Menu actions post to the archive/unarchive routes and trigger the existing rail refresh.

**Why:** Reuses the established filter row and row affordances instead of inventing a new surface, and the rail refresh path already exists for HRTMX partial re-render. Exact component/markup choices are implementation detail; the spec fixes only the observable behaviour and test ids.

### D8: Delete reuses the existing hard delete; only the scope gate moves

**Decision:** Keep `DELETE /api/chat/:id` and its repository delete (messages cascade via the existing `ON DELETE CASCADE` FK), but move the route out of the `chat:admin` subgroup into the `chat:use` group. The rail gains a Delete action guarded by a client-side confirmation dialog.

**Why:** The repository delete already applies the owner-or-shared predicate and project scope; the `chat:admin` middleware is the only thing making it admin-only, and that gate was incidental to a route that had no UI and no in-repo caller. Owner-or-shared is the same predicate archive/unarchive use, so the three operations are consistent. The confirmation is a UI concern — the endpoint stays a plain unconfirmed `DELETE`, so API callers are unaffected.

**Alternatives considered:** (a) a separate `POST …/delete` route under `chat:use`, leaving the admin `DELETE` in place — rejected: two delete entry points with different gates is a security trap. (b) Confirmation enforced server-side — rejected: destructive-action confirmation is a client UX affordance, not an API contract.

**Consequence to accept:** this widens who can delete (any member who can access the conversation, matching the read predicate). The spec makes this explicit; the risk below tracks it.

### D9: Delete removes the conversation only, not the backing session thread

**Decision:** Deleting a conversation deletes the `kb.chat_conversations` row and cascades to `kb.chat_messages`. The linked `kb.sessions` thread, its `agent_runs`, and `session_todos` are left in place.

**Why:** Those records are run history, not conversation content, and are keyed independently of the conversation (the conversation holds a nullable `session_id` pointing at the thread; nothing points back). Cascading into run history on a user's list-hygiene action would destroy audit/usage data unexpectedly. If a future need to purge run history arises, it belongs with run retention/cleanup, not here.

## Risks / Trade-offs

- **New accessor skips the ownership predicate** → both operations go through the same repository accessor that applies `(owner_user_id = ? OR is_private = false)`; a foreign/unknown id returns 404. Covered by unit tests.
- **Default-exclude silently hides sessions from unexpected callers** (e.g. a future dashboard) → the flag is explicit (`includeArchived`); tasks audit every `ListConversations` call site and set the param deliberately where history is meant.
- **Archiving while a run is active** → the conversation is only filtered from lists; the run and stream are untouched. No new interaction with run lifecycle.
- **Migration reversibility** → single additive migration with a down step dropping both columns; no data backfill needed.
- **Two archive concepts (conversation vs retained `kb.sessions.is_archived`)** → documented; the retained column stays dead and out of scope to avoid a half-used thread archive.
- **Widening delete from `chat:admin` to `chat:use` removes an admin safeguard** → the underlying predicate is unchanged and already owner-or-shared (the same one reads use), so any member who can read the conversation can delete it, by design; covered by an explicit handler test asserting a non-admin member can delete a conversation they can access and a foreign/private conversation still 404s.
- **Irreversible loss on a mis-click** → the rail Delete action requires a confirmation and is visually separated from Archive/Unarchive in the menu; the endpoint itself stays unconfirmed for API callers.

## Migration Plan

1. Land migration (add `is_archived` default false, `archived_at` nullable) — additive, zero downtime; old code ignores the columns.
2. Add repository/service accessors + routes; wire list filter; move `DELETE` to the `chat:use` group; regenerate swagger.
3. Wire gateway client + rail filter + per-row action menu (including delete confirmation); audit list call sites for the include-archived decision.
4. Verify: `task test` (server) + `task test:integration`; gateway `go build ./...`, `go test ./...`, `task lint`; migration up/down; manual rail smoke (archive hides, filter reveals, unarchive restores, delete confirms then removes).
5. Rollback: revert the branch; the down migration drops both columns. The date of the DELETE scope move is not independently reversible, but the route reverts with the branch.

## Open Questions

- Whether a later change should surface an "Archived only" view (`state=archived`) — additive, no impact on this spec.
- Whether archiving should eventually also cascade to the backing `kb.sessions` thread for A2A continuity — deferred with D1.
