# retrieval-performance-config Specification

## Purpose
Exposes retrieval/search performance knobs as environment configuration: `ivfflat.probes`, the RRF constant and fusion weights, chunk size/overlap (with a project's `chunking_config` taking precedence), embedding worker concurrency and adaptive scaling, pgx pool sizing, and unified-search result limits. Every knob keeps its documented default when unset, so retrieval behavior is unchanged unless an operator opts in.

## Requirements

### Requirement: Vector index probe count is configurable
The `ivfflat.probes` value used in search transactions SHALL be configurable via environment (e.g., `SEARCH_IVFFLAT_PROBES`), defaulting to the current value of 10. The configured value MUST be applied per-query transaction.

#### Scenario: Probe count driven by environment
- **WHEN** the server starts with `SEARCH_IVFFLAT_PROBES=40`
- **THEN** search queries MUST execute with `SET LOCAL ivfflat.probes = 40`

#### Scenario: Omitted setting preserves default
- **WHEN** `SEARCH_IVFFLAT_PROBES` is unset
- **THEN** search queries MUST use the existing default of 10

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
