## Context

Authorization currently resolves at the project/org boundary. Search applies only a
`project_id` filter, so a project member sees every resource in the project. Onyx
introduces document-level visibility; Memory needs the same, generalized to graph
objects and sources because it is graph-first. This is the highest-risk change in this
batch: it touches every read path and requires a data migration that must not change
observed behaviour for existing users.

## Goals / Non-Goals

**Goals**

- A per-resource ACL data model with recursive group resolution.
- One `authorizeResources()` enforcement helper wired into all three search legs and
  graph hybrid search.
- A default/backfill rule that preserves current behaviour.
- A `PermissionSource` interface contract for future syncers.

**Non-Goals (deferred, explicit)**

- 50 connector ACL syncers — the breadth trap this change is designed to avoid.
- Postgres RLS (rejected; see D3).
- Write/update permissions at the resource level — this change is read authorization
  only; writes remain governed by project role.

## Decisions

### D1 — Data model: ACL entries + groups

```
kb.acl_entries (
  id, resource_type {document|canonical|source}, resource_id uuid,
  principal_type {user|group}, principal_id uuid,
  permission text (read), created_at, updated_at,
  UNIQUE (resource_type, resource_id, principal_type, principal_id)
)
kb.groups ( id, project_id, name )
kb.group_members ( group_id, principal_type, principal_id )   -- may nest groups
```

Group resolution is recursive via a `WITH RECURSIVE` CTE over `group_members`
(cycle-safe by visited-set / depth cap). A user is authorized for a resource if any of
the user's (transitive) groups, or the user directly, has an entry.

### D2 — Enforcement point: `authorizeResources()`

A single function computes the set of resource ids (per type) a principal may read in a
project, then each search leg adds that set as a predicate (`IN (allowed)`, or an
existence check against `acl_entries` + group membership). Resolution happens once per
request and is translated into SQL, never a post-hoc in-memory filter (which would break
candidate caps, scores, and the retrieval-trace guarantees).

Default rule: **project member ⇒ read**, **non-member ⇒ deny**, unless an explicit entry
says otherwise. This is implemented as: allowed set = project resources MINUS
explicitly-denied (for members), or PLUS explicitly-granted (for non-members). The
effective computation is folded into the filter so the common case (member, no overrides)
collapses to today's project-id-only predicate with negligible overhead.

### D3 — Why not Postgres RLS

RLS was evaluated and rejected because:

- The enforcement must span three heterogeneous search legs with different join shapes
  and also the graph hybrid search path; RLS would need a policy per table and still not
  cover the graph/hybrid traversal cleanly.
- Application-side enforcement keeps the single-authority posture (`scope-authority`)
  explicit and testable, and lets `PermissionSource` syncers write plain rows instead of
  managing RLS policies.
- RLS complicates the existing `search`/`graph` queries and index usage in ways that are
  hard to reason about; a `IN (allowed_ids)` predicate is predictable and indexable.

### D4 — Phasing

This change ships **Phase 1 only**: data model + migration/backfill + `authorizeResources`
+ search enforcement + `PermissionSource` contract + admin bypass. Phase 2 (connector
ACL syncers) is explicitly out of scope; the interface contract is the seam. Tasks list
Phase 2 as deferred.

## Risks / Trade-offs

- **Migration risk.** The backfill must grant read to every existing project member for
  every existing resource. If it misses resources, existing users silently lose access.
  Mitigation: backfill is generated from existing membership + resource tables with a
  store test asserting access parity before/after.
- **Performance.** Adding an authorization predicate to every leg risks regression.
  Mitigation: the common case collapses to project-id-only (D2); EXPLAIN/bench coverage
  in tasks; an index on `acl_entries (resource_type, resource_id)`.
- **Deny-override semantics complexity.** Default-read + explicit-deny requires careful
  set arithmetic. Mitigation: the effective-permission function is a pure, unit-tested
  helper with a decision table in tests.
- **Group recursion cost.** Recursive CTE on large groups is a risk; mitigated by depth
  cap and a cached, materialized flattened group→members view if needed (future).
- **Admin bypass scatter.** Enforced at one helper, not per call site; a unit test asserts
  every leg routes through it.
