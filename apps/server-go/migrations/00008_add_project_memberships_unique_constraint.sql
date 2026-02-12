-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'project_memberships_project_id_user_id_key'
    ) THEN
        ALTER TABLE kb.project_memberships
        ADD CONSTRAINT project_memberships_project_id_user_id_key UNIQUE (project_id, user_id);
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.project_memberships
DROP CONSTRAINT IF EXISTS project_memberships_project_id_user_id_key;
-- +goose StatementEnd
