-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.project_memberships
ADD CONSTRAINT project_memberships_project_id_user_id_key UNIQUE (project_id, user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.project_memberships
DROP CONSTRAINT IF EXISTS project_memberships_project_id_user_id_key;
-- +goose StatementEnd
