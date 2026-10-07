-- +goose Up
-- Blueprint origin provenance: distinguishes platform built-ins, user-authored
-- blueprints, and GitHub imports. The column is NOT NULL DEFAULT 'user' so any
-- insert path that omits it still lands on a valid value; the CHECK constrains
-- the domain to the three known origins.
--
-- The CHECK is added inside a DO block (no `ADD CONSTRAINT IF NOT EXISTS` in
-- Postgres) so this migration is idempotent and safe to re-apply.
ALTER TABLE kb.blueprints ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'user';

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'blueprints_origin_check'
          AND conrelid = 'kb.blueprints'::regclass
    ) THEN
        ALTER TABLE kb.blueprints
            ADD CONSTRAINT blueprints_origin_check CHECK (origin IN ('builtin', 'user', 'import'));
    END IF;
END $$;
-- +goose StatementEnd

-- Backfill: global scope (project_id IS NULL) means platform built-in. These
-- rows are the shared built-in catalogue seeded at boot, not user-authored.
-- Note: any pre-existing global (non-builtin) row also becomes origin='builtin'
-- and is therefore treated as an immutable built-in by the service guards.
UPDATE kb.blueprints SET origin = 'builtin' WHERE project_id IS NULL;

CREATE INDEX IF NOT EXISTS blueprints_origin_idx ON kb.blueprints (origin);

-- +goose Down
DROP INDEX IF EXISTS kb.blueprints_origin_idx;
ALTER TABLE kb.blueprints DROP CONSTRAINT IF EXISTS blueprints_origin_check;
ALTER TABLE kb.blueprints DROP COLUMN IF EXISTS origin;
