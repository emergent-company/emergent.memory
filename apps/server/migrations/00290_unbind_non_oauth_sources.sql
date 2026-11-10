-- +goose Up
-- +goose StatementBegin
-- Detach credential-only data sources from vendor OAuth apps (issue #554).
--
-- Before the accompanying change, Service.Create persisted an implicit
-- oauth_app_id on every new source whose vendor had exactly one app, whatever
-- auth method the source used. A credential-only source (PAT / API key /
-- config-only) never reads oauth_app_id, but the stale reference made the app
-- undeletable and the refusal named a source that does not use OAuth at all.
--
-- No user-visible credential changes here: a source keeps its own stored
-- credential, and nothing reads oauth_app_id for these providers. Only the
-- invisible app reference is dropped.
--
-- The predicate is a positive list of provider types that do NOT advertise an
-- OAuth auth method in the provider catalog at this commit
-- (apps/server/domain/sources/provider.go, defaultProviders):
--
--     google_drive  oauth
--     gmail         oauth
--     github        oauth + credentials
--     trale         oauth
--     web           config only            <-- no OAuth
--
-- github_app, introduced by 00289, authenticates with a GitHub App installation
-- token and never resolves an OAuth client, so it is listed too. The list is
-- deliberately positive: an immutable migration over a hand-typed NOT IN list
-- of OAuth providers would clear a legitimate binding if a provider were missed,
-- while a provider added later is simply left untouched here.
UPDATE kb.sources
SET oauth_app_id = NULL
WHERE oauth_app_id IS NOT NULL
  AND provider IN ('web', 'github_app');
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
