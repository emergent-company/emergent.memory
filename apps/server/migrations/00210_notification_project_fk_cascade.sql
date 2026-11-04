-- +goose Up
-- +goose StatementBegin

-- #1313: kb.notifications.project_id carried two foreign keys with conflicting
-- delete actions, both created by the baseline schema:
--   "FK_95464140d7dc04d7efb0afd6be0"  -> kb.projects(id) ON DELETE CASCADE
--   "FK_notifications_project_id"     -> kb.projects(id) ON DELETE SET NULL
-- With two FKs on one column the ON DELETE action is ambiguous. Keep CASCADE:
-- notifications are project-scoped activity and must not survive the project.
-- The SET NULL alternative would leave scope='project' rows with a NULL
-- project_id — rows no project-scoped read path (applyScopeFilter / GetCounts /
-- List, all keyed on project_id) can resolve to any inbox.
--
-- Drop both, then re-add exactly one CASCADE constraint so the constraint set is
-- deterministic regardless of the environment's starting state.
ALTER TABLE kb.notifications
    DROP CONSTRAINT IF EXISTS "FK_notifications_project_id";
ALTER TABLE kb.notifications
    DROP CONSTRAINT IF EXISTS "FK_95464140d7dc04d7efb0afd6be0";

ALTER TABLE kb.notifications
    ADD CONSTRAINT "FK_95464140d7dc04d7efb0afd6be0"
    FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;

-- Any project-scope notification with a NULL project_id is residue from the
-- SET NULL action (its project was deleted). There is no project to reattach it
-- to and it is unreachable from every project-scoped read path, so remove it.
-- Account-scope notifications legitimately carry a NULL project_id and are left
-- untouched.
DELETE FROM kb.notifications WHERE scope = 'project' AND project_id IS NULL;

-- Assert the constraint set can no longer regress silently: exactly one FK from
-- kb.notifications to kb.projects(id). (project_id is the only such column.)
DO $$
DECLARE fk_count integer;
BEGIN
    SELECT count(*) INTO fk_count
    FROM pg_constraint
    WHERE conrelid = 'kb.notifications'::regclass
      AND contype = 'f'
      AND confrelid = 'kb.projects'::regclass;
    IF fk_count <> 1 THEN
        RAISE EXCEPTION 'kb.notifications must have exactly one FK to kb.projects(id), found %', fk_count;
    END IF;
END $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore the pre-fix (ambiguous) constraint set. Orphan rows deleted in Up are
-- not recoverable; this migration is schema-reversible only.
ALTER TABLE kb.notifications
    DROP CONSTRAINT IF EXISTS "FK_95464140d7dc04d7efb0afd6be0";

ALTER TABLE kb.notifications
    ADD CONSTRAINT "FK_95464140d7dc04d7efb0afd6be0"
    FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;
ALTER TABLE kb.notifications
    ADD CONSTRAINT "FK_notifications_project_id"
    FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE SET NULL;

-- +goose StatementEnd
