-- +goose Up
-- +goose StatementBegin

-- Server-owned link from a coding issue to its pull request. This is written by
-- the source-event dispatcher and read by the review tool; it is deliberately
-- not a graph object, because project writers can edit graph properties and must
-- not be able to redirect an approval/rework to another issue.
CREATE TABLE IF NOT EXISTS kb.github_issue_pull_requests (
    project_id          UUID NOT NULL REFERENCES kb.projects (id) ON DELETE CASCADE,
    repo                TEXT NOT NULL,
    pr_number           INTEGER NOT NULL,
    issue_canonical_id  UUID NOT NULL,
    issue_number        INTEGER NOT NULL,
    issue_source_id     UUID NOT NULL,
    pr_url              TEXT NOT NULL DEFAULT '',
    branch              TEXT NOT NULL DEFAULT '',
    head_sha            TEXT NOT NULL DEFAULT '',
    state               TEXT NOT NULL DEFAULT 'open',
    closes_issue        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, repo, pr_number)
);

CREATE INDEX IF NOT EXISTS idx_issue_pr_issue
    ON kb.github_issue_pull_requests (project_id, issue_canonical_id);

ALTER TABLE kb.github_issue_pull_requests ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS github_issue_pull_requests_all ON kb.github_issue_pull_requests;
CREATE POLICY github_issue_pull_requests_all ON kb.github_issue_pull_requests FOR ALL USING (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.github_issue_pull_requests;
-- +goose StatementEnd
