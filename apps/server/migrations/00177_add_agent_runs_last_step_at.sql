-- +goose Up
-- kb.agent_runs has only started_at for liveness. The stale-run reaper keyed off
-- started_at alone, so a legitimately long run (big sandbox build, many MCP
-- round-trips) was falsely marked errored after the idle threshold even though
-- it was still making progress. Add last_step_at so the executor can heartbeat
-- each pipeline step and the reaper can key off the latest activity instead of
-- the start time. Nullable with no default: never-heartbeat runs fall back to
-- started_at via COALESCE in the reaper.
ALTER TABLE kb.agent_runs ADD COLUMN last_step_at timestamp with time zone;

-- +goose Down
ALTER TABLE kb.agent_runs DROP COLUMN last_step_at;
