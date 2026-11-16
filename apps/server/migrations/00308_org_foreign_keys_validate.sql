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
-- Each VALIDATE scans its table once and marks the constraint valid without
-- rewriting anything. It deliberately fails on a pre-existing orphan row: an org
-- id with no org is the bug this closes, and leaving the constraint unvalidated
-- (enforced only for new writes) would hide it. The operator's remedy is to
-- decide what the row belongs to, or delete it, and re-run — this migration is a
-- single transaction, so a failure leaves every constraint unvalidated and the
-- version unrecorded, and a retry re-scans.
--
-- The indexes from 00307 predate this on purpose: they are what an org delete
-- uses to find the rows that cascade.

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
-- Validation is not reversible (and does not need to be): 00306 drops the
-- constraints themselves and a re-added constraint starts NOT VALID again, which
-- is the correct pre-00308 state.
SELECT 1;
-- +goose StatementEnd
