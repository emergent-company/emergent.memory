## Context

Authorization currently resolves at the project/org boundary. Search applies only a
`project_id` filter (via `domain/search` for text/relationship and `domain/graph` for the
graph-object leg), so a project member sees every resource in the project. Onyx
introduces document-level visibility; Memory needs the same, generalized to graph objects
because it is graph-first. This is the highest-risk change in this batch: it touches every
read path and requires a data migration that must not change observed behaviour for
existing users.

## Goals / Non-Goals

**Goals**

- A per-resource ACL data model (with `deny`) and recursive group resolution.
- One `authorizeResources()` enforcement helper wired into the text, relationship, and
  graph-object legs.
- A default rule (project-member read) that preserves current behaviour, with a
  schema-only migration (no per-member × per-resource grant backfill).
- A `PermissionSource` interface contract for future syncers.

**Non-Goals (deferred, explicit)**

- 50 connector ACL syncers — the breadth trap this change is designed to avoid.
- A `source` resource type (no `kb.sources` table exists; matches collections v1). Adding
  this type is a follow-up migration that depends on `add-source-ingestion` creating
  `kb.sources` — it is not part of this change.
- Write/update permissions at the resource level — this change is read authorization
  only; writes remain governed by project role.

## Decisions

### D1 — Data model: ACL entries + groups

**No group primitive exists today.** A grep of `apps/server/migrations/` for
`groups`/`teams`/`group_members` is empty; the only principal concepts are
`kb.project_memberships`, `kb.organization_memberships`, and `core.superadmins`. So
`kb.groups` and `kb.group_members` are **new** tables in this change. (There are no
"role groups" — `principal_type` is simply `{user, group}`.)

Define the `principal_type` enum **once** (both tables share it), and the resource
vocabulary **`{document, object}`** (NOT `canonical`):

```
principal_type ∈ {user, group}         -- the single enum, defined once
resource_type  ∈ {document, object}    -- document id / graph object

kb.acl_entries (
  id, resource_type, resource_id uuid,
  principal_type, principal_id uuid,   -- principal_type = the shared enum
  permission text CHECK (permission IN ('read','deny')),
  created_at, updated_at,
  UNIQUE (resource_type, resource_id, principal_type, principal_id)
)
kb.groups ( id, project_id, name )
kb.group_members ( group_id, principal_type, principal_id )   -- shared enum; may nest groups
```

- The `UNIQUE (resource_type, resource_id, principal_type, principal_id)` **deliberately
  excludes** `permission`: a principal holds at most one effect row per resource (`read`
  OR `deny`), and flipping the effect is an in-place upsert. Including `permission` in the
  key would admit contradictory `read` **and** `deny` rows for the same principal+resource.
- For `object` entries, `resource_id` stores a graph object's `canonical_id`; at
  enforcement time it is mapped to `graph_objects.id → canonical_id` (head-resolved) so
  the ACL follows the live object.
- `deny` rows are first-class storage: an explicit deny overrides the default member-read
  (see D2).

Group resolution is recursive via a `WITH RECURSIVE` CTE over `group_members`
(cycle-safe by visited-set / depth cap). A user is authorized for a resource if any of
the user's (transitive) groups, or the user directly, has an entry (subject to deny).

### D2 — Enforcement point: `authorizeResources()`

A single function computes the set of resource ids (per type) a principal may read in a
project, then **every read path** — each search leg **and** the direct document / graph
object / relationship read routes — adds that set as a predicate (`IN (allowed)`, or an
existence check against `acl_entries` + group membership). Resolution happens once per
request and is translated into SQL, never a post-hoc in-memory filter (which would break
candidate caps, scores, and the retrieval-trace guarantees).

Default rule: **project member ⇒ read**, **non-member ⇒ deny**, unless an explicit entry
says otherwise. This is implemented as: for a member, `allowed = project_resources MINUS
explicitly-denied` (deny rows subtract); for a non-member, `allowed = explicitly-granted`.
The effective computation is folded into the filter so the common case (member, no
overrides) collapses to today's project-id-only predicate with negligible overhead.

**Relationship-leg rule:** a relationship is visible iff **both** of its endpoint objects
are readable — i.e. `src` OR `dst` unreadable hides the relationship. This matches the
existing endpoint-visibility semantics (a relationship to an object you cannot see must
not leak that object).

**Direct read paths.** The same `authorizeResources()` helper SHALL gate direct reads, not
only search, covering **every** graph query/read route in
`apps/server/domain/graph/routes.go` and the document/chunk read surfaces. Concretely:

- `domain/documents` — `List`, `GetByID`, `GetContent`, `Download`, `GetExtractionSummary`.
- `domain/chunks` — `List` (chunks are joined to their document's authorization, so a
  chunk of a denied document is not returned even when the request filters by `documentId`).
- `domain/graph` object reads — `ListObjects`, `CountObjects`, `FTSSearch`, `VectorSearch`,
  `GetTags`, `GetObject`, `GetSimilarObjects`, `GetObjectHistory`, `GetObjectEdges`.
- `domain/graph` hybrid/expansion — `HybridSearch`, `SearchWithNeighbors`, `ExpandGraph`,
  `TraverseGraph`.
- `domain/graph` branch reads — `MergeReadiness`, `CompareBranches` (results restricted to
  readable objects/branches).
- `domain/graph` analytics reads — `GetMostAccessed`, `GetUnused` (aggregate over readable
  objects only).
- `domain/graph` relationship reads — `ListRelationships`, `CountRelationships`,
  `GetRelationship`, `GetRelationshipHistory` (both-endpoints-readable rule).

A resource denied to a caller SHALL be unreadable/uncountable via these routes exactly as
it is unsearchable, so the ACL has no search-only gap. (Collections are a separate change;
ACL does not add a collection resource type in this change.)

### D3 — Relationship to the existing Postgres RLS layer

RLS is **already pervasive** (`00001_baseline.sql`): `kb.graph_objects` (876) and
`kb.graph_relationships` (920) are `FORCE ROW LEVEL SECURITY`, and `kb.chunks` (3797),
`kb.chat_*`, `kb.branches`, `kb.data_source_integrations` (3841), etc. all carry policies
keyed on `app.current_project_id` with membership subqueries. So the RLS-vs-app question
is not "RLS or app" — there is already an RLS project-scoping layer.

The new `authorizeResources()` predicate is **ADDITIONAL** to that RLS layer, not a
replacement: RLS continues to scope rows to the project, and `authorizeResources()`
adds the finer per-resource read/deny filter on top. Reasons for keeping the finer layer
in the application rather than extending RLS:

- The finer filter must span heterogeneous joins (text via `kb.documents`/`kb.chunks`,
  object via `kb.graph_objects`, relationship via `kb.graph_relationships`) and the graph
  hybrid traversal; a single shared `authorizeResources()` computes the allowed-id set
  once per request and is easier to reason about than one new policy per table.
- `PermissionSource` syncers write plain `acl_entries` rows; they do not manage RLS
  policies.
- The existing RLS layer stays project-scoped, but it is **not** entirely untouched: the
  entry gate for granted non-members (D5) requires a coordinated RLS change, because the
  current membership subqueries would hide rows from any non-member even after the app
  admits them. See D5.

**RLS on the new tables:** `kb.acl_entries`, `kb.groups`, and `kb.group_members` SHALL
get RLS policies scoped to `app.current_project_id` (via the resource's/group's
`project_id`), matching the existing layer, so ACL rows themselves are project-isolated.
(`kb.groups` carries `project_id`; `kb.acl_entries` resolves its project via the
`resource_id`/`resource_type`, so its RLS policy joins through the resource table.)

### D4 — Phasing

This change ships **Phase 1 only**: data model + schema-only migration + `authorizeResources`
+ read-path enforcement + `PermissionSource` contract + admin bypass. Phase 2 (connector
ACL syncers) is explicitly out of scope; the interface contract is the seam. Tasks list
Phase 2 as deferred.

### D5 — ACL-aware entry gate for granted non-members

The default rule has a non-member leg: **non-member ⇒ deny unless an explicit ACL entry
grants read**. But today every document/graph/chunks read route runs
`RequireProjectMember()` (see `domain/documents/routes.go`, `domain/graph/routes.go`,
`domain/chunks/routes.go`), which 403s any non-member **before** a service or
`authorizeResources()` runs. As written, an explicit grant to a non-member is unreachable.

This change therefore introduces an **ACL-aware entry gate** (`RequireACLRead`), applied on
the ACL-protected read routes in place of `RequireProjectMember()`:

- It keeps `RequireAuth` + `RequireProjectTokenScope` (unchanged identity/context).
- For a project **member**, it behaves exactly as `RequireProjectMember()` today
  (pass-through; default-read applies).
- For a **non-member**, it does not blanket-403. It resolves the caller's explicit ACL
  grants in the requested project and admits the request with a principal context only if
  at least one grant exists; otherwise it returns the same 403 as today (no observed
  behaviour change for grant-less non-members). The admitted non-member is then narrowed by
  `authorizeResources()` to exactly the granted ids.

**RLS reconciliation.** Project-scoping RLS policies on `kb.documents`, `kb.chunks`,
`kb.graph_objects`, and `kb.graph_relationships` key their membership subqueries on
`kb.organization_memberships`, which returns empty for a granted non-member and would hide
every row even after admission. The gate SHALL therefore also set the session project
context (`app.current_project_id`) to the granted project so project scoping is satisfied,
**and** the read-table RLS policies SHALL be extended (membership **OR** a `read` grant in
`kb.acl_entries` for this principal) so a granted non-member sees their granted rows and
nothing else. This is the one coordinated RLS change in the change; member behavior is
unchanged.

## Risks / Trade-offs

- **Migration risk.** The migration SHALL **not** materialize a read row per member ×
  resource: the member-default-read rule (D2) already preserves every existing member's
  access, so a per-resource grant backfill is O(members × resources) with zero added
  permission information and would fight the default rule. The migration is therefore
  **schema-only** (create `acl_entries`/`groups`/`group_members`) — an identity migration
  with no row backfill. Only real overrides (an explicit `deny`, or a `read` grant to a
  non-member) are ever written, and none exist at migration time. A parity test must prove
  observed behaviour is unchanged: an org member with **no** `kb.project_memberships` row
  (keyed on `kb.organization_memberships`, the real project-read gate via `lookupOrgMember`
  in `apps/server/pkg/auth/middleware.go`, esp. 663) still reads every resource they could
  before, and a non-member still cannot. If the default rule is mis-evaluated, existing
  users silently lose access.
- **Performance.** Adding an authorization predicate to every leg risks regression.
  Mitigation: the common case collapses to project-id-only (D2); an EXPLAIN/bench task is
  in the plan; an index on `acl_entries (resource_type, resource_id)`.
- **Deny-override semantics complexity.** Default-read + explicit-deny requires careful
  set arithmetic. Mitigation: the effective-permission function is a pure, unit-tested
  helper with a decision table in tests.
- **Group recursion cost.** Recursive CTE on large groups is a risk; mitigated by depth
  cap and a cached, materialized flattened group→members view if needed (future).
- **Admin bypass scatter.** Enforced at one helper, not per call site; a unit test asserts
  every leg routes through it.
