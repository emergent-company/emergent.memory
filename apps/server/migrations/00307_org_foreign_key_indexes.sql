-- +goose NO TRANSACTION
-- +goose Up
-- Org-leading indexes for the four foreign keys added in 00306 (issue #844).
--
-- A foreign key is enforced without an index on the referencing column, but a
-- DELETE or UPDATE of the referenced kb.orgs row then has to sequentially scan
-- every child table to find rows that cascade. 12 of the 14 org-scoped tables
-- already carry an org-leading index (kb.idx_restores_org, kb.idx_skills_org_id,
-- kb.idx_org_tool_settings_org_id, kb.idx_backups_org_project ...); only
-- kb.projects and kb.backup_schedules have none. These four close the gap for
-- discovery_jobs, document_parsing_jobs, invites and skill_reviews.
--
-- CONCURRENTLY because all four tables are created by earlier migrations and can
-- be populated in a running environment: a plain CREATE INDEX holds SHARE and
-- blocks writes for the whole build (the debt issue #844 records in section 2;
-- 00012/00161/00170 are the pattern for doing it right). CONCURRENTLY cannot run
-- inside a transaction, hence NO TRANSACTION — each statement commits on its
-- own.
--
-- A failed CONCURRENTLY build leaves an INVALID index behind, and
-- `IF NOT EXISTS` would then skip it forever. So each index is dropped first
-- (that also drops an invalid leftover) and re-created — which, like 00170, means
-- re-running the file rebuilds even a healthy index, at the cost of four
-- concurrent builds and a window with no index; that is the price of a retry that
-- cannot silently skip a broken build. The DO block at the end
-- re-derives the expected catalog state from pg_index and raises if any index is
-- missing, invalid or not ready, so goose refuses to record the version and a
-- retry rebuilds. Same shape as 00170; `go run ./cmd/migrate -c verify` runs the
-- equivalent read-only check at any time.
--
-- Index names stay unqualified: CREATE INDEX derives the schema from the
-- schema-qualified table reference and rejects a qualified index name.

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_discovery_jobs_organization_id;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_discovery_jobs_organization_id
    ON kb.discovery_jobs (organization_id);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_document_parsing_jobs_organization_id;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_document_parsing_jobs_organization_id
    ON kb.document_parsing_jobs (organization_id);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_invites_organization_id;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_invites_organization_id
    ON kb.invites (organization_id);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_skill_reviews_org_id;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_skill_reviews_org_id
    ON kb.skill_reviews (org_id);

-- +goose StatementBegin
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(e.name, ', ') INTO bad
    FROM (VALUES ('idx_discovery_jobs_organization_id'),
                 ('idx_document_parsing_jobs_organization_id'),
                 ('idx_invites_organization_id'),
                 ('idx_skill_reviews_org_id')) AS e(name)
    WHERE NOT EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = e.name
          AND i.indisvalid
          AND i.indisready
    );
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION '00307: org foreign-key index missing or invalid: %', bad;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_skill_reviews_org_id;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_invites_organization_id;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_document_parsing_jobs_organization_id;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_discovery_jobs_organization_id;
