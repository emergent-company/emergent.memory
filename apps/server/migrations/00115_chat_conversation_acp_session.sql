-- +goose Up

-- Link each chat conversation to its backing ACP session.
-- One conversation = one ACP session (1:1). Nullable because existing rows
-- and non-agent conversations have no session.
ALTER TABLE kb.chat_conversations
    ADD COLUMN IF NOT EXISTS acp_session_id uuid REFERENCES kb.acp_sessions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_chat_conversations_acp_session_id
    ON kb.chat_conversations (acp_session_id)
    WHERE acp_session_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS kb.idx_chat_conversations_acp_session_id;

ALTER TABLE kb.chat_conversations
    DROP COLUMN IF EXISTS acp_session_id;
