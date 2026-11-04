-- +goose Up
-- Add domain classification fields to kb.documents
ALTER TABLE kb.documents
    ADD COLUMN IF NOT EXISTS domain_name TEXT,
    ADD COLUMN IF NOT EXISTS domain_confidence FLOAT4,
    ADD COLUMN IF NOT EXISTS classification_signals JSONB;

-- +goose Down
ALTER TABLE kb.documents
    DROP COLUMN IF EXISTS domain_name,
    DROP COLUMN IF EXISTS domain_confidence,
    DROP COLUMN IF EXISTS classification_signals;
