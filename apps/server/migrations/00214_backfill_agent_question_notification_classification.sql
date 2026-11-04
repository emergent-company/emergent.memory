-- +goose Up
-- +goose StatementBegin

-- Reclassify legacy agent-question notifications (issue #1364).
--
-- Before the scoped-inbox subsystem (#1312), the ask_user tool and the
-- tool-policy confirmation gate inserted notifications directly, without
-- scope / event_key / requires_action. Migration 00206 added those columns with
-- their defaults, so every pre-existing agent-question row was backfilled to
-- scope='account', requires_action=false, event_key=NULL, category=NULL.
--
-- The taxonomy classifies this type as `agent.question`: project scope,
-- requires action, category agents. Because the stored row disagreed, legacy
-- "Agent needs your input" / "Agent wants to call tool … Do you approve?"
-- notifications never appeared under Action required (and surfaced in the
-- account inbox). Reclassify them to match the taxonomy. New rows emitted by
-- the current producer are already classified correctly; this only repairs the
-- rows the additive 00206 backfill mis-defaulted.
--
-- The predicate is intentionally narrow: `type = 'agent_question'` rows that
-- still carry the pre-#1312 shape. Rows already at the taxonomy value are not
-- touched.
UPDATE kb.notifications
SET scope = 'project',
    requires_action = true,
    event_key = COALESCE(event_key, 'agent.question'),
    category = COALESCE(category, 'agents'),
    updated_at = now()
WHERE type = 'agent_question'
  AND (scope <> 'project' OR requires_action = false OR event_key IS NULL);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: the pre-backfill shape (account scope, requires_action=false,
-- event_key NULL) is an invalid classification for this type, so there is
-- nothing correct to restore.
-- +goose StatementEnd
