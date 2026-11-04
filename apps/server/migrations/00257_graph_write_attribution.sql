-- +goose NO TRANSACTION
-- +goose Up
-- Write attribution on graph rows (slice 1 of #2376).
--
-- Until now a version row recorded only (actor_type, actor_id). That answers
-- "a user / an agent definition / the system" and nothing else: the credential
-- that authenticated the write was resolved at the HTTP boundary
-- (AuthUser.APITokenID) and dropped before the service layer, and an agent write
-- recorded the agent *definition*, never the run.
--
-- This migration adds the rest of the write stamp as columns. It is deliberately
-- NOT a `properties` entry: computeContentHash hashes properties/status/key/
-- labels, so a provenance property would flip the content hash on a
-- metadata-only write and defeat carryEmbeddingIfUnchanged (#1478).
--
-- credential_kind is NOT NULL with an explicit 'none' so an unattributed write
-- is a diagnosable state rather than an absent value. run_id / root_run_id /
-- credential_id stay nullable because they are genuinely absent for background
-- writes.
--
-- data_class defaults to 'synthesized': the three-valued enum is relative to
-- external sources (source | synthesized | adopted), so everything Memory owns
-- today — user notes, agent writes, extraction output — is synthesized. A source
-- push sets 'source' together with owner_source_id (slice 2/3).
--
-- input_hash records the content hash of the row a new version supersedes (NULL
-- when there was none), so an edit made against an older version is detectable
-- by comparing what the writer reported against the superseded row's hash.
--
-- Lock profile: kb.graph_objects is the busiest table in the schema. NO
-- TRANSACTION + NOT VALID constraints means each statement commits on its own
-- and no full scan is held under an ACCESS EXCLUSIVE lock; the indexes are built
-- CONCURRENTLY, matching 00199 / 00202 / 00249. Constraints are validated in
-- 00258. A failed CONCURRENTLY build leaves an INVALID index behind, so drop any
-- leftover first.

ALTER TABLE kb.graph_objects
    ADD COLUMN IF NOT EXISTS credential_kind text NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS credential_id   uuid,
    ADD COLUMN IF NOT EXISTS run_id          uuid,
    ADD COLUMN IF NOT EXISTS root_run_id     uuid,
    ADD COLUMN IF NOT EXISTS data_class      text NOT NULL DEFAULT 'synthesized',
    ADD COLUMN IF NOT EXISTS owner_source_id uuid,
    ADD COLUMN IF NOT EXISTS input_hash      text;

ALTER TABLE kb.graph_relationships
    ADD COLUMN IF NOT EXISTS credential_kind text NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS credential_id   uuid,
    ADD COLUMN IF NOT EXISTS run_id          uuid,
    ADD COLUMN IF NOT EXISTS root_run_id     uuid;

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.graph_objects
        ADD CONSTRAINT graph_objects_credential_kind_check
        CHECK (credential_kind = ANY (ARRAY['api_key'::text, 'mcp_link'::text, 'session'::text, 'none'::text])) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.graph_objects
        ADD CONSTRAINT graph_objects_data_class_check
        CHECK (data_class = ANY (ARRAY['source'::text, 'synthesized'::text, 'adopted'::text])) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.graph_objects
        ADD CONSTRAINT graph_objects_owner_source_requires_source_class
        CHECK (owner_source_id IS NULL OR data_class = 'source') NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- A credential id accompanies a token-based write and nothing else: a session
-- write has a kind but no token row to point at.
-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.graph_objects
        ADD CONSTRAINT graph_objects_credential_id_pairs_with_kind
        CHECK (credential_id IS NULL OR credential_kind = ANY (ARRAY['api_key'::text, 'mcp_link'::text])) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.graph_objects
        ADD CONSTRAINT graph_objects_owner_source_id_fkey
        FOREIGN KEY (owner_source_id) REFERENCES kb.sources (id) ON DELETE SET NULL NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.graph_relationships
        ADD CONSTRAINT graph_relationships_credential_kind_check
        CHECK (credential_kind = ANY (ARRAY['api_key'::text, 'mcp_link'::text, 'session'::text, 'none'::text])) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE kb.graph_relationships
        ADD CONSTRAINT graph_relationships_credential_id_pairs_with_kind
        CHECK (credential_id IS NULL OR credential_kind = ANY (ARRAY['api_key'::text, 'mcp_link'::text])) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- +goose StatementEnd

-- "What did this run write?" and "what did this key write?" are the recurring
-- support questions, so both get a partial index. Rows written before this
-- migration have no run or credential and fall outside the indexes.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_run;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_run
    ON kb.graph_objects (project_id, run_id)
    WHERE run_id IS NOT NULL;

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_credential;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_credential
    ON kb.graph_objects (project_id, credential_id)
    WHERE credential_id IS NOT NULL;

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_project_run;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_relationships_project_run
    ON kb.graph_relationships (project_id, run_id)
    WHERE run_id IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_project_run;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_credential;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_run;

ALTER TABLE kb.graph_relationships
    DROP CONSTRAINT IF EXISTS graph_relationships_credential_id_pairs_with_kind,
    DROP CONSTRAINT IF EXISTS graph_relationships_credential_kind_check,
    DROP COLUMN IF EXISTS root_run_id,
    DROP COLUMN IF EXISTS run_id,
    DROP COLUMN IF EXISTS credential_id,
    DROP COLUMN IF EXISTS credential_kind;

ALTER TABLE kb.graph_objects
    DROP CONSTRAINT IF EXISTS graph_objects_owner_source_id_fkey,
    DROP CONSTRAINT IF EXISTS graph_objects_credential_id_pairs_with_kind,
    DROP CONSTRAINT IF EXISTS graph_objects_owner_source_requires_source_class,
    DROP CONSTRAINT IF EXISTS graph_objects_data_class_check,
    DROP CONSTRAINT IF EXISTS graph_objects_credential_kind_check,
    DROP COLUMN IF EXISTS input_hash,
    DROP COLUMN IF EXISTS owner_source_id,
    DROP COLUMN IF EXISTS data_class,
    DROP COLUMN IF EXISTS root_run_id,
    DROP COLUMN IF EXISTS run_id,
    DROP COLUMN IF EXISTS credential_id,
    DROP COLUMN IF EXISTS credential_kind;
