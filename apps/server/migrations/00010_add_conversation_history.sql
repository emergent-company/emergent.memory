-- +goose Up
-- +goose StatementBegin
-- Add conversation history fields to chat messages
-- These fields enable multi-turn conversations with context awareness

-- context_summary: Compressed summary of previous conversation turns
-- Used to maintain conversation coherence while limiting token usage
ALTER TABLE kb.chat_messages
    ADD COLUMN IF NOT EXISTS context_summary TEXT,
    ADD COLUMN IF NOT EXISTS retrieval_context JSONB;

-- Create index for efficient history queries
-- Queries typically fetch last N messages by conversation_id + created_at DESC
CREATE INDEX IF NOT EXISTS idx_chat_messages_conversation_history
    ON kb.chat_messages(conversation_id, created_at DESC);

COMMENT ON COLUMN kb.chat_messages.context_summary IS 'Compressed summary of previous conversation turns for maintaining context';
COMMENT ON COLUMN kb.chat_messages.retrieval_context IS 'JSON array of graph object IDs used in generating this message (for tracking which knowledge was accessed)';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove conversation history features
DROP INDEX IF EXISTS kb.idx_chat_messages_conversation_history;

ALTER TABLE kb.chat_messages
    DROP COLUMN IF EXISTS context_summary,
    DROP COLUMN IF EXISTS retrieval_context;
-- +goose StatementEnd
