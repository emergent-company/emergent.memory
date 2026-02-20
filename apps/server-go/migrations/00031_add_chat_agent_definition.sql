-- +goose Up
ALTER TABLE kb.chat_conversations
    ADD COLUMN agent_definition_id UUID;

ALTER TABLE kb.chat_conversations
    ADD CONSTRAINT chat_conversations_agent_definition_fk
    FOREIGN KEY (agent_definition_id) REFERENCES kb.agent_definitions(id);

-- +goose Down
ALTER TABLE kb.chat_conversations
    DROP CONSTRAINT IF EXISTS chat_conversations_agent_definition_fk;

ALTER TABLE kb.chat_conversations
    DROP COLUMN IF EXISTS agent_definition_id;
