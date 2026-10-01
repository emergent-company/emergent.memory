-- +goose Up
-- +goose StatementBegin

-- Object-driven agent work: board-enabled graph objects carry an `assignee`
-- (the lane) alongside the existing built-in `status` (the work column).
ALTER TABLE kb.graph_objects
    ADD COLUMN IF NOT EXISTS assignee TEXT;

COMMENT ON COLUMN kb.graph_objects.assignee IS 'Work lane for board-enabled types: the agent identity expected to claim the object. NULL = any listening agent.';

CREATE INDEX IF NOT EXISTS idx_graph_objects_project_assignee
    ON kb.graph_objects (project_id, assignee)
    WHERE assignee IS NOT NULL;

-- Work configuration on the agent definition: status phase mapping,
-- requiresReview, failureLimit, retryPolicy. Empty object = defaults.
ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS work_config JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN kb.agent_definitions.work_config IS 'Object-driven work config: {status:{ready,inProgress,review,revision,blocked,done}, requiresReview, failureLimit, retryPolicy}.';

-- Link a run to the work object it was dispatched for, and classify failures.
-- subject_object_id stores the object canonical_id (the physical id changes
-- on every version).
ALTER TABLE kb.agent_runs
    ADD COLUMN IF NOT EXISTS subject_object_id UUID,
    ADD COLUMN IF NOT EXISTS subject_object_type TEXT,
    ADD COLUMN IF NOT EXISTS failure_class TEXT;

CREATE INDEX IF NOT EXISTS idx_agent_runs_subject_object
    ON kb.agent_runs (subject_object_id)
    WHERE subject_object_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS kb.idx_agent_runs_subject_object;
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS failure_class;
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS subject_object_type;
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS subject_object_id;
ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS work_config;
DROP INDEX IF EXISTS kb.idx_graph_objects_project_assignee;
ALTER TABLE kb.graph_objects DROP COLUMN IF EXISTS assignee;

-- +goose StatementEnd
