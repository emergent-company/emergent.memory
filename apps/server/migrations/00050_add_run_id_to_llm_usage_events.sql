-- +goose Up
ALTER TABLE kb.llm_usage_events
    ADD COLUMN run_id uuid REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

CREATE INDEX idx_llm_usage_events_run_id
    ON kb.llm_usage_events (run_id)
    WHERE run_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_llm_usage_events_run_id;

ALTER TABLE kb.llm_usage_events
    DROP COLUMN IF EXISTS run_id;
