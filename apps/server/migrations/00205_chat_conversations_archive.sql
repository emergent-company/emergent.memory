-- +goose Up

-- Add a non-destructive archive state to chat conversations. The boolean is the
-- default list filter predicate (WHERE is_archived = false); the nullable
-- timestamptz stamp records when the conversation was archived and is cleared on
-- unarchive, so archived rows can later be sorted or expired. Existing rows
-- default to not archived (is_archived = false, archived_at NULL).
ALTER TABLE kb.chat_conversations
    ADD COLUMN is_archived boolean NOT NULL DEFAULT false,
    ADD COLUMN archived_at timestamptz NULL;

-- +goose Down

ALTER TABLE kb.chat_conversations
    DROP COLUMN archived_at,
    DROP COLUMN is_archived;
