-- +goose Up

-- Backfill sub-chats created before parent_conversation_id existed. A
-- conversation's earliest parented run carries the caller's parent run id;
-- that run's session identifies the parent conversation. Runs without a parent
-- (user, scheduled, board, direct API callers) stay NULL, as do child rows
-- whose parent session has no conversation.
--
-- This is a separate migration from the DDL (00247) so the ACCESS EXCLUSIVE
-- lock from ALTER TABLE is released before this scan of kb.agent_runs runs.
-- The project and visibility guards keep a corrupt or cross-project run chain
-- from linking two conversations the owner cannot see together.
-- +goose StatementBegin
WITH first_parented_run AS (
    SELECT DISTINCT ON (ar.session_id)
           ar.session_id,
           ar.parent_run_id
    FROM kb.agent_runs ar
    WHERE ar.session_id IS NOT NULL
      AND ar.parent_run_id IS NOT NULL
    ORDER BY ar.session_id, ar.created_at ASC
),
resolved AS (
    SELECT c.id AS child_id,
           pc.id AS parent_id
    FROM kb.chat_conversations c
    JOIN first_parented_run fr ON fr.session_id = c.session_id
    JOIN kb.agent_runs pr ON pr.id = fr.parent_run_id
    JOIN kb.chat_conversations pc ON pc.session_id = pr.session_id
    WHERE pc.id <> c.id
      AND pc.project_id = c.project_id
      AND (pc.is_private = false OR pc.owner_user_id IS NOT DISTINCT FROM c.owner_user_id)
)
UPDATE kb.chat_conversations c
SET parent_conversation_id = r.parent_id
FROM resolved r
WHERE c.id = r.child_id
  AND c.parent_conversation_id IS NULL;
-- +goose StatementEnd

-- +goose Down

-- No-op: the links are derived from kb.agent_runs and remain valid if this
-- migration is rolled back on its own. Dropping 00247 removes the columns.
SELECT 1;
