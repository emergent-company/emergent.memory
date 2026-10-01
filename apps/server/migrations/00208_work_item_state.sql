-- +goose Up
-- +goose StatementBegin

-- Per-work-item failure accounting (object-driven work, P2). The sidecar is
-- keyed by (project_id, canonical_id) — stable across object versions — so a
-- failure count survives re-enqueue and version churn without polluting the
-- versioned kb.graph_objects rows.
CREATE TABLE IF NOT EXISTS kb.work_item_state (
    project_id          UUID        NOT NULL,
    canonical_id        UUID        NOT NULL,
    failure_count       INTEGER     NOT NULL DEFAULT 0,
    last_failure_class  TEXT,
    requeue_backoff_at  TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, canonical_id)
);

COMMENT ON TABLE kb.work_item_state IS 'Per-work-item failure budget ledger (object-driven work). Keyed by canonical_id so the count survives re-enqueue and version churn.';

CREATE INDEX IF NOT EXISTS idx_work_item_state_updated_at
    ON kb.work_item_state (updated_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.work_item_state;

-- +goose StatementEnd
