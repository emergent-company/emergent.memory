-- +goose Up

-- The proactive grant probe (#23) revalidates a long-interval source's stored
-- credential without running a sync, so a dead grant is found within an hour
-- rather than at the source's next (possibly daily) sync. It needs to remember
-- when it last asked, or it would spend a provider call on every sweep.
--
-- Why a dedicated column instead of reusing `last_error_at` / `last_synced_at`:
-- a successful probe is neither a sync (so it must not move `last_synced_at`,
-- which is the scheduler's admission clock) nor a failure (so it must not move
-- `last_error_at`), and a transient probe failure must not look like a sync
-- failure in the UI. Nullable because "never probed" and "probed just now" are
-- different answers: only the second should suppress the next probe.
--
-- It is deliberately not part of `Repository.Update`: a settings save must not
-- reset the probe clock, and a probe must not touch the row's `updated_at`.
ALTER TABLE kb.sources
    ADD COLUMN IF NOT EXISTS last_probed_at timestamptz;

-- +goose Down

ALTER TABLE kb.sources DROP COLUMN IF EXISTS last_probed_at;
