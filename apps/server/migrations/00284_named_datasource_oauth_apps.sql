-- +goose Up
-- +goose StatementBegin
-- Named, vendor-level data-source OAuth apps (add-named-datasource-oauth-apps).
--
-- An app is identified by (scope, vendor, name) and backs every provider of that
-- vendor: one Google app serves google_drive and gmail. A scope may hold several
-- apps per vendor. The legacy provider/scopes/strategy columns are retained for
-- the per-provider shim and for rows created before this migration.
ALTER TABLE kb.oauth_client_configs
    ADD COLUMN IF NOT EXISTS vendor text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS provider_configs jsonb NOT NULL DEFAULT '{}'::jsonb;

UPDATE kb.oauth_client_configs
SET vendor = CASE provider
        WHEN 'google_drive' THEN 'google'
        WHEN 'gmail' THEN 'google'
        ELSE provider
    END
WHERE vendor = '';

UPDATE kb.oauth_client_configs
SET name = CASE provider
        WHEN 'google_drive' THEN 'Google (Drive)'
        WHEN 'gmail' THEN 'Google (Gmail)'
        WHEN 'github' THEN 'GitHub'
        ELSE initcap(vendor) || ' (' || provider || ')'
    END
WHERE name = '';

UPDATE kb.oauth_client_configs
SET provider_configs = jsonb_build_object(
        provider,
        jsonb_strip_nulls(jsonb_build_object(
            'scopes', CASE WHEN scopes IS NULL OR scopes = '[]'::jsonb THEN NULL ELSE scopes END,
            'strategy', CASE WHEN strategy = '' THEN NULL ELSE strategy END
        ))
    )
WHERE provider_configs = '{}'::jsonb;

-- Legacy rows are deliberately NOT merged here. The encrypted client blobs use
-- a fresh AES-GCM nonce per write, so two rows holding the same client id/secret
-- have different ciphertext and SQL cannot tell them apart. Merging on vendor
-- alone would silently drop one provider's client. Each legacy row survives as
-- its own named app; an operator can merge them after reviewing the client ids.
-- Distinct clients that would otherwise share a name get a stable suffix.
WITH dup AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY organization_id, project_id, vendor, lower(name)
               ORDER BY created_at, id
           ) AS rn
    FROM kb.oauth_client_configs
)
UPDATE kb.oauth_client_configs c
SET name = c.name || ' ' || dup.rn
FROM dup
WHERE c.id = dup.id
  AND dup.rn > 1;

DROP INDEX IF EXISTS kb.idx_oauth_client_configs_org_provider;
DROP INDEX IF EXISTS kb.idx_oauth_client_configs_project_provider;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_org_vendor_name
    ON kb.oauth_client_configs (organization_id, vendor, lower(name))
    WHERE project_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_project_vendor_name
    ON kb.oauth_client_configs (project_id, vendor, lower(name))
    WHERE project_id IS NOT NULL;

-- A source names the app it uses. NULL means "resolve from project/org or fall
-- back to the deployment default" — the pre-migration behavior.
ALTER TABLE kb.sources
    ADD COLUMN IF NOT EXISTS oauth_app_id uuid REFERENCES kb.oauth_client_configs(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_sources_oauth_app_id
    ON kb.sources (oauth_app_id)
    WHERE oauth_app_id IS NOT NULL;

-- Preserve current behavior: bind each source to the app its old resolution
-- would have chosen, project first and then organization. Sources that used the
-- deployment default keep NULL.
UPDATE kb.sources s
SET oauth_app_id = (
    SELECT c.id
    FROM kb.oauth_client_configs c
    WHERE c.project_id = s.project_id
      AND c.provider = s.provider
    ORDER BY c.created_at, c.id
    LIMIT 1
)
WHERE s.oauth_app_id IS NULL
  AND EXISTS (
    SELECT 1 FROM kb.oauth_client_configs c
    WHERE c.project_id = s.project_id AND c.provider = s.provider
  );

UPDATE kb.sources s
SET oauth_app_id = (
    SELECT c.id
    FROM kb.oauth_client_configs c
    JOIN kb.projects p ON p.id = s.project_id
    WHERE c.project_id IS NULL
      AND c.organization_id = p.organization_id
      AND c.provider = s.provider
    ORDER BY c.created_at, c.id
    LIMIT 1
)
WHERE s.oauth_app_id IS NULL
  AND EXISTS (
    SELECT 1
    FROM kb.oauth_client_configs c
    JOIN kb.projects p ON p.id = s.project_id
    WHERE c.project_id IS NULL
      AND c.organization_id = p.organization_id
      AND c.provider = s.provider
  );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Lossy for the new multi-app model: only the earliest app per
-- (scope, provider) can be represented after the vendor model is removed.
-- Legacy Drive and Gmail rows survive because their provider values differ.
-- Per-provider scope configuration collapses to the legacy columns.
DROP INDEX IF EXISTS kb.idx_sources_oauth_app_id;
ALTER TABLE kb.sources DROP COLUMN IF EXISTS oauth_app_id;

WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY organization_id, project_id, provider
               ORDER BY created_at, id
           ) AS rn
    FROM kb.oauth_client_configs
)
DELETE FROM kb.oauth_client_configs c
USING ranked r
WHERE c.id = r.id
  AND r.rn > 1;

UPDATE kb.oauth_client_configs
SET provider = COALESCE(
        (SELECT key FROM jsonb_each(provider_configs) LIMIT 1),
        vendor
    )
WHERE provider = vendor AND provider_configs <> '{}'::jsonb;

DROP INDEX IF EXISTS kb.idx_oauth_client_configs_org_vendor_name;
DROP INDEX IF EXISTS kb.idx_oauth_client_configs_project_vendor_name;

ALTER TABLE kb.oauth_client_configs DROP COLUMN IF EXISTS provider_configs;
ALTER TABLE kb.oauth_client_configs DROP COLUMN IF EXISTS name;
ALTER TABLE kb.oauth_client_configs DROP COLUMN IF EXISTS vendor;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_org_provider
    ON kb.oauth_client_configs (organization_id, provider)
    WHERE project_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_project_provider
    ON kb.oauth_client_configs (project_id, provider)
    WHERE project_id IS NOT NULL;
-- +goose StatementEnd
