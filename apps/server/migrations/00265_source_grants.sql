-- +goose Up
-- Credential → source grants (slice 2 of #2376, issue #152).
--
-- Enforcing write ownership needs an answer to "may this caller act as source
-- S?", which the data model does not have: POST /api/graph/changesets names
-- source_id in the body and authenticates the caller only as a project member,
-- so any credential that can write the project can push as any source in it.
-- This table is that answer as project-scoped data — one row per (source,
-- credential) — so revoking authority is a DELETE and auditing it is a query,
-- not a config change.
--
-- credential_id NULL means the grant covers any credential of credential_kind.
-- The backfill below writes credential_kind = 'project', credential_id = NULL
-- for every existing source: a "project" grant is the wildcard "any
-- project-scoped credential (api_key / mcp_link / session) may push as this
-- source", so integrations that predate grants keep working the day this
-- deploys. Sources created after this change get no wildcard and must be
-- granted explicitly — that is what closes the impersonation window (the
-- Back-compat "option 1" of the credential-scoping proposal).

CREATE TABLE IF NOT EXISTS kb.source_grants (
    id              uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id      uuid NOT NULL,
    source_id       uuid NOT NULL REFERENCES kb.sources (id) ON DELETE CASCADE,
    credential_kind text NOT NULL,
    credential_id   uuid,
    granted_by_type text NOT NULL DEFAULT 'system',
    granted_by_id   uuid,
    granted_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT source_grants_credential_kind_check
        CHECK (credential_kind = ANY (ARRAY['api_key'::text, 'mcp_link'::text, 'session'::text, 'project'::text]))
);

-- One grant per (source, credential). credential_id is nullable, and NULLs are
-- distinct in a plain unique index, so the wildcard (NULL credential_id) is
-- deduped by its own partial index.
CREATE UNIQUE INDEX IF NOT EXISTS uq_source_grants_source_credential
    ON kb.source_grants (project_id, source_id, credential_kind, credential_id)
    WHERE credential_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_source_grants_source_credential_wildcard
    ON kb.source_grants (project_id, source_id, credential_kind)
    WHERE credential_id IS NULL;

-- Backfill: every source that exists at migration time may be pushed as by any
-- project credential. Existing integrations keep working without an explicit
-- grant; the impersonation window closes for sources created after this runs.
INSERT INTO kb.source_grants (project_id, source_id, credential_kind, credential_id, granted_by_type, granted_by_id)
SELECT project_id, id, 'project', NULL, 'system', NULL
FROM kb.sources
ON CONFLICT DO NOTHING;

-- Adoption (data_class = 'adopted') carries an owner, but the 00257 CHECK only
-- permits an owner on a 'source' row. Relax it to admit 'adopted'. The weaker
-- predicate is satisfied by every existing row, so ADD NOT VALID avoids a scan
-- under ACCESS EXCLUSIVE here; validation is split into 00266, matching the
-- 00257/00258 lock pattern on the busiest table in the schema.
ALTER TABLE kb.graph_objects DROP CONSTRAINT graph_objects_owner_source_requires_source_class;
ALTER TABLE kb.graph_objects
    ADD CONSTRAINT graph_objects_owner_source_requires_source_class
    CHECK (owner_source_id IS NULL OR data_class = ANY (ARRAY['source'::text, 'adopted'::text])) NOT VALID;

-- +goose Down
ALTER TABLE kb.graph_objects DROP CONSTRAINT graph_objects_owner_source_requires_source_class;
ALTER TABLE kb.graph_objects
    ADD CONSTRAINT graph_objects_owner_source_requires_source_class
    CHECK (owner_source_id IS NULL OR data_class = 'source');

DROP TABLE IF EXISTS kb.source_grants;
