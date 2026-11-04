-- +goose Up
ALTER TABLE kb.agent_sandboxes ADD COLUMN IF NOT EXISTS base_image TEXT;
ALTER TABLE kb.agent_sandboxes ADD COLUMN IF NOT EXISTS image_digest TEXT;

-- +goose Down
ALTER TABLE kb.agent_sandboxes DROP COLUMN IF EXISTS image_digest;
ALTER TABLE kb.agent_sandboxes DROP COLUMN IF EXISTS base_image;
