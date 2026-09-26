-- +goose Up
-- Project custom pricing is per provider instance: two instances of one dialect
-- may have different negotiated rates. Add provider_slug and re-key uniqueness.
-- Organization custom pricing stays dialect-scoped (a project-scoped slug cannot
-- identify an org-level row).
ALTER TABLE kb.project_custom_pricing
  ADD COLUMN IF NOT EXISTS provider_slug varchar(63);

UPDATE kb.project_custom_pricing SET provider_slug = provider WHERE provider_slug IS NULL OR provider_slug = '';

ALTER TABLE kb.project_custom_pricing ALTER COLUMN provider_slug SET NOT NULL;

ALTER TABLE kb.project_custom_pricing
  DROP CONSTRAINT IF EXISTS uq_project_custom_pricing;

CREATE UNIQUE INDEX IF NOT EXISTS uq_project_custom_pricing_project_slug_model
  ON kb.project_custom_pricing (project_id, provider_slug, model);

-- +goose Down
DROP INDEX IF EXISTS uq_project_custom_pricing_project_slug_model;

ALTER TABLE kb.project_custom_pricing
  ADD CONSTRAINT uq_project_custom_pricing UNIQUE (project_id, provider, model);

ALTER TABLE kb.project_custom_pricing DROP COLUMN IF EXISTS provider_slug;
