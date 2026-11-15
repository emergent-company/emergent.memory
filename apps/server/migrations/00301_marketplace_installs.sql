-- +goose Up
-- Marketplace installs: accept 'market' as a blueprint origin and record
-- per-project install provenance. One row per (project, issuer, listing,
-- digest), so a repeat install of the same licensed digest is idempotent.
ALTER TABLE kb.blueprints DROP CONSTRAINT IF EXISTS blueprints_origin_check;
ALTER TABLE kb.blueprints
    ADD CONSTRAINT blueprints_origin_check
    CHECK (origin IN ('builtin', 'user', 'import', 'market'));

CREATE TABLE kb.market_installs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    blueprint_id UUID NOT NULL REFERENCES kb.blueprints(id) ON DELETE CASCADE,
    market_issuer TEXT NOT NULL,
    listing_slug TEXT NOT NULL,
    semver TEXT NOT NULL,
    digest TEXT NOT NULL,
    install_id TEXT NOT NULL DEFAULT '',
    license_jti TEXT NOT NULL DEFAULT '',
    installed_by UUID,
    installed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    state TEXT NOT NULL DEFAULT 'applied',
    CONSTRAINT market_installs_project_issuer_slug_digest_key
        UNIQUE (project_id, market_issuer, listing_slug, digest)
);

CREATE INDEX market_installs_project_idx ON kb.market_installs (project_id);
CREATE INDEX market_installs_blueprint_idx ON kb.market_installs (blueprint_id);

-- +goose Down
DROP INDEX IF EXISTS kb.market_installs_blueprint_idx;
DROP INDEX IF EXISTS kb.market_installs_project_idx;
DROP TABLE IF EXISTS kb.market_installs;

-- Restore the pre-market CHECK. Any row created with origin='market' cannot
-- satisfy the narrower constraint, so fold those rows back into the GitHub
-- import origin before re-adding it.
ALTER TABLE kb.blueprints DROP CONSTRAINT IF EXISTS blueprints_origin_check;
UPDATE kb.blueprints SET origin = 'import' WHERE origin = 'market';
ALTER TABLE kb.blueprints
    ADD CONSTRAINT blueprints_origin_check
    CHECK (origin IN ('builtin', 'user', 'import'));
