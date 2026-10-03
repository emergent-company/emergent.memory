## ADDED Requirements

### Requirement: ACL entries model per-resource permissions

An ACL entry SHALL identify a resource by `(resource_type, resource_id)` where `resource_type ∈ {document, object}`, a principal by `(principal_type, principal_id)` where `principal_type ∈ {user, group}`, and a `permission ∈ {read, deny}` enforced by a `CHECK (permission IN ('read','deny'))` constraint.

#### Scenario: Grant read on a resource

- **WHEN** an ACL entry grants `read` to a principal on a resource
- **THEN** that principal SHALL be authorized to read that resource

#### Scenario: Entry types are constrained

- **WHEN** an ACL entry is written
- **THEN** its `resource_type` and `principal_type` SHALL be from the supported sets, and `permission` SHALL be `read` or `deny` (any other value rejected by the CHECK constraint)

### Requirement: Deny-by-default outside project membership

A principal who is not a member of the resource's project SHALL have no access to the resource unless an explicit ACL entry grants it. Project membership SHALL be resolved through `kb.organization_memberships` (the project-read gate). Within a project, a member SHALL have `read` on the project's resources unless an explicit ACL entry revokes it.

#### Scenario: Non-member denied by default

- **WHEN** a principal is not a member of a project (no `kb.organization_memberships` row) and no ACL entry grants access
- **THEN** the principal SHALL be denied access to the project's resources

#### Scenario: Member read by default

- **WHEN** a project member has no explicit ACL entry on a resource
- **THEN** they SHALL be authorized to read it (default project-member read)

### Requirement: ACL grants, denies, and revokes

A principal SHALL be grantable (`read`), explicitly deniable (`deny`), and revocable for a resource. A `deny` entry SHALL override the default project-member read. Applying the same grant, deny, or revoke twice SHALL be idempotent (no duplicate entry, no effective-permission change). Each entry SHALL carry `created_at`/`updated_at` timestamps.

#### Scenario: Deny overrides default

- **WHEN** a `deny` entry exists for a principal on a resource
- **THEN** the principal SHALL be denied that resource even though they are a project member

#### Scenario: Grant and revoke are idempotent

- **WHEN** the same grant (or revoke) is applied twice
- **THEN** no duplicate entry is created and the effective permission is unchanged

### Requirement: Group inheritance resolves recursively

A principal SHALL be authorized for a resource if any group they belong to (transitively) is granted that resource. Group resolution SHALL be recursive and cycle-safe.

#### Scenario: Transitive group membership grants access

- **WHEN** a user belongs to a group that belongs to a parent group granted `read` on a resource
- **THEN** the user SHALL be authorized to read that resource

#### Scenario: Cycles do not hang resolution

- **WHEN** group membership contains a cycle
- **THEN** resolution SHALL terminate and return the correct effective permission

### Requirement: Search never returns unauthorized resources

Authorization SHALL be enforced at every search and graph hybrid search entry point, so no search path can bypass the ACL. Enforcement SHALL be centralized in a single `authorizeResources()` helper used by all legs.

#### Scenario: All legs share one enforcement point

- **WHEN** text, relationship, and graph-object legs run
- **THEN** each SHALL apply the same `authorizeResources()` helper, so there is no divergent authorization logic

#### Scenario: Unauthorized object never returned

- **WHEN** a resource is denied to a caller
- **THEN** it SHALL NOT appear in any leg's results

### Requirement: Direct read paths enforce the ACL

The per-resource `authorizeResources()` predicate SHALL gate **direct** reads — not only search. This SHALL cover document list/get/content/download/extraction-summary; chunk list (each chunk joined to its document's authorization, so a chunk of a denied document is never returned even when filtered by `documentId`); and **every** graph query/read route — object get/list/edges/similar/traverse/expand/count/fts/vector-search/tags/history, hybrid search, search-with-neighbors, branch merge-readiness/compare, analytics most-accessed/unused, and relationship get/list/count/history. A relationship SHALL be hidden on direct read when either endpoint is unreadable, matching the search-leg rule.

#### Scenario: Denied document is not directly readable

- **WHEN** a document is denied to a caller
- **THEN** the caller SHALL NOT read, list, download, or fetch its content or extraction-summary through the direct document routes

#### Scenario: Denied document's chunks are not directly readable

- **WHEN** a document is denied to a caller
- **THEN** its chunks SHALL NOT be returned by `domain/chunks` list — whether the list is unfiltered or filtered by that `documentId`

#### Scenario: Denied object is not directly readable or counted

- **WHEN** a graph object is denied to a caller
- **THEN** the caller SHALL NOT fetch, list, traverse, expand, or see it in object/edge counts through the direct graph routes

#### Scenario: Denied object is excluded from every graph query surface

- **WHEN** a graph object is denied to a caller
- **THEN** it SHALL NOT appear in FTS, vector-search, tags, similar, history, search-with-neighbors, analytics (`most-accessed`/`unused`), or branch `compare`/`merge-readiness` results

#### Scenario: Relationship to an unreadable endpoint is hidden on direct read

- **WHEN** one endpoint object of a relationship is denied to a caller
- **THEN** the relationship SHALL NOT be returned by the direct relationship get/list/count/history routes

### Requirement: ACL-aware entry gate admits granted non-members

A non-member with an explicit `read` grant SHALL be admitted on the ACL-protected read routes rather than being rejected by the project-membership middleware before the authorization helper runs. The read routes SHALL use an ACL-aware entry gate that admits members unchanged, admits non-members only when they hold an explicit grant, and returns the same denial as today to a grant-less non-member. The project-scoping RLS SHALL be reconciled so a granted non-member can read their granted rows and nothing else.

#### Scenario: Granted non-member can read granted resources

- **WHEN** a non-member has an explicit `read` grant on a resource and calls a read route
- **THEN** the caller SHALL be admitted and SHALL read that resource (and only that resource), not being rejected by the project-membership middleware

#### Scenario: Grant-less non-member still denied

- **WHEN** a non-member has no explicit grant and calls a read route
- **THEN** the caller SHALL receive the same denial as before this change (no behaviour change for grant-less non-members)

#### Scenario: Granted non-member is project-isolated by RLS

- **WHEN** a granted non-member reads
- **THEN** project-scoping RLS SHALL admit only their granted rows and SHALL NOT leak un-granted rows from the same project

### Requirement: Relationship visibility follows endpoint readability

A relationship SHALL be visible to a caller only when **both** of its endpoint objects are readable to that caller; a relationship whose `src` OR `dst` object is unreadable SHALL be hidden.

#### Scenario: Relationship to an unreadable object is hidden

- **WHEN** one endpoint object of a relationship is denied to a caller
- **THEN** the relationship SHALL NOT be returned to that caller

### Requirement: Admin bypass policy

An explicit admin (superadmin / org admin) SHALL have a documented, controlled bypass that is applied at a single point, not scattered across call sites.

#### Scenario: Admin bypasses resource ACL

- **WHEN** an org/super admin reads resources
- **THEN** they SHALL bypass the per-resource ACL (subject to project/org membership where applicable), applied through one documented path

### Requirement: Migration preserves existing access (schema-only)

The migration that introduces ACL SHALL create the `kb.acl_entries`, `kb.groups`, and `kb.group_members` tables and SHALL **not** materialize a read row per member × resource. Existing project members' access SHALL be preserved by the member-default-read rule (default project-member read), keyed on `kb.organization_memberships` projected onto each org's projects, so the migration is an identity migration with no observed change for existing users and no O(members × resources) backfill. Only real overrides (`deny`, or `read` to a non-member) are ever written, and none exist at migration time.

#### Scenario: Existing members unaffected by migration

- **WHEN** the ACL migration runs against existing data
- **THEN** existing project members SHALL retain read access to their project's resources exactly as before the migration, via the default rule rather than materialized grant rows

#### Scenario: Org member without a project_memberships row keeps access

- **WHEN** a user is an org member with no `kb.project_memberships` row
- **THEN** the default rule SHALL still grant them `read` on the org's projects' resources (keyed on `kb.organization_memberships`), with no materialized ACL row required

#### Scenario: No per-member × per-resource backfill rows

- **WHEN** the ACL migration runs
- **THEN** it SHALL be schema-only: zero `acl_entries` rows are inserted by the migration, so the migration cost is bounded regardless of member or resource count

### Requirement: PermissionSource contract for future syncers

The system SHALL define a `PermissionSource` interface contract that a future connector ACL syncer SHALL implement, so external permission changes can be synced into ACL entries. This change SHALL define the contract and SHALL NOT implement any connector syncer.

#### Scenario: Contract defined, no implementations shipped

- **WHEN** this change lands
- **THEN** a `PermissionSource` interface contract SHALL exist and be documented, and no connector ACL syncer SHALL be implemented

### Requirement: ACL is additional to the existing RLS layer

Postgres RLS already scopes rows to the project (`kb.graph_objects`, `kb.graph_relationships`, `kb.chunks`, etc.). The per-resource `authorizeResources()` predicate SHALL be **additional** to that layer, not a replacement, and the new ACL tables (`kb.acl_entries`, `kb.groups`, `kb.group_members`) SHALL themselves carry project-scoped RLS policies consistent with the existing layer.

#### Scenario: Finer filter is additive

- **WHEN** a search runs
- **THEN** the existing project-scoping RLS SHALL still apply, and `authorizeResources()` SHALL add the per-resource read/deny filter on top

#### Scenario: ACL tables are project-isolated

- **WHEN** ACL rows are read or written
- **THEN** they SHALL be constrained by project-scoped RLS policies on `kb.acl_entries`, `kb.groups`, and `kb.group_members`
