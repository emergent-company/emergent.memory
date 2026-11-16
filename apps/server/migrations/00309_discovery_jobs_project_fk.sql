-- +goose Up
-- Give kb.discovery_jobs.project_id a real foreign key to kb.projects
-- (issue #850).
--
-- 00111 created the table with `project_id UUID NOT NULL` and no REFERENCES, so
-- a project id that does not exist — a zero UUID, a deleted project, a typo from
-- a caller — inserted cleanly and was only noticed when a join returned nothing.
-- It is a live table (discoveryjobs.Module, registered in cmd/server/main.go),
-- and the sibling tables in the same shape already carry the constraint:
-- kb.document_parsing_jobs.project_id and kb.skill_reviews.project_id are both
-- `REFERENCES kb.projects (id) ON DELETE CASCADE`.
--
-- ON DELETE CASCADE is the convention those sibling tables and the 46 other
-- project-scoped foreign keys use: the row belongs to the project and cannot
-- outlive it. Deleting a project already cascades to discovery_jobs through
-- nothing today, which means a project delete would otherwise leave jobs
-- pointing at a project that no longer exists.
--
-- This migration ONLY adds the constraint, NOT VALID. That is metadata-only: no
-- table scan, no rewrite, so the statement finishes immediately and the lock is
-- released when the transaction commits. ADD CONSTRAINT ... FOREIGN KEY ... NOT
-- VALID takes SHARE ROW EXCLUSIVE on the child table (blocks writes, not reads)
-- and on the referenced kb.projects for the same instant. There is deliberately
-- no DROP CONSTRAINT first — ALTER TABLE ... DROP CONSTRAINT takes ACCESS
-- EXCLUSIVE even for a name that does not exist and would wait for every
-- in-flight reader. Idempotency is handled the 00257/00306 way instead: the ADD
-- is wrapped in a DO block that swallows duplicate_object, so re-running the
-- file is a no-op and does not downgrade a constraint 00310 has validated.
--
-- Validating is what scans the table, and VALIDATE takes SHARE UPDATE EXCLUSIVE
-- — read and write compatible. Doing it here, in the same transaction, would
-- hold the ADD's SHARE ROW EXCLUSIVE until the scan finished and block writes on
-- discovery_jobs for the duration, so validation is its own migration, 00310,
-- exactly the 00257 -> 00258 / 00306 -> 00308 split. Between the two versions
-- the constraint is enforced for new rows but unvalidated for existing ones,
-- which is the intended intermediate state.
--
-- There is no index migration here on purpose: 00111 created
-- idx_discovery_jobs_project_id on (project_id) when it built the table, and
-- 00260 added the partial unique index uq_discovery_jobs_one_active, also
-- leading on project_id. A foreign key does not need an index to be enforced,
-- and the cascade lookup on a project delete already has one to use, so a new
-- btree would only duplicate an existing one.

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.discovery_jobs
        ADD CONSTRAINT discovery_jobs_project_id_fkey
        FOREIGN KEY (project_id) REFERENCES kb.projects (id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.discovery_jobs
    DROP CONSTRAINT IF EXISTS discovery_jobs_project_id_fkey;
-- +goose StatementEnd
