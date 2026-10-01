## 1. Notification project FK (#1313)

- [x] 1.1 Add `apps/server/migrations/00210_notification_project_fk_cascade.sql`: drop `FK_notifications_project_id` (SET NULL) and `FK_95464140d7dc04d7efb0afd6be0`, re-add a single `ON DELETE CASCADE` FK, and include a reversible Down.
- [x] 1.2 Handle existing NULL rows: delete `scope='project' AND project_id IS NULL` residue; leave account-scope NULLs untouched.
- [x] 1.3 Add the in-migration assertion that `kb.notifications` has exactly one FK to `kb.projects(id)`.
- [x] 1.4 Verify `go build ./...` in `apps/server` and the migration-number/immutability guards pass.

## 2. Chat archive list index (#1316)

- [x] 2.1 Add `apps/server/migrations/00211_chat_conversations_archived_list_index.sql` creating the partial index `kb.idx_chat_conversations_project_active_updated ON kb.chat_conversations (project_id, updated_at DESC) WHERE is_archived = false`, with a reversible Down.
- [x] 2.2 Verify the index matches the `ListConversations` default query shape (project_id equality, `is_archived = false`, `ORDER BY updated_at DESC`) and does not duplicate an existing index.

## 3. Spec

- [x] 3.1 Add the `kb-schema-integrity` delta requirements documenting both schema guarantees.
- [x] 3.2 Verify `openspec validate fix-notification-fk-and-chat-archive-index --strict` passes.

## 4. Verification

- [x] 4.1 Run `go build ./...` in `apps/server` (exit 0).
- [x] 4.2 Run `go test -count=1 ./domain/notifications/... ./domain/chat/...` (all packages ok; DB-backed cases skip without a configured Postgres).
- [x] 4.3 Applied both migrations against a throwaway `pgvector/pgvector:pg17` container: `migrate -c up` reached version 211; `EXPLAIN` showed `Index Scan`/`Index Only Scan using idx_chat_conversations_project_active_updated` with no Sort; `migrate -c down` twice restored the pre-fix double-FK state, and a second `up` re-applied both; `migrate -c verify` reported no invalid indexes. See PR notes for anything not covered (production data volume).
