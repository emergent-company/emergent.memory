## Why

Chat sessions accumulate without end: the session rail lists every conversation forever, so an active user's list becomes noise. The only lifecycle actions today are a hard `DELETE` (`apps/server/domain/chat/repository.go:186`) that no user can reach — it is gated behind `chat:admin` and has no UI — and no archive at all. The database column `kb.sessions.is_archived` was retained but never wired to anything when the old ACP archive endpoints were removed as dead code (#1163, `agent-session-consolidate`), which explicitly deferred the user-facing archive feature. This change delivers session lifecycle management: archive (reversible hide) and permanent delete, both user-facing.

## What Changes

- **Conversation archive flag.** Add `is_archived` (boolean, default false) and `archived_at` (nullable timestamptz) to `kb.chat_conversations`.
- **Archive / unarchive endpoints.** `POST /api/chat/:id/archive` and `POST /api/chat/:id/unarchive`, both applying the existing owner-or-shared access predicate and requiring `chat:use` (a user archives their own sessions; not `chat:admin`). Archive is idempotent; unarchive is idempotent.
- **List filtering.** `GET /api/chat/conversations` SHALL exclude archived conversations by default and accept an `includeArchived=true` (alternatively `state=active|archived|all`) query parameter to include them. No query parameter means the existing active-only behaviour.
- **Rail filter option.** The chat session rail's existing filter row (alongside agent and origin filters) gains an "Include archived" option; when enabled, archived rows are shown and visually marked.
- **Per-row action menu.** Each session row in the rail gains an action menu with **Archive** (on active rows), **Unarchive** (on archived rows), and **Delete** (any row). Delete requires an explicit confirmation before it is performed.
- **User-facing permanent delete.** Move `DELETE /api/chat/:id` out of the `chat:admin` subgroup into the `chat:use` group so a member can permanently delete a session they can access (the repository already applies the owner-or-shared predicate and project scope). Deletion cascades to the conversation's messages (existing FK `ON DELETE CASCADE`) and is irreversible.
- **Archive is non-destructive.** An archived conversation keeps its messages, history, and sessions; it is hidden, not deleted. Archiving the currently-open conversation leaves it open. Deleting the open conversation, by contrast, exits the workspace because the conversation no longer exists.

Scope note: the rail also renders scheduled-agent runs and owner-shared sessions. Scheduled runs are not conversations and owner-shared sessions already have their own archive (`kb.agent_share_sessions`); both are out of scope for this change.

## Capabilities

### New Capabilities

- `chat-conversation-lifecycle`: the lifecycle contract for chat sessions (conversations) — the persisted archive flag, archive/unarchive/delete operations, the default-exclude list contract, and the session-rail filter and per-row action menu that expose hide, restore, and permanent removal.

### Modified Capabilities

<!-- None: new archive/unarchive accessors fall under the existing "every by-id accessor" predicate in chat-conversation-ownership, and no existing requirement text changes. -->

## Impact

- **Migration**: one Goose migration adding `is_archived` + `archived_at` to `kb.chat_conversations` (additive, reversible; existing rows default to not archived).
- **Server**: `domain/chat` (`entity.go`, `repository.go`, `service.go`, `handler.go`, `routes.go`) — new columns, archive/unarchive accessors with the owner-or-shared predicate, list filter, and the `DELETE` route moved to the `chat:use` group; regenerate swagger.
- **Gateway/web-ui**: `memory_conversations.go` (client calls + list param + delete), `chat.templ` / `chat.js` (rail filter option, per-row action menu, delete confirmation), `sessions.go`/`sessions.templ` (default-excluded list parity); `backend.go` interface.
- **Clients**: no change required for CLI/iOS (they call the API; default behaviour hides archived; delete is now owner-scoped but the route and verb are unchanged).
- **Tests**: chat domain unit tests (archive/unarchive/delete, list filter, ownership), gateway unit tests (client params, rail render, action menu, confirmation).
