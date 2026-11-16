-- +goose Up
-- +goose StatementBegin
-- Validate the kb.discovery_jobs.project_id foreign key added NOT VALID in
-- 00309 (issue #850).
--
-- Split from 00309 for the reason 00308 gives: ADD CONSTRAINT ... NOT VALID is
-- metadata-only, so it commits immediately; VALIDATE CONSTRAINT scans the table
-- under SHARE UPDATE EXCLUSIVE, which does not conflict with reads or writes.
-- Keeping both in one transaction would hold 00309's SHARE ROW EXCLUSIVE on
-- discovery_jobs for the whole scan.
--
-- This is the one step that can fail on a real environment, so the failure has
-- to be the finding. A plain VALIDATE reports only
-- `insert or update on table "discovery_jobs" violates foreign key constraint
-- "discovery_jobs_project_id_fkey"` — an "insert or update" that is misleading
-- for a validation scan, and no value. The DO block below therefore looks for
-- the offending rows first and raises with up to five project ids before
-- anything is validated. It costs one extra scan under the same SHARE UPDATE
-- EXCLUSIVE lock, which is the price of a diagnosable failure; on a clean
-- database it finds nothing and the VALIDATE below is the only long work.
-- Deleting or re-pointing those rows and re-running is the whole remedy — this
-- migration is a single transaction, so a failure leaves the constraint
-- unvalidated and the version unrecorded.
--
-- Leaving the constraint unvalidated instead (enforced only for new writes)
-- would hide the corruption, so failure is the intended outcome.

DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(DISTINCT s.ref::text, ', ') INTO bad
    FROM (
        SELECT j.project_id AS ref
        FROM kb.discovery_jobs j
        WHERE j.project_id IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM kb.projects p WHERE p.id = j.project_id)
        LIMIT 5
    ) s;

    IF bad IS NOT NULL THEN
        RAISE EXCEPTION '00310: kb.discovery_jobs rows reference a project that does not exist (project_id: %); delete or re-point them, then re-run', bad;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE kb.discovery_jobs
    VALIDATE CONSTRAINT discovery_jobs_project_id_fkey;

-- +goose Down
-- +goose StatementBegin
-- Validation is not reversible (and does not need to be): 00309 drops the
-- constraint itself, and a re-added constraint starts NOT VALID again, which is
-- the correct pre-00310 state.
SELECT 1;
-- +goose StatementEnd
