## Context

The `memory blueprints` CLI command currently applies template packs and agent definitions from a structured directory. Users can define their schema (object/relationship types) and agents declaratively, then apply them to a project in one command. However, there is no equivalent mechanism for seeding the knowledge graph with actual data — objects and relationships.

Currently the graph SDK already exposes `BulkCreateObjects` and `BulkCreateRelationships` endpoints. This design adds a `seed/` directory convention to blueprints and a new seeder that uses those endpoints. All changes are confined to `tools/cli/internal/blueprints/`; no backend server changes are needed.

## Goals / Non-Goals

**Goals:**
- Add `seed/` directory support to the blueprint format
- Load per-type JSONL seed files defining objects and relationships
- Apply seed data after packs and agents in the same `memory blueprints` command
- Support large datasets via bulk API calls (chunked batches of 100)
- Key-based deduplication: pre-check existing keys, skip or upsert based on `--upgrade`
- Honor `--dry-run` and `--upgrade` flags consistently with existing behavior
- Relationship references: resolve source/target by object `key` within the same seed directory (so files are self-contained and order-independent)
- New `memory blueprints dump` subcommand: export objects and relationships from an existing project into per-type JSONL seed files (round-trip: dump → edit → re-apply)
- Dump supports filtering by one or more object types

**Non-Goals:**
- Cross-directory key resolution (relationships cannot reference keys defined in other seed directories)
- Rollback of seed data on partial failure
- UI changes for seed data management
- Server-side API changes
- Dumping template packs or agent definitions (existing `blueprints` command handles schema; dump is data-only)

## Decisions

### D1: Per-type JSONL in seed/objects/ and seed/relationships/

**Decision**: Seed data lives in two subdirectories:
- `seed/objects/<TypeName>.jsonl` — one object per line, all objects of that type
- `seed/relationships/<TypeName>.jsonl` — one relationship per line, all rels of that type

Files auto-split at 50 MB: `<TypeName>.001.jsonl`, `<TypeName>.002.jsonl`, etc.

**Rationale**: Per-type files are diffable, streamable, and git-friendly for normal project sizes. Auto-splitting keeps individual files under GitHub's 100 MB hard push limit. Separating objects and relationships into subdirectories makes the apply order explicit and allows the seeder to build the full key→entityID map before processing any relationship file.

**Alternative considered**: Single YAML/JSON file per seed. Rejected — not practical at IMDB scale (400k objects, 2M relationships); a single file would exceed GitHub's hard limit.

**Alternative considered**: Mixed JSONL with `kind` field. Rejected — per-type files are more readable, allow parallel processing by type, and make `--types` filtering trivial.

### D2: Key-based identity for deduplication

**Decision**: Objects in seed files use an optional `key` string field. On apply, the seeder:
1. Collects all keys in the current batch
2. Calls `ListObjects` with those keys to find existing objects
3. Skips existing objects (or upserts with `--upgrade`)
4. Bulk-creates the remainder via `BulkCreateObjects`

Keyless objects are always created (no dedup possible without a stable identity).

**Rationale**: The DB enforces a partial unique index on `(project_id, type, key)` for HEAD rows. The `UpsertObject` endpoint uses advisory locking + content-hash no-op detection, making re-seeding safe for keyed objects. Bulk create has no upsert semantics, so pre-checking keys is the correct approach for batch workflows.

**Alternative considered**: Call `/api/graph/objects/upsert` per object. Correct, but O(N) API calls for large datasets. Pre-check + bulk-create reduces this to O(N/100) bulk calls plus one key-lookup call per batch.

### D3: Relationship deduplication is automatic

**Decision**: Relationships are submitted via `BulkCreateRelationships` without any pre-check. The server uses `INSERT ... ON CONFLICT DO NOTHING` against the HEAD unique index on `(project_id, type, src_id, dst_id)`. Re-seeding relationships is always safe.

**Rationale**: The DB schema already enforces relationship deduplication at the HEAD level — only one HEAD row can exist per `(project, type, src, dst)` tuple. The API reflects this: no duplicate can be created. No client-side dedup logic is needed.

### D4: Relationship references via key map

**Decision**: After all objects from all seed files are applied, the seeder builds a `key → entityID` map from results. Relationship JSONL lines use `srcKey`/`dstKey` fields which are resolved before the API call. Raw `srcId`/`dstId` UUIDs are also accepted as a fallback.

**Rationale**: Allows self-contained seed directories where relationships reference objects by stable human-readable keys. Dump output uses keys when available, making dump files portable across projects.

### D5: Batch size of 100

**Decision**: Both object and relationship bulk API calls chunk at 100 items per request.

**Rationale**: Aligns with the documented maximum for the bulk endpoints. Constant is named `seedBatchSize` for easy tuning.

### D6: Seed applied after packs and agents

**Decision**: Seed is the last phase — packs first, agents second, seed data third.

**Rationale**: Seed data depends on object types defined in packs. Applying packs first ensures type definitions exist before objects are created.

### D7: `memory blueprints dump` as a subcommand

**Decision**: `memory blueprints dump <output-dir> [--types type1,type2]`

Output structure mirrors the seed directory:
```
<output-dir>/
  seed/
    objects/
      Movie.jsonl
      Person.jsonl
    relationships/
      ACTED_IN.jsonl
      DIRECTED.jsonl
```

**Rationale**: The output is immediately usable as a blueprint source directory. Dump is the inverse of apply — keeping it under `blueprints` makes the round-trip workflow explicit.

### D8: Dump uses cursor pagination and 50 MB auto-split

**Decision**: Dumper paginates `ListObjects` and `ListRelationships` with cursor (page size 250), streams lines to per-type JSONL files, and opens a new split file when the current file exceeds 50 MB.

**Rationale**: Projects may have millions of objects. Streaming to disk avoids holding the full dataset in memory. The 50 MB split threshold keeps individual files under GitHub's 100 MB hard limit.

### D9: Dump uses object `key` as relationship reference when available

**Decision**: When dumping relationships, `srcKey`/`dstKey` are populated if the referenced objects have a `key`. Otherwise `srcId`/`dstId` (entityID) are used.

**Rationale**: Files with keys are portable across projects. Files with only UUIDs only work against the same project. When all objects have keys (the recommended practice for seeded data), the dump is fully portable.

## DB Schema Facts (verified)

| | graph_objects | graph_relationships |
|---|---|---|
| Key uniqueness | Partial unique index on `(project_id, type, key)` WHERE HEAD row | Partial unique index on `(project_id, type, src_id, dst_id)` WHERE HEAD row |
| Upsert support | `PUT /api/graph/objects/upsert` (advisory lock + content-hash no-op) | `INSERT ON CONFLICT DO NOTHING` via bulk create |
| Bulk upsert | Not available — bulk create is insert-only | N/A — auto-deduplicated |
| COPY fast path | Not available — backup domain uses paginated SELECT + NDJSON | Same |

## Risks / Trade-offs

- **Partial failure on large datasets** → Mitigation: continue processing remaining batches on error; report counts of successes and failures in summary. Do not abort the run.
- **Relationship references to missing keys** → Mitigation: if `srcKey`/`dstKey` cannot be resolved, record an error result for that relationship and continue.
- **Duplicate keyless objects** → Mitigation: document clearly that keyless objects are always created. Users who need idempotency must provide keys.
- **Very large files** → Mitigation: auto-split at 50 MB; streaming write avoids memory pressure.
- **Dump completeness** → Mitigation: dump always fetches all pages; progress is printed to stderr so users can see it's working on large projects.

## Migration Plan

- No server migration needed.
- Existing blueprint directories without a `seed/` directory are unaffected — `LoadDir` returns empty results, seeder is a no-op.
- CLI binary is deployed via `task cli:install`; no coordinated rollout required.
