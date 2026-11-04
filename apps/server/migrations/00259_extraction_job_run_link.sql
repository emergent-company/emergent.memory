-- +goose Up
-- +goose StatementBegin
-- Link an extraction job to the agent run that records its pipeline transcript.
--
-- Extraction runs a bespoke staged pipeline (unpacker → extractor →
-- relationships → quality) with an in-memory ADK session, so unlike chat,
-- scheduled and board runs it had no thread, no run and no stored transcript:
-- the stage prompts and model responses only reached a per-job log file and
-- OpenTelemetry spans. The chat workspace renders a run's transcript from
-- kb.agent_run_messages, so recording the pipeline as an agent run is what makes
-- an extraction debuggable as a session (and keeps it debuggable when the
-- extraction agent is replaced by a user-defined one).
--
-- Nullable on purpose: jobs created before this column existed (and jobs whose
-- recording failed) fall back to the extraction history page. ON DELETE SET NULL
-- keeps the job when its run row is removed.
ALTER TABLE kb.object_extraction_jobs
    ADD COLUMN IF NOT EXISTS run_id uuid
        REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_object_extraction_jobs_run_id
    ON kb.object_extraction_jobs(run_id);

COMMENT ON COLUMN kb.object_extraction_jobs.run_id IS
    'Agent run that records this job''s pipeline transcript (kb.agent_runs); NULL for jobs that predate the link or when transcript recording is unavailable.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_object_extraction_jobs_run_id;
ALTER TABLE kb.object_extraction_jobs DROP COLUMN IF EXISTS run_id;
-- +goose StatementEnd
