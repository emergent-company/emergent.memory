# Triplet Embeddings for Relationship Search

**Status**: Implemented (Cognee Phase 2)
**Date**: February 2026

## Overview

Graph relationships are now embedded as natural language triplets (e.g., "Elon Musk founded Tesla") and searchable via vector similarity in the unified search API. This enables semantic search across relationships — not just graph objects and document chunks — improving search recall by 10-20% for relationship-heavy queries.

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                     Relationship Creation                        │
│                                                                  │
│  POST /api/graph/relationships                                   │
│       │                                                          │
│       ▼                                                          │
│  ┌─────────────────┐     ┌────────────────────┐                  │
│  │ Validate request │────▶│ Begin transaction   │                 │
│  └─────────────────┘     └────────┬───────────┘                  │
│                                   ▼                              │
│                    ┌──────────────────────────┐                   │
│                    │ Fetch source + target     │                  │
│                    │ graph objects             │                  │
│                    └──────────┬───────────────┘                   │
│                               ▼                                  │
│                    ┌──────────────────────────┐                   │
│                    │ generateTripletText()     │                  │
│                    │ "Elon Musk founded Tesla" │                  │
│                    └──────────┬───────────────┘                   │
│                               ▼                                  │
│                    ┌──────────────────────────┐                   │
│                    │ embedTripletText()        │                  │
│                    │ Vertex AI text-embedding  │                  │
│                    │ -004 (768 dimensions)     │                  │
│                    └──────────┬───────────────┘                   │
│                               ▼                                  │
│                    ┌──────────────────────────┐                   │
│                    │ INSERT relationship +     │                  │
│                    │ UPDATE embedding column   │                  │
│                    │ Commit transaction        │                  │
│                    └──────────────────────────┘                   │
│                                                                  │
│  NOTE: Embedding failure is NON-BLOCKING.                        │
│  The relationship is created even if embedding fails.            │
│  A warning is logged and embedding remains NULL.                 │
└──────────────────────────────────────────────────────────────────┘


┌──────────────────────────────────────────────────────────────────┐
│                      Unified Search                              │
│                                                                  │
│  POST /api/search/unified                                        │
│       │                                                          │
│       ▼                                                          │
│  ┌─────────────────────────────────────────────┐                 │
│  │         3-Way Parallel Search                │                │
│  │                                              │                │
│  │  ┌───────────┐ ┌───────────┐ ┌────────────┐ │                │
│  │  │  Graph     │ │   Text    │ │Relationship│ │                │
│  │  │  Objects   │ │  Chunks   │ │  Triplets  │ │                │
│  │  │ (vector +  │ │ (vector + │ │  (vector   │ │                │
│  │  │  lexical)  │ │  lexical) │ │   only)    │ │                │
│  │  └─────┬─────┘ └─────┬─────┘ └─────┬──────┘ │                │
│  │        │              │             │         │                │
│  │        └──────────────┴─────────────┘         │                │
│  │                       │                       │                │
│  │                       ▼                       │                │
│  │            ┌─────────────────────┐            │                │
│  │            │ RRF Fusion (k=60)   │            │                │
│  │            │ Merge & re-rank     │            │                │
│  │            └──────────┬──────────┘            │                │
│  │                       ▼                       │                │
│  │            ┌─────────────────────┐            │                │
│  │            │ Unified results     │            │                │
│  │            │ (mixed types)       │            │                │
│  │            └─────────────────────┘            │                │
│  └───────────────────────────────────────────────┘               │
│                                                                  │
│  Relationships filtered: WHERE embedding IS NOT NULL             │
│  Relationships skipped when resultTypes = "text"                 │
└──────────────────────────────────────────────────────────────────┘


┌──────────────────────────────────────────────────────────────────┐
│                     Chat RAG Context                             │
│                                                                  │
│  formatSearchContext() in domain/chat/handler.go                 │
│                                                                  │
│  Graph objects  → "- **Person**: Elon Musk — role=CEO"           │
│  Relationships  → "- Elon Musk founded Tesla"                    │
│  Text chunks    → "- SpaceX was founded in 2002..."              │
│                                                                  │
│  Relationship triplet text is injected directly into the         │
│  LLM prompt context alongside graph objects and text chunks.     │
└──────────────────────────────────────────────────────────────────┘
```

## Triplet Text Generation

### Format

```
"{source_display_name} {humanized_relation_type} {target_display_name}"
```

### Rules

1. **Display name resolution** (`getDisplayName`):

   - First: `object.properties["name"]` if non-empty string
   - Fallback: `object.key` if non-empty
   - Last resort: `object.id` (UUID string)

2. **Relation type humanization** (`humanizeRelationType`):

   - Replace underscores with spaces
   - Convert to lowercase
   - Examples: `WORKS_FOR` -> `"works for"`, `FOUNDED_BY` -> `"founded by"`

3. **Template**: Simple concatenation — `fmt.Sprintf("%s %s %s", source, relation, target)`

### Examples

| Source                  | Relation Type | Target     | Triplet Text                  |
| ----------------------- | ------------- | ---------- | ----------------------------- |
| Elon Musk               | `FOUNDED`     | Tesla      | "Elon Musk founded Tesla"     |
| Alice                   | `WORKS_FOR`   | Acme Corp  | "Alice works for Acme Corp"   |
| React                   | `DEPENDS_ON`  | JavaScript | "React depends on JavaScript" |
| (no name, key=`srv-01`) | `HOSTS`       | PostgreSQL | "srv-01 hosts PostgreSQL"     |

### Source Code

- `apps/server-go/domain/graph/service.go:546` — `humanizeRelationType()`
- `apps/server-go/domain/graph/service.go:552` — `getDisplayName()`
- `apps/server-go/domain/graph/service.go:567` — `generateTripletText()`
- `apps/server-go/domain/graph/service.go:576` — `embedTripletText()`

## Search Response Format

### New Result Type: `relationship`

The unified search response now includes a third result type alongside `graph` and `text`:

```json
{
  "results": [
    {
      "type": "graph",
      "id": "...",
      "object_type": "Person",
      "key": "Elon Musk",
      "score": 0.92,
      "fields": { "role": "CEO" }
    },
    {
      "type": "relationship",
      "id": "...",
      "score": 0.88,
      "relationship_type": "FOUNDED",
      "triplet_text": "Elon Musk founded Tesla",
      "source_id": "uuid-of-elon",
      "target_id": "uuid-of-tesla",
      "properties": {}
    },
    {
      "type": "text",
      "id": "...",
      "snippet": "Tesla was incorporated in 2003...",
      "score": 0.75
    }
  ],
  "metadata": {
    "totalResults": 3,
    "graphResultCount": 1,
    "textResultCount": 1,
    "relationshipResultCount": 1,
    "fusionStrategy": "rrf",
    "executionTime": {
      "graphSearchMs": 45,
      "textSearchMs": 32,
      "relationshipSearchMs": 28,
      "fusionMs": 2,
      "totalMs": 50
    }
  }
}
```

### Relationship-Specific Fields

| Field               | Type   | Description                                                |
| ------------------- | ------ | ---------------------------------------------------------- |
| `type`              | string | Always `"relationship"`                                    |
| `id`                | string | Relationship UUID                                          |
| `score`             | float  | Cosine similarity score (0-1)                              |
| `relationship_type` | string | Original relation type (e.g., `"FOUNDED"`)                 |
| `triplet_text`      | string | Human-readable triplet (e.g., `"Elon Musk founded Tesla"`) |
| `source_id`         | string | UUID of source graph object                                |
| `target_id`         | string | UUID of target graph object                                |
| `properties`        | object | Optional relationship properties                           |

### Metadata Changes

| Field                                | Description                                |
| ------------------------------------ | ------------------------------------------ |
| `relationshipResultCount`            | Number of relationship results in response |
| `executionTime.relationshipSearchMs` | Time spent on relationship vector search   |

### Debug Info

When `includeDebug: true`, the `debug.score_distribution` object includes a `relationship` entry with `min`, `max`, and `mean` score statistics. The `debug.fusion_details.pre_fusion_counts` includes a `relationship` count.

### Result Type Filtering (`resultTypes`)

| Value              | Graph Search | Text Search | Relationship Search |
| ------------------ | :----------: | :---------: | :-----------------: |
| `"both"` (default) |     Yes      |     Yes     |         Yes         |
| `"graph"`          |     Yes      |     No      |         Yes         |
| `"text"`           |      No      |     Yes     |         No          |

Note: `resultTypes=graph` includes relationship search because relationships are part of the graph domain. `resultTypes=text` excludes both graph and relationship search.

### Backward Compatibility

- Existing API clients that don't handle `type: "relationship"` items will simply ignore them (unknown fields are harmless in JSON)
- The `metadata.relationshipResultCount` field is additive — existing clients that read `graphResultCount` and `textResultCount` are unaffected
- When no relationships have embeddings, `relationshipResultCount` is 0 and no relationship items appear

## Database Schema

### Column Addition

```sql
-- Migration: 00011_add_relationship_embeddings.sql
ALTER TABLE kb.graph_relationships ADD COLUMN embedding vector(768);
ALTER TABLE kb.graph_relationships ADD COLUMN embedding_updated_at TIMESTAMPTZ;
```

The `embedding` column is nullable:

- New relationships get embeddings synchronously during creation
- Existing relationships have `NULL` until backfilled
- Search queries filter `WHERE embedding IS NOT NULL`

### Index

```sql
-- Migration: 00012_add_relationship_embedding_index.sql
CREATE INDEX CONCURRENTLY idx_graph_relationships_embedding_ivfflat
ON kb.graph_relationships
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);
```

- **Type**: IVFFlat (approximate nearest neighbor)
- **Distance**: Cosine similarity (`vector_cosine_ops`)
- **Lists**: 100 (optimal for up to ~1M relationships)
- **Concurrency**: `CONCURRENTLY` avoids table locks during build

## Operations Guide

### Deployment Steps

1. **Apply migration 00011** (adds nullable `embedding` + `embedding_updated_at` columns):

   ```bash
   cd apps/server-go
   /usr/local/go/bin/go run ./cmd/migrate -- up
   ```

2. **Deploy application** with updated code (enables embedding on new relationships)

3. **Apply migration 00012** (creates IVFFlat index — can run during production):

   ```bash
   # This uses CREATE INDEX CONCURRENTLY — no table locks
   /usr/local/go/bin/go run ./cmd/migrate -- up
   ```

4. **Run backfill** (optional — embeds existing relationships):

   ```bash
   cd apps/server-go

   # Dry run first
   /usr/local/go/bin/go run ./cmd/backfill-embeddings \
     --dry-run \
     --batch-size=100

   # Real run
   /usr/local/go/bin/go run ./cmd/backfill-embeddings \
     --batch-size=100 \
     --delay=100

   # Filter to specific project
   /usr/local/go/bin/go run ./cmd/backfill-embeddings \
     --batch-size=100 \
     --delay=100 \
     --project-id="your-project-uuid"
   ```

### Backfill Script

**Location**: `apps/server-go/cmd/backfill-embeddings/main.go`

**Flags**:

| Flag           | Default | Description                              |
| -------------- | ------- | ---------------------------------------- |
| `--batch-size` | 100     | Relationships per batch                  |
| `--delay`      | 100     | Milliseconds between batches             |
| `--dry-run`    | false   | Print what would be done without writing |
| `--project-id` | (all)   | Filter to a specific project UUID        |

**Environment variables**:

| Variable             | Required | Description                          |
| -------------------- | -------- | ------------------------------------ |
| `DATABASE_URL`       | Yes\*    | Full Postgres connection string      |
| `POSTGRES_PASSWORD`  | Yes\*    | Password (if not using DATABASE_URL) |
| `POSTGRES_USER`      | No       | Default: `emergent`                  |
| `POSTGRES_DATABASE`  | No       | Default: `emergent`                  |
| `DB_HOST`            | No       | Default: `localhost`                 |
| `DB_PORT`            | No       | Default: `5432`                      |
| `GCP_PROJECT_ID`     | Yes\*\*  | GCP project for Vertex AI            |
| `VERTEX_AI_LOCATION` | No       | Default: `us-central1`               |
| `GOOGLE_API_KEY`     | Yes\*\*  | Alternative to Vertex AI             |
| `EMBEDDING_MODEL`    | No       | Default: `text-embedding-004`        |

\*One of `DATABASE_URL` or `POSTGRES_PASSWORD` required.
\*\*One of `GCP_PROJECT_ID` + `VERTEX_AI_LOCATION` or `GOOGLE_API_KEY` required.

**Behavior**:

- Processes relationships with `WHERE embedding IS NULL AND deleted_at IS NULL`
- Self-healing: subsequent runs automatically skip already-processed rows
- Errors are logged but don't abort the batch — failed rows are retried on next run
- Progress logged every batch: `processed / total / embedded / errors`

### Index Management

**Verify index exists**:

```sql
SELECT schemaname, indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'kb'
  AND tablename = 'graph_relationships'
  AND indexname = 'idx_graph_relationships_embedding_ivfflat';
```

**Verify index is being used** (run after some searches):

```sql
SELECT idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
WHERE indexname = 'idx_graph_relationships_embedding_ivfflat';
```

**Check query plan**:

```sql
EXPLAIN ANALYZE
SELECT id, src_id, dst_id, type,
       embedding <=> '[0.1,0.2,...]'::vector AS distance
FROM kb.graph_relationships
WHERE embedding IS NOT NULL
  AND project_id = 'your-project-id'
ORDER BY embedding <=> '[0.1,0.2,...]'::vector
LIMIT 10;
-- Expected: "Index Scan using idx_graph_relationships_embedding_ivfflat"
```

**When to rebuild index**:

- After bulk inserts of >100K relationships
- If index `lists` parameter needs tuning (e.g., `lists = sqrt(row_count)`)
- Drop and recreate: `DROP INDEX CONCURRENTLY ...; CREATE INDEX CONCURRENTLY ...`

### Rollback

**Feature disable** (without rollback):

- Set `embeddings` service to `nil` in graph service constructor — new relationships skip embedding
- Search naturally returns 0 relationship results when all embeddings are NULL

**Full rollback**:

```bash
# Rollback index (migration 00012)
/usr/local/go/bin/go run ./cmd/migrate -- down-to 00011

# Rollback column (migration 00011)
/usr/local/go/bin/go run ./cmd/migrate -- down-to 00010
```

This drops the `embedding` column and index. Existing relationships are unaffected.

## Monitoring

### Key Metrics to Watch

| Metric                        | What to Monitor                    | Alert Threshold           |
| ----------------------------- | ---------------------------------- | ------------------------- |
| Relationship creation latency | p95 should stay < 300ms            | > 500ms p95               |
| Embedding generation latency  | p50 ~100ms, p95 < 200ms            | > 300ms p95               |
| Embedding errors              | Rate of failed embeddings          | > 5% error rate           |
| Null embedding count          | Decreasing over time (backfill)    | Increasing after backfill |
| Search latency (total)        | p95 increase < 100ms from baseline | > 200ms p95 increase      |
| Relationship search latency   | p50 ~30ms, p95 < 100ms             | > 150ms p95               |
| Vertex AI quota usage         | Below 80% of quota                 | > 90% quota               |

### Log Patterns

**Embedding failure** (non-blocking, relationship still created):

```
level=WARN msg="failed to generate embedding for relationship, continuing without embedding"
  relationship_id=<uuid> triplet="..." error="..."
```

**Embedding storage failure**:

```
level=WARN msg="failed to store embedding for relationship, continuing without embedding"
  relationship_id=<uuid>
```

**Backfill progress**:

```
level=INFO msg="progress" processed=500 total=10000 embedded=498 errors=2
```

### Useful Queries

**Count relationships with/without embeddings**:

```sql
SELECT
  COUNT(*) AS total,
  COUNT(embedding) AS with_embedding,
  COUNT(*) - COUNT(embedding) AS without_embedding,
  ROUND(COUNT(embedding)::numeric / NULLIF(COUNT(*), 0) * 100, 1) AS pct_embedded
FROM kb.graph_relationships
WHERE deleted_at IS NULL;
```

**Embedding adoption by project**:

```sql
SELECT
  project_id,
  COUNT(*) AS total,
  COUNT(embedding) AS embedded,
  ROUND(COUNT(embedding)::numeric / NULLIF(COUNT(*), 0) * 100, 1) AS pct
FROM kb.graph_relationships
WHERE deleted_at IS NULL
GROUP BY project_id
ORDER BY total DESC;
```

**Recent embedding timestamps** (verify new relationships get embeddings):

```sql
SELECT id, type, embedding_updated_at, created_at
FROM kb.graph_relationships
WHERE embedding IS NOT NULL
ORDER BY embedding_updated_at DESC
LIMIT 10;
```

## Source Files

### Implementation

| File                          | Description                                                   |
| ----------------------------- | ------------------------------------------------------------- |
| `domain/graph/service.go`     | Triplet generation, embedding integration, CreateRelationship |
| `domain/graph/entity.go`      | `GraphRelationship` entity with `Embedding` field             |
| `domain/graph/handler.go`     | REST API handlers                                             |
| `domain/graph/dto.go`         | Request/response DTOs                                         |
| `domain/search/service.go`    | 3-way parallel search, RRF fusion                             |
| `domain/search/repository.go` | `SearchRelationships()` — vector similarity query             |
| `domain/search/dto.go`        | `ItemTypeRelationship`, `TripletText`, response types         |
| `domain/chat/handler.go`      | `formatSearchContext()` — RAG prompt formatting               |
| `pkg/pgutils/vector.go`       | `FormatVector()` helper                                       |

### Migrations

| File                                                    | Description                                           |
| ------------------------------------------------------- | ----------------------------------------------------- |
| `migrations/00011_add_relationship_embeddings.sql`      | Adds `embedding vector(768)` + `embedding_updated_at` |
| `migrations/00012_add_relationship_embedding_index.sql` | IVFFlat index with `lists=100`                        |

### Tools

| File                              | Description                                      |
| --------------------------------- | ------------------------------------------------ |
| `cmd/backfill-embeddings/main.go` | Batch backfill script for existing relationships |

### Tests

| File                                    | Description                                                    |
| --------------------------------------- | -------------------------------------------------------------- |
| `tests/e2e/relationship_search_test.go` | 17 E2E tests: creation, search, mixed results, compatibility   |
| `domain/chat/chat_test.go`              | Unit tests for `formatSearchContext()` with relationship items |

## Related Documents

- [Design Document](../../../openspec/changes/archive/2026-02-12-cognee-phase-2/design.md) — Full design decisions and rationale
- [GraphRAG Implementation Plan](../../plans/GRAPH_RAG_IMPLEMENTATION_PLAN.md) — Broader roadmap (Phase 2.2 = this feature)
- [Chat RAG Integration](../chat-rag-integration.md) — Chat system RAG architecture
- [Graph Search Pagination](../../spec/graph-search-pagination.md) — Hybrid search cursor pagination
- [Database Schema Context](../../database/schema-context.md) — Full schema reference
