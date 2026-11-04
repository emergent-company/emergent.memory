-- +goose Up
-- +goose StatementBegin

-- Named agent work queues: a routing/organisation layer over kb.agent_run_jobs.
CREATE TABLE IF NOT EXISTS kb.agent_queues (
    project_id   UUID        NOT NULL,
    name         TEXT        NOT NULL,
    display_name TEXT        NOT NULL DEFAULT '',
    description  TEXT        NOT NULL DEFAULT '',
    concurrency  INTEGER     NOT NULL DEFAULT 1,
    priority     INTEGER     NOT NULL DEFAULT 100,
    enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, name)
);

COMMENT ON TABLE kb.agent_queues IS 'Named agent work queues. Each project has a default queue; agents bind to a queue and their dispatch jobs are enqueued there.';

-- Seed a default queue for every existing project so no run is ever unroutable.
INSERT INTO kb.agent_queues (project_id, name, display_name, description, concurrency, priority)
SELECT p.id, 'default', 'Default', 'Default agent work queue', 5, 100
FROM kb.projects p
ON CONFLICT (project_id, name) DO NOTHING;

-- Route/priority/project are persisted on the job so claiming needs no joins.
-- project_id scopes queue identity: a queue name is unique per project, so a
-- claim must match BOTH the project and the queue (two projects may each own a
-- queue named e.g. "security-review").
ALTER TABLE kb.agent_run_jobs
    ADD COLUMN IF NOT EXISTS queue TEXT NOT NULL DEFAULT 'default',
    ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS project_id UUID;

-- Backfill project ownership from the run's agent for pre-existing jobs.
UPDATE kb.agent_run_jobs arj
SET project_id = a.project_id
FROM kb.agent_runs r
JOIN kb.agents a ON a.id = r.agent_id
WHERE arj.run_id = r.id
  AND arj.project_id IS NULL;

-- Claim index: project+queue-scoped, priority-ordered polls.
CREATE INDEX IF NOT EXISTS idx_agent_run_jobs_queue_poll
    ON kb.agent_run_jobs (project_id, queue, status, priority, next_run_at)
    WHERE status = 'pending';

-- Queue binding on the agent definition (runtime Agent may override via config).
ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS default_queue TEXT NOT NULL DEFAULT 'default';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS default_queue;
DROP INDEX IF EXISTS kb.idx_agent_run_jobs_queue_poll;
ALTER TABLE kb.agent_run_jobs DROP COLUMN IF EXISTS priority;
ALTER TABLE kb.agent_run_jobs DROP COLUMN IF EXISTS project_id;
ALTER TABLE kb.agent_run_jobs DROP COLUMN IF EXISTS queue;
DROP TABLE IF EXISTS kb.agent_queues;

-- +goose StatementEnd
