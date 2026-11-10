-- +goose Up
-- +goose StatementBegin
-- Detach credential-only data sources from vendor OAuth apps (issue #554).
--
-- Before the accompanying change, Service.Create persisted an implicit
-- oauth_app_id on every new source whose vendor had exactly one app, whatever
-- auth method the source used. A credentials-only source (PAT / API key) never
-- reads oauth_app_id, but the stale reference made the app undeletable and the
-- refusal named a source that does not use OAuth at all.
--
-- The predicate is data-driven, not provider-typed: an OAuth completion writes
-- grant metadata into the source's `config` jsonb, and a credentials create
-- writes none. The marker keys are the ones recorded by CompleteOAuth today
-- (apps/server/domain/sources/service_oauth.go):
--
--   oauth_granted_scopes    always written when the token response carries a
--                           scope (Drive, Gmail, GitHub and Trale all do)
--   oauth_granted_strategy  written when a provider reports a scope strategy
--                           (Drive's drive.file)
--   oauth_account_email     written when the connector captured the authorized
--                           account (Google)
--   refresh_token           included as a belt-and-braces marker for a
--                           provider that keeps it in config; Google strips it
--                           via CredentialKeys, so absence alone is not proof
--
-- None of these keys is in any provider's CredentialKeys(), so
-- stripCredentialKeys leaves them in the stored config. Rows created before
-- #541 (10539b261) cannot be implicit bindings — that path did not exist yet —
-- and any oauth_app_id they carry came from 00283's backfill of the old
-- provider-keyed resolution. An explicit binding on a credentials source is
-- meaningless, so clearing it is safe; a pre-#541 OAuth row still carries the
-- grant metadata above and is preserved.
--
-- No user-visible credential changes: a source keeps its own stored credential,
-- and nothing reads oauth_app_id for the detached rows.
UPDATE kb.sources
SET oauth_app_id = NULL
WHERE oauth_app_id IS NOT NULL
  AND NOT (config ? 'oauth_granted_scopes'
        OR config ? 'oauth_granted_strategy'
        OR config ? 'oauth_account_email'
        OR config ? 'refresh_token');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Lossy by design, like 00283's Down. The cleared values cannot be
-- reconstructed: they were whatever the old implicit resolution happened to
-- pick, not a user choice. Re-deriving them would reintroduce the bug, and no
-- user-visible credential depends on the column, so the Down leaves the rows
-- detached.
SELECT 1;
-- +goose StatementEnd
