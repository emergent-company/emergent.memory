-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.projects
    ADD COLUMN IF NOT EXISTS budget_usd              NUMERIC(10, 4)       NULL,
    ADD COLUMN IF NOT EXISTS budget_alert_threshold  NUMERIC(3, 2) NOT NULL DEFAULT 0.80;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.projects
    DROP COLUMN IF EXISTS budget_usd,
    DROP COLUMN IF EXISTS budget_alert_threshold;
-- +goose StatementEnd
