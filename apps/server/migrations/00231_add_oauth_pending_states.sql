-- +goose Up
-- +goose StatementBegin
-- OAuth pending states for the provider-generic connect flow
-- (add-provider-oauth-connect §2). A pending state binds a short-lived OAuth
-- authorize request to its project/provider/initiating user and holds the PKCE
-- code verifier, redirect URI, and intended source config until the callback
-- consumes it exactly once. Rows are single-use (consumed_at) and short-lived
-- (expires_at), so the table is swept lazily.

CREATE TABLE IF NOT EXISTS kb.oauth_pending_states (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    state text NOT NULL,
    project_id uuid NOT NULL,
    provider text NOT NULL,
    user_id uuid,
    code_verifier text NOT NULL,
    redirect_uri text NOT NULL,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    scopes jsonb NOT NULL DEFAULT '[]'::jsonb,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_pending_states_state
    ON kb.oauth_pending_states (state);
CREATE INDEX IF NOT EXISTS idx_oauth_pending_states_expires_at
    ON kb.oauth_pending_states (expires_at);

ALTER TABLE kb.oauth_pending_states ENABLE ROW LEVEL SECURITY;

CREATE POLICY oauth_pending_states_select ON kb.oauth_pending_states FOR SELECT USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY oauth_pending_states_insert ON kb.oauth_pending_states FOR INSERT WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY oauth_pending_states_update ON kb.oauth_pending_states FOR UPDATE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY oauth_pending_states_delete ON kb.oauth_pending_states FOR DELETE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.oauth_pending_states;
-- +goose StatementEnd
