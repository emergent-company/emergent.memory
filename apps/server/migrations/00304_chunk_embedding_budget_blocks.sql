-- +goose Up
-- +goose StatementBegin

-- Shared "this project is over budget" cache for the chunk embedding worker
-- (issue #793).
--
-- WHY a table: the verdict used to be a per-process map, so an un-park served by
-- one replica left every other replica's warm cache parking jobs until
-- budgetBlockCacheTTL (10 minutes) expired. One row per blocked project makes
-- the verdict shared state: the un-park deletes the row and every replica then
-- re-reads the budget on its next job instead of re-parking on a stale verdict.
--
-- The row is a cache, never the source of truth: blocked_until mirrors
-- budgetBlockCacheTTL, the budget check stays authoritative, and deleting the
-- row degrades to "re-check the budget", not to "embedding is unbounded".
--
-- No RLS: worker-internal queue state with no user content, like
-- kb.backup_schedules and kb.synthesis_lineage. The FK keeps it tidy when a
-- project is purged.
CREATE TABLE IF NOT EXISTS kb.chunk_embedding_budget_blocks (
    project_id    uuid PRIMARY KEY REFERENCES kb.projects (id) ON DELETE CASCADE,
    blocked_until timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.chunk_embedding_budget_blocks;
-- +goose StatementEnd
