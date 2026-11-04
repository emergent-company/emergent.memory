-- +goose Up
-- Per-project flag: when true, a source sync that creates documents also starts
-- a schema-discovery job. It defaults to true so existing projects keep the
-- behaviour they have had since the sync worker first delegated discovery.
-- Turning it off leaves discovery available only through the explicit start /
-- retry endpoints (Schema → Schema discovery and the document page).
ALTER TABLE kb.projects ADD COLUMN auto_discovery_enabled boolean NOT NULL DEFAULT true;

-- One run per project at a time, enforced by the database. The service also
-- pre-checks for a friendlier error, but a pre-check cannot stop two concurrent
-- starts (an HTTP start racing a sync delegation, a double-submitted form): a
-- second job would double the model spend and race the first to finalise a pack
-- over the same documents.
--
-- Any duplicate in-flight rows from before this constraint are cancelled first
-- (the newest run keeps its claim) so the index can be built on a live database
-- rather than failing the migration.
UPDATE kb.discovery_jobs
   SET status = 'cancelled',
       error_message = 'cancelled when one-run-per-project was enforced: a duplicate in-flight job was superseded',
       completed_at = now(),
       updated_at = now()
 WHERE status NOT IN ('completed', 'failed', 'cancelled')
   AND id NOT IN (
       SELECT DISTINCT ON (project_id) id
         FROM kb.discovery_jobs
        WHERE status NOT IN ('completed', 'failed', 'cancelled')
        ORDER BY project_id, created_at DESC
   );

CREATE UNIQUE INDEX IF NOT EXISTS uq_discovery_jobs_one_active
    ON kb.discovery_jobs (project_id)
    WHERE status NOT IN ('completed', 'failed', 'cancelled');

-- +goose Down
DROP INDEX IF EXISTS kb.uq_discovery_jobs_one_active;
ALTER TABLE kb.projects DROP COLUMN auto_discovery_enabled;
