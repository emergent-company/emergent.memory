-- +goose Up
-- +goose StatementBegin

-- #1316: ListConversations excludes archived rows by default:
--   SELECT ... FROM kb.chat_conversations
--   WHERE project_id = $1 AND is_archived = false
--   ORDER BY updated_at DESC LIMIT $2 OFFSET $3
-- (the total count uses the same WHERE). 00205 added is_archived but no
-- supporting index, so the default list scanned the project's rows and sorted.
--
-- This partial B-tree matches the query shape: project_id equality as the
-- leading key, updated_at DESC for the ORDER BY and keyset pagination, and the
-- is_archived = false predicate as the partial condition so archived rows stay
-- out of the index entirely. is_archived is deliberately not a key column — the
-- partial predicate already fixes its value. The optional owner/shared filter
-- (owner_user_id = ? OR is_private = false) is applied after the index scan and
-- is not a key, because it is absent from the shared-list path this index serves.
CREATE INDEX IF NOT EXISTS idx_chat_conversations_project_active_updated
    ON kb.chat_conversations (project_id, updated_at DESC)
    WHERE is_archived = false;

COMMENT ON INDEX kb.idx_chat_conversations_project_active_updated IS
    'Default chat conversation list: project scope, archived excluded, most-recently-updated first (issue #1316).';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_chat_conversations_project_active_updated;
-- +goose StatementEnd
