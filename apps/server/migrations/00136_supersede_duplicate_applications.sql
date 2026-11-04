-- +goose Up
-- One-time cleanup: for each (project, blueprint name), keep only the most
-- recently applied application row as 'applied' and mark the rest 'superseded'.
-- The supersede-on-apply logic (see blueprint_applications) prevents future
-- duplicates, but rows created before that fix still show multiple versions of
-- the same blueprint as applied.
UPDATE kb.blueprint_applications AS bpa
SET status = 'superseded', updated_at = now()
FROM kb.blueprints AS bp
WHERE bpa.blueprint_id = bp.id
  AND bpa.status = 'applied'
  AND bpa.applied_at < (
      SELECT MAX(bpa2.applied_at)
      FROM kb.blueprint_applications bpa2
      JOIN kb.blueprints bp2 ON bp2.id = bpa2.blueprint_id
      WHERE bpa2.project_id = bpa.project_id
        AND bp2.name = bp.name
        AND bpa2.status = 'applied'
  );

-- +goose Down
-- No-op: superseded rows are retained (only their applied-list visibility is
-- lost), and this cleanup cannot be un-done without knowing the prior state.
