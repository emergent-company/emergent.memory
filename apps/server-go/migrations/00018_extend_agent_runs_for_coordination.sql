-- +goose Up
-- +goose StatementBegin

-- Extend agent_runs for multi-agent coordination
-- parent_run_id: links sub-agent runs to the parent orchestrator run
-- step_count: cumulative step counter (persists across resumes)
-- max_steps: per-run step limit (nullable = use global default)
-- resumed_from: links to the prior run this one continues from

ALTER TABLE kb.agent_runs
    ADD COLUMN IF NOT EXISTS parent_run_id UUID REFERENCES kb.agent_runs(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS step_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_steps INT,
    ADD COLUMN IF NOT EXISTS resumed_from UUID REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

-- Index for efficient sub-agent queries (find all children of a parent run)
CREATE INDEX IF NOT EXISTS idx_agent_runs_parent_run_id
    ON kb.agent_runs(parent_run_id)
    WHERE parent_run_id IS NOT NULL;

-- Index for resume chain lookups
CREATE INDEX IF NOT EXISTS idx_agent_runs_resumed_from
    ON kb.agent_runs(resumed_from)
    WHERE resumed_from IS NOT NULL;

COMMENT ON COLUMN kb.agent_runs.parent_run_id IS 'References the parent orchestrator run that spawned this sub-agent run';
COMMENT ON COLUMN kb.agent_runs.step_count IS 'Cumulative step counter across resumes (increments with each LLM turn)';
COMMENT ON COLUMN kb.agent_runs.max_steps IS 'Per-run step limit; NULL means use global default (MaxTotalStepsPerRun=500)';
COMMENT ON COLUMN kb.agent_runs.resumed_from IS 'References the prior run this execution was resumed from';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_agent_runs_resumed_from;
DROP INDEX IF EXISTS kb.idx_agent_runs_parent_run_id;

ALTER TABLE kb.agent_runs
    DROP COLUMN IF EXISTS resumed_from,
    DROP COLUMN IF EXISTS max_steps,
    DROP COLUMN IF EXISTS step_count,
    DROP COLUMN IF EXISTS parent_run_id;
-- +goose StatementEnd
