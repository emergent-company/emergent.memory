-- +goose Up
-- +goose StatementBegin

-- Agent run jobs table: dispatch ledger for queued agent runs.
-- Each row represents one pending/processing/completed job for a queued agent run.
-- Workers claim jobs using FOR UPDATE SKIP LOCKED to prevent duplicate execution.
CREATE TABLE kb.agent_run_jobs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID        NOT NULL REFERENCES kb.agent_runs(id) ON DELETE CASCADE,
    status          TEXT        NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    attempt_count   INT         NOT NULL DEFAULT 0,
    max_attempts    INT         NOT NULL DEFAULT 1,
    next_run_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);

-- Index for worker poll query: pending jobs ordered by next_run_at
CREATE INDEX idx_agent_run_jobs_poll
    ON kb.agent_run_jobs (status, next_run_at)
    WHERE status IN ('pending');

-- Index for orphan-recovery lookup: find jobs by run_id
CREATE INDEX idx_agent_run_jobs_run_id
    ON kb.agent_run_jobs (run_id);

COMMENT ON TABLE kb.agent_run_jobs IS 'Dispatch ledger for queued agent runs. Workers claim rows with FOR UPDATE SKIP LOCKED.';
COMMENT ON COLUMN kb.agent_run_jobs.run_id IS 'FK to kb.agent_runs — the run this job drives';
COMMENT ON COLUMN kb.agent_run_jobs.attempt_count IS 'Number of execution attempts so far';
COMMENT ON COLUMN kb.agent_run_jobs.max_attempts IS 'Maximum allowed attempts (from agent definition max_retries + 1)';
COMMENT ON COLUMN kb.agent_run_jobs.next_run_at IS 'Earliest time a worker may claim this job (supports exponential backoff)';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.agent_run_jobs;

-- +goose StatementEnd
