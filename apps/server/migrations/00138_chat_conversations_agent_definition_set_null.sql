-- +goose Up
-- Deleting an agent definition failed with a foreign-key violation whenever a
-- conversation still referenced it: kb.chat_conversations.agent_definition_id
-- was added without an ON DELETE rule, so Postgres defaulted to NO ACTION.
-- Null the link instead (matching kb.agent_runs and kb.agents, which already
-- use ON DELETE SET NULL) so conversation history survives the definition it
-- was created under.
ALTER TABLE kb.chat_conversations
    DROP CONSTRAINT IF EXISTS chat_conversations_agent_definition_fk;

ALTER TABLE kb.chat_conversations
    ADD CONSTRAINT chat_conversations_agent_definition_fk
    FOREIGN KEY (agent_definition_id) REFERENCES kb.agent_definitions(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE kb.chat_conversations
    DROP CONSTRAINT IF EXISTS chat_conversations_agent_definition_fk;

ALTER TABLE kb.chat_conversations
    ADD CONSTRAINT chat_conversations_agent_definition_fk
    FOREIGN KEY (agent_definition_id) REFERENCES kb.agent_definitions(id);
