## Why

Authorization is org/project only. `kb.project_memberships` / `kb.organization_memberships`
answer "can this user see this project", and search filters solely on `project_id`
(`domain/search` for text/relationship; `domain/graph` for the graph-object leg). There is
**no per-document or per-object ACL**: within a project, every member sees every resource.
Onyx's document-level ACL (`DocumentSet` + per-document visibility) is the shape Memory is
missing, and it matters the moment a single project holds resources from multiple
connectors with different sharing rules (e.g. a GitHub repo synced to a project where only
some members should see it).

This change designs the ACL data model and enforcement point now, and ships only the
core + default rule. Connector ACL syncers are explicitly deferred — 50 hand-written
syncers up front would be a breadth trap; the interface contract is what unblocks them.

## What Changes

- Add `kb.acl_entries (resource_type, resource_id, principal_type, principal_id,
  permission)` with `permission ∈ {read, deny}` plus recursive group resolution
  (principals are users or groups).
- A single `authorizeResources()` helper, enforced as a filter in `domain/search` (text +
  relationship legs), in `domain/graph` hybrid search (graph-object leg), **and on every
  direct read path** — `domain/documents` list/get/content/download/extraction-summary,
  `domain/chunks` list (chunk→document join), and all `domain/graph` query/read routes
  (object get/list/edges/similar/traverse/expand/count/fts/vector-search/tags/history,
  search-with-neighbors, branch merge-readiness/compare, analytics most-accessed/unused,
  relationship get/list/count/history) — no read path returns an unauthorized resource.
- An **ACL-aware entry gate** (`RequireACLRead`) on the read routes (in place of
  `RequireProjectMember()`) so an explicitly granted non-member is admitted to the
  authorization helper rather than rejected by the membership middleware; with a
  coordinated project-scoping RLS reconciliation so granted non-members read their granted
  rows and nothing else.
- A `PermissionSource` interface contract for future connector ACL syncers (no connector
  implementations in this change).
- Default rule: existing project members get `read` on their project's resources via the
  member-default-read rule keyed on **`kb.organization_memberships`** (the real
  project-read gate, via `lookupOrgMember`). The migration is **schema-only** — no
  per-member × per-resource grant rows are backfilled — and preserves current behaviour.
- Explicitly **out of scope (deferred)**: 50 connector ACL syncers; a `source` resource
  type (no `kb.sources` table exists — matches collections' v1).

## Capabilities

### New Capabilities

- `resource-acl`: per-resource (document / graph object) read authorization via ACL
  entries (including `deny`) with recursive group resolution, enforced as a search
  filter, with a default project-member-read rule.

### Modified Capabilities

- `search`: the text and relationship legs are additionally filtered by
  `authorizeResources()`, so unauthorized resources are never returned.
- `graph` (related, consumed): the graph-object leg is filtered in `domain/graph` hybrid
  search (described in this change; no separate `graph` delta is written).

### Related / Consumed Capabilities

- `scope-authority`: the ACL is an application-side grant tier consistent with the
  single-authority posture; token-carried scopes are never an ACL grant. No delta.

## Impact

- **DB** (`apps/server/migrations/`): `kb.acl_entries` + a recursive group-resolution
  structure (`kb.groups`/`kb.group_members`), plus a **schema-only** migration that
  introduces no backfilled grant rows (member-default-read preserves existing access).
- **Server** (`apps/server/domain/`): new `acl` package (`authorizeResources` +
  `PermissionSource` interface + group resolution); `domain/search/repository.go` +
  `service.go` (text + relationship legs); `domain/graph` hybrid search (graph-object leg)
  and every direct object/relationship/branch/analytics read; `domain/documents` and
  `domain/chunks` direct reads; an ACL-aware entry gate (`RequireACLRead`) plus a
  coordinated read-table RLS policy extension for granted non-members.
- **Authz posture**: consistent with `scope-authority` and `project-viewer-role`
  (viewer = read-only is a member-level rule; ACL is a finer resource-level rule).

## Dependency

Relates to `knowledge-collections` (collections become ACL targets) and to future
`source-ingestion` (per-source permissions). ACL itself is unblocked and ships core +
default rule first.
