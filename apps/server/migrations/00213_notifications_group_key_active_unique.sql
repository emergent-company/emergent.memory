-- +goose NO TRANSACTION
-- +goose Up
-- Durable coalescing for group-keyed notifications (issue #1343).
--
-- The budget-alert producer fires a budget check per usage event, each in its
-- own goroutine. Coalescing was a read-then-insert (Repository.GroupKeyExists /
-- UsageService.checkBudget COUNT) followed by an unconstrained INSERT, so when
-- several checks ran concurrently they all observed "no existing row" and each
-- inserted, producing four identical budget alerts at the same second.
--
-- A partial unique index on the active (not-cleared) notification for a
-- (user_id, group_key) pair lets producers use INSERT ... ON CONFLICT DO
-- NOTHING, which is atomic across goroutines and processes/restarts.

-- Collapse pre-existing duplicates created by the race: keep the earliest
-- active row per (user_id, group_key) and soft-clear the rest. Non-destructive
-- (rows are retained) and required before the unique index can be built.
UPDATE kb.notifications n
SET cleared_at = now(),
    updated_at = now()
WHERE n.group_key IS NOT NULL
  AND n.cleared_at IS NULL
  AND n.id <> (
    SELECT m.id
    FROM kb.notifications m
    WHERE m.user_id = n.user_id
      AND m.group_key = n.group_key
      AND m.cleared_at IS NULL
    ORDER BY m.created_at ASC, m.id ASC
    LIMIT 1
  );

-- Build CONCURRENTLY (no write-lock on kb.notifications). A failed
-- CONCURRENTLY build leaves an INVALID index, so drop any leftover first.
DROP INDEX CONCURRENTLY IF EXISTS kb.ux_notifications_user_group_key_active;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ux_notifications_user_group_key_active
    ON kb.notifications (user_id, group_key)
    WHERE group_key IS NOT NULL AND cleared_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.ux_notifications_user_group_key_active;
