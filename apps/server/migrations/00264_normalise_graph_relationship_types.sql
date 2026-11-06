-- +goose Up
-- +goose StatementBegin

-- #175: relationship types are lower_snake_case by contract
-- (normalizeRelationType in domain/graph). Legacy rows and the CreateSubgraph
-- path wrote the caller's casing verbatim, so a ?type= filter could not see
-- them and a normalised write of the same edge minted a twin. Rewrite every
-- stored type — HEAD rows and versions — with the same transformation the
-- write path applies, so reads, writes and the partial unique index all agree.
--
-- The transformation mirrors normalizeRelationType exactly:
--   1. spaces become '_';
--   2. an '_' is inserted before an uppercase letter that follows a lowercase
--      letter (camelCase/PascalCase boundary);
--   3. the result is lowercased.
-- PostgreSQL's [[:lower:]]/[[:upper:]] classes are Unicode-aware in a UTF-8
-- database, matching Go's unicode.IsLower/IsUpper. Relationship types are
-- ASCII in practice; the one known legacy prod row (POC_WorksWith) normalises
-- to poc_works_with, not poc_workswith.
--
-- Normalisation can collapse two HEAD rows onto one uniqueness key
-- (project_id, branch_id, type, src_id, dst_id) WHERE supersedes_id IS NULL.
-- Resolve that deterministically *before* the bulk UPDATE, without deleting a
-- row:
--
--   * the lowest-id HEAD survives;
--   * every other HEAD is merged into the survivor's version chain: its whole
--     chain is re-pointed at the survivor's canonical_id, its versions are
--     shifted above the survivor's HEAD, and its HEAD is superseded by the
--     survivor. All versions stay readable through the survivor's history.
--
-- The partition includes branch_id (NULL = main) because the two unique
-- indexes are branch-scoped: uq_graph_relationships_head_main and
-- uq_graph_relationships_head_branch (00034). Rows forked onto different
-- branches are therefore never merged with each other.
--
-- The chain re-point is branch-scoped too. A branch fork copies the source
-- row's canonical_id verbatim (repository.go:3921), so keying the UPDATE on
-- canonical_id alone would also rewrite a fork of the LOSING chain that lives
-- on another branch, even when that branch has no collision. That silently
-- folds the branch's identity into main's chain (GetRelationshipHistory is
-- canonical-only, repository.go:2144), exactly the cross-branch merge this
-- migration must not perform. Both repair statements therefore match the
-- loser's branch_id as well.
--
-- Idempotent: on a second run there are no colliding HEADs and the final
-- UPDATE matches zero rows.

CREATE TEMP TABLE _rel_type_collisions ON COMMIT DROP AS
WITH normalised AS (
    SELECT r.id,
           r.project_id,
           r.branch_id,
           r.canonical_id,
           r.version,
           r.src_id,
           r.dst_id,
           lower(
               regexp_replace(
                   regexp_replace(r.type, ' ', '_', 'g'),
                   '([[:lower:]])([[:upper:]])',
                   '\1_\2',
                   'g'
               )
           ) AS norm_type
    FROM kb.graph_relationships r
    WHERE r.supersedes_id IS NULL
),
ranked AS (
    SELECT n.id,
           n.canonical_id,
           n.branch_id,
           n.version,
           row_number() OVER (
               PARTITION BY n.project_id, n.branch_id, n.norm_type, n.src_id, n.dst_id
               ORDER BY n.id
           ) AS rn,
           first_value(n.id) OVER (
               PARTITION BY n.project_id, n.branch_id, n.norm_type, n.src_id, n.dst_id
               ORDER BY n.id
           ) AS survivor_id,
           first_value(n.canonical_id) OVER (
               PARTITION BY n.project_id, n.branch_id, n.norm_type, n.src_id, n.dst_id
               ORDER BY n.id
           ) AS survivor_canonical_id,
           first_value(n.version) OVER (
               PARTITION BY n.project_id, n.branch_id, n.norm_type, n.src_id, n.dst_id
               ORDER BY n.id
           ) AS survivor_version
    FROM normalised n
)
SELECT id AS loser_id,
       canonical_id AS loser_canonical_id,
       branch_id AS loser_branch_id,
       survivor_id,
       survivor_canonical_id,
       survivor_version,
       rn - 1 AS loser_rank   -- 1 for the first losing HEAD, so its chain keeps its offsets
FROM ranked
WHERE rn > 1;

-- Re-point each losing chain onto the survivor's canonical identity and shift
-- its version numbers past the survivor HEAD so history ordering stays stable.
-- Restrict to the loser's branch so a fork of that chain on another branch
-- keeps its own canonical_id and version.
UPDATE kb.graph_relationships r
SET canonical_id = c.survivor_canonical_id,
    version = r.version + c.survivor_version + ((c.loser_rank - 1) * 1000)
FROM _rel_type_collisions c
WHERE r.canonical_id = c.loser_canonical_id
  AND r.branch_id IS NOT DISTINCT FROM c.loser_branch_id;

-- Demote the losing HEAD; the survivor is now the only HEAD for the key.
UPDATE kb.graph_relationships r
SET supersedes_id = c.survivor_id
FROM _rel_type_collisions c
WHERE r.id = c.loser_id
  AND r.branch_id IS NOT DISTINCT FROM c.loser_branch_id;

DROP TABLE _rel_type_collisions;

UPDATE kb.graph_relationships
SET type = lower(
        regexp_replace(
            regexp_replace(type, ' ', '_', 'g'),
            '([[:lower:]])([[:upper:]])',
            '\1_\2',
            'g'
        )
    )
WHERE type <> lower(
        regexp_replace(
            regexp_replace(type, ' ', '_', 'g'),
            '([[:lower:]])([[:upper:]])',
            '\1_\2',
            'g'
        )
    );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Irreversible data normalisation: the original casing is not recorded, and a
-- merged collision cannot be split back into two identities. Intentionally a
-- no-op.
-- +goose StatementEnd
