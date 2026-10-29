-- +goose Up
-- +goose StatementBegin

-- Project-scoped reusable data-source credentials. A saved credential is a
-- named, encrypted credential payload for one provider; sources reference it by
-- id instead of copying the secret. Existing sources keep their inline
-- encrypted_credentials blob (credential_id stays NULL), so the legacy path
-- remains fully functional.
CREATE TABLE IF NOT EXISTS kb.source_credentials (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id uuid NOT NULL,
    provider text NOT NULL,
    name text NOT NULL,
    encrypted_credentials bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT source_credentials_project_provider_name_key
        UNIQUE (project_id, provider, name)
);

CREATE INDEX IF NOT EXISTS idx_source_credentials_project_provider
    ON kb.source_credentials (project_id, provider);

ALTER TABLE kb.source_credentials ENABLE ROW LEVEL SECURITY;

CREATE POLICY source_credentials_select ON kb.source_credentials FOR SELECT USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_credentials_insert ON kb.source_credentials FOR INSERT WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_credentials_update ON kb.source_credentials FOR UPDATE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_credentials_delete ON kb.source_credentials FOR DELETE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

-- A source either references a saved credential or keeps its legacy inline blob.
-- RESTRICT keeps a referenced credential from being deleted out from under a
-- source; the service reports a conflict before reaching the constraint.
ALTER TABLE kb.sources
    ADD COLUMN IF NOT EXISTS credential_id uuid
    REFERENCES kb.source_credentials(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_sources_credential_id
    ON kb.sources (credential_id)
    WHERE credential_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_sources_credential_id;
ALTER TABLE kb.sources DROP COLUMN IF EXISTS credential_id;
DROP TABLE IF EXISTS kb.source_credentials;
-- +goose StatementEnd
