-- +goose Up

-- Persist the caller's explicit merge confirmation on an update (apply) job so
-- the asynchronous executor applies the upstream blueprint with
-- MergeTypeConflicts instead of blocking on an object-type name collision.
ALTER TABLE kb.blueprint_update_jobs
    ADD COLUMN IF NOT EXISTS merge_type_conflicts boolean NOT NULL DEFAULT false;
