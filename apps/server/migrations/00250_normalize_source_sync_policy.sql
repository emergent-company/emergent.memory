-- +goose Up

-- Align the stored sync policy with the cadence a source has actually been
-- running at.
--
-- Before sync policy gated the scheduler sweep, a source was admitted whenever
-- it carried a sync_interval_minutes — the policy column was display-only. The
-- Gmail provider declares a 60-minute default cadence (gmailDefaultIntervalMin),
-- so every Gmail source created through OAuth was admitted hourly while its row
-- said sync_policy = 'manual'.
--
-- The sweep now admits only sync_policy = 'scheduled' sources, so without this
-- backfill those Gmail sources would stop syncing silently on upgrade. Only
-- Gmail is rewritten: it is the one shipped provider whose design calls for
-- polling, and prod/dev carry no Gmail sources, so the blast radius is
-- self-hosted connections made since the provider shipped.
--
-- Web and Google Drive sources are left alone on purpose: a manual web source
-- that was syncing hourly is the bug this release fixes, not behaviour to
-- preserve. Their stored interval becomes inert (and clears on the next settings
-- save), which is why it is not rewritten here.
--
-- A Gmail source whose owner genuinely chose Manual is indistinguishable from
-- one that took the form default, because Manual was the form default for every
-- provider. Owners can switch it back to Manual, which now sticks.
-- +goose StatementBegin
UPDATE kb.sources
SET sync_policy = 'scheduled',
    updated_at = now()
WHERE provider = 'gmail'
  AND sync_policy = 'manual'
  AND sync_interval_minutes IS NOT NULL;
-- +goose StatementEnd

-- The public API documented syncMode 'recurring' for interval-driven syncs, and
-- nothing validated the value, so rows can carry an alias rather than the
-- canonical policy. The sweep admits only 'scheduled', so leave an alias alone
-- and those sources stop syncing. A row with an interval was syncing hourly
-- before this release: keep it running as scheduled. A row without one was
-- doing nothing: make that explicit as manual.
-- +goose StatementBegin
UPDATE kb.sources
SET sync_policy = 'scheduled',
    updated_at = now()
WHERE sync_policy NOT IN ('manual', 'scheduled')
  AND sync_interval_minutes IS NOT NULL;

UPDATE kb.sources
SET sync_policy = 'manual',
    updated_at = now()
WHERE sync_policy NOT IN ('manual', 'scheduled');
-- +goose StatementEnd

-- +goose Down

-- No-op: reverting would re-create the original mismatch (a manual row on a
-- polling cadence), which is worse than leaving the aligned policy in place.
SELECT 1;
