-- +goose Up
-- +goose StatementBegin
-- Validate the four org foreign keys added NOT VALID in 00306 (issue #844).
--
-- Split from 00306 for the reason 00258 gives: ADD CONSTRAINT ... NOT VALID is
-- metadata-only, so it commits immediately; VALIDATE CONSTRAINT scans the table
-- under SHARE UPDATE EXCLUSIVE, which does not conflict with reads or writes.
-- Keeping them in one transaction would hold 00306's SHARE ROW EXCLUSIVE on all
-- four tables for the whole scan.
--
-- This is the one step that can fail on a real environment, so the failure has
-- to be the finding. A plain VALIDATE reports only
-- `insert or update on table "invites" violates foreign key constraint ...` —
-- no table in operator terms, no value, and an "insert or update" that is
-- misleading for a validation scan. The DO block below therefore looks for the
-- offending rows first and raises with the table name and up to five org ids per
-- table before anything is validated. It costs one extra scan per table under
-- the same SHARE UPDATE EXCLUSIVE lock, which is the price of a diagnosable
-- failure; on a clean database it finds nothing and the VALIDATEs below are the
-- only long work. Deleting or re-pointing those rows and re-running is the whole
-- remedy — this migration is a single transaction, so a failure leaves all four
-- unvalidated and the version unrecorded.
--
-- Leaving the constraints unvalidated instead (enforced only for new writes)
-- would hide the corruption, so failure is the intended outcome.
--
-- The indexes from 00307 predate this on purpose: they are what an org delete
-- uses to find the rows that cascade.

DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(x.tbl || ' -> ' || x.ids, '; ' ORDER BY x.tbl) INTO bad
    FROM (
        SELECT 'kb.discovery_jobs' AS tbl, string_agg(DISTINCT s.ref::text, ', ') AS ids
        FROM (
            SELECT j.organization_id AS ref
            FROM kb.discovery_jobs j
            WHERE j.organization_id IS NOT NULL
              AND NOT EXISTS (SELECT 1 FROM kb.orgs o WHERE o.id = j.organization_id)
            LIMIT 5
        ) s
        UNION ALL
        SELECT 'kb.document_parsing_jobs', string_agg(DISTINCT s.ref::text, ', ')
        FROM (
            SELECT j.organization_id AS ref
            FROM kb.document_parsing_jobs j
            WHERE j.organization_id IS NOT NULL
              AND NOT EXISTS (SELECT 1 FROM kb.orgs o WHERE o.id = j.organization_id)
            LIMIT 5
        ) s
        UNION ALL
        SELECT 'kb.invites', string_agg(DISTINCT s.ref::text, ', ')
        FROM (
            SELECT i.organization_id AS ref
            FROM kb.invites i
            WHERE i.organization_id IS NOT NULL
              AND NOT EXISTS (SELECT 1 FROM kb.orgs o WHERE o.id = i.organization_id)
            LIMIT 5
        ) s
        UNION ALL
        SELECT 'kb.skill_reviews', string_agg(DISTINCT s.ref::text, ', ')
        FROM (
            SELECT r.org_id AS ref
            FROM kb.skill_reviews r
            WHERE r.org_id IS NOT NULL
              AND NOT EXISTS (SELECT 1 FROM kb.orgs o WHERE o.id = r.org_id)
            LIMIT 5
        ) s
    ) x
    WHERE x.ids IS NOT NULL;

    IF bad IS NOT NULL THEN
        RAISE EXCEPTION '00308: org-scoped rows reference an org that does not exist (%); delete or re-point them, then re-run', bad;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE kb.discovery_jobs
    VALIDATE CONSTRAINT discovery_jobs_organization_id_fkey;
ALTER TABLE kb.document_parsing_jobs
    VALIDATE CONSTRAINT document_parsing_jobs_organization_id_fkey;
ALTER TABLE kb.invites
    VALIDATE CONSTRAINT invites_organization_id_fkey;
ALTER TABLE kb.skill_reviews
    VALIDATE CONSTRAINT skill_reviews_org_id_fkey;

-- +goose Down
-- +goose StatementBegin
-- Validation is not reversible (and does not need to be): 00306 drops the
-- constraints themselves, and a re-added constraint starts NOT VALID again,
-- which is the correct pre-00308 state.
SELECT 1;
-- +goose StatementEnd
