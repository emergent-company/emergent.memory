# fix-notification-fk-and-chat-archive-index

## Why

Two schema defects found in follow-up review of the notifications inbox (#1312)
and chat session lifecycle (#1311) work.

**#1313 — `kb.notifications.project_id` has two conflicting foreign keys.** The
baseline schema created both `FK_95464140d7dc04d7efb0afd6be0` (`ON DELETE CASCADE`)
and `FK_notifications_project_id` (`ON DELETE SET NULL`) on the same column. With
two FKs on one column the delete action is ambiguous and the redundant constraint
bloats the catalog. Notifications are project-scoped activity: a project-scoped
notification carries a `project_id`, and every read path (`applyScopeFilter`,
`GetCounts`, `List`) filters on it. A `SET NULL` outcome would leave
`scope='project'` rows with a NULL project that no project inbox can ever show.

**#1316 — the default chat conversation list is unindexed.** `ListConversations`
appends `WHERE is_archived = false` by default (and counts with the same filter)
but `00205` added `is_archived` with no supporting index, so the default list path
scans and sorts.

## What Changes

- **Migration `00210_notification_project_fk_cascade.sql`** drops the `SET NULL`
  constraint, drops and re-adds the single `ON DELETE CASCADE` constraint so the
  set is deterministic, deletes orphaned `scope='project' AND project_id IS NULL`
  residue (account-scope NULLs are legitimate and untouched), and asserts exactly
  one FK from `kb.notifications` to `kb.projects(id)`.
- **Migration `00211_chat_conversations_archived_list_index.sql`** adds the partial
  index `kb.idx_chat_conversations_project_active_updated` on
  `kb.chat_conversations (project_id, updated_at DESC) WHERE is_archived = false`.
- No application code changes. The notifications stats query is owned by the
  concurrent #1314 lane and is deliberately not touched.

## Capabilities

### New Capabilities

- `kb-schema-integrity`: guarantees about the `kb` schema's referential integrity
  and query-supporting indexes — at present the single cascade FK on
  `kb.notifications.project_id` and the partial index backing the default chat
  conversation list.

### Modified Capabilities

<!-- None: both changes are schema-only; no existing requirement text changes. -->

## Impact

- `apps/server/migrations/00210_notification_project_fk_cascade.sql` (new).
- `apps/server/migrations/00211_chat_conversations_archived_list_index.sql` (new).
- OpenSpec change `fix-notification-fk-and-chat-archive-index` (this directory).
- No server, gateway, CLI, or UI code change; no API or response shape change.
