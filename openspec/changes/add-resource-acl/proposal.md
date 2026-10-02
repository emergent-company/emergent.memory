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
core + default/backfill. Connector ACL syncers are explicitly deferred — 50 hand-written
syncers up front would be a breadth trap; the interface contract is what unblocks them.

## What Changes

- Add `kb.acl_entries (resource_type, resource_id, principal_type, principal_id,
  permission)` with `permission ∈ {read, deny}` plus recursive group resolution
  (principals are users or groups).
- A single `authorizeResources()` helper, enforced as a filter in `domain/search` (text +
  relationship legs) and in `domain/graph` hybrid search (graph-object leg) — search never
  returns an unauthorized resource.
- A `PermissionSource` interface contract for future connector ACL syncers (no connector
  implementations in this change).
- Default/backfill rule: existing project members get `read` on their project's
  resources, backfilled from **`kb.organization_memberships`** (the real project-read gate,
  via `lookupOrgMember`), and the migration preserves current behaviour.
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
  structure (`kb.groups`/`kb.group_members`), plus a backfill migration that grants
  project members `read` on existing resources, keyed on `kb.organization_memberships`
  projected onto each org's projects.
- **Server** (`apps/server/domain/`): new `acl` package (`authorizeResources` +
  `PermissionSource` interface + group resolution); `domain/search/repository.go` +
  `service.go` (text + relationship legs); `domain/graph` hybrid search (graph-object leg).
- **Authz posture**: consistent with `scope-authority` and `project-viewer-role`
  (viewer = read-only is a member-level rule; ACL is a finer resource-level rule).

## Dependency

Relates to `knowledge-collections` (collections become ACL targets) and to future
`source-ingestion` (per-source permissions). ACL itself is unblocked and ships core +
backfill first.
