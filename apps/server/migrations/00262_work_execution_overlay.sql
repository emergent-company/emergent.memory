-- +goose Up
-- +goose StatementBegin

-- Execution is an overlay, not a status: claim = one live run per work object.
-- First collapse any duplicate live runs for a subject (legacy data can have
-- more than one after a re-dispatch), keeping the newest.
CREATE TEMP TABLE _work_dup_runs AS
SELECT id
FROM (
    SELECT id,
           row_number() OVER (
               PARTITION BY subject_object_id
               ORDER BY created_at DESC, id DESC
           ) AS rn
    FROM kb.agent_runs
    WHERE subject_object_id IS NOT NULL
      AND status IN ('queued', 'running', 'paused')
) ranked
WHERE rn > 1;

UPDATE kb.agent_run_jobs
SET status = 'failed'
WHERE run_id IN (SELECT id FROM _work_dup_runs)
  AND status IN ('pending', 'processing');

UPDATE kb.agent_runs
SET status = 'cancelled',
    completed_at = now()
WHERE id IN (SELECT id FROM _work_dup_runs);

DROP TABLE _work_dup_runs;

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_runs_live_subject
    ON kb.agent_runs (subject_object_id)
    WHERE subject_object_id IS NOT NULL
      AND status IN ('queued', 'running', 'paused');

-- Persistent blocked state for a work item. The workflow status is preserved;
-- a row here suppresses dispatch/claim/reconciliation until a human unblocks.
CREATE TABLE IF NOT EXISTS kb.work_blocks (
    canonical_id  UUID PRIMARY KEY,
    project_id    UUID NOT NULL,
    reason        TEXT NOT NULL,
    failure_class TEXT,
    blocked_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    blocked_by    TEXT,
    note          TEXT,
    run_id        UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_work_blocks_project
    ON kb.work_blocks (project_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.work_blocks;
DROP INDEX IF EXISTS kb.idx_agent_runs_live_subject;

-- +goose StatementEnd
