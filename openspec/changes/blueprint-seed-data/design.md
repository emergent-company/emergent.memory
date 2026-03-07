## Context

The `memory blueprints` CLI command currently applies template packs and agent definitions from a structured directory. Users can define their schema (object/relationship types) and agents declaratively, then apply them to a project in one command. However, there is no equivalent mechanism for seeding the knowledge graph with actual data — objects and relationships.

Currently the graph SDK already exposes `BulkCreateObjects` and `BulkCreateRelationships` endpoints. This design adds a `seed/` directory convention to blueprints and a new seeder that uses those endpoints. All changes are confined to `tools/cli/internal/blueprints/`; no backend server changes are needed.

## Goals / Non-Goals

**Goals:**
- Add `seed/` directory support to the blueprint format
- Load seed files (JSON/YAML) defining objects and relationships
- Apply seed data after packs and agents in the same `memory blueprints` command
- Support large datasets via bulk API calls (chunked batches)
- Key-based deduplication: skip or update existing objects matched by `key` field
- Honor `--dry-run` and `--upgrade` flags consistently with existing behavior
- Relationship references: resolve source/target by object `key` within the same seed file (so the file is self-contained and order-independent)
- New `memory blueprints dump` subcommand: export objects and relationships from an existing project into a seed file (round-trip: dump → edit → re-apply)
- Dump supports filtering by one or more object types, and outputs JSON or YAML

**Non-Goals:**
- Cross-file key resolution (relationships cannot reference keys defined in other seed files)
- Incremental diff / sync of large datasets
- Rollback of seed data on partial failure
- UI changes for seed data management
- Server-side API changes
- Dumping template packs or agent definitions (existing `blueprints` command handles schema; dump is data-only)

## Decisions

### D1: New `seed/` subdirectory (not extending pack files)

**Decision**: Seed data lives in `seed/<name>.{json,yaml,yml}` — a sibling of `packs/` and `agents/`, not embedded inside pack files.

**Rationale**: Pack files describe *schema* (types), not *data*. Mixing them couples schema versioning with data versioning. A separate directory keeps concerns separated and allows very large seed files (thousands of objects) without bloating pack definitions.

**Alternative considered**: `objectInstances` / `relationshipInstances` arrays inside `PackFile`. Rejected because pack files are schema artifacts and embedding data in them would complicate the `--upgrade` logic for type schemas.

### D2: Key-based identity for deduplication

**Decision**: Objects in seed files use an optional `key` string field. Objects with a matching key in the project are skipped (or updated with `--upgrade`); keyless objects are always created.

**Rationale**: The graph SDK supports `key` on `CreateObjectRequest` and the server uses upsert semantics when `key` is set. This gives blueprint authors stable, human-readable identifiers without needing to track server-assigned UUIDs.

**Alternative considered**: Using object `type + properties` fingerprinting. Rejected — too fragile and expensive.

### D3: Relationship resolution via local key map

**Decision**: After all objects from a seed file are applied, the seeder builds a `key → entityID` map from results. Relationships can then reference objects by `srcKey` / `dstKey` fields which are resolved before the API call.

**Rationale**: Allows self-contained seed files where relationships reference objects defined in the same file. Users don't need to know server-side UUIDs.

**Alternative considered**: Requiring raw UUIDs in `srcId` / `dstId`. Rejected — impractical for human-authored files.

### D4: Batch size of 100

**Decision**: The seeder chunks bulk API calls to 100 items per request.

**Rationale**: Aligns with practical API limits and avoids request timeouts on large datasets. Value is a constant that can be tuned.

### D5: Seed applied after packs and agents

**Decision**: Seed is the last phase — packs first, agents second, seed data third.

**Rationale**: Seed data typically depends on object types defined in packs. Applying packs first ensures type definitions exist before objects are created.

### D6: `memory blueprints dump` as a subcommand of `blueprints`

**Decision**: The dump feature is exposed as `memory blueprints dump <output-dir> [--types type1,type2] [--format yaml|json]` rather than a top-level command or a flag on the main `blueprints` command.

**Rationale**: Dump is the inverse of apply — keeping it under the same `blueprints` group makes discovery natural and keeps the CLI surface small. The output directory mirrors the `seed/` structure so the result can be immediately used with `memory blueprints <output-dir>`.

**Alternative considered**: `memory graph export` as a separate top-level command. Rejected — coupling it to the blueprint format makes the round-trip workflow explicit.

### D7: Dump paginates via cursor and streams to file

**Decision**: The dumper uses `ListObjects` and `ListRelationships` with cursor pagination (page size 250), accumulating all results before writing a single seed file.

**Rationale**: Projects may have thousands of objects. Cursor pagination avoids loading the entire dataset into memory at once — results are accumulated into the output struct and flushed to disk once complete. A single output file per dump keeps the result simple to inspect and re-apply.

**Alternative considered**: Streaming NDJSON to avoid holding all results in memory. Rejected — the seed file format is a structured JSON/YAML document, not a stream; and typical project sizes fit comfortably in memory.

### D8: Dump uses object `key` as the relationship reference if available; falls back to `entityId`

**Decision**: When dumping relationships, `srcKey`/`dstKey` are populated if the referenced objects were also dumped and have a `key`. Otherwise, `srcId`/`dstId` (entityID) are used directly.

**Rationale**: Makes dumped files maximally portable — a file with keys can be re-applied to a different project; a file with UUIDs only works against the same project. When all objects have keys (the recommended practice), the dump is fully portable.

## Risks / Trade-offs

- **Partial failure on large datasets** → Mitigation: continue processing remaining batches on error; report counts of successes and failures in summary. Do not abort the run.
- **Relationship references to missing keys** → Mitigation: if `srcKey`/`dstKey` cannot be resolved, record an error result for that relationship and continue.
- **Duplicate keyless objects** → Mitigation: document clearly that keyless objects are always created. Users who need idempotency must provide keys.
- **Very large files (>10k objects)** → Mitigation: chunked batching handles this gracefully; memory footprint is bounded by batch size, not file size.
- **Dump completeness** → Mitigation: dump always fetches all pages; progress is printed to stderr so users can see it's working on large projects.

## Migration Plan

- No server migration needed.
- Existing blueprint directories without a `seed/` directory are unaffected — `LoadDir` returns empty seed list, seeder is a no-op.
- CLI binary is deployed via `task cli:install`; no coordinated rollout required.

## Open Questions

- Should seed files support a `labels` array on objects? (The API supports it — include for completeness.)
- Should the seeder report per-object results or only batch-level summaries? (Per-object is more useful for debugging but verbose on large datasets — use batch summary with error detail.)
- Should `dump` include deleted objects? Default: no; add `--include-deleted` flag for completeness.
