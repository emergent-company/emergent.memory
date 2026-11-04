-- +goose Up
-- +goose StatementBegin

-- Backfill budget_alert_threshold for projects created through the Bun ORM
-- insert path (issue #1343). The column is NUMERIC(3,2) NOT NULL DEFAULT 0.80
-- and raw-SQL insert paths (standalone bootstrap, MCP project-create) omit it,
-- so they get 0.80. But projects.Repository.Create writes the whole Project
-- model, and the zero Go value for budget_alert_threshold was inserted as 0,
-- overriding the column default. A threshold of 0 makes
-- `spend < budget * threshold` false even at $0 spend, so every project
-- emitted a spurious "budget alert" at 0% usage.
--
-- 0 is not a meaningful alert threshold (it would mean "alert on the first
-- cent"), so reset any such row to the schema default of 0.80.
UPDATE kb.projects
SET budget_alert_threshold = 0.80,
    updated_at = now()
WHERE budget_alert_threshold = 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: the pre-fix value (0) is not a valid threshold, so there is nothing
-- correct to restore.
-- +goose StatementEnd
