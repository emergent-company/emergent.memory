# Tasks

## 1. Diagnosis

- [x] 1.1 Confirm entity-query plans: pagination COUNT is a full Parallel Seq Scan
  over `kb.graph_objects` because no index exists on `properties`; SELECT uses the
  `(project_id, type, created_at DESC, id DESC)` index but per-row JSONB filter.
- [x] 1.2 Confirm wrong scope: `chapter_id` is not unique to one law; 697 matches
  span many `law_ref_id`s in the same project.
- [x] 1.3 Hermetic Postgres repro seeded to the issue's shape
  (150k LegalParagraph / 697 chapter matches / second project) with BEFORE
  `EXPLAIN (ANALYZE, BUFFERS)`.

## 2. Index and query shape

- [x] 2.1 Add migration `00197_add_graph_objects_project_type_props_gin.sql`
  (`btree_gin` + composite partial GIN, `CONCURRENTLY`, drop-if-exists, `NO TRANSACTION`;
  numbered 00197 to avoid the open PR #1163 migrations 00195/00196).
- [x] 2.2 Rewrite entity-query `filters` to a bound JSONB containment predicate.
- [x] 2.3 Re-run AFTER `EXPLAIN (ANALYZE, BUFFERS)` on the hermetic DB; record
  buffer and timing deltas.

## 3. Bounds

- [x] 3.1 Add `MCPConfig` (`MCP_ENTITY_QUERY_TIMEOUT`,
  `MCP_ENTITY_QUERY_FULL_MAX_LIMIT`) with sane defaults.
- [x] 3.2 Enforce a per-call deadline and a `field_strategy="full"` limit cap with a
  caller-visible warning.

## 4. Identity scoping

- [x] 4.1 Add optional `key_prefix` scope parameter and document it in the tool schema.
- [x] 4.2 Fail-first DB test: bare `chapter_id` leaks across laws; `key_prefix`
  scopes to one law.

## 5. Tests and verification

- [x] 5.1 DB tests for full-limit bound and timeout.
- [x] 5.2 `go build ./...`, `task lint`, `gofmt -l`, `REQUIRE_DB=1` DB tests.
