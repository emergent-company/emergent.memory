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
SET name = CASE vendor
        WHEN 'google' THEN 'Google'
        WHEN 'github' THEN 'GitHub'
        ELSE initcap(vendor)
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

-- Rows in one scope sharing a vendor and the exact same encrypted client are the
-- same app configured twice (the Drive and Gmail rows of one Google client).
-- Merge their per-provider config into the earliest row, then drop the rest.
WITH groups AS (
    SELECT organization_id,
           project_id,
           vendor,
           encode(encrypted_client, 'hex') AS client_fp,
           min(id::text)::uuid AS keep_id
    FROM kb.oauth_client_configs
    GROUP BY 1, 2, 3, 4
    HAVING count(*) > 1
),
merged AS (
    SELECT g.keep_id, jsonb_object_agg(e.key, e.value) AS pc
    FROM groups g
    JOIN kb.oauth_client_configs c
      ON c.organization_id = g.organization_id
     AND c.project_id IS NOT DISTINCT FROM g.project_id
     AND c.vendor = g.vendor
     AND encode(c.encrypted_client, 'hex') = g.client_fp
    CROSS JOIN LATERAL jsonb_each(c.provider_configs) e
    GROUP BY g.keep_id
)
UPDATE kb.oauth_client_configs k
SET provider_configs = m.pc,
    updated_at = now()
FROM merged m
WHERE k.id = m.keep_id;

DELETE FROM kb.oauth_client_configs d
USING (
    SELECT organization_id,
           project_id,
           vendor,
           encode(encrypted_client, 'hex') AS client_fp,
           min(id::text)::uuid AS keep_id
    FROM kb.oauth_client_configs
    GROUP BY 1, 2, 3, 4
    HAVING count(*) > 1
) g
WHERE d.organization_id = g.organization_id
  AND d.project_id IS NOT DISTINCT FROM g.project_id
  AND d.vendor = g.vendor
  AND encode(d.encrypted_client, 'hex') = g.client_fp
  AND d.id <> g.keep_id;

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

DROP INDEX IF EXISTS idx_oauth_client_configs_org_provider;
DROP INDEX IF EXISTS idx_oauth_client_configs_project_provider;

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
-- Lossy: only the earliest app per (scope, vendor) can be represented after the
-- vendor model is removed, and per-provider scope configuration collapses to the
-- legacy scopes/strategy columns.
ALTER TABLE kb.sources DROP COLUMN IF EXISTS oauth_app_id;

WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY organization_id, project_id, vendor
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
WHERE provider = '' OR provider NOT IN (SELECT key FROM jsonb_each(provider_configs));

DROP INDEX IF EXISTS idx_oauth_client_configs_org_vendor_name;
DROP INDEX IF EXISTS idx_oauth_client_configs_project_vendor_name;

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
