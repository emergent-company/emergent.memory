-- +goose Up
-- +goose StatementBegin

-- What the user consented to when the grant was approved (#1163).
--
-- 'project' — the grant may only act in its project_id. This is the original
--             consent model, and it is the value every grant minted before
--             account-wide consent existed gets, so no existing grant is
--             silently widened to the user's other projects.
-- 'account' — the app may switch the MCP session to any project the owning user
--             belongs to. Written only by the authorization-code exchange after
--             the consent screen that describes that behaviour.
ALTER TABLE core.oauth_grants
    ADD COLUMN IF NOT EXISTS project_scope TEXT NOT NULL DEFAULT 'project'
        CHECK (project_scope IN ('project', 'account'));

COMMENT ON COLUMN core.oauth_grants.project_scope IS
    'Consent model recorded at authorize time: project (bound to project_id) or account (may switch to any project the owning user belongs to)';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE core.oauth_grants DROP COLUMN IF EXISTS project_scope;

-- +goose StatementEnd
