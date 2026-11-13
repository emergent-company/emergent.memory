-- +goose Up
-- +goose StatementBegin

-- Weekly budget window in addition to the existing monthly cap.
--
-- WHY: a monthly cap can be exhausted on day 2 and then blocks agent runs for
-- weeks; a weekly cap catches runaway spend while the month is still reparable.
-- Both windows are opt-out by absence: budget_weekly_usd NULL means no weekly
-- limit, exactly as budget_usd NULL means no monthly limit.
--
-- budget_week_anchor / budget_month_anchor are the "reset now" anchors, set one
-- window at a time by POST /api/projects/:id/budget/reset:
--   NULL      the window follows the calendar (ISO week starting Monday
--             00:00 UTC; the UTC calendar month), which is the pre-existing
--             behaviour for the monthly cap.
--   non-NULL  the window is [anchor, anchor+1w) / [anchor, +1 calendar month),
--             repeating, so an admin can restart one cycle without touching the
--             other. The anchors are deliberately NOT part of the project PATCH
--             DTO: an anchor only moves through the admin-only reset endpoint.
--
-- budget_weekly_alert_threshold mirrors the monthly column's 0.80 schema
-- default, and every insert path must set it explicitly: the Bun insert writes
-- every field, so a zero Go value would persist as 0 and alert at $0 spend
-- (issue #1343).

ALTER TABLE kb.projects
    ADD COLUMN IF NOT EXISTS budget_weekly_usd             NUMERIC(10, 4) NULL,
    ADD COLUMN IF NOT EXISTS budget_weekly_alert_threshold NUMERIC(3, 2) NOT NULL DEFAULT 0.80,
    ADD COLUMN IF NOT EXISTS budget_week_anchor            TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS budget_month_anchor           TIMESTAMPTZ NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.projects
    DROP COLUMN IF EXISTS budget_weekly_usd,
    DROP COLUMN IF EXISTS budget_weekly_alert_threshold,
    DROP COLUMN IF EXISTS budget_week_anchor,
    DROP COLUMN IF EXISTS budget_month_anchor;
-- +goose StatementEnd
