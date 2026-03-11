-- +goose Up
ALTER TABLE kb.agent_runs ADD COLUMN trace_id text;
ALTER TABLE kb.agent_runs ADD COLUMN root_run_id uuid REFERENCES kb.agent_runs(id) ON DELETE SET NULL;

CREATE INDEX idx_agent_runs_trace_id ON kb.agent_runs(trace_id) WHERE trace_id IS NOT NULL;
CREATE INDEX idx_agent_runs_root_run_id ON kb.agent_runs(root_run_id) WHERE root_run_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_agent_runs_root_run_id;
DROP INDEX IF EXISTS kb.idx_agent_runs_trace_id;
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS root_run_id;
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS trace_id;
