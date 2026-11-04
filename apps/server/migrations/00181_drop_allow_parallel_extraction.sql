-- +goose Up
-- allow_parallel_extraction is dead: the per-project serialisation guard it
-- advertised was dropped in 97c8a8823 when Dequeue was rewritten to
-- DequeueBatch with WorkerConcurrency and adaptive scaling. No code reads the
-- column, so an operator setting it has no effect. Drop it rather than keep a
-- misleading, inert flag (issue #924).
ALTER TABLE kb.projects DROP COLUMN allow_parallel_extraction;

-- +goose Down
ALTER TABLE kb.projects ADD COLUMN allow_parallel_extraction boolean NOT NULL DEFAULT false;
