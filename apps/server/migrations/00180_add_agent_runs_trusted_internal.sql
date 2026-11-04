-- +goose Up
-- Fail-closed trust marker for agent runs (issue #954). A run's trust (whether it
-- was started through a trusted surface and may therefore reach internal-visible
-- agents) is fixed at creation and inherited through delegation and resume. The
-- column defaults to FALSE = untrusted/external-facing, so a run that was never
-- explicitly marked trusted cannot reach internal agents.
ALTER TABLE kb.agent_runs
  ADD COLUMN IF NOT EXISTS trusted_internal boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE kb.agent_runs
  DROP COLUMN IF EXISTS trusted_internal;
