-- +goose Up
-- Add an opaque ui_config JSONB column to agent definitions so the UI can store
-- a per-agent appearance (icon + color) alongside the definition. Mirrors the
-- object-type registry's ui_config: the server stores raw JSON and applies no
-- icon/color validation. Empty object means "no appearance".
ALTER TABLE kb.agent_definitions
  ADD COLUMN IF NOT EXISTS ui_config JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE kb.agent_definitions
  DROP COLUMN IF EXISTS ui_config;
