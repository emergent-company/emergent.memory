-- +goose Up
-- +goose StatementBegin

-- Backfill the notification category for agent-question rows (issue #1375).
--
-- The ask_user / tool-policy producer never set Category, so every
-- agent-question notification stored category = NULL. Migration 00214 repaired
-- the scope / requires_action / event_key fields for the pre-#1312 rows but
-- deliberately skipped rows already at the taxonomy scope, so those rows kept
-- category = NULL and disagreed with the taxonomy entry (`agent.question` →
-- category `agents`) and with the rows 00214 did touch.
--
-- Service.Create now falls back to the taxonomy category when a producer omits
-- it; this repairs the rows written before that fix. The predicate is narrow
-- (agent_question only, category still NULL) so an explicit category is never
-- overwritten.
UPDATE kb.notifications
SET category = 'agents',
    updated_at = now()
WHERE type = 'agent_question'
  AND category IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: NULL was the inconsistent value being repaired; there is nothing
-- correct to restore.
-- +goose StatementEnd
