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

Authorization SHALL be enforced at every search and graph hybrid search entry point, so no read path can bypass the ACL. Enforcement SHALL be centralized in a single `authorizeResources()` helper used by all legs.

#### Scenario: All legs share one enforcement point

- **WHEN** text, relationship, and graph-object legs run
- **THEN** each SHALL apply the same `authorizeResources()` helper, so there is no divergent authorization logic

#### Scenario: Unauthorized object never returned

- **WHEN** a resource is denied to a caller
- **THEN** it SHALL NOT appear in any leg's results

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

### Requirement: Backfill preserves existing access

The migration that introduces ACL SHALL backfill entries such that every existing project member SHALL have `read` on their project's existing resources, preserving current behaviour with no observed change for existing users. The backfill SHALL be keyed on `kb.organization_memberships` projected onto each org's projects, and SHALL cover an org member who has **no** `kb.project_memberships` row.

#### Scenario: Existing members unaffected by migration

- **WHEN** the ACL migration runs against existing data
- **THEN** existing project members SHALL retain read access to their project's resources exactly as before the migration

#### Scenario: Org member without a project_memberships row keeps access

- **WHEN** a user is an org member with no `kb.project_memberships` row
- **THEN** the backfill SHALL still grant them `read` on the org's projects' resources (keyed on `kb.organization_memberships`)

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
