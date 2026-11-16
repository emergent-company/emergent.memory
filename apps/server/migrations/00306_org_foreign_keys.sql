-- +goose Up
-- +goose StatementBegin
-- Give the four org-scoped tables that were missing one a real foreign key to
-- kb.orgs (issue #844).
--
-- Every other org-scoped table in kb/core already carries
-- `... REFERENCES kb.orgs (id) ON DELETE CASCADE` (14 of them in docs/schema.dbml
-- at the time of writing: backups, backup_schedules, projects, restores,
-- llm_usage_events, skills, source_apps, org_* ...). These four were the
-- exceptions, so an organization_id pointing at a project id, a zero UUID or a
-- deleted org inserted cleanly and was only noticed when a join returned
-- nothing:
--
--   * kb.discovery_jobs.organization_id          NOT NULL (00111)
--   * kb.invites.organization_id                 NOT NULL (baseline)
--   * kb.document_parsing_jobs.organization_id   nullable (baseline; 00004
--                                                 dropped NOT NULL so a user
--                                                 without an org can upload)
--   * kb.skill_reviews.org_id                    nullable (00270)
--
-- ON DELETE CASCADE is the convention those 14 tables use: the row belongs to
-- the org and cannot outlive it. The two nullable columns are unaffected — NULL
-- never violates a foreign key, and no NULL is backfilled or rewritten here.
--
-- This migration ONLY adds the constraints, NOT VALID. That is metadata: no
-- scan, no table rewrite, so each statement finishes immediately, and the
-- SHARE ROW EXCLUSIVE lock it takes on the table (which blocks writes, not
-- reads) is released when the four-statement transaction commits. New writes are
-- checked by the constraint from that moment.
--
-- Validating is what scans a populated table, and VALIDATE takes SHARE UPDATE
-- EXCLUSIVE — read and write compatible. Doing it here, inside this
-- transaction, is what 00258 warns against: the ADD's SHARE ROW EXCLUSIVE would
-- then be held until the last VALIDATE of the last table completed, blocking
-- INSERT/UPDATE/DELETE on all four tables for the whole scan. So validation is
-- its own migration, 00308, exactly the 00257 -> 00258 (and 00266 -> 00267)
-- split. Between the two versions the constraints are enforced for new rows but
-- unvalidated for existing ones, which is the intended intermediate state.
--
-- DROP ... IF EXISTS first keeps the migration re-runnable at statement level
-- without changing what a first run does.
--
-- The supporting indexes are built CONCURRENTLY in 00307; a foreign key does not
-- need one to be enforced, but every other org-scoped table has an org-leading
-- index so that an org delete does not sequentially scan its children.

ALTER TABLE kb.discovery_jobs
    DROP CONSTRAINT IF EXISTS discovery_jobs_organization_id_fkey;
ALTER TABLE kb.discovery_jobs
    ADD CONSTRAINT discovery_jobs_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES kb.orgs (id) ON DELETE CASCADE NOT VALID;

ALTER TABLE kb.document_parsing_jobs
    DROP CONSTRAINT IF EXISTS document_parsing_jobs_organization_id_fkey;
ALTER TABLE kb.document_parsing_jobs
    ADD CONSTRAINT document_parsing_jobs_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES kb.orgs (id) ON DELETE CASCADE NOT VALID;

ALTER TABLE kb.invites
    DROP CONSTRAINT IF EXISTS invites_organization_id_fkey;
ALTER TABLE kb.invites
    ADD CONSTRAINT invites_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES kb.orgs (id) ON DELETE CASCADE NOT VALID;

ALTER TABLE kb.skill_reviews
    DROP CONSTRAINT IF EXISTS skill_reviews_org_id_fkey;
ALTER TABLE kb.skill_reviews
    ADD CONSTRAINT skill_reviews_org_id_fkey
    FOREIGN KEY (org_id) REFERENCES kb.orgs (id) ON DELETE CASCADE NOT VALID;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.skill_reviews DROP CONSTRAINT IF EXISTS skill_reviews_org_id_fkey;
ALTER TABLE kb.invites DROP CONSTRAINT IF EXISTS invites_organization_id_fkey;
ALTER TABLE kb.document_parsing_jobs DROP CONSTRAINT IF EXISTS document_parsing_jobs_organization_id_fkey;
ALTER TABLE kb.discovery_jobs DROP CONSTRAINT IF EXISTS discovery_jobs_organization_id_fkey;
-- +goose StatementEnd
