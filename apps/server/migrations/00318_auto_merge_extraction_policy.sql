-- +goose Up
--
-- #1074: let a project choose how extraction auto-merge resolves identities.
--
-- Before this column, auto-merge always used the "partial" merge policy: clean
-- additions merged, but anything that matched an existing object (by exact
-- key or by cosine similarity at 0.92) stayed on the staging branch for human
-- review. For an unattended ingest with no reviewer that parked exactly the
-- new facts about known entities that memory most needs to record.
--
-- "partial" remains the default so existing projects do not change behaviour.
-- A project can opt into "enrich", which fills empty target fields from the
-- source and lets a re-mention reach the existing object instead of waiting
-- on a branch. The check constraint rejects any other value: merge policy
-- names are validated by the API, and a value outside this small set would
-- otherwise make the worker fall back silently.
ALTER TABLE kb.projects
    ADD COLUMN IF NOT EXISTS auto_merge_extraction_policy TEXT NOT NULL DEFAULT 'partial';

ALTER TABLE kb.projects
    DROP CONSTRAINT IF EXISTS kb_projects_auto_merge_extraction_policy_check;

ALTER TABLE kb.projects
    ADD CONSTRAINT kb_projects_auto_merge_extraction_policy_check
    CHECK (auto_merge_extraction_policy IN ('partial', 'enrich'));

-- +goose Down
ALTER TABLE kb.projects
    DROP CONSTRAINT IF EXISTS kb_projects_auto_merge_extraction_policy_check;

ALTER TABLE kb.projects
    DROP COLUMN IF EXISTS auto_merge_extraction_policy;
