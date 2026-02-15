-- +goose Up
-- +goose StatementBegin

-- Agent definitions store agent configurations from product manifests.
-- Each agent definition describes an agent's prompt, tools, flow type,
-- visibility, and trigger configuration. Definitions are project-scoped
-- and optionally linked to a product version.

CREATE TABLE IF NOT EXISTS kb.agent_definitions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id      UUID REFERENCES kb.product_versions(id) ON DELETE SET NULL,
    project_id      UUID NOT NULL,
    name            VARCHAR(255) NOT NULL,
    description     TEXT,
    system_prompt   TEXT,
    model           JSONB DEFAULT '{}',
    tools           TEXT[] DEFAULT '{}',
    trigger         VARCHAR(255),
    flow_type       VARCHAR(50) NOT NULL DEFAULT 'single',
    is_default      BOOLEAN NOT NULL DEFAULT false,
    max_steps       INT,
    default_timeout INT,
    visibility      VARCHAR(50) NOT NULL DEFAULT 'project',
    acp_config      JSONB,
    config          JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT current_timestamp
);

-- Unique constraint: one agent definition per name per project
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_definitions_project_name
    ON kb.agent_definitions(project_id, name);

-- Index for product-scoped lookups (re-sync on product update)
CREATE INDEX IF NOT EXISTS idx_agent_definitions_product_id
    ON kb.agent_definitions(product_id)
    WHERE product_id IS NOT NULL;

-- Index for default agent lookup
CREATE INDEX IF NOT EXISTS idx_agent_definitions_project_default
    ON kb.agent_definitions(project_id, is_default)
    WHERE is_default = true;

COMMENT ON TABLE kb.agent_definitions IS 'Agent definitions from product manifests defining agent behavior, tools, and configuration';
COMMENT ON COLUMN kb.agent_definitions.product_id IS 'References the product version this definition came from (NULL for manually created)';
COMMENT ON COLUMN kb.agent_definitions.flow_type IS 'Agent execution flow: single, sequential, or loop';
COMMENT ON COLUMN kb.agent_definitions.visibility IS 'Agent visibility level: external (ACP-discoverable), project (admin-visible), internal (agent-only)';
COMMENT ON COLUMN kb.agent_definitions.acp_config IS 'Agent Card Protocol metadata for externally-visible agents';
COMMENT ON COLUMN kb.agent_definitions.trigger IS 'Trigger type: NULL (manual-only), event name (e.g. on_document_ingested), or cron expression';
COMMENT ON COLUMN kb.agent_definitions.default_timeout IS 'Default execution timeout in seconds';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.agent_definitions;
-- +goose StatementEnd
