-- +goose NO TRANSACTION
-- +goose Up
-- Supports the Mailgun delivery-event job-mapping query (issues #1219, #1221):
--
--   SELECT id FROM kb.email_jobs WHERE btrim(mailgun_message_id, '<>') = ? LIMIT 1
--
-- The existing plain index on mailgun_message_id cannot serve a predicate on a
-- functional expression of the column, so this expression index mirrors the
-- query's btrim(mailgun_message_id, '<>') character-for-character. If the query
-- or the expression changes, this index must be updated in lockstep or the
-- planner will not use it.
--
-- Built CONCURRENTLY under NO TRANSACTION so the migration does not take an
-- access-exclusive lock on kb.email_jobs; a failed CONCURRENTLY build leaves an
-- INVALID index behind, so drop any leftover first (same rationale as 00200).

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_email_jobs_mailgun_message_id_btrim;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_email_jobs_mailgun_message_id_btrim
  ON kb.email_jobs (btrim(mailgun_message_id, '<>'));

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_email_jobs_mailgun_message_id_btrim;
