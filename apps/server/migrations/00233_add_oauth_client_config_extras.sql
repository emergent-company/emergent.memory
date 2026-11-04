-- +goose Up
-- +goose StatementBegin
-- Provider-specific, non-secret extras for an org OAuth client
-- (add-provider-oauth-connect §6.1). Google's drive.file strategy is paired
-- with the client-side Picker, which needs a Picker developer key and app id;
-- these are not secrets and travel in the browser, so they live in a plaintext
-- jsonb map rather than the encrypted client blob.
ALTER TABLE kb.oauth_client_configs
    ADD COLUMN IF NOT EXISTS config jsonb NOT NULL DEFAULT '{}'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.oauth_client_configs DROP COLUMN IF EXISTS config;
-- +goose StatementEnd
