-- +goose Up
-- +goose StatementBegin

-- Clear the spurious budget-alert notifications left in inboxes by the pre-#1347
-- logic (issue #1343). Before migration 00212 backfilled the threshold and #1347
-- added the positive-threshold guard, projects whose budget_alert_threshold had
-- been persisted as 0 alerted at $0 usage, so users still hold one false
-- "Project has used $0.00 of its $10.00 monthly budget (0%)." per project per
-- month. The code fix stops new alerts but does not remove the old rows, which
-- still surface as unread in the account inbox.
--
-- Rows are cleared, not deleted, so the audit trail survives. Clearing also
-- frees the (user_id, group_key) active-unique slot created by 00213, so a
-- genuine breach later in the same month can still produce an alert instead of
-- being swallowed as a duplicate by the stale false positive.
--
-- The predicate only clears alerts that are NOT actual breaches: it re-reads the
-- spend from the rendered message and compares it to the project's current
-- budget * threshold. A genuine breach alert (spend >= budget * threshold) is
-- left untouched. Rows whose message does not match the expected format, or
-- whose project/budget can no longer be resolved, are also left untouched.
UPDATE kb.notifications AS n
SET cleared_at = now(),
    read = true,
    read_at = COALESCE(n.read_at, now()),
    updated_at = now()
WHERE n.cleared_at IS NULL
  AND (n.category = 'budget' OR n.event_key = 'account.usage.budget_alert')
  AND n.project_id IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM kb.projects AS p
    WHERE p.id = n.project_id
      AND p.budget_usd IS NOT NULL
      AND p.budget_usd > 0
      AND p.budget_alert_threshold > 0
      AND substring(n.message FROM 'used \$([0-9]+\.[0-9]+) of') IS NOT NULL
      AND substring(n.message FROM 'used \$([0-9]+\.[0-9]+) of')::numeric
            < p.budget_usd * p.budget_alert_threshold
  );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: the cleared notifications were false positives and the pre-fix state
-- (unread false alerts) is not something to restore.
-- +goose StatementEnd
