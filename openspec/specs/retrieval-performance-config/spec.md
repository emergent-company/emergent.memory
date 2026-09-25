# retrieval-performance-config Specification

## Purpose
Exposes retrieval/search performance knobs as environment configuration: `ivfflat.probes`, the RRF constant and fusion weights, chunk size/overlap (with a project's `chunking_config` taking precedence), embedding worker concurrency and adaptive scaling, pgx pool sizing, and unified-search result limits. Every knob keeps its documented default when unset, so retrieval behavior is unchanged unless an operator opts in.

## Requirements

### Requirement: RRF constant and fusion weights are configurable
The reciprocal-rank-fusion constant (`k`, default 60) and the weighted-fusion weights (graph/text/relationship, default 0.25/0.75/0) SHALL be configurable via environment.

#### Scenario: Fusion weights overridden
- **WHEN** the server starts with custom fusion weight env values
- **THEN** unified search MUST apply the configured weights instead of defaults

#### Scenario: Omitted settings preserve defaults
- **WHEN** no fusion override is set
- **THEN** unified search MUST use the documented defaults (0.25/0.75/0, k=60)

### Requirement: Chunk size and overlap are configurable
Chunk size and overlap for document chunking SHALL be configurable. The current hard-coded defaults (1000/200) MUST become env-overridable via `CHUNK_SIZE`/`CHUNK_OVERLAP`, and the existing project-level `kb.projects.chunking_config` JSONB column MUST be honored when present (keys `maxChunkSize` and `overlap`), with precedence project config > env > default.

#### Scenario: Project chunking config honored
- **WHEN** the project has a `chunking_config` with `maxChunkSize=4000` and `overlap=800`
- **AND** a document in that project is re-chunked
- **THEN** the produced chunks MUST use `chunk_size=4000` and `chunk_overlap=800`

#### Scenario: Env default used when no project config
- **WHEN** the project has no `chunking_config`
- **AND** `CHUNK_SIZE`/`CHUNK_OVERLAP` env are set
- **THEN** the env values MUST be used as the default

#### Scenario: Omitted settings preserve current defaults
- **WHEN** no project config and no env override exist
- **THEN** chunking MUST use 1000/200

### Requirement: Embedding worker concurrency and adaptive scaling are configurable
Embedding worker concurrency, batch size, and adaptive scaling SHALL be configurable via environment for both graph and chunk embedding queues. Adaptive scaling SHALL default to enabled with sensible bounds.

#### Scenario: Chunk queue concurrency overridden
- **WHEN** the server starts with a chunk-embedding concurrency override
- **THEN** the chunk embedding worker MUST use the configured concurrency instead of the default of 10

#### Scenario: Adaptive scaling enabled by default
- **WHEN** no explicit adaptive-scaling override is set
- **THEN** embedding workers MUST enable adaptive scaling (bounded by configured min/max)

### Requirement: pgx pool sizing is configurable
The pgx connection pool `MaxConns` and `MinConns` SHALL be configurable via environment (already present as `DB_MAX_OPEN_CONNS`/`DB_MAX_IDLE_CONNS`). Documentation and defaults SHALL be reviewed so multi-tenant deployments are not limited to a 25-connection ceiling without recourse.

#### Scenario: Pool size honored at startup
- **WHEN** the server starts with `DB_MAX_OPEN_CONNS=100`
- **THEN** the pgx pool MUST be configured with `MaxConns=100`

### Requirement: Search result limits are configurable
The unified search default and maximum result limits SHALL be configurable via environment, with the default (32) and maximum (100) preserved unless overridden.

#### Scenario: Higher max limit requested
- **WHEN** `MEMORY_SEARCH_DEFAULT_LIMIT` and a max-limit env override are set above the current ceiling
- **THEN** search MUST honor the raised maximum up to the configured bound

### Requirement: Relationship namespace predicate stays on the embedding table
Relationship vector search SHALL apply the namespace predicate on `kb.graph_relationships.namespace`, denormalised from the source object, rather than on the joined `kb.graph_objects` row. `namespace` SHALL be populated on every relationship insert and inherited across relationship versions (tombstone, restore, patch, fast-forward, similarity-merge, branch copy) so it mirrors the source object's namespace. The predicate MUST remain absent when namespace is unset (`namespace: "all"` or no namespace requested), preserving the unfiltered path.

#### Scenario: Namespace-scoped search filters on the relationship row
- **WHEN** a relationship vector search runs with an explicit namespace
- **THEN** the query MUST filter on `kb.graph_relationships.namespace = ?` and MUST NOT filter on the joined `kb.graph_objects.namespace`

#### Scenario: Unset namespace leaves the query unfiltered
- **WHEN** a relationship vector search runs with no namespace (unset or `"all"`)
- **THEN** the query MUST contain no namespace predicate

#### Scenario: Namespace survives relationship versioning
- **WHEN** a version of an existing relationship is created (patch, restore, tombstone, merge, or branch copy)
- **THEN** the new row MUST carry the previous HEAD's namespace

#### Scenario: Namespace is backfilled for existing relationships
- **WHEN** migration `00172` is applied to a database with existing relationships
- **THEN** every relationship with a resolvable source object MUST be backfilled with that object's namespace

### Requirement: Relationship namespace denormalisation is reversible
The namespace denormalisation migration SHALL be reversible: its Down migration MUST drop the index and the `namespace` column without affecting other relationship data.

#### Scenario: Rolling back the migration
- **WHEN** migration `00172` is rolled back
- **THEN** `kb.graph_relationships.namespace` and `idx_graph_relationships_namespace` MUST no longer exist

### Requirement: Embedding ANN indexes use HNSW
The embedding ANN indexes on `kb.graph_objects.embedding_v2` (migration `00164`), `kb.chunks.embedding` and `kb.skills.description_embedding` (migration `00170`), and `kb.graph_relationships.embedding` (migration `00171`) SHALL be pgvector HNSW indexes using `vector_cosine_ops` with `m = 16` and `ef_construction = 64`. HNSW indexes require neither probe tuning nor a training/list step. There SHALL be no periodic embedding-index `REINDEX` scheduler task: the former `embedding_index_reindex` task targeted only ivfflat indexes and was permanently empty once every embedding index was migrated to HNSW, so it has been removed along with its schedule/interval configuration.

#### Scenario: HNSW indexes present
- **WHEN** migrations `00164`, `00170` and `00171` have applied
- **THEN** `kb.graph_objects.embedding_v2`, `kb.chunks.embedding`, `kb.skills.description_embedding` and `kb.graph_relationships.embedding` are served by HNSW indexes and their ivfflat indexes have been dropped

#### Scenario: Reindex task skips migrated indexes
- **WHEN** the scheduler starts and its registered task list is inspected
- **THEN** it MUST NOT register an `embedding_index_reindex` task, MUST NOT read `EMBEDDING_REINDEX_SCHEDULE` or `EMBEDDING_REINDEX_INTERVAL`, and MUST NOT issue `REINDEX` against any HNSW or dropped embedding index
