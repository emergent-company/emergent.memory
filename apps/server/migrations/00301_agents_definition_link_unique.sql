-- +goose Up
-- +goose StatementBegin

-- A definition can own more than one kb.agents row: there has never been a
-- unique index on agent_definition_id (and the old agents_role_key
-- UNIQUE (strategy_type) constraint was dropped by migration 00046). Two
-- concurrent binds of one definition mint two runtime rows. Enforce one runtime
-- row per (project, definition) with a partial unique index, and first dedupe
-- the rows that already violate it.

-- Dedupe is non-destructive. For each (project_id, agent_definition_id) group
-- with more than one row, keep one — prefer enabled, then earliest created_at,
-- then lowest id — and unlink the rest (agent_definition_id = NULL). Six foreign
-- keys into kb.agents are ON DELETE CASCADE (kb.agent_runs among them), so
-- deleting a loser would destroy run history; unlinking keeps every row and its
-- runs while satisfying the new index.

-- Unlinking alone does not stop a loser from resolving to the definition:
-- ResolveDefinitionForAgent step 4 is an exact-name match (agent.Name ==
-- definition.Name), and a loser still carries the definition's name. Two edits
-- neutralise that for real, non-destructively:
--   * a loser whose strategy_type is exactly 'agent-def:<definition_id>' has its
--     marker rewritten to 'definition' — the neutral value the gateway also
--     sends for plain agents, so the resolver (which only special-cases
--     "chat-session:"/"agent-def:" markers) treats it as "no definition";
--   * the loser's name is disambiguated with a short id suffix so step 4's
--     exact-name match can no longer hit it.
-- A source_triggers.agent_id bound to a renamed loser keeps working: it now
-- points at an independent agent (no definition link), and dispatch resolves
-- that row directly rather than through the definition.
WITH ranked AS (
    SELECT
        id,
        agent_definition_id,
        row_number() OVER (
            PARTITION BY project_id, agent_definition_id
            ORDER BY enabled DESC, created_at ASC, id ASC
        ) AS rn
    FROM kb.agents
    WHERE agent_definition_id IS NOT NULL
)
UPDATE kb.agents a
SET
    agent_definition_id = NULL,
    strategy_type = CASE
        WHEN a.strategy_type = 'agent-def:' || ranked.agent_definition_id::text
             THEN 'definition'
        ELSE a.strategy_type
    END,
    name = a.name || ' (unlinked ' || left(a.id::text, 8) || ')'
FROM ranked
WHERE a.id = ranked.id
  AND ranked.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agents_project_agent_definition
    ON kb.agents (project_id, agent_definition_id)
    WHERE agent_definition_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.uq_agents_project_agent_definition;
-- +goose StatementEnd
