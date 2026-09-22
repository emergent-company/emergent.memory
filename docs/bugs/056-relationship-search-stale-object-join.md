# Bug Report: Relationship search joins stale v1 graph objects (canonical_id mismatch)

**Status:** Open
**Severity:** Medium
**Component:** Search / Database
**Discovered:** 2026-09-22
**Discovered by:** AI Agent
**Assigned to:** Unassigned

---

## Summary

Relationship vector search joins `kb.graph_relationships.src_id`/`dst_id` to `kb.graph_objects.id`, but those columns store **canonical_id**, not the physical row id — so for versioned objects (v2+, where `id != canonical_id`) the query returns stale v1 metadata for `src_key`/`src_type`/`src_properties` (and `dst_*`).

---

## Description

- **Actual behavior:** For a relationship whose source/target object has been updated (new version row created), `SearchRelationships` returns the *old* version's `key`, `type`, and `properties` because the JOIN matches on the physical `id` instead of `canonical_id`.
- **Expected behavior:** The search should return the *current* (canonical) object's metadata.
- **When it occurs:** Whenever a relationship's endpoint object has been versioned (v2+). v1 objects have `id == canonical_id`, so they are unaffected.

Evidence:

- `apps/server/domain/graph/entity.go:115-117` — `SrcObject`/`DstObject` are joined via `join:src_id=canonical_id`, confirming `src_id`/`dst_id` store `canonical_id`.
- `apps/server/migrations/00015_backfill_relationship_canonical_ids.sql` — migration rewrites `src_id`/`dst_id` to `canonical_id` values.
- `apps/server/domain/search/repository.go` (SearchRelationships) — `JOIN kb.graph_objects src ON src.id = r.src_id` / `JOIN kb.graph_objects dst ON dst.id = r.dst_id`.

---

## Reproduction Steps

1. Create a graph object `A` (v1) and a relationship `R` with `A` as source.
2. Update `A` so a new version row (v2) is created (`id` changes, `canonical_id` stays).
3. Run relationship search that returns `R`.
4. Observe `src_key`/`src_type`/`src_properties` reflect the v1 row, not the current v2 row.

---

## Impact

- **User Impact:** Search results surface outdated object names/types/properties.
- **System Impact:** Incorrect metadata in unified search graph/relationship results.
- **Frequency:** Any relationship whose endpoint was ever versioned.
- **Workaround:** None at the query level; would require a code fix.

---

## Root Cause Analysis

The JOIN uses `src.id = r.src_id` / `dst.id = r.dst_id`, but `src_id`/`dst_id` store `canonical_id` values. The Bun relationship definitions in `entity.go` correctly map `join:src_id=canonical_id`, but the hand-written SQL in `SearchRelationships` uses `id`.

**Related Files:**

- `apps/server/domain/search/repository.go` — relationship ANN SQL JOINs.
- `apps/server/domain/graph/entity.go:115-117` — canonical_id join mapping.
- `apps/server/migrations/00015_backfill_relationship_canonical_ids.sql` — canonical_id backfill.

---

## Proposed Solution

Change the JOIN conditions to match on `canonical_id`:

```sql
JOIN kb.graph_objects src ON src.canonical_id = r.src_id
JOIN kb.graph_objects dst ON dst.canonical_id = r.dst_id
```

**Changes Required:**

1. Update both JOIN clauses in `SearchRelationships` to use `src.canonical_id = r.src_id` and `dst.canonical_id = r.dst_id`.
2. Verify no other hand-written relationship SQL uses `id` for `src_id`/`dst_id`.

**Testing Plan:**

- [ ] Unit/integration test: create + version an endpoint object, then assert relationship search returns current metadata.

---

## Related Issues

- Discovered during review of PR #696 (`fix/search: keep ivfflat index for relationship vector search`).

---

## Notes

Not introduced by PR #696 (the JOIN clauses predate it), but directly adjacent to the metadata the relationship search returns. Filed separately because it is out of scope for that PR.

---

**Last Updated:** 2026-09-22 by AI Agent
