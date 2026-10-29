-- +goose Up

-- Link a conversation to the conversation whose agent run spawned it. A chat
-- agent's tool call (for example search-knowledge) loops back into the chat API
-- and opens an internal agent conversation; without this link the session rail
-- lists those sub-chats as unrelated top-level sessions.
--
-- The link is persisted (not derived from kb.agent_runs at read time) so the
-- rail can group a page without joining run history, and so a conversation that
-- has aged out of the run table keeps its place in the tree.
--
-- The backfill lives in 00248 so this migration's ACCESS EXCLUSIVE lock covers
-- only the metadata changes, not a scan of kb.agent_runs.
ALTER TABLE kb.chat_conversations
    ADD COLUMN parent_conversation_id uuid NULL
        REFERENCES kb.chat_conversations(id) ON DELETE SET NULL,
    -- spawn_source names the mechanism (e.g. "search-knowledge"). Informational:
    -- it labels a nested row without needing the internal agent definition.
    ADD COLUMN spawn_source text NULL;

-- Partial index: only sub-chats carry a parent, and the rail/cascade paths
-- always look up by parent_conversation_id.
CREATE INDEX idx_chat_conversations_parent
    ON kb.chat_conversations (parent_conversation_id)
    WHERE parent_conversation_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS kb.idx_chat_conversations_parent;

ALTER TABLE kb.chat_conversations
    DROP COLUMN spawn_source,
    DROP COLUMN parent_conversation_id;
