-- +goose Up
-- +goose StatementBegin

-- Append-only durable comments on an object-driven work item. A comment is
-- authored by a human (actor_type='user') through the work path, or by a work
-- agent (actor_type='agent') through its comment tool, and is surfaced in the
-- board's merged "Activity" thread alongside rework feedback rounds. Unlike
-- kb.work_item_feedback, a comment carries no round and never transitions the
-- item.
CREATE TABLE IF NOT EXISTS kb.object_comments (
    id           UUID        NOT NULL DEFAULT gen_random_uuid(),
    project_id   UUID        NOT NULL,
    canonical_id UUID        NOT NULL,
    author       TEXT,
    body         TEXT        NOT NULL,
    actor_type   TEXT,
    run_id       UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

COMMENT ON TABLE kb.object_comments IS 'Append-only durable comments on object-driven work items. Authored by humans (actor_type=user) or work agents (actor_type=agent), optionally tied to a run.';

CREATE INDEX IF NOT EXISTS idx_object_comments_item_created
    ON kb.object_comments (project_id, canonical_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.object_comments;

-- +goose StatementEnd
