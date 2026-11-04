-- +goose Up
-- Unify agent run statuses to ACP vocabulary.
-- Internal values previously differed from ACP-layer values; now they match.
-- queued    → submitted
-- running   → working
-- paused    → input-required
-- success   → completed
-- error     → failed
-- skipped   → skipped (unchanged)
-- cancelled → cancelled (unchanged)
-- cancelling → cancelling (unchanged)

UPDATE kb.agent_runs SET status = 'submitted'     WHERE status = 'queued';
UPDATE kb.agent_runs SET status = 'working'       WHERE status = 'running';
UPDATE kb.agent_runs SET status = 'input-required' WHERE status = 'paused';
UPDATE kb.agent_runs SET status = 'completed'     WHERE status = 'success';
UPDATE kb.agent_runs SET status = 'failed'        WHERE status = 'error';

-- Also update last_run_status on agents table (stored as text mirror of run status)
UPDATE kb.agents SET last_run_status = 'submitted'      WHERE last_run_status = 'queued';
UPDATE kb.agents SET last_run_status = 'working'        WHERE last_run_status = 'running';
UPDATE kb.agents SET last_run_status = 'input-required' WHERE last_run_status = 'paused';
UPDATE kb.agents SET last_run_status = 'completed'      WHERE last_run_status = 'success';
UPDATE kb.agents SET last_run_status = 'failed'         WHERE last_run_status = 'error';

-- +goose Down
UPDATE kb.agent_runs SET status = 'queued'  WHERE status = 'submitted';
UPDATE kb.agent_runs SET status = 'running' WHERE status = 'working';
UPDATE kb.agent_runs SET status = 'paused'  WHERE status = 'input-required';
UPDATE kb.agent_runs SET status = 'success' WHERE status = 'completed';
UPDATE kb.agent_runs SET status = 'error'   WHERE status = 'failed';

UPDATE kb.agents SET last_run_status = 'queued'  WHERE last_run_status = 'submitted';
UPDATE kb.agents SET last_run_status = 'running' WHERE last_run_status = 'working';
UPDATE kb.agents SET last_run_status = 'paused'  WHERE last_run_status = 'input-required';
UPDATE kb.agents SET last_run_status = 'success' WHERE last_run_status = 'completed';
UPDATE kb.agents SET last_run_status = 'error'   WHERE last_run_status = 'failed';
