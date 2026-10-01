-- +goose Up
-- +goose StatementBegin

-- Append-only review feedback for object-driven work (P3). Each row is one
-- rework request round; a round is authored by a human reviewer against a work
-- item (canonical_id), optionally tied to the rework run it produced. The
-- revision count for the revision-cap escalation is derived from MAX(round).
CREATE TABLE IF NOT EXISTS kb.work_item_feedback (
    id           UUID        NOT NULL DEFAULT gen_random_uuid(),
    project_id   UUID        NOT NULL,
    canonical_id UUID        NOT NULL,
    round        INTEGER     NOT NULL,
    author       TEXT,
    text         TEXT        NOT NULL,
    run_id       UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

COMMENT ON TABLE kb.work_item_feedback IS 'Append-only rework feedback for object-driven work items. Keyed by (project_id, canonical_id, round); MAX(round) is the revision count.';

CREATE INDEX IF NOT EXISTS idx_work_item_feedback_item_round
    ON kb.work_item_feedback (project_id, canonical_id, round);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.work_item_feedback;

-- +goose StatementEnd
