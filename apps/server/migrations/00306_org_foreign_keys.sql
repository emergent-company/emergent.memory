-- +goose Up
-- +goose StatementBegin
-- Give the four org-scoped tables that were missing one a real foreign key to
-- kb.orgs (issue #844).
--
-- Every other org-scoped table in kb/core already carries
-- `... REFERENCES kb.orgs(id) ON DELETE CASCADE` (14 of them in docs/schema.dbml
-- at the time of writing: backups, backup_schedules, projects, restores,
-- llm_usage_events, skills, source_apps, org_* ...). These four were the
-- exceptions, so an organization_id pointing at a project id, a zero UUID or a
-- deleted org inserted cleanly and was only noticed when a join returned
-- nothing:
--
--   * kb.discovery_jobs.organization_id          NOT NULL (00111 / #844)
--   * kb.document_parsing_jobs.organization_id   NOT NULL (baseline)
--   * kb.invites.organization_id                 NOT NULL (baseline)
--   * kb.skill_reviews.org_id                    nullable (00270)
--
-- ON DELETE CASCADE is the convention those 14 tables use: the row belongs to
-- the org and cannot outlive it. skill_reviews.org_id is nullable (NULL for
-- rows written before the column was populated), which a FK allows unchanged —
-- NULL never violates a foreign key.
--
-- Lock profile: all four tables can be populated in an existing environment, so
-- each constraint is added NOT VALID (metadata-only, no scan of the existing
-- rows) and then validated by a separate statement. ADD CONSTRAINT ... NOT VALID
-- still takes SHARE ROW EXCLUSIVE for the duration of the statement, and
-- VALIDATE CONSTRAINT takes SHARE UPDATE EXCLUSIVE — it does not block reads or
-- writes — so no table is held under ACCESS EXCLUSIVE for a long scan. This is
-- the 00257/00258 (and 00266/00267) discipline.
--
-- VALIDATE deliberately fails loudly on a pre-existing orphan row: an org id
-- with no org is the bug this migration is closing, and silently leaving the
-- constraint NOT VALID (enforced only for new writes) would hide it. The failure
-- names the constraint, and the operator's remedy is to decide what that row
-- belongs to (or delete it) and re-run — the migration rolls back as a unit,
-- so a retry re-adds the (still NOT VALID) constraint from scratch.
--
-- DROP ... IF EXISTS first keeps the migration re-runnable at the statement
-- level without changing what a first run does.
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

-- Separate statements: each VALIDATE scans its table once under SHARE UPDATE
-- EXCLUSIVE and marks the constraint valid without rewriting anything.
ALTER TABLE kb.discovery_jobs
    VALIDATE CONSTRAINT discovery_jobs_organization_id_fkey;
ALTER TABLE kb.document_parsing_jobs
    VALIDATE CONSTRAINT document_parsing_jobs_organization_id_fkey;
ALTER TABLE kb.invites
    VALIDATE CONSTRAINT invites_organization_id_fkey;
ALTER TABLE kb.skill_reviews
    VALIDATE CONSTRAINT skill_reviews_org_id_fkey;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.skill_reviews DROP CONSTRAINT IF EXISTS skill_reviews_org_id_fkey;
ALTER TABLE kb.invites DROP CONSTRAINT IF EXISTS invites_organization_id_fkey;
ALTER TABLE kb.document_parsing_jobs DROP CONSTRAINT IF EXISTS document_parsing_jobs_organization_id_fkey;
ALTER TABLE kb.discovery_jobs DROP CONSTRAINT IF EXISTS discovery_jobs_organization_id_fkey;
-- +goose StatementEnd
