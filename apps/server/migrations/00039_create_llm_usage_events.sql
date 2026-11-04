-- +goose Up
-- +goose StatementBegin

-- LLM usage events for cost tracking per project
CREATE TABLE IF NOT EXISTS kb.llm_usage_events (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id          UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    org_id              UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    provider            VARCHAR(50) NOT NULL,   -- 'google-ai' or 'vertex-ai'
    model               VARCHAR(255) NOT NULL,  -- model name used
    operation           VARCHAR(50) NOT NULL DEFAULT 'generate', -- 'generate' or 'embed'

    -- Multi-modal token counts
    text_input_tokens   BIGINT NOT NULL DEFAULT 0,
    image_input_tokens  BIGINT NOT NULL DEFAULT 0,
    video_input_tokens  BIGINT NOT NULL DEFAULT 0,
    audio_input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens       BIGINT NOT NULL DEFAULT 0,

    -- Estimated cost in USD
    estimated_cost_usd  NUMERIC(12, 8) NOT NULL DEFAULT 0,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_llm_usage_events_project ON kb.llm_usage_events(project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_llm_usage_events_org ON kb.llm_usage_events(org_id, created_at);
CREATE INDEX IF NOT EXISTS idx_llm_usage_events_model ON kb.llm_usage_events(provider, model, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.llm_usage_events;
-- +goose StatementEnd
