-- migrate:up
-- Widen the role column in agent_run_messages from varchar(20) to text.
-- The original limit was too short for role names like "cli-assistant-agent-go" (22 chars).
-- Consistent with all other role columns in the kb schema which use text.
ALTER TABLE kb.agent_run_messages ALTER COLUMN role TYPE text;

-- migrate:down
ALTER TABLE kb.agent_run_messages ALTER COLUMN role TYPE character varying(20);
