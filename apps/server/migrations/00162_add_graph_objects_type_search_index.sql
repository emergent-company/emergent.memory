-- +goose NO TRANSACTION
-- +goose Up
-- Composite index for type-filtered, cursor-paginated object search.
-- GET /api/graph/objects/search?type=... filters by project_id + type and
-- orders by (created_at DESC, id DESC) for keyset cursor pagination. The only
-- type index was single-column (IDX_b8c7752534a444c2f16ebf3d91), forcing a
-- full scan of the type's rows plus a sort before LIMIT, and detoasting the
-- large `properties` jsonb for every candidate row. This index lets the
-- planner scan in order and stop at LIMIT, and the (created_at, id) < (?, ?)
-- cursor comparison is served directly as an index condition.
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- high-volume graph table. See https://github.com/emergent-company/emergent.memory/issues/663
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_type_created_id
  ON kb.graph_objects (project_id, type, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_objects_project_type_created_id;
