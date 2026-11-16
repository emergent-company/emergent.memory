-- +goose Up
-- +goose StatementBegin

-- Server-owned review aggregation. Reviewers submit head-bound verdicts; the
-- gate is the only authority on whether every active reviewer accepted.
CREATE TABLE IF NOT EXISTS kb.review_gates (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID NOT NULL REFERENCES kb.projects (id) ON DELETE CASCADE,
    subject_type       TEXT NOT NULL,
    subject_ref        TEXT NOT NULL,
    head_sha           TEXT NOT NULL,
    required_reviewers JSONB NOT NULL DEFAULT '[]'::jsonb,
    status             TEXT NOT NULL DEFAULT 'pending',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, subject_type, subject_ref, head_sha)
);

CREATE TABLE IF NOT EXISTS kb.review_verdicts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gate_id           UUID NOT NULL REFERENCES kb.review_gates (id) ON DELETE CASCADE,
    project_id        UUID NOT NULL REFERENCES kb.projects (id) ON DELETE CASCADE,
    reviewer_agent_id UUID NOT NULL,
    reviewer_name     TEXT NOT NULL,
    head_sha          TEXT NOT NULL,
    verdict           TEXT NOT NULL,
    evidence          TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (gate_id, reviewer_agent_id)
);

CREATE INDEX IF NOT EXISTS idx_review_gates_subject
    ON kb.review_gates (project_id, subject_type, subject_ref, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_review_verdicts_gate
    ON kb.review_verdicts (gate_id, created_at DESC);

ALTER TABLE kb.review_gates ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS review_gates_all ON kb.review_gates;
CREATE POLICY review_gates_all ON kb.review_gates FOR ALL USING (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
);

ALTER TABLE kb.review_verdicts ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS review_verdicts_all ON kb.review_verdicts;
CREATE POLICY review_verdicts_all ON kb.review_verdicts FOR ALL USING (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.review_verdicts;
DROP TABLE IF EXISTS kb.review_gates;
-- +goose StatementEnd
