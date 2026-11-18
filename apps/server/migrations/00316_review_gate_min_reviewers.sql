-- +goose Up
-- +goose StatementBegin

-- Head-bound reviewer quorum. A subject authored by a source-trigger run (a
-- GitHub App bot) must not be approved by a single reviewer agent: the required
-- `reviewed` check would then go green from the reviewing App's own verdict
-- with no human or independent review (#1005). Existing rows default to the
-- historical single-reviewer rule; Ensure raises the value to 2 on the next
-- event for a bot-authored pull request and never lowers it for that head.
ALTER TABLE kb.review_gates
    ADD COLUMN IF NOT EXISTS min_reviewers INTEGER NOT NULL DEFAULT 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.review_gates DROP COLUMN IF EXISTS min_reviewers;

-- +goose StatementEnd
