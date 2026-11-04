-- +goose NO TRANSACTION
-- +goose Up
-- Idempotency for Mailgun delivery-event ingestion (issue #1215).
--
-- The public Mailgun webhook records every accepted event in kb.email_logs and
-- reconciles the job's delivery_status. Mailgun retries (and duplicate event
-- deliveries) must not double-apply an event, so this unique partial index on
-- mailgun_event_id is the dedupe key: the store inserts with
-- ON CONFLICT (mailgun_event_id) DO NOTHING and skips the job update when the
-- insert conflicts.
--
-- Partial on mailgun_event_id IS NOT NULL because the column is nullable for
-- any future non-Mailgun log writers.
--
-- Built CONCURRENTLY under NO TRANSACTION so adding it does not take an
-- access-exclusive lock on kb.email_logs; a failed CONCURRENTLY build leaves an
-- INVALID index behind, so drop any leftover first (same rationale as 00162).

DROP INDEX CONCURRENTLY IF EXISTS kb.ux_email_logs_mailgun_event_id;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ux_email_logs_mailgun_event_id
  ON kb.email_logs (mailgun_event_id)
  WHERE mailgun_event_id IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.ux_email_logs_mailgun_event_id;
