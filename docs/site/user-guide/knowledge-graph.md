# Knowledge Graph

The knowledge graph is the core of Memory. It stores **objects** (nodes) and **relationships** (edges) in a typed, versioned, searchable graph.

## Concepts

| Concept | Description |
|---|---|
| **Object** | A typed node in the graph (e.g. a `Decision`, `Person`, `Requirement`) |
| **Relationship** | A directed, typed edge between two objects (e.g. `depends_on`, `authored_by`) |
| **Type** | The schema that defines valid properties for an object or relationship |
| **CanonicalID** | Stable ID for an object across versions — use this to reference an object |
| **VersionID** | The ID of a specific version of an object (changes on every edit) |
| **Branch** | An isolated copy of the graph for safe editing; see [Branches](branches.md) |

!!! tip "CanonicalID vs VersionID"
    Always use `canonical_id` when linking objects, building queries, or storing references. The `id` field is the version-specific row; it changes every time the object is updated.

---

## Object Types

Types are defined by your project's installed **template packs** or custom type definitions. Common built-in types include:

- `Decision` — architectural or strategic decisions
- `Requirement` — product or technical requirements
- `Person` — individuals mentioned in documents
- `Meeting` — meeting records with participants and outcomes
- `Risk` — identified risks with severity and mitigation

List available types for your project:

```http
GET /api/type-registry/projects/{projectId}
```

---

## Creating Objects

=== "CLI"
    ```bash
    memory graph objects create \
      --type Decision \
      --properties '{"title": "Use PostgreSQL", "status": "approved", "rationale": "Existing expertise"}' \
      --project my-project

    # With a stable key for idempotent operations (skip if already exists):
    memory graph objects create \
      --type Decision \
      --key "decision-use-postgresql" \
      --properties '{"title": "Use PostgreSQL", "status": "approved"}' \
      --project my-project

    # --upsert creates-or-updates by key:
    memory graph objects create \
      --type Decision \
      --key "decision-use-postgresql" \
      --properties '{"title": "Use PostgreSQL", "status": "approved"}' \
      --upsert --project my-project
    ```

=== "API"
    ```http
    POST /api/graph/objects
    Content-Type: application/json

    {
      "type": "Decision",
      "properties": {
        "title": "Use PostgreSQL as primary database",
        "status": "approved",
        "rationale": "Team expertise and existing infrastructure"
      },
      "labels": ["architecture", "database"]
    }
    ```

    Response includes both `id` (version) and `canonical_id` (stable reference).

### Object fields

| Field | Description |
|---|---|
| `type` | Object type name (must exist in the type registry) |
| `properties` | Key-value map of typed properties per the type schema |
| `labels` | Free-form string tags for filtering and organization |
| `status` | Optional status flag (e.g. `draft`, `approved`, `archived`) |
| `name` | Human-readable title shown in the UI and ranked first in search |
| `ref` | Read-only display reference (`#` + last 12 hex of the canonical ID); never used for writes |
| `id` | Stable canonical UUID — the addressable handle for reads, writes, and relationships |
| `key` | Internal dedup/upsert identity; not a display label |

---

## Creating Relationships

```http
POST /api/graph/relationships
Content-Type: application/json

{
  "type": "depends_on",
  "src_id": "<canonical_id of source object>",
  "dst_id": "<canonical_id of target object>",
  "properties": {
    "note": "Must be resolved before deployment"
  }
}
```

Relationships are **directed**: `src → dst`. Use `type` to describe the semantic of the edge.

---

## Bulk creating objects and relationships

For more than a handful of objects, use the batch commands instead of one API
call per node:

```bash
# Objects only: a JSON array of {type, name, description, properties}
memory graph objects create-batch --file objects.json

# Objects + relationships: a subgraph with _ref/src_ref/dst_ref placeholders
memory graph objects create-batch --file subgraph.json

# Relationships only: a JSON array of {type, from, to}
memory graph relationships create-batch --file relationships.json
```

Both commands accept `--branch <name-or-uuid>` to write into a graph branch
instead of the main graph. Objects are chunked to the server's per-call limit
(500 for a subgraph, 100 per bulk item), and each row reports its own error
instead of failing the whole batch.

`create-batch` on objects also reads the JSONL seed layout written by
`memory blueprints dump` — pass the dump root, a `seed/` directory, or a single
`.jsonl` file. Records are upserted by `key` and relationship `srcKey`/`dstKey`
endpoints are resolved after the objects are written, so dump → edit → import
into a branch is a complete migration loop:

```bash
memory blueprints dump ./migration
memory graph objects create-batch --file ./migration --branch migration-review
```

See [Branches § Bulk importing a dump into a branch](branches.md#bulk-importing-a-dump-into-a-branch).

---

## Updating Objects

```http
PATCH /api/graph/objects/{id}
Content-Type: application/json

{
  "properties": {
    "status": "superseded",
    "supersededBy": "Use CockroachDB"
  }
}
```

Every update creates a new version row. The previous version is preserved in history.

---

## Searching

### Hybrid search (recommended)

Combines keyword, semantic, and graph structure in a single ranked result:

=== "CLI"
    ```bash
    memory query "decisions about database architecture" --project my-project
    ```

=== "API"
    ```http
    POST /api/graph/search
    {
      "query": "decisions about database architecture",
      "types": ["Decision"],
      "limit": 20
    }
    ```

### Full-text search

```http
GET /api/graph/objects/fts?q=postgresql&types=Decision
```

### Vector (semantic) search

```http
POST /api/graph/objects/vector-search
{
  "query": "scalability trade-offs",
  "topK": 10
}
```

### Filter by type, label, or status

```http
GET /api/graph/objects/search?type=Decision&status=approved&labels=architecture
```

---

## Viewing Object History

Every change is versioned. Retrieve the full revision history:

```http
GET /api/graph/objects/{id}/history
```

Restore a previous version:

```http
POST /api/graph/objects/{id}/restore
{ "versionId": "<old-version-id>" }
```

---

## Traversal and Expansion

### Get all edges for an object

```http
GET /api/graph/objects/{id}/edges
```

### Expand a subgraph from a starting node

```http
POST /api/graph/expand
{
  "objectId": "<canonical_id>",
  "depth": 2,
  "relationshipTypes": ["depends_on", "authored_by"]
}
```

### Traverse by relationship path

```http
POST /api/graph/traverse
{
  "startId": "<canonical_id>",
  "direction": "outbound",
  "maxDepth": 3
}
```

### Find similar objects

```http
GET /api/graph/objects/{id}/similar?limit=5
```

---

## Bulk Operations

```http
POST /api/graph/objects/bulk          # create multiple objects
POST /api/graph/relationships/bulk    # create multiple relationships
POST /api/graph/objects/bulk-update-status  # update status on many objects at once
```

---

## Deleting Objects

Deletion is **soft** — the object is marked `deleted_at` but preserved in history.

```http
DELETE /api/graph/objects/{id}
```

To restore:

```http
POST /api/graph/objects/{id}/restore
```

---

## Analytics

```http
GET /api/graph/analytics/most-accessed   # objects viewed most recently
GET /api/graph/analytics/unused          # objects with no recent access
```
