-- +goose Up

-- The grant epoch names **which grant a source reads**, not when its row was last
-- edited. It exists so a sync job can refuse to park a source whose credential was
-- replaced while the job was in flight (#2425): the job failed against a grant that
-- the user has already repaired, so parking the source turns one reconnect into a
-- second failure and a second reconnect.
--
-- Why a dedicated column instead of `updated_at`: a successful run writes to the
-- source too — `UpdateCredentials` persists the rotated refresh token a rotating
-- authorization server (Trale) issues on nearly every use. If rotation counted as
-- "the credential changed", the guard would suppress the park for every rotating
-- provider and a dead grant would produce a doomed job on every sweep forever,
-- which is the loop #2406 exists to end. The epoch is therefore written only when
-- the *identity* of the grant changes: an OAuth consent (`ReplaceOAuthGrant`), a
-- reconnect of a shared saved credential (`ReplaceSavedCredentialGrant`, for every
-- source that reads it), or a settings save that attaches a credential
-- (`Update` — inline blob or a different `credential_id`).
--
-- NOT NULL with a default so no caller has to think about it, and so the backfill
-- is the migration itself. A parked source from before this change can still be
-- parked again; only a row whose epoch moved is protected.
ALTER TABLE kb.sources
    ADD COLUMN IF NOT EXISTS credentials_updated_at timestamptz NOT NULL DEFAULT now();

-- +goose Down

ALTER TABLE kb.sources DROP COLUMN IF EXISTS credentials_updated_at;
