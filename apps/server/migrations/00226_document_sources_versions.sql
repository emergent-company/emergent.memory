-- +goose Up
-- +goose StatementBegin

-- Document sources, versioning, and attribution.
--
-- This is deliberately NOT a resurrection of the kb.data_source_integrations /
-- kb.data_source_sync_jobs design dropped in 00089. That design carried sync
-- state as columns directly on kb.documents (external_source_id,
-- data_source_integration_id, sync_version, integration_metadata); this change
-- removes none of those (they are already gone) and instead introduces:
--
--   * kb.sources        — a durable, project-scoped source (provider + config +
--                         encrypted credentials + sync policy/cursor/status).
--   * kb.source_sync_jobs — a sync-job lifecycle carrying the internal/jobs.Queue
--                         column contract plus durable per-item progress, with
--                         "at most one active job per source" enforced by a
--                         partial unique index.
--   * kb.documents      — a reconciled attribution schema (source_id +
--                         source_external_id + source_revision_id +
--                         source_text_hash + source_synced_at + source_status +
--                         source_deleted_at) and a supersedes-chained versioning
--                         model (canonical_id / supersedes_id / version).
--
-- kb.external_sources is the closer prior art: a project-scoped source row with
-- sync_policy / sync_interval_minutes / status / error_count / last_error /
-- last_synced_at. kb.sources generalizes it with a provider-agnostic `provider`
-- + `config` jsonb + encrypted credentials + a sync cursor, and
-- kb.source_sync_jobs is its job counterpart.

-- ---------------------------------------------------------------------------
-- kb.sources
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kb.sources (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id uuid NOT NULL,
    provider text NOT NULL,
    display_name text,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    encrypted_credentials bytea,
    sync_policy text NOT NULL DEFAULT 'manual',
    sync_interval_minutes integer,
    "cursor" jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'active',
    error_count integer NOT NULL DEFAULT 0,
    last_error text,
    last_error_at timestamptz,
    last_synced_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE kb.sources ENABLE ROW LEVEL SECURITY;

CREATE POLICY sources_select ON kb.sources FOR SELECT USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY sources_insert ON kb.sources FOR INSERT WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY sources_update ON kb.sources FOR UPDATE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY sources_delete ON kb.sources FOR DELETE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

-- ---------------------------------------------------------------------------
-- kb.source_sync_jobs
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kb.source_sync_jobs (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    status text NOT NULL DEFAULT 'pending',
    scheduled_at timestamptz NOT NULL DEFAULT now(),
    priority integer NOT NULL DEFAULT 0,
    started_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    attempt_count integer NOT NULL DEFAULT 0,
    last_error text,
    completed_at timestamptz,
    project_id uuid NOT NULL,
    source_id uuid NOT NULL REFERENCES kb.sources(id) ON DELETE CASCADE,
    trigger_type text NOT NULL DEFAULT 'manual',
    total_items integer NOT NULL DEFAULT 0,
    processed_items integer NOT NULL DEFAULT 0,
    successful_items integer NOT NULL DEFAULT 0,
    failed_items integer NOT NULL DEFAULT 0,
    skipped_items integer NOT NULL DEFAULT 0,
    current_phase text,
    status_message text,
    error jsonb,
    cursor_before jsonb,
    cursor_after jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- At most one active (pending/processing) job per source: the DB invariant that
-- makes concurrent periodic + manual triggers safe (design D6).
CREATE UNIQUE INDEX IF NOT EXISTS idx_source_sync_jobs_active_source
    ON kb.source_sync_jobs (source_id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS idx_source_sync_jobs_status_scheduled
    ON kb.source_sync_jobs (status, scheduled_at);

CREATE INDEX IF NOT EXISTS idx_source_sync_jobs_project_id
    ON kb.source_sync_jobs (project_id);

ALTER TABLE kb.source_sync_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY source_sync_jobs_select ON kb.source_sync_jobs FOR SELECT USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_sync_jobs_insert ON kb.source_sync_jobs FOR INSERT WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_sync_jobs_update ON kb.source_sync_jobs FOR UPDATE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_sync_jobs_delete ON kb.source_sync_jobs FOR DELETE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

-- ---------------------------------------------------------------------------
-- kb.documents: versioning + attribution
-- ---------------------------------------------------------------------------

-- canonical_id: stable identity shared by all versions. Added nullable first,
-- backfilled to id, then made NOT NULL with a default so pre-existing rows keep
-- their own id as their canonical identity and version 1.
ALTER TABLE kb.documents ADD COLUMN IF NOT EXISTS canonical_id uuid NULL;
UPDATE kb.documents SET canonical_id = id WHERE canonical_id IS NULL;
ALTER TABLE kb.documents ALTER COLUMN canonical_id SET DEFAULT public.uuid_generate_v4();
ALTER TABLE kb.documents ALTER COLUMN canonical_id SET NOT NULL;

ALTER TABLE kb.documents
    ADD COLUMN IF NOT EXISTS supersedes_id uuid NULL,
    ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS source_id uuid NULL,
    ADD COLUMN IF NOT EXISTS source_external_id text NULL,
    ADD COLUMN IF NOT EXISTS source_revision_id text NULL,
    ADD COLUMN IF NOT EXISTS source_text_hash text NULL,
    ADD COLUMN IF NOT EXISTS source_synced_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS source_status text NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS source_deleted_at timestamptz NULL;

-- supersedes_id self-FK: the prior HEAD's supersedes_id points FORWARD at the
-- version that supersedes it (HEAD is the row whose supersedes_id IS NULL).
-- Deleting a version row clears the pointer rather than cascading (history is
-- retained). The constraint is DEFERRABLE so a CreateVersion transaction can
-- retire the old head (point it at the not-yet-inserted new row) before
-- inserting the new head, without tripping the immediate FK check — the same
-- ordering the graph-object versioning uses (which has no FK here).
ALTER TABLE kb.documents
    DROP CONSTRAINT IF EXISTS documents_supersedes_id_fkey;
ALTER TABLE kb.documents
    ADD CONSTRAINT documents_supersedes_id_fkey
    FOREIGN KEY (supersedes_id) REFERENCES kb.documents(id) ON DELETE SET NULL
    DEFERRABLE INITIALLY DEFERRED;

-- source_id FK: deleting a source keeps its documents (source_id -> NULL).
ALTER TABLE kb.documents
    DROP CONSTRAINT IF EXISTS documents_source_id_fkey;
ALTER TABLE kb.documents
    ADD CONSTRAINT documents_source_id_fkey
    FOREIGN KEY (source_id) REFERENCES kb.sources(id) ON DELETE SET NULL;

-- One row per (canonical_id, version).
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_canonical_version
    ON kb.documents (canonical_id, version);

-- Exactly one HEAD (supersedes_id IS NULL) per (source_id, source_external_id);
-- many version rows may share the same external id.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_source_external_head
    ON kb.documents (source_id, source_external_id)
    WHERE source_external_id IS NOT NULL AND supersedes_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_project_source_status
    ON kb.documents (project_id, source_id, source_status);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- kb.documents attribution/versioning columns and their indexes/FKs.
DROP INDEX IF EXISTS idx_documents_project_source_status;
DROP INDEX IF EXISTS idx_documents_source_external_head;
DROP INDEX IF EXISTS idx_documents_canonical_version;

ALTER TABLE kb.documents
    DROP CONSTRAINT IF EXISTS documents_source_id_fkey;
ALTER TABLE kb.documents
    DROP CONSTRAINT IF EXISTS documents_supersedes_id_fkey;

ALTER TABLE kb.documents
    DROP COLUMN IF EXISTS source_deleted_at,
    DROP COLUMN IF EXISTS source_status,
    DROP COLUMN IF EXISTS source_synced_at,
    DROP COLUMN IF EXISTS source_text_hash,
    DROP COLUMN IF EXISTS source_revision_id,
    DROP COLUMN IF EXISTS source_external_id,
    DROP COLUMN IF EXISTS source_id,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS supersedes_id,
    DROP COLUMN IF EXISTS canonical_id;

-- kb.source_sync_jobs and kb.sources (policies drop with their tables).
DROP TABLE IF EXISTS kb.source_sync_jobs;
DROP TABLE IF EXISTS kb.sources;

-- +goose StatementEnd
