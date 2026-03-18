-- Add skills column to agent_definitions table.
-- Stores a list of skill names the agent is permitted to load via the skill tool.
-- When non-empty, the skill tool is automatically injected into the agent pipeline.
-- ["*"] means all agent-visible skills; an empty array means no skill tool.
ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS skills TEXT[] NOT NULL DEFAULT '{}';
