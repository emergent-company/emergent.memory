-- +goose Up
-- +goose StatementBegin

-- #175: relationship types are lower_snake_case by contract
-- (normalizeRelationType in domain/graph). Legacy rows and the CreateSubgraph
-- path wrote the caller's casing verbatim, so a ?type= filter could not see
-- them and a normalised write of the same edge minted a twin. Lower every
-- stored type — HEAD rows and versions — so reads, writes and the partial
-- unique index all agree.
--
-- Lowering can collapse two HEAD rows onto one uniqueness key
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
-- Idempotent: on a second run there are no colliding HEADs and the final
-- UPDATE matches zero rows.

CREATE TEMP TABLE _rel_type_collisions ON COMMIT DROP AS
WITH ranked AS (
    SELECT r.id,
           r.canonical_id,
           r.version,
           row_number() OVER (
               PARTITION BY r.project_id, r.branch_id, lower(r.type), r.src_id, r.dst_id
               ORDER BY r.id
           ) AS rn,
           first_value(r.id) OVER (
               PARTITION BY r.project_id, r.branch_id, lower(r.type), r.src_id, r.dst_id
               ORDER BY r.id
           ) AS survivor_id,
           first_value(r.canonical_id) OVER (
               PARTITION BY r.project_id, r.branch_id, lower(r.type), r.src_id, r.dst_id
               ORDER BY r.id
           ) AS survivor_canonical_id,
           first_value(r.version) OVER (
               PARTITION BY r.project_id, r.branch_id, lower(r.type), r.src_id, r.dst_id
               ORDER BY r.id
           ) AS survivor_version
    FROM kb.graph_relationships r
    WHERE r.supersedes_id IS NULL
)
SELECT id AS loser_id,
       canonical_id AS loser_canonical_id,
       survivor_id,
       survivor_canonical_id,
       survivor_version,
       rn - 1 AS loser_rank   -- 1 for the first losing HEAD, so its chain keeps its offsets
FROM ranked
WHERE rn > 1;

-- Re-point each losing chain onto the survivor's canonical identity and shift
-- its version numbers past the survivor HEAD so history ordering stays stable.
UPDATE kb.graph_relationships r
SET canonical_id = c.survivor_canonical_id,
    version = r.version + c.survivor_version + ((c.loser_rank - 1) * 1000)
FROM _rel_type_collisions c
WHERE r.canonical_id = c.loser_canonical_id;

-- Demote the losing HEAD; the survivor is now the only HEAD for the key.
UPDATE kb.graph_relationships r
SET supersedes_id = c.survivor_id
FROM _rel_type_collisions c
WHERE r.id = c.loser_id;

DROP TABLE _rel_type_collisions;

UPDATE kb.graph_relationships
SET type = lower(type)
WHERE type <> lower(type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Irreversible data normalisation: the original casing is not recorded, and a
-- merged collision cannot be split back into two identities. Intentionally a
-- no-op.
-- +goose StatementEnd
