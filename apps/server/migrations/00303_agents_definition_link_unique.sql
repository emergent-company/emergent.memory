-- +goose Up
-- +goose StatementBegin

-- A definition can own more than one kb.agents row: there has never been a
-- unique index on agent_definition_id (and the old agents_role_key
-- UNIQUE (strategy_type) constraint was dropped by migration 00046). Two
-- concurrent binds of one definition mint two runtime rows. Enforce one runtime
-- row per (project, definition) with a partial unique index, and first dedupe
-- the rows that already violate it.

-- Step 1 — re-mark and unlink extraction shells BEFORE the dedupe below. The
-- extraction worker's internal runtime row is auto-managed and never scheduled,
-- so it must not hold the definition link: under the new partial unique index a
-- disabled extraction shell becomes the ONLY row a definition can own, which
-- breaks the blueprint apply and trigger paths that link their own row (23505)
-- and then operate on the disabled shell. Move the definition identity into the
-- strategy_type marker and drop the FK. Order matters: run before the dedupe,
-- otherwise the dedupe would rename/unlink these shells as ordinary duplicates.
UPDATE kb.agents
SET
    strategy_type = 'extraction-agent:' || agent_definition_id::text,
    agent_definition_id = NULL
WHERE name LIKE 'extraction-agent:%'
  AND agent_definition_id IS NOT NULL;

-- Step 2 — dedupe. Non-destructive: for each (project_id, agent_definition_id)
-- group with more than one row, keep one and unlink the rest
-- (agent_definition_id = NULL). Six foreign keys into kb.agents are ON DELETE
-- CASCADE (kb.agent_runs among them), so deleting a loser would destroy run
-- history; unlinking keeps every row and its runs while satisfying the index.

-- Keeper rule, in order: prefer a row that is enabled AND actively triggered
-- (trigger_type <> 'manual'), then the row with the most kb.agent_runs, then
-- enabled, then earliest created_at, then lowest id. The first two tiebreakers
-- matter in production: a definition may own both a manual "alfred" shell
-- (1 run) and a live hourly "Test" schedule (thousands of runs). The old rule
-- (enabled, created_at, id) made the manual row the keeper and unlinked the
-- live schedule, which then kept firing with no system prompt, tools or rules —
-- the #737 failure shape reintroduced by the fix. Ranking the actively
-- triggered, high-run-count row first keeps the live schedule as the keeper.
DROP TABLE IF EXISTS _00303_losers;
CREATE TEMP TABLE _00303_losers AS
WITH ranked AS (
    SELECT
        a.id,
        a.agent_definition_id,
        row_number() OVER (
            PARTITION BY a.project_id, a.agent_definition_id
            ORDER BY
                (a.enabled AND a.trigger_type <> 'manual') DESC,
                (SELECT count(*) FROM kb.agent_runs ar WHERE ar.agent_id = a.id) DESC,
                a.enabled DESC,
                a.created_at ASC,
                a.id ASC
        ) AS rn
    FROM kb.agents a
    WHERE a.agent_definition_id IS NOT NULL
)
SELECT a.id, a.name, a.trigger_type, ranked.agent_definition_id
FROM kb.agents a
JOIN ranked ON ranked.id = a.id
WHERE ranked.rn > 1;

-- Unlink the losers. Unlinking alone does not stop a loser from resolving to
-- the definition: ResolveDefinitionForAgent step 4 is an exact-name match
-- (agent.Name == definition.Name), and a loser still carries the definition's
-- name. Two edits neutralise that for real, non-destructively:
--   * a loser whose strategy_type is exactly 'agent-def:<definition_id>' or
--     'chat-session:<definition_id>' has its marker rewritten to 'definition' —
--     the neutral value the gateway also sends for plain agents, so the resolver
--     (which only special-cases "chat-session:"/"agent-def:" markers) treats it
--     as "no definition";
--   * the loser's name is disambiguated with a short id suffix so step 4's
--     exact-name match can no longer hit it.
-- A source_triggers.agent_id bound to a renamed loser keeps working: it now
-- points at an independent agent (no definition link), and dispatch resolves
-- that row directly rather than through the definition.
--
-- A loser that kept a live schedule must not keep firing unlinked (no system
-- prompt, tool policy, skills or write rules — silently). For any loser whose
-- trigger_type is not 'manual', disable it and reset its cron to the inert
-- daily default in the same UPDATE; a manual loser (which only ever runs on
-- explicit invocation) is left enabled with its schedule untouched.
UPDATE kb.agents a
SET
    agent_definition_id = NULL,
    strategy_type = CASE
        WHEN a.strategy_type IN ('agent-def:' || l.agent_definition_id::text,
                                 'chat-session:' || l.agent_definition_id::text)
             THEN 'definition'
        ELSE a.strategy_type
    END,
    name = a.name || ' (unlinked ' || left(a.id::text, 8) || ')',
    enabled = CASE WHEN a.trigger_type <> 'manual' THEN false ELSE a.enabled END,
    cron_schedule = CASE WHEN a.trigger_type <> 'manual' THEN '0 0 * * *' ELSE a.cron_schedule END
FROM _00303_losers l
WHERE a.id = l.id;

-- Audit: name every loser whose live schedule was disabled so the action is
-- visible in the migration log rather than silent.
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT id, name FROM _00303_losers WHERE trigger_type <> 'manual' ORDER BY name, id
    LOOP
        RAISE NOTICE '00303: disabled unlinked runtime agent % (%)', r.id, r.name;
    END LOOP;
END $$;

DROP TABLE IF EXISTS _00303_losers;

-- Step 3 — the index itself. IF NOT EXISTS keeps the whole Up body idempotent:
-- step 1's re-mark is a no-op once the shells carry no FK, step 2's dedupe
-- finds no rn > 1 rows once each (project, definition) owns one row, and the
-- audit loop is empty. Note the index is NOT created CONCURRENTLY — the goose
-- StatementBegin wrapper forbids it — so it takes a brief ACCESS EXCLUSIVE lock
-- on kb.agents, which is acceptable at this table's size.
CREATE UNIQUE INDEX IF NOT EXISTS uq_agents_project_agent_definition
    ON kb.agents (project_id, agent_definition_id)
    WHERE agent_definition_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Lossy by design: dropping the index re-opens the (project_id, definition)
-- link; it does not reverse the re-marks, unlinks, marker rewrites, renames or
-- the loser schedule disables applied in Up, and it cannot (the pre-index state
-- is not recoverable from the rows that remain).
DROP INDEX IF EXISTS kb.uq_agents_project_agent_definition;
-- +goose StatementEnd
