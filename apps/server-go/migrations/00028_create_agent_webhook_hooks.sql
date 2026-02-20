-- +goose Up

-- Create kb.agent_webhook_hooks table
CREATE TABLE IF NOT EXISTS kb.agent_webhook_hooks (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id                UUID NOT NULL REFERENCES kb.agents(id) ON DELETE CASCADE,
    project_id              TEXT NOT NULL,
    label                   TEXT NOT NULL,
    token_hash              TEXT NOT NULL,
    enabled                 BOOLEAN NOT NULL DEFAULT true,
    rate_limit_config       JSONB,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for querying hooks by agent
CREATE INDEX IF NOT EXISTS idx_agent_webhook_hooks_agent_id ON kb.agent_webhook_hooks(agent_id);

-- Add source tracking columns to agent_runs
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS trigger_source TEXT;
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS trigger_metadata JSONB;

-- +goose Down

ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS trigger_metadata;
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS trigger_source;

DROP TABLE IF EXISTS kb.agent_webhook_hooks;
