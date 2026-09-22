-- +goose Up
-- kb.email_jobs had no in-flight timestamp, so the stale-job sweep keyed on
-- created_at (enqueue time) and could terminal-fail an ACTIVE email job that had
-- been queued longer than the stale window before a worker claimed it (issue
-- #705 follow-up). Stamp the claim time so the sweep, like the other four job
-- tables, only reaps processing/running rows whose actual start is stale.
ALTER TABLE kb.email_jobs ADD COLUMN started_at timestamp with time zone;

-- +goose Down
ALTER TABLE kb.email_jobs DROP COLUMN started_at;
